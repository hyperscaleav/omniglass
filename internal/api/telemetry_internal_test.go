package api

import (
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
