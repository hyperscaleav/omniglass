package scripts

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These drive scripts/agent-sync.sh, which agent-fix runs around the agent
// (#2232) and make preflight runs as its last step (#2231). Two PRs on
// Nov 19 show what it guards: #1993 and #1948, far behind main, committed
// main's CLAUDE.md and .claude files as their own change instead of merging
// main, because claude-code-action had put main's copies in the work tree.
// The tests build a real temporary repo with a real main and a real branch.

type syncRepo struct {
	t    *testing.T
	root string
}

func (r *syncRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *syncRepo) write(path, body string) {
	r.t.Helper()
	full := filepath.Join(r.root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *syncRepo) commit(msg string, files map[string]string) {
	r.t.Helper()
	for p, b := range files {
		r.write(p, b)
	}
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
}

func (r *syncRepo) read(path string) string {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.root, path))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(b)
}

// newSyncRepo: main holds CLAUDE.md, a skill and some code; the branch "work"
// forks there, and main then moves on (its agent config included), the way
// main moved 120 commits under #1993.
func newSyncRepo(t *testing.T) *syncRepo {
	t.Helper()
	requireGit(t)
	r := &syncRepo{t: t, root: filepath.Join(t.TempDir(), "repo")}
	if err := os.MkdirAll(r.root, 0o755); err != nil {
		t.Fatal(err)
	}
	r.git("init", "-q", "-b", "main")
	r.commit("base", map[string]string{
		"CLAUDE.md":                 "rules v1\n",
		".claude/skills/a/SKILL.md": "skill v1\n",
		"code.go":                   "package x\n",
		"other.go":                  "package x\n",
	})
	r.git("checkout", "-q", "-b", "work")
	r.git("checkout", "-q", "main")
	r.commit("main moves", map[string]string{
		"CLAUDE.md":                 "rules v2\n",
		".claude/skills/a/SKILL.md": "skill v2\n",
		"other.go":                  "package x // v2\n",
	})
	r.git("checkout", "-q", "work")
	r.commit("the ticket's change", map[string]string{"code.go": "package x // feature\n"})
	return r
}

func (r *syncRepo) sync(env []string, args ...string) (string, string, int) {
	r.t.Helper()
	script, err := filepath.Abs("agent-sync.sh")
	if err != nil {
		r.t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"AGENT_RUN=", "AGENT_CONFIG_OK=")
	cmd.Env = append(cmd.Env, env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err = cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		r.t.Fatal(err)
	}
	return strings.TrimSpace(out.String()), strings.TrimSpace(errb.String()), code
}

var agentRun = []string{"AGENT_RUN=1"}

// #1993's shape: the branch commits main's copies of the agent config instead
// of merging main. The guard names them in an agent run and fails.
func TestAgentSyncGuardFailsAnAgentBranchThatCommitsMainsConfig(t *testing.T) {
	r := newSyncRepo(t)
	r.commit("chore: carry forward main's skill and CLAUDE.md updates", map[string]string{
		"CLAUDE.md":                 "rules v2\n",
		".claude/skills/a/SKILL.md": "skill v2\n",
	})
	out, _, code := r.sync(agentRun, "guard", "main")
	if code != 1 {
		t.Fatalf("guard exit %d, want 1 (out %q)", code, out)
	}
	for _, p := range []string{"CLAUDE.md", ".claude/skills/a/SKILL.md"} {
		if !strings.Contains(out, p) {
			t.Fatalf("guard output %q does not name %s", out, p)
		}
	}
}

func TestAgentSyncGuardPassesABranchThatLeavesTheConfigAlone(t *testing.T) {
	r := newSyncRepo(t)
	if out, _, code := r.sync(agentRun, "guard", "main"); code != 0 || out != "" {
		t.Fatalf("guard = %q, exit %d; want nothing, 0", out, code)
	}
}

// Merging main brings main's config in through the merge, which is no change
// of the branch's own: the guard passes after a real merge.
func TestAgentSyncGuardPassesOnceMainIsMerged(t *testing.T) {
	r := newSyncRepo(t)
	if out, _, code := r.sync(nil, "merge", "main"); code != 0 || out != "merged" {
		t.Fatalf("merge = %q, exit %d; want merged, 0", out, code)
	}
	if out, _, code := r.sync(agentRun, "guard", "main"); code != 0 || out != "" {
		t.Fatalf("guard after merge = %q, exit %d; want nothing, 0", out, code)
	}
	if got := r.read("CLAUDE.md"); got != "rules v2\n" {
		t.Fatalf("CLAUDE.md after merge = %q, want main's", got)
	}
}

// A person editing CLAUDE.md, an area:ci ticket (AGENT_CONFIG_OK) and the
// daily lessons branch are not stopped.
func TestAgentSyncGuardIsNotAGateOutsideAnAgentRun(t *testing.T) {
	cases := map[string]struct {
		env    []string
		branch string
	}{
		"a person":       {nil, ""},
		"an area:ci run": {[]string{"AGENT_RUN=1", "AGENT_CONFIG_OK=1"}, ""},
		"lessons branch": {agentRun, "lessons/2026-11-20"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newSyncRepo(t)
			if c.branch != "" {
				r.git("checkout", "-q", "-b", c.branch)
			}
			r.commit("edit the rules", map[string]string{"CLAUDE.md": "rules, edited\n"})
			out, _, code := r.sync(c.env, "guard", "main")
			if code != 0 || !strings.Contains(out, "CLAUDE.md") {
				t.Fatalf("guard = %q, exit %d; want CLAUDE.md named, exit 0", out, code)
			}
		})
	}
}

// drop takes the config back out of the PR's diff and keeps the ticket's work.
func TestAgentSyncDropRestoresTheMergeBaseConfigAndKeepsTheWork(t *testing.T) {
	r := newSyncRepo(t)
	r.commit("carry forward", map[string]string{
		"CLAUDE.md":                 "rules v2\n",
		".claude/skills/b/SKILL.md": "a skill main never had here\n",
	})
	out, _, code := r.sync(nil, "drop", "main")
	if code != 0 || !strings.Contains(out, "CLAUDE.md") || !strings.Contains(out, ".claude/skills/b/SKILL.md") {
		t.Fatalf("drop = %q, exit %d", out, code)
	}
	if out, _, code := r.sync(agentRun, "guard", "main"); code != 0 || out != "" {
		t.Fatalf("guard after drop = %q, exit %d; want clean", out, code)
	}
	if got := r.read("CLAUDE.md"); got != "rules v1\n" {
		t.Fatalf("CLAUDE.md = %q, want the merge base's", got)
	}
	if _, err := os.Stat(filepath.Join(r.root, ".claude/skills/b/SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("a config file the merge base lacks is still there: %v", err)
	}
	if got := r.read("code.go"); got != "package x // feature\n" {
		t.Fatalf("the ticket's change was lost: code.go = %q", got)
	}
	if msg := r.git("log", "-1", "--format=%s"); msg != "chore: keep main's agent config out of this PR" {
		t.Fatalf("drop's commit subject = %q", msg)
	}
}

func TestAgentSyncDropDoesNothingOnACleanBranch(t *testing.T) {
	r := newSyncRepo(t)
	head := r.git("rev-parse", "HEAD")
	if out, _, code := r.sync(nil, "drop", "main"); code != 0 || out != "" {
		t.Fatalf("drop = %q, exit %d; want nothing, 0", out, code)
	}
	if got := r.git("rev-parse", "HEAD"); got != head {
		t.Fatal("drop committed on a branch with nothing to drop")
	}
}

func TestAgentSyncMergeSaysCurrentWhenMainIsIn(t *testing.T) {
	r := newSyncRepo(t)
	r.sync(nil, "merge", "main")
	if out, _, code := r.sync(nil, "merge", "main"); code != 0 || out != "current" {
		t.Fatalf("second merge = %q, exit %d; want current, 0", out, code)
	}
}

// A conflict is the agent's first job: the markers stay and the paths are
// listed, exit 3.
func TestAgentSyncMergeListsConflictsAndLeavesTheMarkers(t *testing.T) {
	r := newSyncRepo(t)
	r.commit("touch the same line main did", map[string]string{"other.go": "package x // branch\n"})
	out, _, code := r.sync(nil, "merge", "main")
	if code != 3 {
		t.Fatalf("merge exit %d, want 3 (out %q)", code, out)
	}
	lines := strings.Split(out, "\n")
	if lines[0] != "conflict" || len(lines) != 2 || lines[1] != "other.go" {
		t.Fatalf("merge output %q, want conflict then other.go", out)
	}
	if !strings.Contains(r.read("other.go"), "<<<<<<<") {
		t.Fatal("the conflict markers were not left for the agent")
	}
}

// What blocked #1993's merge: main's copies of the config sitting unstaged in
// the work tree. merge refuses a dirty tree rather than half-merging, which is
// why agent-fix runs it before claude-code-action restores anything.
func TestAgentSyncMergeRefusesADirtyTree(t *testing.T) {
	r := newSyncRepo(t)
	r.write("CLAUDE.md", "rules v2\n")
	if _, errOut, code := r.sync(nil, "merge", "main"); code != 2 || !strings.Contains(errOut, "uncommitted") {
		t.Fatalf("merge on a dirty tree: exit %d, stderr %q; want 2 naming the uncommitted changes", code, errOut)
	}
}

func TestAgentSyncRefusesAnUnknownBaseAndBadUsage(t *testing.T) {
	r := newSyncRepo(t)
	if _, _, code := r.sync(nil, "guard", "no-such-ref"); code != 2 {
		t.Fatalf("unknown base: exit %d, want 2", code)
	}
	if _, _, code := r.sync(nil, "rebase"); code != 64 {
		t.Fatalf("unknown mode: exit %d, want 64", code)
	}
}

// agent-fix no longer merges main on every run (#2597): a merge is a new head
// commit, and a new head restarts every check. `needed` says whether this run
// should: only for a conflict, or when the branch is behind main and a required
// check is red (the failing check names arrive on stdin).
func (r *syncRepo) needed(failing string) (string, int) {
	r.t.Helper()
	script, err := filepath.Abs("agent-sync.sh")
	if err != nil {
		r.t.Fatal(err)
	}
	cmd := exec.Command("bash", script, "needed", "main")
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	cmd.Stdin = strings.NewReader(failing)
	out, err := cmd.Output()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		r.t.Fatal(err)
	}
	return strings.TrimSpace(string(out)), code
}

func TestAgentSyncNeededSkipsAMergeNothingAsksFor(t *testing.T) {
	r := newSyncRepo(t)
	if out, code := r.needed(""); code != 0 || out != "no" {
		t.Fatalf("behind main, no conflict, no red check: %q (exit %d), want no, 0", out, code)
	}
}

func TestAgentSyncNeededMergesForAConflict(t *testing.T) {
	r := newSyncRepo(t)
	r.commit("touch the same line main did", map[string]string{"other.go": "package x // branch\n"})
	if out, code := r.needed(""); code != 0 || out != "conflict" {
		t.Fatalf("conflicting branch: %q (exit %d), want conflict, 0", out, code)
	}
}

func TestAgentSyncNeededMergesWhenBehindAndARequiredCheckIsRed(t *testing.T) {
	r := newSyncRepo(t)
	if out, code := r.needed("e2e\n"); code != 0 || out != "check" {
		t.Fatalf("behind main with e2e red: %q (exit %d), want check, 0", out, code)
	}
}

func TestAgentSyncNeededSaysCurrentOnceMainIsIn(t *testing.T) {
	r := newSyncRepo(t)
	r.sync(nil, "merge", "main")
	// A red check on a branch that already has main is not main's doing.
	if out, code := r.needed("e2e\n"); code != 0 || out != "current" {
		t.Fatalf("branch has main: %q (exit %d), want current, 0", out, code)
	}
}
