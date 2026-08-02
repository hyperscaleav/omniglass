package docslint

import (
	"strings"
	"testing"
)

// TestScanMakeIn pins the make-target extraction: spans and shell example
// lines are checked, prose uses of the word make are not.
func TestScanMakeIn(t *testing.T) {
	targets := map[string]bool{"test": true, "dev": true, "gen": true}
	cases := []struct {
		line string
		want int
	}{
		{"run `make test` before the PR", 0},
		{"run `make ship-it` before the PR", 1},
		{"this will make sense later", 0},
		{"make targets, env vars, and paths land later", 0},
		{"`make test-short` iterates", 1},
	}
	for _, c := range cases {
		if got := scanMakeIn(c.line, false, targets); len(got) != c.want {
			t.Errorf("scanMakeIn(%q) = %v, want %d", c.line, got, c.want)
		}
	}
}

// TestScanMakeInCode pins the shell-example branch: a command line in a code
// block is checked bare, prose wrapping is not possible there.
func TestScanMakeInCode(t *testing.T) {
	targets := map[string]bool{"dev": true}
	if got := scanMakeIn("make dev", true, targets); len(got) != 0 {
		t.Errorf("real target in example fired: %v", got)
	}
	if got := scanMakeIn("make sandwiches", true, targets); len(got) != 1 {
		t.Errorf("phantom target in example did not fire: %v", got)
	}
}

// TestScanEnvIn pins the forward env lint: an unread name fires wherever it
// appears; an illustrative mention is exempt.
func TestScanEnvIn(t *testing.T) {
	universe := map[string]bool{"OMNIGLASS_SERVER": true}
	if got := scanEnvIn("set `OMNIGLASS_SERVER` first", universe); len(got) != 0 {
		t.Errorf("read var fired: %v", got)
	}
	if got := scanEnvIn("set `OMNIGLASS_TURBO_MODE` first", universe); len(got) != 1 {
		t.Errorf("unread var did not fire: %v", got)
	}
	if got := scanEnvIn("a hypothetical `OMNIGLASS_TURBO_MODE` knob", universe); len(got) != 0 {
		t.Errorf("illustrative mention fired: %v", got)
	}
}

// TestScanPathsIn pins the path lint: real paths and line refs resolve, the
// package.Symbol form resolves by its package directory, globs are patterns,
// and a phantom path fires.
func TestScanPathsIn(t *testing.T) {
	cases := []struct {
		line string
		want int
	}{
		{"the suite is `internal/docslint/docslint.go`", 0},
		{"the constant at `internal/docslint/docslint_test.go:11`", 0},
		{"the walker in `internal/docslint.Regions`", 0},
		{"migrations under `db/migrations/*.sql`", 0},
		{"the ghost file `internal/phantom/nowhere.go`", 1},
		{"see `internal/docslint/`", 0},
	}
	for _, c := range cases {
		if got := scanPathsIn(c.line); len(got) != c.want {
			t.Errorf("scanPathsIn(%q) = %v, want %d", c.line, got, c.want)
		}
	}
}

// TestEnvUniverseSpansTheBinary guards the source-of-truth rule: the universe
// must include vars read outside internal/config (the KEK source, the CLI
// client, the node runner), or the forward lint fires on documented, real
// knobs.
func TestEnvUniverseSpansTheBinary(t *testing.T) {
	universe, err := loadEnvUniverse()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"OMNIGLASS_SECRET_KEY", "OMNIGLASS_SERVER", "OMNIGLASS_NODE_TOKEN"} {
		if !universe[name] {
			t.Errorf("env universe is missing %s; the scan must span the whole binary", name)
		}
	}
}

// The three corpus runs, enforced.

func TestMakeTargetReferences(t *testing.T) {
	findings, err := ScanMakeTargets()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "make-target", findings, true)
}

func TestEnvVarReferences(t *testing.T) {
	findings, err := ScanEnvVars()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "env-var", findings, true)
}

func TestEnvDocsCoverage(t *testing.T) {
	findings, err := ScanEnvDocsCoverage()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "env-coverage", findings, true)
}

func TestRepoPathReferences(t *testing.T) {
	findings, err := ScanRepoPaths()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "repo-path", findings, true)
}

// TestMakeTargetsLoad guards the empty-universe failure mode.
func TestMakeTargetsLoad(t *testing.T) {
	targets, err := loadMakeTargets()
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) < 10 || !targets["test"] || !targets["gen"] {
		t.Fatalf("Makefile targets did not load sanely: %d found", len(targets))
	}
	var names []string
	for n := range targets {
		names = append(names, n)
	}
	_ = strings.Join(names, ",")
}
