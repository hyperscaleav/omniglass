package cli

import (
	"strings"
	"testing"
)

// The script-only flags of lab run (#1197) exist for the demo pipeline's
// recorders and the probe, and stay out of a learner's help.
func TestLabRunScriptFlagsAreHidden(t *testing.T) {
	cmd := newLabRunCmd("test")
	for _, name := range []string{"project", "ephemeral", "ready-file", "while-pid"} {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("lab run has no --%s", name)
			continue
		}
		if !f.Hidden {
			t.Errorf("--%s shows in a learner's help", name)
		}
	}
	if strings.Contains(cmd.UsageString(), "ready-file") {
		t.Error("the usage names --ready-file")
	}
}
