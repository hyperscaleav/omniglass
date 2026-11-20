package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// make preflight is the pre-push gate the agents run (#2231): it is only worth
// running if it does what the PR's required checks do. Each step names the
// workflow whose check it mirrors and the command both must run; if either
// side changes alone, this fails, the way test-go-shards-check holds make
// test and test.yml together.

var preflightMirrors = []struct {
	step     string // in the Makefile's preflight recipe
	workflow string // under .github/workflows
	command  string // that workflow must still run
}{
	{"$(MAKE) gen", "gen-drift.yml", "make gen"},
	{"$(MAKE) test", "test.yml", "make test-go-storage"},
	{"$(MAKE) test", "test.yml", "make test-go-devseed"},
	{"$(MAKE) test", "test.yml", "make test-go-rest"},
	{"$(MAKE) test", "test.yml", "make test-web"},
	{"$(MAKE) test", "test.yml", "make test-netadmin"},
	{"pnpm install --frozen-lockfile && pnpm build", "docs-build.yml", "pnpm build"},
	{"$(MAKE) migration-check", "migrations.yml", "go run ./cmd/migrationcheck"},
}

func preflightRecipe(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		if l != "preflight:" {
			continue
		}
		var recipe []string
		for _, r := range lines[i+1:] {
			if !strings.HasPrefix(r, "\t") {
				break
			}
			recipe = append(recipe, r)
		}
		return strings.Join(recipe, "\n")
	}
	t.Fatal("the Makefile has no preflight target")
	return ""
}

func TestPreflightRunsWhatTheRequiredChecksRun(t *testing.T) {
	recipe := preflightRecipe(t)
	for _, m := range preflightMirrors {
		if !strings.Contains(recipe, m.step) {
			t.Errorf("make preflight no longer runs %q, which mirrors %s", m.step, m.workflow)
		}
		wf, err := os.ReadFile(filepath.Join("..", ".github", "workflows", m.workflow))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(wf), m.command) {
			t.Errorf("%s no longer runs %q; make preflight's %q mirrors it, so update both", m.workflow, m.command, m.step)
		}
	}
}

// make test is the roll-up preflight runs for test.yml's shards; it must still
// hold every shard, or preflight passes what CI fails.
func TestPreflightsMakeTestHoldsEveryShard(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	mk := string(b)
	for _, shard := range []string{"$(MAKE) test-web", "$(MAKE) test-go", "$(MAKE) test-netadmin",
		"$(MAKE) test-go-storage", "$(MAKE) test-go-devseed", "$(MAKE) test-go-rest"} {
		if !strings.Contains(mk, shard) {
			t.Errorf("make test (or test-go) no longer runs %q", shard)
		}
	}
}

func TestPreflightEndsWithTheAgentConfigGuard(t *testing.T) {
	if !strings.Contains(preflightRecipe(t), "scripts/agent-sync.sh guard") {
		t.Fatal("make preflight no longer runs the agent-config guard (scripts/agent-sync.sh guard)")
	}
}

// The agents' runner installs make preflight's tools with agent-toolchain.sh;
// its pins must be the ones the required checks install, or preflight can pass
// where CI fails.
func TestAgentToolchainPinsMatchTheChecks(t *testing.T) {
	read := func(p string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("..", p))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	script := read("scripts/agent-toolchain.sh")
	pins := []struct{ script, workflow, line string }{
		{`PROTOC_VERSION="34.1"`, ".github/workflows/gen-drift.yml", `PROTOC_VERSION: "34.1"`},
		{`PROTOC_GEN_GO_VERSION="v1.36.11"`, ".github/workflows/gen-drift.yml", `PROTOC_GEN_GO_VERSION: "v1.36.11"`},
		{`PNPM_VERSION="10"`, ".github/workflows/docs-build.yml", `version: 10`},
	}
	for _, p := range pins {
		if !strings.Contains(script, p.script) {
			t.Errorf("agent-toolchain.sh no longer pins %s; update this test with it", p.script)
		}
		if !strings.Contains(read(p.workflow), p.line) {
			t.Errorf("%s no longer has %q: move agent-toolchain.sh's pin with it", p.workflow, p.line)
		}
	}
}
