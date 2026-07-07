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
		// Sensitive resources (audit) are exempt from a partial global wildcard: a
		// viewer's *:read must not open the audit trail, but *:* and an explicit
		// grant do.
		{"star-read does NOT reach audit (sensitive)", []string{"*:read"}, "audit", "read", false},
		{"star-star (owner) reaches audit", []string{"*:*"}, "audit", "read", true},
		{"explicit audit:read reaches audit", []string{"audit:read"}, "audit", "read", true},
		{"explicit audit:read does not imply audit:delete", []string{"audit:read"}, "audit", "delete", false},
		{"star-read still reaches non-sensitive reads", []string{"*:read"}, "component", "read", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rbac.NewSet(c.perms).Allows(c.res, c.act); got != c.want {
				t.Errorf("Allows(%q, %q) over %v = %v, want %v", c.res, c.act, c.perms, got, c.want)
			}
		})
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
