package rbac_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/rbac"
)

func TestSetAllows(t *testing.T) {
	cases := []struct {
		name  string
		perms []string
		res   string
		act   string
		want  bool
	}{
		{"owner star allows create", []string{"*:*"}, "component", "create", true},
		{"viewer reads any resource", []string{"*:read"}, "alarm", "read", true},
		{"viewer cannot create", []string{"*:read"}, "alarm", "create", false},
		{"read floor: ack implies read", []string{"alarm:ack"}, "alarm", "read", true},
		{"explicit ack allowed", []string{"alarm:ack"}, "alarm", "ack", true},
		{"ack does not imply create", []string{"alarm:ack"}, "alarm", "create", false},
		{"no grant on resource denies read", []string{"alarm:ack"}, "component", "read", false},
		{"comma actions allow update", []string{"component:create,update"}, "component", "update", true},
		{"comma actions deny delete", []string{"component:create,update"}, "component", "delete", false},
		{"resource wildcard action", []string{"component:*"}, "component", "anything", true},
		{"empty set denies", nil, "x", "read", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rbac.NewSet(c.perms).Allows(c.res, c.act); got != c.want {
				t.Errorf("Allows(%q, %q) over %v = %v, want %v", c.res, c.act, c.perms, got, c.want)
			}
		})
	}
}

// TestTopicPatternMatching pins the NATS-style semantics: `*` matches exactly one
// token and `>` matches the tail, so a two-token pattern (`*:read`, `*:*`,
// `principal:*`) structurally cannot reach a three-token `:admin` permission,
// while `>`, `<resource>:>`, and an explicit `:admin` grant can. This is what
// makes admin-sensitivity a deeper token rather than a special case.
func TestTopicPatternMatching(t *testing.T) {
	cases := []struct {
		name   string
		perms  []string
		tokens []string
		want   bool
	}{
		// A partial wildcard (2 tokens) cannot reach a 3-token :admin permission.
		{"viewer *:read misses audit:read:admin", []string{"*:read"}, []string{"audit", "read", "admin"}, false},
		{"*:* misses audit:read:admin", []string{"*:*"}, []string{"audit", "read", "admin"}, false},
		{"principal:* misses principal:delete:admin", []string{"principal:*"}, []string{"principal", "delete", "admin"}, false},
		// The tail wildcard and explicit grants do reach it.
		{"owner > reaches audit:read:admin", []string{">"}, []string{"audit", "read", "admin"}, true},
		{"audit:> reaches audit:read:admin", []string{"audit:>"}, []string{"audit", "read", "admin"}, true},
		{"explicit audit:read:admin reaches it", []string{"audit:read:admin"}, []string{"audit", "read", "admin"}, true},
		{"audit:*:admin reaches it", []string{"audit:*:admin"}, []string{"audit", "read", "admin"}, true},
		// A 3-token grant does not leak to a different tier or action.
		{"audit:read:admin is not audit:delete:admin", []string{"audit:read:admin"}, []string{"audit", "delete", "admin"}, false},
		{"audit:read:admin is not a 2-token audit:read match", []string{"audit:read:admin"}, []string{"audit", "read"}, true}, // via the read floor
		// Normal 2-token permissions still resolve, and > covers everything.
		{"> covers a normal 2-token perm", []string{">"}, []string{"location", "delete"}, true},
		{"*:read covers a normal read", []string{"*:read"}, []string{"location", "read"}, true},
		{"location:> covers location:read", []string{"location:>"}, []string{"location", "read"}, true},
		{"location:> does not cover system:read", []string{"location:>"}, []string{"system", "read"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rbac.NewSet(c.perms).Allows(c.tokens...); got != c.want {
				t.Errorf("Allows(%v) over %v = %v, want %v", c.tokens, c.perms, got, c.want)
			}
		})
	}
}

// TestCoversSubsumption pins the escalation-guard semantics under topic patterns:
// pattern subsumption plus the read floor, so a broader pattern covers a narrower
// one, `>` (owner) covers everything, and no partial wildcard covers `>`.
func TestCoversSubsumption(t *testing.T) {
	set := func(p ...string) rbac.Set { return rbac.NewSet(p) }
	owner := set(">")
	admin := set("principal:*", "location:create,update,delete", "audit:read:admin", "*:read")

	if !owner.Covers(admin) {
		t.Error("owner (>) must cover admin")
	}
	if admin.Covers(owner) {
		t.Error("admin must NOT cover owner (>): no escalation to the superuser tail")
	}
	// A writer covers a reader of the same resource via the floor.
	if !set("location:create,update,delete").Covers(set("location:read")) {
		t.Error("a location writer should cover location:read via the read floor")
	}
	// A 2-token holder does not cover a 3-token :admin permission.
	if set("audit:read").Covers(set("audit:read:admin")) {
		t.Error("audit:read (2 tokens) must not cover audit:read:admin")
	}
	// admin holds audit:read:admin explicitly, so it covers an auditor role.
	if !admin.Covers(set("audit:read:admin")) {
		t.Error("admin should cover an explicit audit:read:admin")
	}
	// A `>` on one resource covers any permission under it, including :admin.
	if !set("audit:>").Covers(set("audit:read:admin")) {
		t.Error("audit:> should cover audit:read:admin")
	}
	// A specific-action pattern never covers a resource wildcard.
	if set("location:read").Covers(set("*:read")) {
		t.Error("location:read must not cover *:read")
	}
}

func TestFlattenInheritance(t *testing.T) {
	idx := rbac.NewRoleIndex([]rbac.Role{
		{ID: "viewer", Permissions: []string{"*:read"}},
		{ID: "operator", Inherits: []string{"viewer"}, Permissions: []string{"component:create,update"}},
	})
	s := idx.Flatten([]string{"operator"})
	if !s.Allows("component", "create") {
		t.Error("operator should allow component:create")
	}
	if !s.Allows("alarm", "read") {
		t.Error("operator should inherit viewer's *:read")
	}
	if s.Allows("component", "delete") {
		t.Error("operator should not allow component:delete")
	}
}

func TestFlattenCycleSafe(t *testing.T) {
	idx := rbac.NewRoleIndex([]rbac.Role{
		{ID: "a", Inherits: []string{"b"}, Permissions: []string{"x:read"}},
		{ID: "b", Inherits: []string{"a"}, Permissions: []string{"y:read"}},
	})
	s := idx.Flatten([]string{"a"})
	if !s.Allows("x", "read") || !s.Allows("y", "read") {
		t.Error("cyclic inheritance should still union both roles' perms without looping")
	}
}
