package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestComponentRenameAndCheckName drives the :rename custom method and the
// collection-level :checkName advisory over HTTP: checkName reports valid +
// available (scope-blind), :rename moves the name, a rename onto a taken name is a
// 409, and a bad slug is rejected at the edge by the Huma pattern (422). Skipped
// under -short.
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
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "cmp-one", "product": "generic-device"}, http.StatusCreated)

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

	// Rename via the custom method. It is not a PATCH: a rename breaks stored
	// external references, so it is an explicit act with its own permission.
	out := c.do(ownerTok, http.MethodPost, "/components/cmp-one:rename", map[string]any{"name": "cmp-renamed"}, http.StatusOK)
	var renamed struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &renamed); err != nil {
		t.Fatalf("decode rename: %v", err)
	}
	if renamed.Name != "cmp-renamed" {
		t.Fatalf("name = %q, want cmp-renamed", renamed.Name)
	}

	// Afterwards the component answers to the new name and to its uuid, and the old
	// name is gone.
	c.do(ownerTok, http.MethodGet, "/components/cmp-renamed", nil, http.StatusOK)
	c.do(ownerTok, http.MethodGet, "/components/"+renamed.ID, nil, http.StatusOK)
	c.do(ownerTok, http.MethodGet, "/components/cmp-one", nil, http.StatusNotFound)

	// The name is no longer patchable: two ways to rename is what this method removed.
	c.do(ownerTok, http.MethodPatch, "/components/cmp-renamed", map[string]any{"name": "cmp-sneaky"}, http.StatusUnprocessableEntity)

	// Dup rename -> 409.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "cmp-two", "product": "generic-device"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/components/cmp-two:rename", map[string]any{"name": "cmp-renamed"}, http.StatusConflict)

	// Bad format -> 422 (Huma pattern rejects at the edge).
	c.do(ownerTok, http.MethodPost, "/components/cmp-two:rename", map[string]any{"name": "Bad Name"}, http.StatusUnprocessableEntity)

	// A uuid-shaped name passes the slug pattern and is refused by the gateway, so
	// the name and the id can never be the same shape.
	c.do(ownerTok, http.MethodPost, "/components/cmp-two:rename",
		map[string]any{"name": "019f8754-461f-7b82-b5f2-fc4bbe1c3765"}, http.StatusUnprocessableEntity)

	// Create-tightening: a bad name is rejected at create too, not just rename.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "Bad Name", "product": "generic-device"}, http.StatusUnprocessableEntity)
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
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "scope-disp", "product": "generic-device"}, http.StatusCreated), &disp); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "scope-cam", "product": "generic-device"}, http.StatusCreated)

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

// TestComponentRequiresProduct drives the #614 component floor over HTTP: a
// create with no product is a 422 whose message names generic-device (the
// escape hatch, an operator can act on the message immediately), a create
// with a product succeeds, patch still reclassifies, and patch no longer
// treats an explicit empty string as a clear (also a 422). Skipped under
// -short.
func TestComponentRequiresProduct(t *testing.T) {
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

	// No product at all: 422, naming generic-device.
	status, body := c.send(ownerTok, http.MethodPost, "/components", map[string]any{"name": "no-product"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("create with no product status = %d, want 422\nbody: %s", status, body)
	}
	if !strings.Contains(string(body), "generic-device") {
		t.Fatalf("create with no product body = %s, want it to name generic-device", body)
	}

	// An explicit empty-string product is the same 422, not a silent no-op.
	status, body = c.send(ownerTok, http.MethodPost, "/components", map[string]any{"name": "empty-product", "product": ""})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("create with empty product status = %d, want 422\nbody: %s", status, body)
	}

	// A named product succeeds.
	c.do(ownerTok, http.MethodPost, "/components", map[string]any{"name": "has-product", "product": "generic-device"}, http.StatusCreated)

	// PATCH still reclassifies to a real product.
	c.do(ownerTok, http.MethodPatch, "/components/has-product", map[string]any{"product": "cisco-room-bar"}, http.StatusOK)
	reread := struct {
		Product string `json:"product"`
	}{}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/has-product", nil, http.StatusOK), &reread); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if reread.Product != "cisco-room-bar" {
		t.Fatalf("product after reclassify = %q, want cisco-room-bar", reread.Product)
	}

	// PATCH no longer clears with an explicit empty string: 422, and the
	// product is left exactly as the reclassify left it.
	c.do(ownerTok, http.MethodPatch, "/components/has-product", map[string]any{"product": ""}, http.StatusUnprocessableEntity)
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/components/has-product", nil, http.StatusOK), &reread); err != nil {
		t.Fatalf("decode get after refused clear: %v", err)
	}
	if reread.Product != "cisco-room-bar" {
		t.Fatalf("product after refused clear = %q, want unchanged cisco-room-bar", reread.Product)
	}
}
