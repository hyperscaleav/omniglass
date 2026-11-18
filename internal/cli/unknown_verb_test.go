package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// An unknown verb under a CLI group must fail, not print the group's help and
// exit 0, so a pipeline step that guesses a verb cannot pass green while nothing
// happened (#1311). Repro:
//
//	bin/omniglass component alarm raise x; echo "exit=$?"
//	# before: prints the alarm help, exit=0
//	# after:  non-zero, 'unknown command "raise"' on stderr
func TestUnknownVerbUnderGroupIsAnError(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"component", "alarm", "raise", "x"}, `unknown command "raise" for "omniglass component alarm"`},
		{[]string{"system", "bogus"}, `unknown command "bogus" for "omniglass system"`},
		{[]string{"sim", "bogus"}, `unknown command "bogus" for "omniglass sim"`},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			root := Root("test")
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			root.SetArgs(tc.args)
			err := root.Execute()
			if err == nil {
				t.Fatalf("want an unknown-command error, got nil (exit 0)")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// A group invoked bare still prints its help and succeeds.
func TestBareGroupPrintsHelp(t *testing.T) {
	root := Root("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"component", "alarm"})
	if err := root.Execute(); err != nil {
		t.Fatalf("bare group returned %v, want nil", err)
	}
	if !strings.Contains(out.String(), "Available Commands") {
		t.Fatalf("bare group did not print help:\n%s", out.String())
	}
}

// Every pure group (children, no action of its own) rejects an unknown
// positional, so a new generated or hand-written group cannot regress silently.
func TestEveryGroupRejectsUnknownPositional(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.HasSubCommands() && c != c.Root() {
			if c.Run == nil && c.RunE == nil {
				t.Errorf("%q is a group with no run: cobra never validates its Args", c.CommandPath())
			} else if IsGroup(c) {
				if c.Args == nil || c.Args(c, []string{"bogus"}) == nil {
					t.Errorf("group %q accepts an unknown positional", c.CommandPath())
				}
			}
		}
		for _, k := range c.Commands() {
			walk(k)
		}
	}
	walk(Root("test"))
}
