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

// invocationTail captures the whole documented invocation through its flags
// and argument values (up to the closing backtick or end of line), for the
// flag check: the word-only regex above stops at the first non-lowercase
// token, which is exactly why a wrong --flag was invisible (#449).
var invocationTail = regexp.MustCompile("(?m)(?:^|`)omniglass ([^`\n]+)")

// commandWord is a command-path token, camelCase verbs included (setTag,
// removeTag); the first token that is not one ends the command words and
// starts arguments and flags.
var commandWord = regexp.MustCompile(`^[a-z][a-zA-Z0-9-]*$`)

// checkFlags validates every --flag in a documented invocation against the
// resolved command's flag set, inherited persistent flags included. A tail
// whose command words do not resolve is checkInvocation's finding, not ours.
func checkFlags(idx map[string]*cobra.Command, tail string) error {
	tokens := strings.Fields(tail)
	var words []string
	for _, t := range tokens {
		if !commandWord.MatchString(t) {
			break
		}
		words = append(words, t)
	}
	var cmd *cobra.Command
	for k := len(words); k > 0; k-- {
		if c, ok := idx[strings.Join(words[:k], " ")]; ok {
			cmd = c
			break
		}
	}
	if cmd == nil {
		return nil
	}
	for i, t := range tokens {
		if !strings.HasPrefix(t, "--") {
			continue
		}
		name := strings.TrimPrefix(t, "--")
		hasValue := false
		if j := strings.IndexByte(name, '='); j >= 0 {
			name, hasValue = name[:j], true
		}
		if name == "" || !flagNameShape.MatchString(name) {
			continue // a literal placeholder like --<flag> is not a claim
		}
		f := cmd.Flags().Lookup(name)
		if f == nil {
			f = cmd.InheritedFlags().Lookup(name)
		}
		if f == nil {
			return fmt.Errorf("%q: command %q has no --%s flag", "omniglass "+tail, cmd.CommandPath(), name)
		}
		if hasValue || f.Value.Type() != "bool" || i+1 >= len(tokens) {
			continue
		}
		if next := strings.ToLower(tokens[i+1]); next == "true" || next == "false" {
			return fmt.Errorf("%q: --%s is a bool flag, so %q is read as a positional argument, not its value; write --%s=%s",
				"omniglass "+tail, name, tokens[i+1], name, next)
		}
	}
	return nil
}

var flagNameShape = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

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
// The walk spans the guides, architecture, and contributing trees (#449):
// commands are taught outside guides/ too. Three files are excluded as
// historical records (the decision log, the build log, and the status page's
// legend quoting them): a command named there was true at the time, and
// rewriting them to match today would falsify the record. The generated CLI
// reference is excluded because it cannot drift.
var docsCommandExcluded = map[string]bool{
	"decisions.md": true,
	"build-log.md": true,
	"status.mdx":   true,
}

func TestDocsOnlyNameRealCommands(t *testing.T) {
	idx := commandIndex(Root("test"))

	base := filepath.Join("..", "..", "docs", "src", "content", "docs")
	var bad []string
	for _, tree := range []string{"guides", "architecture", "contributing"} {
		err := filepath.Walk(filepath.Join(base, tree), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			if ext := filepath.Ext(path); ext != ".md" && ext != ".mdx" {
				return nil
			}
			if docsCommandExcluded[filepath.Base(path)] {
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
			for _, m := range invocationTail.FindAllStringSubmatch(string(b), -1) {
				if err := checkFlags(idx, m[1]); err != nil {
					bad = append(bad, filepath.Base(path)+": "+err.Error())
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", tree, err)
		}
	}
	if len(bad) > 0 {
		t.Errorf("%d documented command(s) do not check out:\n  %s", len(bad), strings.Join(bad, "\n  "))
	}
}

// TestCheckFlagsValidatesDocumentedFlags pins the flag check: a real flag and
// an inherited persistent flag pass, a phantom flag fires, =value forms parse,
// an unresolvable command is not this check's finding, and a bool flag handed a
// space-separated value fires.
//
// The last one is #711's guard. Body flags now carry the schema's type, so a
// boolean is a real bool flag and `--required true` no longer means what it
// reads as: pflag takes `true` as a positional argument and the command fails on
// its argument count. That is a documented line which cannot work, which is
// exactly what this check exists to catch, and it caught one in the CLI guide.
func TestCheckFlagsValidatesDocumentedFlags(t *testing.T) {
	idx := commandIndex(Root("test"))

	if err := checkFlags(idx, "component setTag codec-1 --key environment --value dev"); err != nil {
		t.Errorf("real flags on a camelCase verb rejected: %v", err)
	}
	if err := checkFlags(idx, "location list --turbo"); err == nil {
		t.Error("phantom flag accepted; want a rejection")
	}
	if err := checkFlags(idx, "location list --server https://x"); err != nil {
		t.Errorf("persistent flag rejected: %v", err)
	}
	if err := checkFlags(idx, "location create hq --display-name=\"HQ\""); err != nil {
		t.Errorf("=value form rejected: %v", err)
	}
	if err := checkFlags(idx, "nonexistent-noun list --whatever"); err != nil {
		t.Errorf("unresolvable command is checkInvocation's finding, not ours: %v", err)
	}
	if err := checkFlags(idx, "tag create --name rack-position --propagates false"); err == nil {
		t.Error("a bool flag given a space-separated value was accepted; want a rejection naming the =value form")
	}
	if err := checkFlags(idx, "tag create --name rack-position --propagates=false"); err != nil {
		t.Errorf("the =value form of a bool flag rejected: %v", err)
	}
	if err := checkFlags(idx, "command-type create --name set-input --settle-window-seconds 15"); err != nil {
		t.Errorf("an int flag with its value rejected: %v", err)
	}
}
