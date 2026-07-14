package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/rbac"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// scopeGrants mirrors the API's storage.Grant -> scope.Grant mapping so a test can
// resolve a principal's effective scope from its (possibly group-inherited) grants.
func scopeGrants(grants []storage.Grant) []scope.Grant {
	out := make([]scope.Grant, 0, len(grants))
	for _, g := range grants {
		sg := scope.Grant{Role: g.Role, ScopeKind: g.ScopeKind, ScopeOp: g.ScopeOp}
		if g.ScopeID != nil {
			sg.ScopeID = *g.ScopeID
		}
		out = append(out, sg)
	}
	return out
}

// TestGroupGrantsInheritScopeAndPerms proves the whole point of principal groups
// against real Postgres: a role x scope granted to a GROUP is inherited by its
// members, resolving into scope and permissions exactly like a direct grant, and
// the inheritance rides the single grant-loader union (nothing else changes).
// Removing the member, and deleting the group, both drop the inherited access.
func TestGroupGrantsInheritScopeAndPerms(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

	// seed.Run seeds the official roles (viewer = *:read, owner = >) and the four
	// location types, so the tree below is valid.
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	idx := rbac.NewRoleIndex([]rbac.Role{{ID: "viewer", Permissions: []string{"*:read"}}})
	zeros := make([]byte, 32)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: zeros, Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	owner, _ := gw.AuthenticateBearer(ctx, zeros)

	// A small location tree: hq (campus) > hq-b1 (building); lab is a sibling root.
	hq, err := gw.CreateLocation(ctx, owner.ID, storage.LocationSpec{Name: "hq", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create hq: %v", err)
	}
	hqName := "hq"
	if _, err := gw.CreateLocation(ctx, owner.ID, storage.LocationSpec{Name: "hq-b1", LocationType: "building", ParentName: &hqName}, all); err != nil {
		t.Fatalf("create hq-b1: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, owner.ID, storage.LocationSpec{Name: "lab", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create lab: %v", err)
	}

	bob, err := gw.CreateHumanPrincipal(ctx, owner.ID, storage.HumanSpec{Username: "bob"}, all)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	// Bob starts with no access at all.
	if got, _ := gw.GetPrincipal(ctx, bob.ID, all); len(got.Grants) != 0 {
		t.Fatalf("bob should start with no grants, got %+v", got.Grants)
	}

	grp, err := gw.CreateGroup(ctx, owner.ID, storage.GroupSpec{Name: "field-crew", DisplayName: "Field Crew"}, all)
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	// A duplicate group name is refused.
	if _, err := gw.CreateGroup(ctx, owner.ID, storage.GroupSpec{Name: "field-crew"}, all); !errors.Is(err, storage.ErrGroupExists) {
		t.Fatalf("duplicate group name: want ErrGroupExists, got %v", err)
	}
	if err := gw.AddGroupMember(ctx, owner.ID, grp.ID, bob.ID, all); err != nil {
		t.Fatalf("add member: %v", err)
	}
	// Re-adding an existing member is idempotent (still one member, no error).
	if err := gw.AddGroupMember(ctx, owner.ID, grp.ID, bob.ID, all); err != nil {
		t.Fatalf("re-add member: %v", err)
	}
	if members, _ := gw.ListGroupMembers(ctx, grp.ID, all); len(members) != 1 || members[0].Username != "bob" {
		t.Fatalf("members = %+v, want [bob]", members)
	}

	// Grant viewer @ hq (location subtree) to the GROUP. Bob is not granted directly.
	if _, err := gw.CreateGroupGrant(ctx, owner.ID, grp.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "location", ScopeID: hq.ID}, all); err != nil {
		t.Fatalf("group grant: %v", err)
	}
	// A duplicate group grant is refused by the group dedup index.
	if _, err := gw.CreateGroupGrant(ctx, owner.ID, grp.ID, storage.GrantSpec{Role: "viewer", ScopeKind: "location", ScopeID: hq.ID}, all); !errors.Is(err, storage.ErrGrantExists) {
		t.Fatalf("duplicate group grant: want ErrGrantExists, got %v", err)
	}

	// Bob now inherits the grant: it shows on his principal, tagged with the group.
	got, err := gw.GetPrincipal(ctx, bob.ID, all)
	if err != nil {
		t.Fatalf("get bob: %v", err)
	}
	if len(got.Grants) != 1 {
		t.Fatalf("bob should inherit 1 grant, got %+v", got.Grants)
	}
	ig := got.Grants[0]
	if ig.Role != "viewer" || ig.GroupID == nil || *ig.GroupID != grp.ID || ig.ScopeID == nil || *ig.ScopeID != hq.ID {
		t.Fatalf("inherited grant wrong: role=%s group=%v scope=%v", ig.Role, ig.GroupID, ig.ScopeID)
	}

	// The inherited grant resolves into scope exactly like a direct one: bob's read
	// scope is the hq subtree (hq + hq-b1), never lab.
	readSet := scope.Resolve(scopeGrants(got.Grants), idx, "location", "read")
	locs, err := gw.ListLocations(ctx, readSet)
	if err != nil {
		t.Fatalf("list as bob: %v", err)
	}
	if len(locs) != 2 {
		t.Fatalf("bob's inherited read scope = %d locations, want 2 (hq subtree)", len(locs))
	}
	for _, l := range locs {
		if l.Name == "lab" {
			t.Fatal("inherited hq scope leaked lab")
		}
	}
	// And into permissions: the group's viewer role flattens to location:read for bob.
	if !idx.Flatten([]string{ig.Role}).Allows("location", "read") {
		t.Fatal("inherited viewer role should confer location:read")
	}

	// Removing bob from the group drops the inherited access.
	if err := gw.RemoveGroupMember(ctx, owner.ID, grp.ID, bob.ID, all); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	if got, _ := gw.GetPrincipal(ctx, bob.ID, all); len(got.Grants) != 0 {
		t.Fatalf("bob should inherit nothing after removal, got %+v", got.Grants)
	}

	// Re-add, then delete the group: the cascade drops the membership and grant, so
	// bob again inherits nothing.
	if err := gw.AddGroupMember(ctx, owner.ID, grp.ID, bob.ID, all); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if err := gw.DeleteGroup(ctx, owner.ID, grp.ID, all); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	if got, _ := gw.GetPrincipal(ctx, bob.ID, all); len(got.Grants) != 0 {
		t.Fatalf("deleting the group should drop bob's inherited access, got %+v", got.Grants)
	}
	if _, err := gw.GetGroup(ctx, grp.ID, all); !errors.Is(err, storage.ErrGroupNotFound) {
		t.Fatalf("group should be gone: %v", err)
	}
}
