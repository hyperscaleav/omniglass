package cli

import (
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
	valid := map[string]bool{}
	var walk func(c *cobra.Command, path string)
	walk = func(c *cobra.Command, path string) {
		valid[strings.TrimSpace(path)] = true
		for _, s := range c.Commands() {
			walk(s, path+" "+s.Name())
		}
	}
	walk(Root("test"), "")

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
			words := strings.Fields(m[1])
			// The longest prefix that resolves is the command; the rest are
			// positional arguments, which this does not check.
			resolved := false
			for k := len(words); k > 0; k-- {
				if valid[strings.Join(words[:k], " ")] {
					resolved = true
					break
				}
			}
			if !resolved {
				bad = append(bad, filepath.Base(path)+": omniglass "+m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk guides: %v", err)
	}
	if len(bad) > 0 {
		t.Errorf("%d documented command(s) do not exist:\n  %s", len(bad), strings.Join(bad, "\n  "))
	}
}
