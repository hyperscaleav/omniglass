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

// TestSystemAPI drives the system surface over HTTP: an owner builds a system
// tree and runs CRUD, and a system-scoped viewer sees only its subtree, gets a
// non-disclosing 404 outside it, and is forbidden a write (capability
// fast-reject). Mirrors TestLocationAPI; reuses its helpers. Skipped under -short.
func TestSystemAPI(t *testing.T) {
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

	// Owner builds av (root) > av-sub; plus lab (root).
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "av", "system_type": "meeting-room"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "av-sub", "system_type": "huddle-room", "parent": "av"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "lab", "system_type": "classroom"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "bad", "system_type": "galaxy"}, http.StatusUnprocessableEntity)

	// Owner lists all three.
	var listed struct {
		Systems []struct {
			ID, Name string
		} `json:"systems"`
	}
	json.Unmarshal(c.do(ownerTok, http.MethodGet, "/systems", nil, http.StatusOK), &listed)
	if len(listed.Systems) != 3 {
		t.Fatalf("owner list = %d, want 3", len(listed.Systems))
	}
	var avID string
	for _, s := range listed.Systems {
		if s.Name == "av" {
			avID = s.ID
		}
	}

	// A viewer scoped to av: sees av + av-sub only, 404 on lab, 403 on write.
	viewerTok := setupScopedViewer(t, ctx, dsn, "viewer-av", "viewer", "system", avID)
	var vlist struct {
		Systems []struct{ Name string } `json:"systems"`
	}
	json.Unmarshal(c.do(viewerTok, http.MethodGet, "/systems", nil, http.StatusOK), &vlist)
	if len(vlist.Systems) != 2 {
		t.Fatalf("viewer-av list = %d, want 2 (av subtree)", len(vlist.Systems))
	}
	c.do(viewerTok, http.MethodGet, "/systems/lab", nil, http.StatusNotFound)
	c.do(viewerTok, http.MethodGet, "/systems/av-sub", nil, http.StatusOK)
	c.do(viewerTok, http.MethodPatch, "/systems/av-sub", map[string]any{"display_name": "nope"}, http.StatusForbidden)

	// Owner CRUD: patch, delete-occupied 409, leaf delete, then 404.
	c.do(ownerTok, http.MethodPatch, "/systems/av-sub", map[string]any{"display_name": "Subsystem"}, http.StatusOK)
	c.do(ownerTok, http.MethodDelete, "/systems/av", nil, http.StatusConflict)
	c.do(ownerTok, http.MethodDelete, "/systems/av-sub", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodGet, "/systems/av-sub", nil, http.StatusNotFound)
}

// TestSystemRenameAndCheckName drives the rename input and the collection-level
// :checkName advisory over HTTP: checkName reports valid + available (scope-blind),
// a PATCH renames by the new technical name, a rename onto a taken name is a 409,
// and a bad slug is rejected at the edge by the Huma pattern (422). Skipped under
// -short.
func TestSystemRenameAndCheckName(t *testing.T) {
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

	// Seed a system.
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "av-one", "system_type": "meeting-room"}, http.StatusCreated)

	type nameCheck struct {
		Valid     bool   `json:"valid"`
		Available bool   `json:"available"`
		Reason    string `json:"reason"`
	}
	check := func(name string) nameCheck {
		out := c.do(ownerTok, http.MethodPost, "/systems:checkName", map[string]any{"name": name}, http.StatusOK)
		var nc nameCheck
		if err := json.Unmarshal(out, &nc); err != nil {
			t.Fatalf("decode checkName: %v", err)
		}
		return nc
	}

	// checkName: taken.
	if nc := check("av-one"); !nc.Valid || nc.Available {
		t.Fatalf("checkName(av-one) = %+v, want valid=true available=false", nc)
	}
	// checkName: available.
	if nc := check("av-free"); !nc.Valid || !nc.Available {
		t.Fatalf("checkName(av-free) = %+v, want valid=true available=true", nc)
	}
	// checkName: bad format -> valid:false, still 200.
	if nc := check("Bad Name"); nc.Valid {
		t.Fatalf("checkName(Bad Name) = %+v, want valid=false", nc)
	}

	// Rename via PATCH.
	out := c.do(ownerTok, http.MethodPatch, "/systems/av-one", map[string]any{"name": "av-renamed"}, http.StatusOK)
	var renamed struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &renamed); err != nil {
		t.Fatalf("decode rename: %v", err)
	}
	if renamed.Name != "av-renamed" {
		t.Fatalf("name = %q, want av-renamed", renamed.Name)
	}

	// Dup rename -> 409.
	c.do(ownerTok, http.MethodPost, "/systems", map[string]any{"name": "av-two", "system_type": "meeting-room"}, http.StatusCreated)
	c.do(ownerTok, http.MethodPatch, "/systems/av-two", map[string]any{"name": "av-renamed"}, http.StatusConflict)

	// Bad format via PATCH -> 422 (Huma pattern rejects at the edge).
	c.do(ownerTok, http.MethodPatch, "/systems/av-two", map[string]any{"name": "Bad Name"}, http.StatusUnprocessableEntity)
}
