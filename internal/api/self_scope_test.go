package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSelfScopeAPI drives the self operator end to end against a real Postgres: a
// `deploy @ location:room-42 (self)` grant reaches exactly the one room row and
// none of its descendants. Unlike subtree_excl_root (which strips the root from
// the modify actions but keeps the subtree), self keeps the root for every action
// and never walks down: the holder can read and update room-42 itself, but rack-1
// (a child) is invisible, and the list returns only room-42. Skipped under -short.
func TestSelfScopeAPI(t *testing.T) {
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
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	ownerTok := principalWithGrants(t, ctx, dsn, "owner-all", []grant{{role: "owner", scopeKind: "all"}})

	body := func(name, parent string) map[string]any {
		// A child needs a type placement-compatible with a campus parent
		// (allowed_parent_types constrains same-type nesting); campus at root,
		// building under it, since only the tree shape matters here.
		lt := "campus"
		if parent != "" {
			lt = "building"
		}
		b := map[string]any{"name": name, "location_type": lt}
		if parent != "" {
			b["parent"] = parent
		}
		return b
	}
	c.do(ownerTok, http.MethodPost, "/locations", body("room-42", ""), http.StatusCreated)
	c.do(ownerTok, http.MethodPost, "/locations", body("rack-1", "room-42"), http.StatusCreated)
	roomID := entityID(t, c, ownerTok, "/locations", "room-42")

	// A deploy grant scoped to exactly room-42 (self): read + update on the tree
	// tiers, read via the viewer floor, but only the one row.
	selfTok := principalWithGrants(t, ctx, dsn, "single-node",
		[]grant{{role: "deploy", scopeKind: "location", scopeID: roomID, scopeOp: "self"}})
	patch := map[string]any{"display_name": "x"}

	// The self row is fully reachable: read and update the room itself. This is the
	// difference from subtree_excl_root, which would 403 the root on update.
	c.do(selfTok, http.MethodGet, "/locations/room-42", nil, http.StatusOK)
	c.do(selfTok, http.MethodPatch, "/locations/room-42", patch, http.StatusOK)

	// But self is a leaf-lock: it grants no create-placement, so a child cannot be
	// added under the node even though the role carries location:create. The room is
	// readable but outside the create scope, so this is a 403 (not a 404).
	if code, _ := c.send(selfTok, http.MethodPost, "/locations", body("under-self", "room-42")); code != http.StatusForbidden {
		t.Fatalf("self grant creating a child: want 403 (no create-placement), got %d", code)
	}

	// Descendants are NOT in scope: a child is invisible (404, non-disclosing), not a
	// readable-but-forbidden 403, because self never walks the subtree.
	if code, _ := c.send(selfTok, http.MethodGet, "/locations/rack-1", nil); code != http.StatusNotFound {
		t.Fatalf("self grant reading a descendant: want 404, got %d", code)
	}
	if code, _ := c.send(selfTok, http.MethodPatch, "/locations/rack-1", patch); code != http.StatusNotFound {
		t.Fatalf("self grant updating a descendant: want 404, got %d", code)
	}

	// The list returns exactly the self row, never its descendants.
	locs := listNames(t, c, selfTok, "/locations")
	if len(locs) != 1 || locs[0] != "room-42" {
		t.Fatalf("self-scoped list = %v, want [room-42] only", locs)
	}
}
