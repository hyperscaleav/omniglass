package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestComponentModelsAPI drives the component_model registry over HTTP: a
// viewer reads the registry under the model:read floor but cannot create, an
// admin (owner) creates a custom row referencing a custom make, a duplicate
// id is a 409, an unknown make_id is a 422 (the FK violation mapped, not a
// raw 500), the admin patches and deletes the custom row, and deleting a
// make a model references is refused (409, make-in-use) until the model is
// gone. A seeded (official) make is also exercised as a valid make_id.
// Mirrors TestComponentMakesAPI; component_model is a flat registry like
// component_make, so the model:* permission is wired exactly like make:*.
func TestComponentModelsAPI(t *testing.T) {
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
	ownerTok := bootstrapOwnerTok(t, ctx, gw)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// A plain viewer (read everywhere, write nothing) reads the
	// component_model registry via the model:read floor (*:read).
	// Unlike component_make, component_model has no boot-seeded rows (only
	// the dev-seed phase populates examples), so the list starts empty; this
	// only asserts the route is reachable at 200 for the viewer.
	viewerTok := principalWithGrants(t, ctx, dsn, "model-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	c.do(viewerTok, http.MethodGet, "/component-models", nil, http.StatusOK)

	// The viewer cannot create (403, capability fast-reject).
	c.do(viewerTok, http.MethodPost, "/component-models",
		map[string]any{"id": "nope", "display_name": "Nope", "make_id": "crestron", "model_number": "1"}, http.StatusForbidden)

	// Admin creates a custom (non-official) make, since deleting a seeded
	// official make would fail its own read-only guard (422) before ever
	// reaching the in-use check below.
	c.do(ownerTok, http.MethodPost, "/component-makes",
		map[string]any{"id": "acme-mfg", "display_name": "Acme Mfg"}, http.StatusCreated)

	// Admin (owner) creates a custom model referencing the custom make.
	var created struct {
		ID       string `json:"id"`
		MakeID   string `json:"make_id"`
		Official bool   `json:"official"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/component-models",
		map[string]any{"id": "acme-123a", "display_name": "Acme 123A", "make_id": "acme-mfg", "model_number": "123A"}, http.StatusCreated), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID != "acme-123a" || created.MakeID != "acme-mfg" || created.Official {
		t.Fatalf("created = %+v, want id=acme-123a make_id=acme-mfg official=false", created)
	}

	// A seeded (official) make is also a valid make_id reference.
	c.do(ownerTok, http.MethodPost, "/component-models",
		map[string]any{"id": "crestron-nvx", "display_name": "Crestron NVX", "make_id": "crestron", "model_number": "NVX-1"}, http.StatusCreated)

	// Duplicate id is a 409, exercising the shared mapTypeErr ErrTypeExists branch.
	c.do(ownerTok, http.MethodPost, "/component-models",
		map[string]any{"id": "acme-123a", "display_name": "Dup", "make_id": "acme-mfg", "model_number": "1"}, http.StatusConflict)

	// An unknown make_id fails the foreign key; the API maps it to a clean 422
	// rather than a raw 500.
	c.do(ownerTok, http.MethodPost, "/component-models",
		map[string]any{"id": "bad", "display_name": "Bad", "make_id": "nope", "model_number": "1"}, http.StatusUnprocessableEntity)

	// The custom row is mutable.
	c.do(ownerTok, http.MethodPatch, "/component-models/acme-123a",
		map[string]any{"display_name": "Acme 123A Rev B"}, http.StatusOK)
	c.do(ownerTok, http.MethodGet, "/component-models/acme-123a", nil, http.StatusOK)

	// The custom make is now referenced by a model, so deleting it is
	// refused (409, the make-in-use guard on DeleteComponentMake).
	c.do(ownerTok, http.MethodDelete, "/component-makes/acme-mfg", nil, http.StatusConflict)

	// Admin deletes the custom model; the make is no longer referenced and
	// can now be deleted.
	c.do(ownerTok, http.MethodDelete, "/component-models/acme-123a", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodGet, "/component-models/acme-123a", nil, http.StatusNotFound)
	c.do(ownerTok, http.MethodDelete, "/component-makes/acme-mfg", nil, http.StatusNoContent)

	// Unknown id is a 404.
	c.do(ownerTok, http.MethodDelete, "/component-models/nope", nil, http.StatusNotFound)
}
