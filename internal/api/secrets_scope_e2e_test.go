package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestListSecretsScopedDirectoryAPI drives #458's acceptance through the live
// HTTP stack: the secret directory is per-row scope-filtered by the injected
// query, so a location-scoped operator lists only the secrets under its
// subtree; a location-owned secret in a sibling wing, a component-owned secret
// in that sibling, and the platform row are all absent, while the all-scope
// owner sees every one of them. Skipped under -short.
func wingB() *string { s := "wing-b"; return &s }

func TestListSecretsScopedDirectoryAPI(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	prov := secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(prov))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	ownerTok := principalWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})

	// hq (campus) > wing-a, wing-b (buildings); a component in the sibling wing.
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "hq", "location_type": "campus"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "wing-a", "location_type": "building", "parent": "hq"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", map[string]any{"name": "wing-b", "location_type": "building", "parent": "hq"}, http.StatusCreated)
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "codec-b", LocationName: wingB(),
	}, scope.Set{All: true}, scope.Set{All: true}, scope.Set{All: true}, scope.Set{All: true}); err != nil {
		t.Fatalf("component: %v", err)
	}

	c.do(ownerTok, http.MethodPost, "/secrets", secretReq("mine", "location", "wing-a", "a"), http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/secrets", secretReq("sibling", "location", "wing-b", "b"), http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/secrets", secretReq("on-codec", "component", "codec-b", "c"), http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/secrets", secretReq("install-wide", "platform", "", "d"), http.StatusCreated)

	wingA := entityID(t, c, ownerTok, "/locations", "wing-a")
	opTok := principalWithGrants(t, ctx, dsn, "wing-a-operator",
		[]grant{{role: "operator", scopeKind: "location", scopeID: wingA}})

	list := func(tok string) map[string]bool {
		t.Helper()
		body := c.do(tok, http.MethodGet, "/secrets", nil, http.StatusOK)
		var out struct {
			Secrets []struct {
				Name string `json:"name"`
			} `json:"secrets"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("parse list: %v", err)
		}
		names := map[string]bool{}
		for _, s := range out.Secrets {
			names[s.Name] = true
		}
		return names
	}

	got := list(opTok)
	if !got["mine"] || got["sibling"] || got["on-codec"] || got["install-wide"] {
		t.Errorf("wing-a operator directory = %v, want mine only", got)
	}
	got = list(ownerTok)
	for _, want := range []string{"mine", "sibling", "on-codec", "install-wide"} {
		if !got[want] {
			t.Errorf("owner directory missing %s (got %v)", want, got)
		}
	}

	// ADR-0052 (#459): the system band is retired; the enum refuses it at
	// validation, so a system-owned secret cannot be created at all.
	c.do(ownerTok, http.MethodPost, "/secrets", secretReq("dead", "system", "any", "x"), http.StatusUnprocessableEntity)
}
