package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// reservedRejection refuses every reserved key (each native probe key,
// collection-failed among them, and the platform's health) by the name the
// caller sent, and passes every other name through to the lane's own catalog
// check.
func TestReservedRejection(t *testing.T) {
	for _, key := range writerclass.ReservedNames() {
		rej, refused := reservedRejection(key)
		if !refused || rej.Name != key || !strings.Contains(rej.Reason, key) {
			t.Errorf("reservedRejection(%q) = %+v (refused %v), want the key refused and named in name and reason", key, rej, refused)
		}
	}
	if rej, refused := reservedRejection("Session-State"); !refused || rej.Name != "Session-State" || !strings.Contains(rej.Reason, "Session-State") {
		t.Errorf("a casing variant = %+v (refused %v), want it refused under the name sent", rej, refused)
	}
	for _, name := range []string{"uptime", "video-input", "call-started"} {
		if _, refused := reservedRejection(name); refused {
			t.Errorf("reservedRejection(%q) refused a name a push may write", name)
		}
	}
}

// classRejection refuses a registered key a push may not write (a calc rule's
// output, naming the rule) and passes a collected one.
func TestClassRejection(t *testing.T) {
	rej, refused := classRejection("avg-temp", writerclass.Calc, &storage.CalcWriter{Rule: "temp-avg"})
	if !refused || !strings.Contains(rej.Reason, "avg-temp") || !strings.Contains(rej.Reason, "temp-avg") {
		t.Errorf("a calc output = %+v (refused %v), want it refused naming the key and the rule", rej, refused)
	}
	if _, refused := classRejection("video-input", writerclass.Collected, nil); refused {
		t.Error("a collected key was refused")
	}
}

// TestEndpointListing: the push route's refusal of an unknown instance lists
// the owner's endpoints, bounded, so a component with many endpoints costs the
// refusal a fixed size.
func TestEndpointListing(t *testing.T) {
	many := make([]string, 0, 15)
	for i := 0; i < 15; i++ {
		many = append(many, fmt.Sprintf("ep-%02d", i))
	}
	for _, c := range []struct {
		names []string
		want  string
	}{
		{nil, "it has no endpoints"},
		{[]string{"tcp", "https", "webhook"}, "its endpoints: https, tcp, webhook"},
		{many, "its endpoints: ep-00, ep-01, ep-02, ep-03, ep-04, ep-05, ep-06, ep-07, ep-08, ep-09, and 5 more"},
	} {
		if got := endpointListing(c.names); got != c.want {
			t.Errorf("endpointListing(%d names) = %q, want %q", len(c.names), got, c.want)
		}
	}
}
