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
		{ID: "operator", Inherits: []string{"viewer"}, Permissions: []string{"secret:create,update", "variable:create,update", "field:create,update", "telemetry:push"}},
		{ID: "owner", Permissions: []string{"*:*"}},
	})
}

// A secret is owned on the exclusive arc, so a grant scoped to any owning tier
// (location / system / component) confers secret scope over that subtree. A
// component-scoped operator resolves its own component as a create root, so it
// can seal a secret owned there; a bare read floor never carries create.
func TestResolveSecretArcKinds(t *testing.T) {
	idx := index()
	for _, kind := range []string{"location", "system", "component"} {
		g := []scope.Grant{{Role: "operator", ScopeKind: kind, ScopeID: "ROOT"}}
		set := scope.Resolve(g, idx, "secret", "create")
		if len(set.IDs) != 1 || set.IDs[0] != "ROOT" {
			t.Fatalf("operator@%s secret:create scope = %+v, want ROOT", kind, set)
		}
	}
	// A viewer (read floor only) gets no create scope for secrets.
	vr := scope.Resolve([]scope.Grant{{Role: "viewer", ScopeKind: "component", ScopeID: "C"}}, idx, "secret", "create")
	if !vr.Empty() {
		t.Fatalf("viewer secret:create scope = %+v, want empty", vr)
	}
}

// Pushed telemetry is owned on the same exclusive arc, so an arc-tier grant can
// contain its owner. This is the fence on POST /telemetry:push, and it is resolved
// from telemetry:push rather than component:read on purpose: a principal routinely
// holds a wide read and a narrow write (viewer over the fleet plus operator on one
// component), so resolving the read scope would let it push anywhere it can see.
func TestResolveTelemetryArcKinds(t *testing.T) {
	idx := index()
	for _, kind := range []string{"location", "system", "component"} {
		g := []scope.Grant{{Role: "operator", ScopeKind: kind, ScopeID: "ROOT"}}
		set := scope.Resolve(g, idx, "telemetry", "push")
		if len(set.IDs) != 1 || set.IDs[0] != "ROOT" {
			t.Fatalf("operator@%s telemetry:push scope = %+v, want ROOT", kind, set)
		}
	}
	// The read floor confers no push scope: a viewer can see telemetry, not write it.
	vr := scope.Resolve([]scope.Grant{{Role: "viewer", ScopeKind: "component", ScopeID: "C"}}, idx, "telemetry", "push")
	if !vr.Empty() {
		t.Fatalf("viewer telemetry:push scope = %+v, want empty", vr)
	}
	// The escalation shape, at the resolver: a wide read plus a narrow push must
	// resolve the NARROW set for push, never the wide one.
	wide := scope.Resolve([]scope.Grant{
		{Role: "viewer", ScopeKind: "all"},
		{Role: "operator", ScopeKind: "component", ScopeID: "NARROW"},
	}, idx, "telemetry", "push")
	if wide.All {
		t.Fatal("a wide read grant widened the telemetry:push scope to all")
	}
	if len(wide.IDs) != 1 || wide.IDs[0] != "NARROW" {
		t.Fatalf("wide-read narrow-push scope = %+v, want only NARROW", wide)
	}
}

// A variable is owned on the same exclusive arc as a secret, so it resolves
// scope against the three owner tiers identically.
func TestResolveVariableArcKinds(t *testing.T) {
	idx := index()
	for _, kind := range []string{"location", "system", "component"} {
		g := []scope.Grant{{Role: "operator", ScopeKind: kind, ScopeID: "ROOT"}}
		set := scope.Resolve(g, idx, "variable", "create")
		if len(set.IDs) != 1 || set.IDs[0] != "ROOT" {
			t.Fatalf("operator@%s variable:create scope = %+v, want ROOT", kind, set)
		}
	}
	vr := scope.Resolve([]scope.Grant{{Role: "viewer", ScopeKind: "component", ScopeID: "C"}}, idx, "variable", "create")
	if !vr.Empty() {
		t.Fatalf("viewer variable:create scope = %+v, want empty", vr)
	}
}

// A field value is owned on a component (the same exclusive arc as a secret or
// variable), so a grant scoped to any owning tier (location / system /
// component) confers field scope over that subtree; a component-scoped operator
// resolves its own component as a create root, and a bare read floor carries no
// create.
func TestResolveFieldArcKinds(t *testing.T) {
	idx := index()
	for _, kind := range []string{"location", "system", "component"} {
		g := []scope.Grant{{Role: "operator", ScopeKind: kind, ScopeID: "ROOT"}}
		set := scope.Resolve(g, idx, "field", "create")
		if len(set.IDs) != 1 || set.IDs[0] != "ROOT" {
			t.Fatalf("operator@%s field:create scope = %+v, want ROOT", kind, set)
		}
	}
	vr := scope.Resolve([]scope.Grant{{Role: "viewer", ScopeKind: "component", ScopeID: "C"}}, idx, "field", "create")
	if !vr.Empty() {
		t.Fatalf("viewer field:create scope = %+v, want empty", vr)
	}
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

func TestResolveInterfaceTaskCascade(t *testing.T) {
	// interface and task inherit the component tier: a component-scoped grant whose
	// role carries interface:<action> / task:<action> confers that scope on the
	// interface/task, while a wrong-tier (location/system) grant does not.
	idx := rbac.NewRoleIndex([]rbac.Role{
		{ID: "viewer", Permissions: []string{"*:read"}},
		{ID: "operator", Permissions: []string{"endpoint:create,update", "task:create,update"}},
		{ID: "owner", Permissions: []string{"*:*"}},
	})
	for _, resource := range []string{"endpoint", "task"} {
		// A component-scoped operator grant cascades onto the interface/task.
		s := scope.Resolve([]scope.Grant{{Role: "operator", ScopeKind: "component", ScopeID: "comp-a"}}, idx, resource, "update")
		if s.All || len(s.IDs) != 1 || s.IDs[0] != "comp-a" {
			t.Fatalf("%s update via component grant = %+v, want [comp-a]", resource, s)
		}
		// The read floor also cascades (holding any action implies read).
		r := scope.Resolve([]scope.Grant{{Role: "operator", ScopeKind: "component", ScopeID: "comp-a"}}, idx, resource, "read")
		if len(r.IDs) != 1 || r.IDs[0] != "comp-a" {
			t.Fatalf("%s read floor via component grant = %+v, want [comp-a]", resource, r)
		}
		// A location- or system-tier grant does NOT confer interface/task scope: the
		// cascade is exactly through the component, no wider.
		for _, kind := range []string{"location", "system"} {
			w := scope.Resolve([]scope.Grant{{Role: "owner", ScopeKind: kind, ScopeID: "x"}}, idx, resource, "read")
			if !w.Empty() {
				t.Fatalf("%s via %s grant = %+v, want empty (cascade is component-only)", resource, kind, w)
			}
		}
		// An all grant still short-circuits to All.
		a := scope.Resolve([]scope.Grant{{Role: "owner", ScopeKind: "all"}}, idx, resource, "delete")
		if !a.All {
			t.Fatalf("%s delete via all grant = %+v, want All", resource, a)
		}
	}
}

// TestResolveAlarmResolvesOnTheComponentTierFromItsOwnAction pins the scope half
// of #728. An alarm hangs off a component, so the component tier is what contains
// it; the load-bearing part is that the acknowledgement resolves from
// alarm:acknowledge and NOT from the component gate, since a principal commonly
// holds a wide component read beside a narrow write and either one deciding who
// may acknowledge is a defect.
func TestResolveAlarmResolvesOnTheComponentTierFromItsOwnAction(t *testing.T) {
	idx := rbac.NewRoleIndex([]rbac.Role{
		// A responder may acknowledge and read, and may not touch the component.
		{ID: "responder", Permissions: []string{"alarm:acknowledge"}},
		// A fleet-wide reader may see everything and acknowledge nothing.
		{ID: "viewer", Permissions: []string{"*:read"}},
	})

	narrow := scope.Grant{Role: "responder", ScopeKind: "component", ScopeID: "comp-a"}
	wide := scope.Grant{Role: "viewer", ScopeKind: "all"}

	s := scope.Resolve([]scope.Grant{narrow}, idx, "alarm", "acknowledge")
	if s.All || len(s.IDs) != 1 || s.IDs[0] != "comp-a" {
		t.Fatalf("acknowledge scope = %+v, want [comp-a]: an alarm resolves on the component tier", s)
	}
	// A role that may acknowledge need not be able to edit the component, so the
	// acknowledgement must not resolve through component:update.
	if u := scope.Resolve([]scope.Grant{narrow}, idx, "component", "update"); !u.Empty() {
		t.Fatalf("component update scope = %+v, want empty: the responder role carries no component write", u)
	}

	// The whole point: the all-scoped viewer grant carries no acknowledgement, so
	// it widens the READ everywhere and the acknowledgement nowhere.
	both := []scope.Grant{narrow, wide}
	if a := scope.Resolve(both, idx, "alarm", "acknowledge"); a.All || len(a.IDs) != 1 || a.IDs[0] != "comp-a" {
		t.Fatalf("acknowledge scope beside an all-scoped viewer = %+v, want [comp-a]: a wide read must not widen the acknowledgement", a)
	}
	if r := scope.Resolve(both, idx, "component", "read"); !r.All {
		t.Fatalf("component read scope = %+v, want All: the viewer floor is what makes this fixture meaningful", r)
	}

	// A location- or system-tier grant contains no alarm, exactly as it contains
	// no component (#714): the cascade is through the component, no wider.
	for _, kind := range []string{"location", "system"} {
		w := scope.Resolve([]scope.Grant{{Role: "responder", ScopeKind: kind, ScopeID: "x"}}, idx, "alarm", "acknowledge")
		if !w.Empty() {
			t.Fatalf("alarm via a %s grant = %+v, want empty", kind, w)
		}
	}
}

// TestResolveCommandResolvesOnTheComponentTierFromItsOwnAction is the same pin
// for #749, and it is the precondition the route's fix depends on rather than a
// restatement of the alarm case. A command is issued TO a component, so the
// component tier is what contains it; without this, `command` falls to
// applicableKinds' default and every grant but an all-scoped one resolves to the
// empty set, which would deny every scoped issuer instead of fencing them.
func TestResolveCommandResolvesOnTheComponentTierFromItsOwnAction(t *testing.T) {
	idx := rbac.NewRoleIndex([]rbac.Role{
		// An issuer may command and read, and may not edit the component.
		{ID: "issuer", Permissions: []string{"command:issue"}},
		// A fleet-wide reader may see everything and command nothing.
		{ID: "viewer", Permissions: []string{"*:read"}},
	})

	narrow := scope.Grant{Role: "issuer", ScopeKind: "component", ScopeID: "comp-a"}
	wide := scope.Grant{Role: "viewer", ScopeKind: "all"}

	s := scope.Resolve([]scope.Grant{narrow}, idx, "command", "issue")
	if s.All || len(s.IDs) != 1 || s.IDs[0] != "comp-a" {
		t.Fatalf("issue scope = %+v, want [comp-a]: a command resolves on the component tier", s)
	}
	// Issuing is a modify action, so a role that may issue need not be able to
	// edit the component and the fence must not resolve through component:update.
	if u := scope.Resolve([]scope.Grant{narrow}, idx, "component", "update"); !u.Empty() {
		t.Fatalf("component update scope = %+v, want empty: the issuer role carries no component write", u)
	}

	// The defect #749 filed, stated as a scope fact: the all-scoped viewer grant
	// carries no issue, so it widens the READ everywhere and the actuation
	// nowhere. A route fencing the write with the read set below gets All.
	both := []scope.Grant{narrow, wide}
	if a := scope.Resolve(both, idx, "command", "issue"); a.All || len(a.IDs) != 1 || a.IDs[0] != "comp-a" {
		t.Fatalf("issue scope beside an all-scoped viewer = %+v, want [comp-a]: a wide read must not widen what may be commanded", a)
	}
	if r := scope.Resolve(both, idx, "component", "read"); !r.All {
		t.Fatalf("component read scope = %+v, want All: the viewer floor is what makes this fixture meaningful", r)
	}

	// A location- or system-tier grant contains no command, exactly as it contains
	// no component (#714): the cascade is through the component, no wider. The
	// route issues only to components today, so admitting the other two tiers
	// would put roots in the set that the component tree can never match.
	for _, kind := range []string{"location", "system"} {
		w := scope.Resolve([]scope.Grant{{Role: "issuer", ScopeKind: kind, ScopeID: "x"}}, idx, "command", "issue")
		if !w.Empty() {
			t.Fatalf("command via a %s grant = %+v, want empty", kind, w)
		}
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
