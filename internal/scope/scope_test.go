package scope_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/rbac"
	"github.com/hyperscaleav/omniglass/internal/scope"
)

// index mirrors the official roles closely enough for the resolver tests: a
// read-only viewer, an operator that can update locations, and an owner with the
// global wildcard.
func index() rbac.RoleIndex {
	return rbac.NewRoleIndex([]rbac.Role{
		{ID: "viewer", Permissions: []string{"*:read"}},
		{ID: "loc-editor", Permissions: []string{"location:create,update,delete"}},
		{ID: "owner", Permissions: []string{"*:*"}},
	})
}

func TestResolveAllScope(t *testing.T) {
	idx := index()
	set := scope.Resolve([]scope.Grant{{Role: "owner", ScopeKind: "all"}}, idx, "location", "update")
	if !set.All {
		t.Fatalf("owner@all should resolve All for update, got %+v", set)
	}
}

func TestResolvePerActionOverPermitFix(t *testing.T) {
	idx := index()
	// A viewer scoped to HQ and a loc-editor scoped to LAB. For read, both grants
	// carry the action (loc-editor gets read via the floor), so both roots union.
	grants := []scope.Grant{
		{Role: "viewer", ScopeKind: "location", ScopeID: "HQ"},
		{Role: "loc-editor", ScopeKind: "location", ScopeID: "LAB"},
	}
	read := scope.Resolve(grants, idx, "location", "read")
	if read.All || len(read.IDs) != 2 {
		t.Fatalf("read scope = %+v, want HQ+LAB", read)
	}

	// For update, only the loc-editor grant carries the action: the viewer@HQ
	// grant must NOT contribute its scope. This is the over-permit fix.
	upd := scope.Resolve(grants, idx, "location", "update")
	if upd.All || len(upd.IDs) != 1 || upd.IDs[0] != "LAB" {
		t.Fatalf("update scope = %+v, want only LAB (viewer@HQ excluded)", upd)
	}
}

func TestResolveReadFloor(t *testing.T) {
	idx := index()
	// loc-editor has no explicit :read, but the floor grants read on a resource it
	// holds any permission for.
	set := scope.Resolve([]scope.Grant{{Role: "loc-editor", ScopeKind: "location", ScopeID: "LAB"}},
		idx, "location", "read")
	if len(set.IDs) != 1 || set.IDs[0] != "LAB" {
		t.Fatalf("read-floor scope = %+v, want LAB", set)
	}
}

func TestResolveDefaultOpIsSubtree(t *testing.T) {
	idx := index()
	// A grant literal that leaves ScopeOp empty means the subtree default: root plus
	// descendants, no exclusion, for every action. This keeps every existing grant
	// (and every storage row that predates an explicit op) behaving as before.
	grants := []scope.Grant{{Role: "loc-editor", ScopeKind: "location", ScopeID: "ROOM"}}
	upd := scope.Resolve(grants, idx, "location", "update")
	if len(upd.IDs) != 1 || upd.IDs[0] != "ROOM" || len(upd.ExcludeRootIDs) != 0 || len(upd.SelfIDs) != 0 {
		t.Fatalf("empty op = %+v, want ROOM as a plain subtree root", upd)
	}
}

func TestResolveSubtreeExclRoot(t *testing.T) {
	idx := index()
	// scope_op = subtree_excl_root: the write scope covers ROOM's descendants but not
	// ROOM itself; read and create keep ROOM (you must see the room and be able to
	// place children under it). This is the old exclude_root=true behavior.
	grants := []scope.Grant{{Role: "loc-editor", ScopeKind: "location", ScopeID: "ROOM", ScopeOp: "subtree_excl_root"}}

	for _, action := range []string{"update", "delete"} {
		s := scope.Resolve(grants, idx, "location", action)
		if len(s.IDs) != 1 || s.IDs[0] != "ROOM" || len(s.ExcludeRootIDs) != 1 || s.ExcludeRootIDs[0] != "ROOM" || len(s.SelfIDs) != 0 {
			t.Fatalf("%s scope = %+v, want ROOM as an excluded root", action, s)
		}
	}
	for _, action := range []string{"read", "create"} {
		s := scope.Resolve(grants, idx, "location", action)
		if len(s.IDs) != 1 || s.IDs[0] != "ROOM" || len(s.ExcludeRootIDs) != 0 {
			t.Fatalf("%s scope = %+v, want ROOM included (no exclusion)", action, s)
		}
	}
}

func TestResolveSubtreeExclRootInclusiveWins(t *testing.T) {
	idx := index()
	// Two grants naming the SAME root, one subtree_excl_root and one plain subtree:
	// the inclusive grant wins, so the root is not excluded for update.
	grants := []scope.Grant{
		{Role: "loc-editor", ScopeKind: "location", ScopeID: "ROOM", ScopeOp: "subtree_excl_root"},
		{Role: "loc-editor", ScopeKind: "location", ScopeID: "ROOM", ScopeOp: "subtree"},
	}
	upd := scope.Resolve(grants, idx, "location", "update")
	if len(upd.IDs) != 1 || len(upd.ExcludeRootIDs) != 0 {
		t.Fatalf("update scope = %+v, want ROOM included (inclusive wins)", upd)
	}
}

func TestResolveSelf(t *testing.T) {
	idx := index()
	// scope_op = self: exactly the ROOM row, no descendant walk. The root goes to
	// SelfIDs (matched by id equality), never to IDs (which the gateway would expand
	// into a subtree). It applies to read/update/delete but NOT create: a leaf-lock
	// grants no authority to place children under its node.
	grants := []scope.Grant{{Role: "loc-editor", ScopeKind: "location", ScopeID: "ROOM", ScopeOp: "self"}}
	for _, action := range []string{"read", "update", "delete"} {
		s := scope.Resolve(grants, idx, "location", action)
		if len(s.SelfIDs) != 1 || s.SelfIDs[0] != "ROOM" || len(s.IDs) != 0 || len(s.ExcludeRootIDs) != 0 {
			t.Fatalf("%s self scope = %+v, want ROOM as a self id only", action, s)
		}
		if s.Empty() {
			t.Fatalf("%s self scope should not be Empty, got %+v", action, s)
		}
	}
	// Create is out of a self grant's scope entirely: it cannot place a child.
	c := scope.Resolve(grants, idx, "location", "create")
	if !c.Empty() {
		t.Fatalf("self create scope = %+v, want empty (no create-placement)", c)
	}
}

func TestResolveSelfReAddsExcludedRoot(t *testing.T) {
	idx := index()
	// A root granted subtree_excl_root (root stripped from the modify subtree) AND
	// self (the root row alone) unions to root + descendants for modify: the self
	// grant re-adds the row the exclusion removed. The root stays in ExcludeRootIDs
	// (so the subtree walk still excludes it) and also appears in SelfIDs (so the row
	// itself matches).
	grants := []scope.Grant{
		{Role: "loc-editor", ScopeKind: "location", ScopeID: "ROOM", ScopeOp: "subtree_excl_root"},
		{Role: "loc-editor", ScopeKind: "location", ScopeID: "ROOM", ScopeOp: "self"},
	}
	upd := scope.Resolve(grants, idx, "location", "update")
	if len(upd.IDs) != 1 || upd.IDs[0] != "ROOM" || len(upd.ExcludeRootIDs) != 1 || upd.ExcludeRootIDs[0] != "ROOM" || len(upd.SelfIDs) != 1 || upd.SelfIDs[0] != "ROOM" {
		t.Fatalf("self+excl_root scope = %+v, want ROOM excluded-from-subtree but re-added via SelfIDs", upd)
	}
}

func TestResolveSelfRedundantUnderSubtree(t *testing.T) {
	idx := index()
	// If a root is already covered inclusively by a subtree grant, a self grant on the
	// same root adds nothing: SelfIDs stays empty (the subtree already matches the row).
	grants := []scope.Grant{
		{Role: "loc-editor", ScopeKind: "location", ScopeID: "ROOM", ScopeOp: "subtree"},
		{Role: "loc-editor", ScopeKind: "location", ScopeID: "ROOM", ScopeOp: "self"},
	}
	upd := scope.Resolve(grants, idx, "location", "update")
	if len(upd.IDs) != 1 || len(upd.ExcludeRootIDs) != 0 || len(upd.SelfIDs) != 0 {
		t.Fatalf("self-under-subtree scope = %+v, want just the subtree root, no SelfIDs", upd)
	}
}

func TestResolveWrongTierIgnored(t *testing.T) {
	idx := index()
	// A grant scoped to a system tier cannot confer location access: a system sits
	// below a location, so it does not contain one.
	set := scope.Resolve([]scope.Grant{{Role: "owner", ScopeKind: "system", ScopeID: "sys-1"}},
		idx, "location", "read")
	if !set.Empty() {
		t.Fatalf("system-scoped grant should not confer location scope, got %+v", set)
	}
}

func TestResolveNoMatchingGrant(t *testing.T) {
	idx := index()
	// A viewer can never update; the resolved update scope is empty even though it
	// can read everywhere.
	set := scope.Resolve([]scope.Grant{{Role: "viewer", ScopeKind: "all"}}, idx, "location", "update")
	if !set.Empty() {
		t.Fatalf("viewer update scope = %+v, want empty", set)
	}
}

func TestResolveSystemKind(t *testing.T) {
	idx := index()
	// A system-scoped grant contributes to the system resource...
	set := scope.Resolve([]scope.Grant{{Role: "owner", ScopeKind: "system", ScopeID: "av"}}, idx, "system", "read")
	if set.All || len(set.IDs) != 1 || set.IDs[0] != "av" {
		t.Fatalf("system scope = %+v, want [av]", set)
	}
	// ...but a location-scoped grant does NOT confer system access (no cross-tier
	// cascade yet).
	other := scope.Resolve([]scope.Grant{{Role: "owner", ScopeKind: "location", ScopeID: "hq"}}, idx, "system", "read")
	if !other.Empty() {
		t.Fatalf("location grant should not confer system scope, got %+v", other)
	}
}

func TestResolveDedupRoots(t *testing.T) {
	idx := index()
	// Two grants to the same root via different roles collapse to one id.
	grants := []scope.Grant{
		{Role: "viewer", ScopeKind: "location", ScopeID: "HQ"},
		{Role: "loc-editor", ScopeKind: "location", ScopeID: "HQ"},
	}
	set := scope.Resolve(grants, idx, "location", "read")
	if len(set.IDs) != 1 {
		t.Fatalf("dedup failed: %+v", set)
	}
}
