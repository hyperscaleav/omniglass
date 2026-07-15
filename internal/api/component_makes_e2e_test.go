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

// TestComponentMakesAPI drives the component_make registry over HTTP: a
// viewer reads the seeded official rows under the make:read floor but cannot
// create, an admin (owner) creates a custom row, an official row is
// read-only (422 on patch), and the admin deletes the custom row. Mirrors
// TestComponentTypesAPI; component_make is a flat registry like
// component_type, so the make:* permission is wired exactly like type:*.
func TestComponentMakesAPI(t *testing.T) {
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

	// A plain viewer (read everywhere, write nothing) reads the seeded
	// official makes via the make:read floor (*:read).
	viewerTok := principalWithGrants(t, ctx, dsn, "make-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	out := c.do(viewerTok, http.MethodGet, "/component-makes", nil, http.StatusOK)
	var listed struct {
		Makes []struct {
			ID       string `json:"id"`
			Official bool   `json:"official"`
		} `json:"makes"`
	}
	if err := json.Unmarshal(out, &listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed.Makes) == 0 {
		t.Fatalf("component-makes empty, want seeded rows")
	}

	// The viewer cannot create (403, capability fast-reject).
	c.do(viewerTok, http.MethodPost, "/component-makes",
		map[string]any{"id": "nope", "display_name": "Nope"}, http.StatusForbidden)

	// Admin (owner) creates a custom make.
	var created struct {
		ID       string `json:"id"`
		Official bool   `json:"official"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/component-makes",
		map[string]any{"id": "acme", "display_name": "Acme"}, http.StatusCreated), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID != "acme" || created.Official {
		t.Fatalf("created = %+v, want id=acme official=false", created)
	}

	// Duplicate id is a 409, exercising the shared mapTypeErr ErrTypeExists branch.
	c.do(ownerTok, http.MethodPost, "/component-makes",
		map[string]any{"id": "acme", "display_name": "Dup"}, http.StatusConflict)

	// The custom row is fully mutable.
	c.do(ownerTok, http.MethodPatch, "/component-makes/acme",
		map[string]any{"display_name": "Acme Corp"}, http.StatusOK)
	c.do(ownerTok, http.MethodGet, "/component-makes/acme", nil, http.StatusOK)

	// The seeded official row (crestron) is read-only: 422 on patch and delete.
	c.do(ownerTok, http.MethodPatch, "/component-makes/crestron",
		map[string]any{"display_name": "X"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodDelete, "/component-makes/crestron", nil, http.StatusUnprocessableEntity)

	// Admin deletes the custom row.
	c.do(ownerTok, http.MethodDelete, "/component-makes/acme", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodGet, "/component-makes/acme", nil, http.StatusNotFound)

	// Unknown id is a 404.
	c.do(ownerTok, http.MethodDelete, "/component-makes/nope", nil, http.StatusNotFound)
}
