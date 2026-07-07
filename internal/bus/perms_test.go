package bus

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// TestNodePermissionsScoped proves a node's grant covers only its own subjects
// and never another node's, the core isolation contract expressed as permissions.
func TestNodePermissionsScoped(t *testing.T) {
	perms := nodePermissions("node-a")

	wantPub := []string{
		collection.WorklistSubject("node-a"),
		collection.HeartbeatSubject("node-a"),
		collection.TelemetrySubject("node-a"),
	}
	if got := perms.Publish.Allow; !equal(got, wantPub) {
		t.Errorf("publish allow = %v, want %v", got, wantPub)
	}
	wantSub := []string{
		collection.WorklistChangedSubject("node-a"),
		collection.InboxPrefix("node-a") + ".>",
	}
	if got := perms.Subscribe.Allow; !equal(got, wantSub) {
		t.Errorf("subscribe allow = %v, want %v", got, wantSub)
	}

	// No grant may reference another node's name.
	for _, s := range append(perms.Publish.Allow, perms.Subscribe.Allow...) {
		if strings.Contains(s, "node-b") {
			t.Errorf("node-a grant leaks node-b: %q", s)
		}
	}
}

func TestFullPermissionsWildcard(t *testing.T) {
	full := fullPermissions()
	if len(full.Publish.Allow) != 1 || full.Publish.Allow[0] != ">" {
		t.Errorf("full publish = %v, want [>]", full.Publish.Allow)
	}
	if len(full.Subscribe.Allow) != 1 || full.Subscribe.Allow[0] != ">" {
		t.Errorf("full subscribe = %v, want [>]", full.Subscribe.Allow)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
