package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/lab"
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

// Every walkthrough's setup, as the lab guide lists it for a learner, is a
// command this CLI has, with every flag it names: the guide never teaches a
// command that does not run.
func TestWalkthroughSetupCommandsAreRealCommands(t *testing.T) {
	b, err := lab.FactsJSON(os.DirFS("../../web/e2e/walkthroughs"))
	if err != nil {
		t.Fatal(err)
	}
	var facts lab.Facts
	if err := json.Unmarshal(b, &facts); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, w := range facts.Walkthroughs {
		for _, line := range w.Setup {
			n++
			words := strings.Fields(line)
			cmd, rest, err := newRoot("test").Find(words[1:])
			if err != nil || cmd.Name() != "create" {
				t.Errorf("%s: %q is not a command: %v", w.ID, line, err)
				continue
			}
			for _, word := range rest {
				if name, ok := strings.CutPrefix(word, "--"); ok && cmd.Flags().Lookup(name) == nil {
					t.Errorf("%s: %q names --%s, which %s lacks", w.ID, line, name, cmd.CommandPath())
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("no walkthrough has a setup to check (set-up-monitoring stages bay-2-display)")
	}
}

// The lab's node runs a command this binary has, with every flag it names
// (#1273 added --probe-every to both): a learner's lab and a script's.
func TestLabNodeCommandIsARealCommand(t *testing.T) {
	for _, ephemeral := range []bool{false, true} {
		s, err := lab.Plan(lab.Options{Image: "omniglass:dev", Project: "check", Port: lab.DefaultPort,
			NodeToken: "tok", NodeTokenHash: "abc", Ephemeral: ephemeral})
		if err != nil {
			t.Fatal(err)
		}
		cmd, rest, err := newRoot("test").Find(s.Node.Cmd)
		if err != nil || cmd.CommandPath() != "omniglass node run" {
			t.Fatalf("lab node %v is not node run: %v", s.Node.Cmd, err)
		}
		for _, word := range rest {
			if name, ok := strings.CutPrefix(word, "--"); ok && cmd.Flags().Lookup(name) == nil {
				t.Errorf("the lab's node passes --%s, which node run lacks", name)
			}
		}
	}
}
