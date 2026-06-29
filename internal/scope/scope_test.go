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
