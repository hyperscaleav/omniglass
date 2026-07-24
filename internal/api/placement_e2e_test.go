package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestComponentPlacementAPI drives the newly patchable placement and classification
// fields over HTTP: a PATCH sets parent, location, and product together; an explicit
// empty string clears the parent to root; a reparent onto a descendant is a 422; an
// unknown product is a 422. Skipped under -short.
func TestComponentPlacementAPI(t *testing.T) {
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

	// Fixtures the API references: a location and a product.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "loc-x", LocationType: "campus"}, scope.Set{All: true}); err != nil {
		t.Fatalf("seed location: %v", err)
	}
	if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: "prod-x", DisplayName: "Prod X", Kind: "device"}); err != nil {
		t.Fatalf("seed product: %v", err)
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

	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "rack"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "dev"}, http.StatusCreated)

	type placement struct {
		Parent   string `json:"parent"`
		Location string `json:"location"`
		Product  string `json:"product"`
	}
	patch := func(p map[string]any, want int) placement {
		out := c.do(ownerTok, http.MethodPatch, "/components/dev", p, want)
		var b placement
		if want == http.StatusOK {
			if err := json.Unmarshal(out, &b); err != nil {
				t.Fatalf("decode component: %v", err)
			}
		}
		return b
	}

	// Set all three at once.
	if got := patch(map[string]any{"parent": "rack", "location": "loc-x", "product": "prod-x"}, http.StatusOK); got.Parent != "rack" || got.Location != "loc-x" || got.Product != "prod-x" {
		t.Fatalf("after set = %+v, want rack / loc-x / prod-x", got)
	}

	// Clear the parent to root with an explicit empty string.
	if got := patch(map[string]any{"parent": ""}, http.StatusOK); got.Parent != "" {
		t.Fatalf("after clear parent = %q, want empty", got.Parent)
	}

	// Reparent onto a descendant is refused (422): sub under dev, then dev under sub.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "sub", "parent": "dev"}, http.StatusCreated)
	patch(map[string]any{"parent": "sub"}, http.StatusUnprocessableEntity)

	// An unknown product is a 422 (by name).
	patch(map[string]any{"product": "ghost"}, http.StatusUnprocessableEntity)
}

// TestSystemPlacementAPI drives the newly patchable system placement fields: a PATCH
// sets parent and location, an empty string clears the parent to root, and a reparent
// onto a descendant is a 422. Skipped under -short.
func TestSystemPlacementAPI(t *testing.T) {
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
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "loc-x", LocationType: "campus"}, scope.Set{All: true}); err != nil {
		t.Fatalf("seed location: %v", err)
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

	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "sys-root"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "sys-x"}, http.StatusCreated)

	type placement struct {
		Parent   string `json:"parent"`
		Location string `json:"location"`
	}
	patch := func(p map[string]any, want int) placement {
		out := c.do(ownerTok, http.MethodPatch, "/systems/sys-x", p, want)
		var b placement
		if want == http.StatusOK {
			if err := json.Unmarshal(out, &b); err != nil {
				t.Fatalf("decode system: %v", err)
			}
		}
		return b
	}

	if got := patch(map[string]any{"parent": "sys-root", "location": "loc-x"}, http.StatusOK); got.Parent != "sys-root" || got.Location != "loc-x" {
		t.Fatalf("after set = %+v, want sys-root / loc-x", got)
	}

	if got := patch(map[string]any{"parent": ""}, http.StatusOK); got.Parent != "" {
		t.Fatalf("after clear parent = %q, want empty", got.Parent)
	}

	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "sys-sub", "parent": "sys-x"}, http.StatusCreated)
	patch(map[string]any{"parent": "sys-sub"}, http.StatusUnprocessableEntity)
}
