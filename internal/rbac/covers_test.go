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
	// secret is sensitive: a bare `*` wildcard neither grants nor covers a secret
	// read, so a granter holding only `*:read` cannot confer secret access it does
	// not itself have. A literal secret grant and owner's `>` do cover it.
	if viewer.Covers(set("secret:read")) {
		t.Error("*:read must not cover secret:read (sensitive resource)")
	}
	if set("*:*").Covers(set("secret:read")) {
		t.Error("*:* must not cover secret:read (sensitive resource)")
	}
	if !set("secret:read").Covers(set("secret:read")) {
		t.Error("literal secret:read should cover secret:read")
	}
	if !set(">").Covers(set("secret:reveal")) {
		t.Error("owner > should cover secret:reveal")
	}
	// platform is sensitive too: a granter holding a bare write wildcard holds
	// estate reach, not install-wide authority, so it must not be able to confer
	// the tier permission (nor impersonate a principal that holds it).
	if set("*:update").Covers(set("platform:update")) {
		t.Error("*:update must not cover platform:update (install-wide authority is not estate reach)")
	}
	if set("*:*").Covers(set("platform:update")) {
		t.Error("*:* must not cover platform:update (install-wide authority is not estate reach)")
	}
	if !set("platform:*").Covers(set("platform:update")) {
		t.Error("platform:* should cover platform:update")
	}
	if !set(">").Covers(set("platform:update")) {
		t.Error("owner > should cover platform:update")
	}
}
