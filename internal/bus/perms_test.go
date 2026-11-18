package bus

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// TestOutpostPermissionsScoped proves an outpost's grant covers only its own subjects
// and never another outpost's, the core isolation contract expressed as permissions.
func TestOutpostPermissionsScoped(t *testing.T) {
	perms := outpostPermissions("outpost-a")

	wantPub := []string{
		collection.WorklistSubject("outpost-a"),
		collection.HeartbeatSubject("outpost-a"),
		collection.TelemetrySubject("outpost-a"),
		collection.CommandSubject("outpost-a"),
		collection.CommandStatusSubject("outpost-a"),
		collection.SimlistSubject("outpost-a"),
		collection.SimHitSubject("outpost-a"),
	}
	if got := perms.Publish.Allow; !equal(got, wantPub) {
		t.Errorf("publish allow = %v, want %v", got, wantPub)
	}
	wantSub := []string{
		collection.WorklistChangedSubject("outpost-a"),
		collection.InboxPrefix("outpost-a") + ".>",
		collection.SimopSubject("outpost-a"),
		collection.RunSubject("outpost-a"),
		collection.SimobsSubject("outpost-a"),
		collection.RequestTallySubject("outpost-a"),
	}
	if got := perms.Subscribe.Allow; !equal(got, wantSub) {
		t.Errorf("subscribe allow = %v, want %v", got, wantSub)
	}
	// The simulator answers the server's operation requests: one reply per
	// request it received, and nothing else outside its grant.
	if perms.Response == nil || perms.Response.MaxMsgs != 1 {
		t.Errorf("response = %+v, want one reply per request", perms.Response)
	}
	// A run on demand (#1163) may take as long as its polls: the one reply
	// stays permitted for the run's whole budget, not only a simulator's.
	if perms.Response.Expires < runTimeout {
		t.Errorf("response expires %v, want at least the run budget %v", perms.Response.Expires, runTimeout)
	}

	// No grant may reference another outpost's name.
	for _, s := range append(perms.Publish.Allow, perms.Subscribe.Allow...) {
		if strings.Contains(s, "outpost-b") {
			t.Errorf("outpost-a grant leaks outpost-b: %q", s)
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
