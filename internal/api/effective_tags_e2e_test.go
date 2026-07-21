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

// TestEffectiveTagsOnListBodies drives the directory list routes and asserts each
// row carries its resolved effective tags (key -> winning value): a component
// resolves the full arc, and a placed system inherits its location's tags.
func TestEffectiveTagsOnListBodies(t *testing.T) {
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

	// Estate: a system placed in a room, a codec under both.
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "campus", "location_type": "campus"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "room", "location_type": "room", "parent": "campus"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "av", "location": "room"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "codec", "system": "av", "location": "room"}, http.StatusCreated)

	// Tags: environment cascades, compliance set only at the campus location.
	c.do(ownerTok, http.MethodPost, "/tags", map[string]any{"name": "environment"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/tags", map[string]any{"name": "compliance"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/tags/environment:setGlobal", map[string]any{"value": "prod"}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/locations/campus:setTag", map[string]any{"key": "compliance", "value": "pci"}, http.StatusOK)
	c.do(ownerTok, http.MethodPost, "/systems/av:setTag", map[string]any{"key": "environment", "value": "dev"}, http.StatusOK)

	// Component list: the codec resolves environment=dev (system band) and
	// compliance=pci (inherited from the campus location).
	comp := listOne(t, c, ownerTok, "/components", "components", "codec")
	if comp.EffectiveTags["environment"] != "dev" {
		t.Errorf("codec environment = %q, want dev", comp.EffectiveTags["environment"])
	}
	if comp.EffectiveTags["compliance"] != "pci" {
		t.Errorf("codec compliance = %q, want pci (inherited from location)", comp.EffectiveTags["compliance"])
	}

	// System list: the placed system inherits the campus compliance tag and its
	// own environment binding.
	sys := listOne(t, c, ownerTok, "/systems", "systems", "av")
	if sys.EffectiveTags["compliance"] != "pci" {
		t.Errorf("av compliance = %q, want pci (a placed system inherits its location)", sys.EffectiveTags["compliance"])
	}
	if sys.EffectiveTags["environment"] != "dev" {
		t.Errorf("av environment = %q, want dev", sys.EffectiveTags["environment"])
	}

	// Location list: campus resolves its own compliance tag and the platform env.
	loc := listOne(t, c, ownerTok, "/locations", "locations", "campus")
	if loc.EffectiveTags["compliance"] != "pci" || loc.EffectiveTags["environment"] != "prod" {
		t.Errorf("campus tags = %v, want compliance=pci environment=prod", loc.EffectiveTags)
	}
}

type rowWithTags struct {
	Name          string            `json:"name"`
	EffectiveTags map[string]string `json:"effective_tags"`
}

// listOne fetches a directory collection and returns the named row.
func listOne(t *testing.T, c *apiClient, tok, path, coll, name string) rowWithTags {
	t.Helper()
	raw := c.do(tok, http.MethodGet, path, nil, http.StatusOK)
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	var rows []rowWithTags
	if err := json.Unmarshal(top[coll], &rows); err != nil {
		t.Fatalf("decode %s.%s: %v", path, coll, err)
	}
	for _, r := range rows {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("%s not found in %s", name, path)
	return rowWithTags{}
}
