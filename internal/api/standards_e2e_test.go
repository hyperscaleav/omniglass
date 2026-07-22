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

// standardWire is the decoded standard wire shape for the e2e assertions.
type standardWire struct {
	ID               string `json:"id"`
	DisplayName      string `json:"display_name"`
	ParentStandardID string `json:"parent_standard_id"`
	Official         bool   `json:"official"`
}

// TestStandardsAPI drives the standard catalog over HTTP: a viewer reads the
// seeded official rows under the standard:read floor but cannot create, an admin
// (owner) creates a custom standard and a variant of it, an unknown parent is a
// 422, an official row is read-only (422 on patch and delete), a standard a
// system conforms to cannot be deleted (409), and the freed row deletes. Mirrors
// TestProductsAPI: standard is the system-side counterpart of product, so
// standard:* is wired exactly like product:*.
func TestStandardsAPI(t *testing.T) {
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

	// A plain viewer (read everywhere, write nothing) reads the seeded official
	// standards via the standard:read floor (*:read).
	viewerTok := principalWithGrants(t, ctx, dsn, "standard-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	var listed struct {
		Standards []struct {
			ID       string `json:"id"`
			Official bool   `json:"official"`
		} `json:"standards"`
	}
	if err := json.Unmarshal(c.do(viewerTok, http.MethodGet, "/standards", nil, http.StatusOK), &listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed.Standards) == 0 {
		t.Fatalf("standards empty, want seeded rows")
	}

	// The viewer cannot create (403, capability fast-reject).
	c.do(viewerTok, http.MethodPost, "/standards",
		map[string]any{"id": "nope", "display_name": "Nope"}, http.StatusForbidden)

	// Admin (owner) creates a custom standard, then a variant of it.
	var created standardWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/standards",
		map[string]any{"id": "kiosk", "display_name": "Kiosk"}, http.StatusCreated), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID != "kiosk" || created.Official {
		t.Fatalf("created = %+v, want id=kiosk official=false", created)
	}
	var variant standardWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/standards",
		map[string]any{"id": "kiosk-outdoor", "display_name": "Outdoor Kiosk", "parent_standard_id": "kiosk"},
		http.StatusCreated), &variant); err != nil {
		t.Fatalf("decode create variant: %v", err)
	}
	if variant.ParentStandardID != "kiosk" {
		t.Fatalf("variant parent = %q, want kiosk", variant.ParentStandardID)
	}
	c.do(ownerTok, http.MethodDelete, "/standards/kiosk-outdoor", nil, http.StatusNoContent)

	// Duplicate id is a 409; an unknown parent is a 422.
	c.do(ownerTok, http.MethodPost, "/standards",
		map[string]any{"id": "kiosk", "display_name": "Dup"}, http.StatusConflict)
	c.do(ownerTok, http.MethodPost, "/standards",
		map[string]any{"id": "orphan", "display_name": "Orphan", "parent_standard_id": "no-such-standard"},
		http.StatusUnprocessableEntity)

	// The custom row is mutable, and the patch reads back on GET.
	c.do(ownerTok, http.MethodPatch, "/standards/kiosk",
		map[string]any{"display_name": "Info Kiosk"}, http.StatusOK)
	var reread standardWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/standards/kiosk", nil, http.StatusOK), &reread); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if reread.DisplayName != "Info Kiosk" {
		t.Fatalf("patched display_name = %q, want Info Kiosk", reread.DisplayName)
	}

	// A system conforming to the standard blocks its delete (409); the freed
	// standard then deletes.
	c.do(ownerTok, http.MethodPost, "/systems",
		map[string]any{"name": "k1", "standard_id": "kiosk"}, http.StatusCreated)
	c.do(ownerTok, http.MethodDelete, "/standards/kiosk", nil, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/systems/k1", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodDelete, "/standards/kiosk", nil, http.StatusNoContent)

	// A shipped standard is operator-owned example content (forked from an in-code
	// template), so it is editable rather than read-only. The official read-only
	// guard still exists for genuinely official rows; it is proven at the storage
	// tier, where a test can mint one.
	c.do(ownerTok, http.MethodPatch, "/standards/meeting-room",
		map[string]any{"display_name": "Meeting Room"}, http.StatusOK)

	// Unknown id is a 404, on both read and delete.
	c.do(ownerTok, http.MethodGet, "/standards/nope", nil, http.StatusNotFound)
	c.do(ownerTok, http.MethodDelete, "/standards/nope", nil, http.StatusNotFound)
}
