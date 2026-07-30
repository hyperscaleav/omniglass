package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// invocation matches a documented command: a code span or code line that STARTS
// with `omniglass `. Requiring the start is what keeps `kubectl -n omniglass
// logs deploy/omniglass` and prose like "omniglass has none" out of the sample,
// without an allow-list to maintain.
var invocation = regexp.MustCompile("(?m)(?:^|`)omniglass ((?:[a-z][a-z0-9-]*)(?:[ \t]+[a-z][a-zA-Z0-9-]*)*)")

// commandIndex maps every resolvable command path to its cobra command, so a
// documented invocation can be checked against the shape of what it resolved
// to, not just whether some prefix of it resolves.
func commandIndex(root *cobra.Command) map[string]*cobra.Command {
	idx := map[string]*cobra.Command{}
	var walk func(c *cobra.Command, path string)
	walk = func(c *cobra.Command, path string) {
		idx[strings.TrimSpace(path)] = c
		for _, s := range c.Commands() {
			walk(s, path+" "+s.Name())
		}
	}
	walk(root, "")
	return idx
}

// checkInvocation validates one documented `omniglass ...` word sequence.
//
// The longest resolving prefix is the command; the remaining words are read as
// positional arguments. Two failure modes:
//
//   - no prefix resolves at all: the command does not exist;
//   - the resolved command is a non-runnable GROUP and words are left over: a
//     group takes no arguments, so the leftover words are a wrong or doubled
//     segment. This is the hole that let `omniglass component component
//     effective-tag list` ship in three guides (#436): `component` resolved,
//     so the old longest-prefix check passed the whole line.
//
// Leftover words after a runnable leaf are NOT checked: the docs write real
// argument values (`omniglass location get hq`), and `<placeholders>` and
// `--flags` are never captured by the invocation regex in the first place.
func checkInvocation(idx map[string]*cobra.Command, words []string) error {
	for k := len(words); k > 0; k-- {
		cmd, ok := idx[strings.Join(words[:k], " ")]
		if !ok {
			continue
		}
		leftover := words[k:]
		if len(leftover) > 0 && cmd.Run == nil && cmd.RunE == nil && cmd.HasSubCommands() {
			return fmt.Errorf("%q resolves to the %q command group, which takes no arguments; %q is a wrong or doubled segment",
				strings.Join(words, " "), strings.Join(words[:k], " "), leftover[0])
		}
		return nil
	}
	return fmt.Errorf("%q does not resolve to any command", strings.Join(words, " "))
}

// TestCheckInvocationRejectsDoubledSegments pins the guard against the exact
// failure class that shipped (#436): a junk or doubled segment after a
// resolvable group name. Regression cases are the three invocations that sat
// in the guides.
func TestCheckInvocationRejectsDoubledSegments(t *testing.T) {
	idx := commandIndex(Root("test"))

	bad := [][]string{
		{"component", "component", "effective-tag", "list", "codec-1"},
		{"principal", "principal", "avatar", "list"},
		{"component", "component", "reachability", "list", "disp-1"},
		{"nonexistent-noun", "list"},
	}
	for _, words := range bad {
		if err := checkInvocation(idx, words); err == nil {
			t.Errorf("checkInvocation accepted %q; want a rejection", strings.Join(words, " "))
		}
	}

	good := [][]string{
		{"component", "effective-tag", "list", "codec-1"},
		{"principal", "avatar", "list"},
		{"location", "get", "hq"},
		{"component"}, // a bare group mention in prose is fine
		{"node", "enroll", "field-1"},
	}
	for _, words := range good {
		if err := checkInvocation(idx, words); err != nil {
			t.Errorf("checkInvocation rejected %q: %v", strings.Join(words, " "), err)
		}
	}
}

// TestDocsOnlyNameRealCommands walks the guides and fails when a documented
// `omniglass ...` invocation does not resolve against the real command tree.
//
// The generated CLI reference cannot drift (it is rendered from this tree), but
// the hand-written guides can, and did: the secrets guide taught `omniglass
// secret-type list`, which has never existed in any build, and the CLI guide
// taught `omniglass effective-secret list` and `effective-variable list`, for
// which there is no API route at all. Both read as working commands.
//
// Renaming a command is the moment this matters most, since the guides are the
// one surface a regeneration does not fix.
//
// Architecture pages are excluded deliberately: `status.mdx` and the decision log
// are historical records of what shipped when, and a command named there was true
// at the time. Rewriting them to match today would falsify the record.
func TestDocsOnlyNameRealCommands(t *testing.T) {
	idx := commandIndex(Root("test"))

	root := filepath.Join("..", "..", "docs", "src", "content", "docs", "guides")
	var bad []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext != ".md" && ext != ".mdx" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range invocation.FindAllStringSubmatch(string(b), -1) {
			if err := checkInvocation(idx, strings.Fields(m[1])); err != nil {
				bad = append(bad, filepath.Base(path)+": "+err.Error())
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk guides: %v", err)
	}
	if len(bad) > 0 {
		t.Errorf("%d documented command(s) do not check out:\n  %s", len(bad), strings.Join(bad, "\n  "))
	}
}
