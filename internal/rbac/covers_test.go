package rbac_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/rbac"
)

func set(perms ...string) rbac.Set { return rbac.NewSet(perms) }

// TestCovers is the security core of impersonation: A may impersonate T only when
// A's capabilities cover T's, so impersonation can never grant a capability the
// impersonator lacks. The check is conservative with wildcards (specific actions
// never cover a "*", so a lesser admin cannot impersonate an owner).
func TestCovers(t *testing.T) {
	owner := set("*:*")
	admin := set("principal:*", "location:create,update,delete", "system:create,update,delete")
	viewer := set("*:read")
	locWriter := set("location:create,update,delete")

	// An owner covers everyone (the wildcard grants all).
	for _, s := range [][2]any{{"admin", admin}, {"viewer", viewer}, {"locWriter", locWriter}, {"owner", owner}} {
		if !owner.Covers(s[1].(rbac.Set)) {
			t.Errorf("owner should cover %s", s[0])
		}
	}
	// A non-owner must NOT cover an owner: specific/partial perms never cover *:*.
	if admin.Covers(owner) {
		t.Error("admin must not cover owner (*:*): no impersonation escalation to owner")
	}
	if viewer.Covers(owner) {
		t.Error("viewer must not cover owner")
	}
	// A viewer covers a reader, never a writer.
	if !viewer.Covers(set("location:read")) {
		t.Error("*:read should cover location:read")
	}
	if viewer.Covers(set("location:create")) {
		t.Error("*:read must not cover location:create")
	}
	// A location writer covers read via the floor, but not the resource wildcard,
	// and not another resource.
	if !locWriter.Covers(set("location:read")) {
		t.Error("location writer should cover location:read via the read floor")
	}
	if locWriter.Covers(set("location:*")) {
		t.Error("specific actions must not cover the resource wildcard location:*")
	}
	if locWriter.Covers(set("system:read")) {
		t.Error("location writer must not cover system:read")
	}
	// A set always covers itself.
	if !admin.Covers(admin) {
		t.Error("a set must cover itself")
	}
	// A resource-scoped reader cannot cover an all-resource reader.
	if set("location:read").Covers(viewer) {
		t.Error("location:read must not cover *:read")
	}
}
