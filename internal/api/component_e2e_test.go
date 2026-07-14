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

// TestComponentRenameAndCheckName drives the rename input and the collection-level
// :checkName advisory over HTTP: checkName reports valid + available (scope-blind),
// a PATCH renames by the new technical name, a rename onto a taken name is a 409,
// and a bad slug is rejected at the edge by the Huma pattern (422). Skipped under
// -short.
func TestComponentRenameAndCheckName(t *testing.T) {
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

	// Seed a component.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "cmp-one", "component_type": "display"}, http.StatusCreated)

	type nameCheck struct {
		Valid     bool   `json:"valid"`
		Available bool   `json:"available"`
		Reason    string `json:"reason"`
	}
	check := func(name string) nameCheck {
		out := c.do(ownerTok, http.MethodPost, "/components:checkName", map[string]any{"name": name}, http.StatusOK)
		var nc nameCheck
		if err := json.Unmarshal(out, &nc); err != nil {
			t.Fatalf("decode checkName: %v", err)
		}
		return nc
	}

	// checkName: taken.
	if nc := check("cmp-one"); !nc.Valid || nc.Available {
		t.Fatalf("checkName(cmp-one) = %+v, want valid=true available=false", nc)
	}
	// checkName: available.
	if nc := check("cmp-free"); !nc.Valid || !nc.Available {
		t.Fatalf("checkName(cmp-free) = %+v, want valid=true available=true", nc)
	}
	// checkName: bad format -> valid:false, still 200.
	if nc := check("Bad Name"); nc.Valid {
		t.Fatalf("checkName(Bad Name) = %+v, want valid=false", nc)
	}

	// Rename via PATCH.
	out := c.do(ownerTok, http.MethodPatch, "/components/cmp-one", map[string]any{"name": "cmp-renamed"}, http.StatusOK)
	var renamed struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &renamed); err != nil {
		t.Fatalf("decode rename: %v", err)
	}
	if renamed.Name != "cmp-renamed" {
		t.Fatalf("name = %q, want cmp-renamed", renamed.Name)
	}

	// Dup rename -> 409.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "cmp-two", "component_type": "display"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPatch, "/components/cmp-two", map[string]any{"name": "cmp-renamed"}, http.StatusConflict)

	// Bad format via PATCH -> 422 (Huma pattern rejects at the edge).
	c.do(ownerTok, http.MethodPatch, "/components/cmp-two", map[string]any{"name": "Bad Name"}, http.StatusUnprocessableEntity)

	// Create-tightening: a bad name is rejected at create too, not just rename.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "Bad Name", "component_type": "display"}, http.StatusUnprocessableEntity)
}

// TestComponentCheckNameScopeBlind is scope-blind: a caller with component:update
// scoped to one subtree still sees a name taken in a subtree it cannot read, so
// its rename never false-positives "available" only to 409 at Save on the global
// unique constraint. Skipped under -short.
func TestComponentCheckNameScopeBlind(t *testing.T) {
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

	// Two components in separate scopes.
	var disp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "scope-disp", "component_type": "display"}, http.StatusCreated), &disp); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "scope-cam", "component_type": "camera"}, http.StatusCreated)

	// A deploy principal (component:update) scoped ONLY to scope-disp.
	deployTok := setupScopedViewer(t, ctx, dsn, "deploy-disp", "deploy", "component", disp.ID)
	// It cannot read scope-cam (out of scope -> non-disclosing 404).
	c.do(deployTok, http.MethodGet, "/components/scope-cam", nil, http.StatusNotFound)

	// But checkName reports scope-cam taken (scope-blind), never available.
	out := c.do(deployTok, http.MethodPost, "/components:checkName", map[string]any{"name": "scope-cam"}, http.StatusOK)
	var nc struct {
		Valid     bool `json:"valid"`
		Available bool `json:"available"`
	}
	if err := json.Unmarshal(out, &nc); err != nil {
		t.Fatalf("decode checkName: %v", err)
	}
	if !nc.Valid || nc.Available {
		t.Fatalf("scope-blind checkName(scope-cam) = %+v, want valid=true available=false (name exists out-of-scope)", nc)
	}
}
