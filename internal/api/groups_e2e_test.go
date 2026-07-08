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

// TestGroupGrantInheritanceAPI drives principal groups over HTTP end to end: a
// role granted to a group is inherited by its members (a no-access user gains
// read once added to a group that holds viewer @ all, and loses it on removal),
// group management is gated by principal_group, and the escalation guard holds for
// group grants exactly as for direct ones (an admin cannot group-grant owner).
func TestGroupGrantInheritanceAPI(t *testing.T) {
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

	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	adminTok := principalWithGrants(t, ctx, dsn, "an-admin", []grant{{role: "admin", scopeKind: "all"}})
	bobTok := principalWithGrants(t, ctx, dsn, "bob", nil)
	bobID := meID(t, c, bobTok)

	// Bob has no grants, so he cannot read locations, and (no principal_group) he
	// cannot manage groups.
	if code, _ := c.send(bobTok, http.MethodGet, "/locations", nil); code != http.StatusForbidden {
		t.Fatalf("bob before: want 403 on /locations, got %d", code)
	}
	if code, _ := c.send(bobTok, http.MethodPost, "/principal-groups", map[string]any{"name": "sneaky"}); code != http.StatusForbidden {
		t.Fatalf("bob creating a group: want 403 (no principal_group), got %d", code)
	}

	// Owner creates a group, adds bob, and grants viewer @ all to the group.
	var grp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/principal-groups", map[string]any{"name": "crew", "display_name": "Crew"}, http.StatusCreated), &grp); err != nil || grp.ID == "" {
		t.Fatalf("create group: %v (id %q)", err, grp.ID)
	}
	// A duplicate name is refused.
	if code, _ := c.send(ownerTok, http.MethodPost, "/principal-groups", map[string]any{"name": "crew"}); code != http.StatusConflict {
		t.Fatalf("duplicate group name: want 409, got %d", code)
	}
	c.do(ownerTok, http.MethodPost, "/principal-groups/"+grp.ID+"/members", map[string]any{"principal_id": bobID}, http.StatusNoContent)
	c.do(ownerTok, http.MethodPost, "/principal-groups/"+grp.ID+"/grants", map[string]any{"role": "viewer", "scope_kind": "all"}, http.StatusCreated)

	// Bob now inherits viewer @ all through the group: he can read locations.
	if code, b := c.send(bobTok, http.MethodGet, "/locations", nil); code != http.StatusOK {
		t.Fatalf("bob after group grant: want 200 on /locations, got %d (%s)", code, b)
	}

	// The escalation guard holds for group grants: an admin cannot grant owner (a
	// tier it does not cover) to a group, no more than to a principal.
	if code, b := c.send(adminTok, http.MethodPost, "/principal-groups/"+grp.ID+"/grants", map[string]any{"role": "owner", "scope_kind": "all"}); code != http.StatusForbidden {
		t.Fatalf("admin group-granting owner: want 403 (escalation), got %d (%s)", code, b)
	}

	// The group's members list shows bob; his membership drives the inheritance.
	var members struct {
		Members []struct {
			PrincipalID string `json:"principal_id"`
			DisplayName string `json:"display_name"`
		} `json:"members"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/principal-groups/"+grp.ID+"/members", nil, http.StatusOK), &members); err != nil || len(members.Members) != 1 || members.Members[0].PrincipalID != bobID || members.Members[0].DisplayName != "bob" {
		t.Fatalf("members = %+v err %v, want [bob]", members, err)
	}

	// Removing bob from the group drops the inherited read.
	c.do(ownerTok, http.MethodDelete, "/principal-groups/"+grp.ID+"/members/"+bobID, nil, http.StatusNoContent)
	if code, _ := c.send(bobTok, http.MethodGet, "/locations", nil); code != http.StatusForbidden {
		t.Fatalf("bob after removal: want 403 on /locations, got %d", code)
	}
}
