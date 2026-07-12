package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

type resolvedTagResp struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	OwnerKind string `json:"owner_kind"`
	OwnerName string `json:"owner_name"`
	Band      int    `json:"band"`
	Winner    bool   `json:"winner"`
}

// TestTagAPI drives the tag surface over HTTP: an owner mints keys and binds
// values at several scopes, reads the effective-tags cascade for a component
// (keys union, values override), and the permission split holds: a
// component-scoped operator may bind on its component but not mint a key nor bind
// on a system it cannot write, and a viewer may read but not mint or bind.
func TestTagAPI(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("mint owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// Estate: a room in a building, a system, and a codec at both.
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "bldg", "location_type": "building"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "room", "location_type": "room", "parent": "bldg"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "sys", "system_type": "meeting-room"}, http.StatusCreated)
	compRaw := c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "codec-1", "component_type": "codec", "system": "sys", "location": "room"}, http.StatusCreated)
	var comp struct {
		ID string `json:"id"`
	}
	json.Unmarshal(compRaw, &comp)

	// Mint keys. A non-normalized key is a 422.
	c.do(ownerTok, http.MethodPost, "/tags", map[string]any{"name": "Environment"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/tags", map[string]any{"name": "environment"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/tags", map[string]any{"name": "asset_id", "propagates": false}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/tags", map[string]any{"name": "rack_key", "applies_to": []string{"location"}}, http.StatusCreated)
	// Duplicate key is a conflict.
	c.do(ownerTok, http.MethodPost, "/tags", map[string]any{"name": "environment"}, http.StatusConflict)

	// Bind environment down the cascade: global -> room -> component.
	c.do(ownerTok, http.MethodPut, "/tags/environment/binding", map[string]any{"value": "prod"}, http.StatusOK)
	c.do(ownerTok, http.MethodPut, "/locations/room/tags/environment", map[string]any{"value": "staging"}, http.StatusOK)
	c.do(ownerTok, http.MethodPut, "/components/codec-1/tags/environment", map[string]any{"value": "dev"}, http.StatusOK)
	// asset_id is non-propagating: bind it above the component (room) and on it.
	c.do(ownerTok, http.MethodPut, "/locations/room/tags/asset_id", map[string]any{"value": "R-1"}, http.StatusOK)
	c.do(ownerTok, http.MethodPut, "/components/codec-1/tags/asset_id", map[string]any{"value": "A-42"}, http.StatusOK)
	// A key that does not apply to the entity kind is a 422 (rack_key on a component).
	c.do(ownerTok, http.MethodPut, "/components/codec-1/tags/rack_key", map[string]any{"value": "x"}, http.StatusUnprocessableEntity)
	// Binding an unknown key is a 404.
	c.do(ownerTok, http.MethodPut, "/components/codec-1/tags/ghost", map[string]any{"value": "x"}, http.StatusNotFound)

	// The effective-tags cascade for the codec.
	resolved := effectiveTags(t, c, ownerTok, "codec-1")
	winners := map[string]resolvedTagResp{}
	for _, r := range resolved {
		if r.Winner {
			winners[r.Key] = r
		}
	}
	if w := winners["environment"]; w.Value != "dev" || w.OwnerKind != "component" {
		t.Errorf("environment winner = %+v, want dev on component", w)
	}
	if w := winners["asset_id"]; w.Value != "A-42" || w.OwnerKind != "component" {
		t.Errorf("asset_id winner = %+v, want A-42 on component", w)
	}
	for _, r := range resolved {
		if r.Key == "asset_id" && r.OwnerKind == "location" {
			t.Errorf("non-propagating location binding leaked into cascade: %+v", r)
		}
	}

	// Direct tags on the component: environment=dev and asset_id=A-42.
	var direct struct {
		Tags []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"tags"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/codec-1/tags", nil, http.StatusOK), &direct)
	if len(direct.Tags) != 2 {
		t.Fatalf("direct tags = %d, want 2", len(direct.Tags))
	}

	// Removing the component environment binding lets the room value resolve.
	c.do(ownerTok, http.MethodDelete, "/components/codec-1/tags/environment", nil, http.StatusNoContent)
	resolved = effectiveTags(t, c, ownerTok, "codec-1")
	for _, r := range resolved {
		if r.Key == "environment" && r.Winner && (r.OwnerKind != "location" || r.Value != "staging") {
			t.Errorf("environment winner after drop = %+v, want staging on location", r)
		}
	}

	// A component-scoped operator: may bind on its component (component:update),
	// but may not mint a key (tag:create, admin) nor bind on the system it cannot
	// write (system:update).
	opTok := setupScopedViewer(t, ctx, dsn, "operator-codec", "operator", "component", comp.ID)
	c.do(opTok, http.MethodPut, "/components/codec-1/tags/environment", map[string]any{"value": "op-set"}, http.StatusOK)
	c.do(opTok, http.MethodPost, "/tags", map[string]any{"name": "coined"}, http.StatusForbidden)
	c.do(opTok, http.MethodPut, "/systems/sys/tags/environment", map[string]any{"value": "x"}, http.StatusForbidden)
	c.do(opTok, http.MethodPut, "/tags/environment/binding", map[string]any{"value": "x"}, http.StatusForbidden)

	// A component-scoped viewer: may read keys and the cascade, forbidden to mint
	// or bind.
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-codec", "viewer", "component", comp.ID)
	c.do(viewerTok, http.MethodGet, "/tags", nil, http.StatusOK)
	if got := effectiveTags(t, c, viewerTok, "codec-1"); len(got) == 0 {
		t.Errorf("viewer cascade empty, want the resolved tags")
	}
	c.do(viewerTok, http.MethodPost, "/tags", map[string]any{"name": "nope"}, http.StatusForbidden)
	c.do(viewerTok, http.MethodPut, "/components/codec-1/tags/environment", map[string]any{"value": "x"}, http.StatusForbidden)
}

func effectiveTags(t *testing.T, c *apiClient, tok, comp string) []resolvedTagResp {
	t.Helper()
	raw := c.do(tok, http.MethodGet, "/components/"+comp+"/effective-tags", nil, http.StatusOK)
	var out struct {
		Tags []resolvedTagResp `json:"tags"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode effective-tags: %v", err)
	}
	return out.Tags
}
