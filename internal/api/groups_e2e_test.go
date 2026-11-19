package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestGroupGrantInheritanceAPI drives user groups over HTTP end to end: a
// role granted to a group is inherited by its members (a no-access user gains
// read once added to a group that holds viewer @ all, and loses it on removal),
// group management is gated by user_group, and the escalation guard holds for
// group grants exactly as for direct ones (an admin cannot group-grant owner).
func TestGroupGrantInheritanceAPI(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	adminTok := userWithGrants(t, ctx, dsn, "an-admin", []grant{{role: "admin", scopeKind: "all"}})
	bobTok := userWithGrants(t, ctx, dsn, "bob", nil)
	bobID := meID(t, c, bobTok)

	// Bob has no grants, so he cannot read locations, and (no user_group) he
	// cannot manage groups.
	if code, _ := c.send(bobTok, http.MethodGet, "/locations", nil); code != http.StatusForbidden {
		t.Fatalf("bob before: want 403 on /locations, got %d", code)
	}
	if code, _ := c.send(bobTok, http.MethodPost, "/user-groups", map[string]any{"name": "sneaky"}); code != http.StatusForbidden {
		t.Fatalf("bob creating a group: want 403 (no user_group), got %d", code)
	}

	// Owner creates a group, adds bob, and grants viewer @ all to the group.
	var grp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/user-groups", map[string]any{"name": "crew", "label": "Crew"}, http.StatusCreated), &grp); err != nil || grp.ID == "" {
		t.Fatalf("create group: %v (id %q)", err, grp.ID)
	}
	// A duplicate name is refused.
	if code, _ := c.send(ownerTok, http.MethodPost, "/user-groups", map[string]any{"name": "crew"}); code != http.StatusConflict {
		t.Fatalf("duplicate group name: want 409, got %d", code)
	}
	c.do(ownerTok, http.MethodPost, "/user-groups/"+grp.ID+"/members", map[string]any{"user_id": bobID}, http.StatusNoContent)
	c.do(ownerTok, http.MethodPost, "/user-groups/"+grp.ID+"/grants", map[string]any{"role": "viewer", "scope_kind": "all"}, http.StatusCreated)

	// Bob now inherits viewer @ all through the group: he can read locations.
	if code, b := c.send(bobTok, http.MethodGet, "/locations", nil); code != http.StatusOK {
		t.Fatalf("bob after group grant: want 200 on /locations, got %d (%s)", code, b)
	}

	// The escalation guard holds for group grants: an admin cannot grant owner (a
	// tier it does not cover) to a group, no more than to a user.
	if code, b := c.send(adminTok, http.MethodPost, "/user-groups/"+grp.ID+"/grants", map[string]any{"role": "owner", "scope_kind": "all"}); code != http.StatusForbidden {
		t.Fatalf("admin group-granting owner: want 403 (escalation), got %d (%s)", code, b)
	}

	// The group's members list shows bob; his membership drives the inheritance.
	//
	// bob is a SERVICE user (userWithGrants inserts one), so "bob" is
	// his NAME and not a label. The roster used to hand it back in
	// label, through a coalesce that fell from a human's friendly string to
	// a machine user's identifier, and this assertion is what that looked like
	// from the wire: an identifier read out of a field named for a label (#563).
	var members struct {
		Members []struct {
			UserID string `json:"user_id"`
			Name   string `json:"name"`
			Label  string `json:"label"`
		} `json:"members"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/user-groups/"+grp.ID+"/members", nil, http.StatusOK), &members); err != nil || len(members.Members) != 1 || members.Members[0].UserID != bobID || members.Members[0].Name != "bob" {
		t.Fatalf("members = %+v err %v, want [bob]", members, err)
	}
	if members.Members[0].Label != "" {
		t.Errorf("the roster gave a machine user the label %q; it has no column for one, and the field that used to carry its identifier is now `name`",
			members.Members[0].Label)
	}

	// Removing bob from the group drops the inherited read.
	c.do(ownerTok, http.MethodDelete, "/user-groups/"+grp.ID+"/members/"+bobID, nil, http.StatusNoContent)
	if code, _ := c.send(bobTok, http.MethodGet, "/locations", nil); code != http.StatusForbidden {
		t.Fatalf("bob after removal: want 403 on /locations, got %d", code)
	}
}

// TestInheritedGrantNamesItsGroupByIdentityTriad holds an inherited grant to the
// identity triad (#1135): the source group is named by `group` (its name, the
// identifier a client can type back into /user-groups/{group}) beside
// `group_id`, and its friendly string rides `group_label`. The old `group_name`
// carried the label, so a client feeding it back broke on any labelled group.
func TestInheritedGrantNamesItsGroupByIdentityTriad(t *testing.T) {
	t.Parallel()
	dsn := seededDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	ownerTok := bootstrapOwnerTok(t, ctx, gw)
	memberTok := userWithGrants(t, ctx, dsn, "member", nil)
	memberID := meID(t, c, memberTok)

	var grp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/user-groups", map[string]any{"name": "ops", "label": "Operations"}, http.StatusCreated), &grp); err != nil || grp.ID == "" {
		t.Fatalf("create group: %v (id %q)", err, grp.ID)
	}
	c.do(ownerTok, http.MethodPost, "/user-groups/ops/members", map[string]any{"user_id": memberID}, http.StatusNoContent)
	c.do(ownerTok, http.MethodPost, "/user-groups/ops/grants", map[string]any{"role": "viewer", "scope_kind": "all"}, http.StatusCreated)

	raw := c.do(ownerTok, http.MethodGet, "/users/"+memberID, nil, http.StatusOK)
	var body struct {
		Grants []map[string]any `json:"grants"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode user: %v", err)
	}
	var inherited map[string]any
	for _, g := range body.Grants {
		if g["group_id"] != nil {
			inherited = g
		}
	}
	if inherited == nil {
		t.Fatalf("no inherited grant in %s", raw)
	}
	if inherited["group"] != "ops" || inherited["group_id"] != grp.ID || inherited["group_label"] != "Operations" {
		t.Errorf("inherited grant = %v, want group=ops group_id=%s group_label=Operations", inherited, grp.ID)
	}
	if _, has := inherited["group_name"]; has {
		t.Errorf("inherited grant still carries group_name: %v", inherited)
	}

	// The carried `group` round-trips as the address of the group.
	carried, _ := inherited["group"].(string)
	if carried == "" {
		t.Fatalf("inherited grant carries no group: %v", inherited)
	}
	c.do(ownerTok, http.MethodGet, "/user-groups/"+carried, nil, http.StatusOK)
}
