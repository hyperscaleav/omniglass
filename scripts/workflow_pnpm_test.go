package scripts

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Every workflow job that runs pnpm sets it up first (#2709 left the release
// build without it). A job runs pnpm when a step calls pnpm, calls a Makefile
// target whose recipe (or a prerequisite's) calls pnpm, or runs goreleaser,
// whose before hook runs make web. setup-node brings no pnpm, so such a job
// needs pnpm/action-setup in an earlier step.

var (
	makeRule   = regexp.MustCompile(`^([A-Za-z0-9_.-]+(?:\s+[A-Za-z0-9_.-]+)*)\s*:([^=].*)?$`)
	makeCall   = regexp.MustCompile(`\bmake\s+(?:-[A-Za-z]+\s+)*([A-Za-z0-9_.-]+(?:\s+[A-Za-z0-9_.-]+)*)`)
	pnpmCall   = regexp.MustCompile(`(^|[\s;&|(])pnpm\s`)
	goreleaser = "goreleaser/goreleaser-action"
	pnpmSetup  = "pnpm/action-setup"
)

// pnpmTargets returns the Makefile targets that run pnpm, directly or through
// a prerequisite.
func pnpmTargets(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	direct := map[string]bool{}
	prereqs := map[string][]string{}
	var current []string
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(l, "\t") {
			if pnpmCall.MatchString(l) {
				for _, c := range current {
					direct[c] = true
				}
			}
			continue
		}
		current = nil
		if m := makeRule.FindStringSubmatch(l); m != nil && !strings.HasPrefix(l, ".") && !strings.Contains(l, ":=") {
			current = strings.Fields(m[1])
			deps := strings.Fields(strings.SplitN(m[2], "#", 2)[0])
			for _, c := range current {
				prereqs[c] = append(prereqs[c], deps...)
			}
		}
	}
	uses := map[string]bool{}
	var visit func(string, map[string]bool) bool
	visit = func(target string, seen map[string]bool) bool {
		if direct[target] {
			return true
		}
		if seen[target] {
			return false
		}
		seen[target] = true
		for _, d := range prereqs[target] {
			if visit(d, seen) {
				return true
			}
		}
		return false
	}
	for target := range prereqs {
		if visit(target, map[string]bool{}) {
			uses[target] = true
		}
	}
	for target := range direct {
		uses[target] = true
	}
	return uses
}

type workflowStep struct {
	Uses string `yaml:"uses"`
	Run  string `yaml:"run"`
}

type workflowJob struct {
	Steps []workflowStep `yaml:"steps"`
}

// stepRunsPnpm says why a step needs pnpm, or "" when it does not.
func stepRunsPnpm(s workflowStep, targets map[string]bool) string {
	if strings.HasPrefix(s.Uses, goreleaser) {
		return "runs goreleaser, whose before hook runs make web"
	}
	for _, l := range strings.Split(s.Run, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue // a shell comment runs nothing
		}
		if pnpmCall.MatchString(" " + l) {
			return "calls pnpm"
		}
		for _, m := range makeCall.FindAllStringSubmatch(l, -1) {
			for _, target := range strings.Fields(m[1]) {
				if targets[target] {
					return "runs make " + target + ", which calls pnpm"
				}
			}
		}
	}
	return ""
}

func TestWorkflowsSetUpPnpm(t *testing.T) {
	targets := pnpmTargets(t)
	if !targets["web"] {
		t.Fatal("make web no longer reads as a pnpm target; the Makefile scan is broken")
	}
	files, err := filepath.Glob("../.github/workflows/*.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var wf struct {
			Jobs map[string]workflowJob `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(raw, &wf); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		names := make([]string, 0, len(wf.Jobs))
		for n := range wf.Jobs {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			setUp := false
			for i, s := range wf.Jobs[n].Steps {
				if strings.HasPrefix(s.Uses, pnpmSetup) {
					setUp = true
					continue
				}
				if why := stepRunsPnpm(s, targets); why != "" && !setUp {
					t.Errorf("%s job %s step %d %s but no earlier step uses %s", strings.TrimPrefix(f, "../"), n, i+1, why, pnpmSetup)
					break
				}
			}
		}
	}
}

func TestStepRunsPnpm(t *testing.T) {
	targets := map[string]bool{"web": true}
	for _, tc := range []struct {
		name string
		step workflowStep
		want bool
	}{
		{"goreleaser", workflowStep{Uses: goreleaser + "@abc"}, true},
		{"pnpm install", workflowStep{Run: "cd web && pnpm install --frozen-lockfile"}, true},
		{"make web", workflowStep{Run: "make web"}, true},
		{"make with a flag", workflowStep{Run: "make -s web"}, true},
		{"another target", workflowStep{Run: "make build-go"}, false},
		{"setup-node", workflowStep{Uses: "actions/setup-node@abc"}, false},
		{"a word ending in pnpm", workflowStep{Run: "echo notpnpm here"}, false},
		{"a shell comment", workflowStep{Run: "  # make web runs later\nls"}, false},
	} {
		if got := stepRunsPnpm(tc.step, targets) != ""; got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
