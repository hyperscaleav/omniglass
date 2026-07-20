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

// TestVendorsAPI drives the vendor registry over HTTP: a
// viewer reads the seeded official rows under the vendor:read floor but cannot
// create, an admin (owner) creates a custom row, an official row is
// read-only (422 on patch), and the admin deletes the custom row. Mirrors
// TestComponentTypesAPI; vendor is a flat registry like
// component_type, so the vendor:* permission is wired exactly like type:*.
func TestVendorsAPI(t *testing.T) {
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
	// official makes via the vendor:read floor (*:read).
	viewerTok := principalWithGrants(t, ctx, dsn, "make-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	out := c.do(viewerTok, http.MethodGet, "/vendors", nil, http.StatusOK)
	var listed struct {
		Vendors []struct {
			ID       string `json:"id"`
			Official bool   `json:"official"`
		} `json:"vendors"`
	}
	if err := json.Unmarshal(out, &listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed.Vendors) == 0 {
		t.Fatalf("vendors empty, want seeded rows")
	}

	// The viewer cannot create (403, capability fast-reject).
	c.do(viewerTok, http.MethodPost, "/vendors",
		map[string]any{"id": "nope", "display_name": "Nope"}, http.StatusForbidden)

	// Admin (owner) creates a custom make.
	var created struct {
		ID       string `json:"id"`
		Official bool   `json:"official"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/vendors",
		map[string]any{"id": "acme", "display_name": "Acme"}, http.StatusCreated), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID != "acme" || created.Official {
		t.Fatalf("created = %+v, want id=acme official=false", created)
	}

	// Duplicate id is a 409, exercising the shared mapTypeErr ErrTypeExists branch.
	c.do(ownerTok, http.MethodPost, "/vendors",
		map[string]any{"id": "acme", "display_name": "Dup"}, http.StatusConflict)

	// The custom row is fully mutable.
	c.do(ownerTok, http.MethodPatch, "/vendors/acme",
		map[string]any{"display_name": "Acme Corp"}, http.StatusOK)
	c.do(ownerTok, http.MethodGet, "/vendors/acme", nil, http.StatusOK)

	// A non-http(s) website scheme is refused server-side (defense-in-depth
	// against a stored javascript:/data: href reaching a non-browser caller
	// that bypasses the client's own scheme check), on both create and update.
	// A normal https:// website succeeds.
	c.do(ownerTok, http.MethodPost, "/vendors",
		map[string]any{"id": "evil", "display_name": "Evil", "website": "javascript:alert(1)"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPost, "/vendors",
		map[string]any{"id": "acme2", "display_name": "Acme 2", "website": "https://acme.example"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPatch, "/vendors/acme",
		map[string]any{"website": "javascript:alert(1)"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodPatch, "/vendors/acme",
		map[string]any{"website": "https://acme.example"}, http.StatusOK)
	c.do(ownerTok, http.MethodDelete, "/vendors/acme2", nil, http.StatusNoContent)

	// The seeded official row (crestron) is read-only: 422 on patch and delete.
	c.do(ownerTok, http.MethodPatch, "/vendors/crestron",
		map[string]any{"display_name": "X"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodDelete, "/vendors/crestron", nil, http.StatusUnprocessableEntity)

	// Admin deletes the custom row.
	c.do(ownerTok, http.MethodDelete, "/vendors/acme", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodGet, "/vendors/acme", nil, http.StatusNotFound)

	// Unknown id is a 404.
	c.do(ownerTok, http.MethodDelete, "/vendors/nope", nil, http.StatusNotFound)
}
