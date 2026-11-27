package sim

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// #2799: the arbiter is the only writer to what makes a simulated device
// suffer. Outside tests, nothing but the arbiter sets a device's behavior
// overrides, writes a pinned path or reconciles the network impairer; the
// override seam's own file defines them and the engine's stop hook stops them.
func TestTheArbiterIsTheOnlyWriterToTheOverrideSetAndTheImpairer(t *testing.T) {
	root := filepath.Join("..")
	rules := []struct {
		what    string
		pattern *regexp.Regexp
		allowed []string
		simOnly bool
	}{
		{"sets the override set", regexp.MustCompile(`\.SetBehaviorOverrides\(`), []string{"sim/arbiter.go"}, false},
		{"writes a pinned path", regexp.MustCompile(`\.WritePath\(`), []string{"sim/arbiter.go"}, false},
		{"stops the override set", regexp.MustCompile(`\.StopBehaviorOverrides\(`), []string{"sim/engine.go"}, false},
		{"reconciles the impairer", regexp.MustCompile(`\.Reconcile\(`), []string{"sim/arbiter.go"}, true},
		{"applies a plan to the network", regexp.MustCompile(`\.Apply\(\s*p\b|ap\.Apply\(`), []string{"sim/simoutpost/impair.go"}, true},
	}
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if de.IsDir() {
			switch de.Name() {
			case "node_modules", ".git", ".claude", "docs", "web", "worktrees":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, r := range rules {
			if !r.pattern.Match(b) || (r.simOnly && !strings.HasPrefix(rel, "sim/")) {
				continue
			}
			ok := false
			for _, a := range r.allowed {
				ok = ok || rel == a
			}
			if rel == "sim/override.go" {
				ok = true // the seam's own definitions
			}
			if !ok {
				t.Errorf("%s %s: only the arbiter may (allowed: %v)", rel, r.what, r.allowed)
			}
			seen[r.what] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rules {
		if !seen[r.what] {
			t.Errorf("no file %s: the guard's pattern is stale", r.what)
		}
	}
}
