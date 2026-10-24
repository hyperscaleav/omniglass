package api

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
)

// nodeOnlyRejection refuses every node-only native key by the name the caller
// sent, and passes every other name through to the lane's own catalog check.
func TestNodeOnlyRejection(t *testing.T) {
	for _, key := range probekeys.Native() {
		rej, refused := nodeOnlyRejection(key)
		if refused != probekeys.IsNodeOnly(key) {
			t.Errorf("nodeOnlyRejection(%q) refused = %v, want %v", key, refused, !refused)
			continue
		}
		if refused && (rej.Name != key || !strings.Contains(rej.Reason, key)) {
			t.Errorf("nodeOnlyRejection(%q) = %+v, want the key named in name and reason", key, rej)
		}
	}
	if rej, refused := nodeOnlyRejection("Session-State"); !refused || rej.Name != "Session-State" || !strings.Contains(rej.Reason, "Session-State") {
		t.Errorf("a casing variant = %+v (refused %v), want it refused under the name sent", rej, refused)
	}
	for _, name := range []string{"uptime", "video-input", "call-started"} {
		if _, refused := nodeOnlyRejection(name); refused {
			t.Errorf("nodeOnlyRejection(%q) refused a name a push may write", name)
		}
	}
}
