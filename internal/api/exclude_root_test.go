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

// TestExcludeRootDeployScopeAPI drives the deploy/integrator use case end to end
// against a real Postgres: a `deploy @ location:room-42 (exclude_root)` grant can
// see the room, add and edit things under it, but cannot modify the room itself.
// This proves the exclude_root grant modifier (read + create-placement keep the
// root; update/delete exclude it) through the live HTTP stack. Skipped under -short.
func TestExcludeRootDeployScopeAPI(t *testing.T) {
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

	// Owner builds room-42 > rack-1: the deploy target subtree.
	body := func(name, parent string) map[string]any {
		b := map[string]any{"name": name, "location_type": "campus"}
		if parent != "" {
			b["parent"] = parent
		}
		return b
	}
	c.do(ownerTok, "POST", "/locations", body("room-42", ""), http.StatusCreated)
	c.do(ownerTok, "POST", "/locations", body("rack-1", "room-42"), http.StatusCreated)
	roomID := entityID(t, c, ownerTok, "/locations", "room-42")

	// An integrator scoped to room-42 with exclude_root (the deploy role: create +
	// update on the three tree tiers, read via the viewer floor).
	deployTok := principalWithGrants(t, ctx, dsn, "integrator",
		[]grant{{role: "deploy", scopeKind: "location", scopeID: roomID, excludeRoot: true}})
	patch := map[string]any{"display_name": "x"}

	// Read keeps the root: the integrator can see the room it deploys into.
	c.do(deployTok, http.MethodGet, "/locations/room-42", nil, http.StatusOK)
	// Create-placement keeps the root: a child can be added UNDER room-42.
	c.do(deployTok, http.MethodPost, "/locations", body("panel-a", "room-42"), http.StatusCreated)
	// A descendant is writable (rack-1 is under the room, not the excluded root).
	c.do(deployTok, http.MethodPatch, "/locations/rack-1", patch, http.StatusOK)
	// The root itself is NOT writable: readable, but outside the update scope -> 403
	// (the crown-jewel readable-but-out-of-write-scope case), not a 404.
	c.do(deployTok, http.MethodPatch, "/locations/room-42", patch, http.StatusForbidden)
	// Delete is not in the deploy role at all: a capability 403 even on a child.
	if code, _ := c.send(deployTok, http.MethodDelete, "/locations/panel-a", nil); code != http.StatusForbidden {
		t.Fatalf("deploy role has no delete: want 403, got %d", code)
	}
}
