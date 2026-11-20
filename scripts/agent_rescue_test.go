package scripts

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// These drive scripts/agent-rescue.sh, which agent-build and agent-fix run
// after the agent so work it never pushed is not lost. Run 44743523903 (#2223)
// ended its turn at turn 142 with one local commit and uncommitted edits in a
// worktree, and the runner threw both away. Each test builds a real "GitHub"
// (a bare repo), the agent's checkout as a clone of it, and runs the script
// against them with file:// URLs.

type rescueEnv struct {
	t      *testing.T
	remote string // the bare repo standing in for GitHub
	ws     string // the agent's checkout
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=agent", "GIT_AUTHOR_EMAIL=a@example.com",
		"GIT_COMMITTER_NAME=agent", "GIT_COMMITTER_EMAIL=a@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, dir, path, body string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, dir, msg string) {
	t.Helper()
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", msg)
}

// newRescueEnv: main holds CLAUDE.md v1, code.go and a .gitignore; the agent's
// checkout is a clone of it.
func newRescueEnv(t *testing.T) *rescueEnv {
	t.Helper()
	requireGit(t)
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, seed, "init", "-q", "-b", "main")
	writeFile(t, seed, "CLAUDE.md", "rules v1\n")
	writeFile(t, seed, "code.go", "package x\n")
	writeFile(t, seed, "gone.go", "package x // delete me\n")
	writeFile(t, seed, ".gitignore", "bin/\n.claude/worktrees/\n")
	commitAll(t, seed, "base")
	e := &rescueEnv{t: t, remote: filepath.Join(root, "remote.git"), ws: filepath.Join(root, "work")}
	gitIn(t, root, "clone", "-q", "--bare", seed, e.remote)
	gitIn(t, root, "clone", "-q", e.remote, e.ws)
	return e
}

// worktree adds a worktree under the checkout the way the agent in #2223 did:
// .claude/worktrees/<name> on a new branch off main.
func (e *rescueEnv) worktree(branch string) string {
	e.t.Helper()
	wt := filepath.Join(e.ws, ".claude", "worktrees", strings.ReplaceAll(branch, "/", "+"))
	gitIn(e.t, e.ws, "worktree", "add", "-q", wt, "-b", branch, "origin/main")
	return wt
}

func (e *rescueEnv) rescue(extra ...string) (map[string]string, string, int) {
	e.t.Helper()
	script, err := filepath.Abs("agent-rescue.sh")
	if err != nil {
		e.t.Fatal(err)
	}
	cmd := exec.Command("bash", script, "snapshot")
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"WORKSPACE="+e.ws, "BARE="+filepath.Join(e.t.TempDir(), "bare"), "ISSUE=2223",
		"REPO_URL=file://"+e.remote, "AUTHOR_NAME=bot", "AUTHOR_EMAIL=bot@example.com")
	cmd.Env = append(cmd.Env, extra...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err = cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		e.t.Fatal(err)
	}
	got := map[string]string{}
	for l := range strings.SplitSeq(out.String(), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			got[k] = v
		}
	}
	got["bare"] = ""
	for _, kv := range cmd.Env {
		if v, ok := strings.CutPrefix(kv, "BARE="); ok {
			got["bare"] = v
		}
	}
	return got, strings.TrimSpace(errb.String()), code
}

func (e *rescueEnv) inBare(got map[string]string, args ...string) string {
	e.t.Helper()
	return gitIn(e.t, got["bare"], args...)
}

// The #2223 shape: one commit and uncommitted edits in a worktree, nothing pushed.
func TestRescueSavesTheCommitAndTheUncommittedEditsOfAWorktree(t *testing.T) {
	e := newRescueEnv(t)
	const branch = "feat/issue-2223-env-secrets"
	wt := e.worktree(branch)
	writeFile(t, wt, "code.go", "package x // feature\n")
	commitAll(t, wt, "feat: the part the agent committed")
	writeFile(t, wt, "docs.md", "uncommitted docs\n")
	writeFile(t, wt, "code.go", "package x // feature, edited after\n")
	if err := os.Remove(filepath.Join(wt, "gone.go")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, wt, "bin/ignored", "build output\n")

	got, errs, code := e.rescue()
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if got["push"] != "true" || got["branch"] != branch || got["dirty"] != "true" || got["ahead"] != "2" {
		t.Fatalf("got %v", got)
	}
	if msg := e.inBare(got, "log", "-1", "--format=%s", "refs/heads/"+branch); msg != "wip: agent stopped before finishing, Refs #2223" {
		t.Fatalf("wip commit message %q", msg)
	}
	if author := e.inBare(got, "log", "-1", "--format=%an", "refs/heads/"+branch); author != "agent" && author != "bot" {
		t.Fatalf("wip commit author %q", author)
	}
	files := e.inBare(got, "ls-tree", "-r", "--name-only", "refs/heads/"+branch)
	for _, want := range []string{"docs.md", "code.go", "CLAUDE.md"} {
		if !strings.Contains(files, want) {
			t.Errorf("snapshot lacks %s:\n%s", want, files)
		}
	}
	for _, not := range []string{"gone.go", "bin/ignored"} {
		if strings.Contains(files, not) {
			t.Errorf("snapshot has %s, which the agent deleted or git ignores:\n%s", not, files)
		}
	}
	if body := e.inBare(got, "show", "refs/heads/"+branch+":code.go"); !strings.Contains(body, "edited after") {
		t.Errorf("the uncommitted edit to code.go is not in the snapshot: %q", body)
	}
	if parents := e.inBare(got, "log", "--format=%s", "refs/heads/"+branch); !strings.Contains(parents, "the part the agent committed") {
		t.Errorf("the agent's own commit is not under the wip commit:\n%s", parents)
	}
}

// Commits and nothing else: pushed as they are, with no wip commit.
func TestRescuePushesCommitsAsTheyAreWhenTheTreeIsClean(t *testing.T) {
	e := newRescueEnv(t)
	wt := e.worktree("fix/issue-2223-x")
	writeFile(t, wt, "code.go", "package x // fixed\n")
	commitAll(t, wt, "fix: x\n\nRefs #2223")
	got, errs, code := e.rescue()
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if got["push"] != "true" || got["dirty"] != "false" || got["ahead"] != "1" {
		t.Fatalf("got %v", got)
	}
	if want := gitIn(t, wt, "rev-parse", "HEAD"); got["sha"] != want {
		t.Fatalf("sha %s, want the agent's own commit %s", got["sha"], want)
	}
}

// claude-code-action puts main's CLAUDE.md in the work tree, unstaged. On a
// branch behind main it differs from the branch's, and committing it is #1993.
func TestRescueNeverCommitsMainsAgentConfigFromTheCheckout(t *testing.T) {
	e := newRescueEnv(t)
	wt := e.worktree("feat/issue-2223-cfg")
	writeFile(t, wt, "code.go", "package x // feature\n")
	commitAll(t, wt, "feat: x")
	writeFile(t, wt, "CLAUDE.md", "rules v2 from main's restore\n")
	writeFile(t, wt, ".claude/skills/a/SKILL.md", "main's skill, restored\n")
	writeFile(t, wt, "notes.md", "real uncommitted work\n")
	got, errs, code := e.rescue()
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if got["dirty"] != "true" {
		t.Fatalf("got %v", got)
	}
	if body := e.inBare(got, "show", "refs/heads/feat/issue-2223-cfg:CLAUDE.md"); body != "rules v1" {
		t.Errorf("CLAUDE.md in the snapshot is %q, want the branch's own", body)
	}
	files := e.inBare(got, "ls-tree", "-r", "--name-only", "refs/heads/feat/issue-2223-cfg")
	if strings.Contains(files, ".claude/") || !strings.Contains(files, "notes.md") {
		t.Errorf("snapshot files:\n%s", files)
	}
}

func TestRescueWithNothingBeyondMainPushesNothing(t *testing.T) {
	e := newRescueEnv(t)
	e.worktree("feat/issue-2223-empty")
	got, _, code := e.rescue()
	if code != 0 || got["push"] != "false" || !strings.Contains(got["reason"], "nothing beyond") {
		t.Fatalf("exit %d, got %v", code, got)
	}
}

func TestRescueLeavesABranchThatIsAlreadyPushed(t *testing.T) {
	e := newRescueEnv(t)
	wt := e.worktree("feat/issue-2223-done")
	writeFile(t, wt, "code.go", "package x // done\n")
	commitAll(t, wt, "feat: done")
	gitIn(t, wt, "push", "-q", e.remote, "feat/issue-2223-done")
	got, _, code := e.rescue()
	if code != 0 || got["push"] != "false" || !strings.Contains(got["reason"], "already pushed") {
		t.Fatalf("exit %d, got %v", code, got)
	}
	// Nothing to push, but the branch and its count still come out: the PR step needs them.
	if got["branch"] != "feat/issue-2223-done" || got["ahead"] != "1" || got["dirty"] != "false" {
		t.Fatalf("a branch found with nothing to push must still be named: %v", got)
	}
}

// The agent pushed some commits, then stopped with more local: a fast-forward.
func TestRescuePushesOnTopOfWhatTheAgentAlreadyPushed(t *testing.T) {
	e := newRescueEnv(t)
	wt := e.worktree("feat/issue-2223-part")
	writeFile(t, wt, "code.go", "package x // one\n")
	commitAll(t, wt, "feat: one")
	gitIn(t, wt, "push", "-q", e.remote, "feat/issue-2223-part")
	writeFile(t, wt, "two.go", "package x\n")
	got, errs, code := e.rescue()
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if got["push"] != "true" || got["dirty"] != "true" || got["remote_sha"] == "" || got["ahead"] != "2" {
		t.Fatalf("got %v", got)
	}
}

// agent-fix merges main into its checkout first. A run that then changed
// nothing has only that merge unpushed, which is not work to save.
func TestRescueIgnoresALocalMergeOfMainAndNothingElse(t *testing.T) {
	e := newRescueEnv(t)
	gitIn(t, e.ws, "checkout", "-q", "-b", "feat/issue-9-pr")
	writeFile(t, e.ws, "code.go", "package x // pr\n")
	commitAll(t, e.ws, "feat: pr")
	gitIn(t, e.ws, "push", "-q", "origin", "feat/issue-9-pr")
	mainClone := filepath.Join(t.TempDir(), "m")
	gitIn(t, filepath.Dir(mainClone), "clone", "-q", e.remote, mainClone)
	writeFile(t, mainClone, "other.go", "package x\n")
	commitAll(t, mainClone, "main moves")
	gitIn(t, mainClone, "push", "-q", "origin", "main")
	gitIn(t, e.ws, "fetch", "-q", "origin")
	gitIn(t, e.ws, "merge", "-q", "--no-edit", "--no-ff", "origin/main")
	got, errs, code := e.rescue("BRANCH=feat/issue-9-pr")
	if code != 0 || got["push"] != "false" || !strings.Contains(got["reason"], "only a merge") {
		t.Fatalf("exit %d, got %v (%s)", code, got, errs)
	}
}

// No force pushes: a branch that moved on GitHub is left alone.
func TestRescueNeverOverwritesABranchThatDiverged(t *testing.T) {
	e := newRescueEnv(t)
	wt := e.worktree("feat/issue-2223-fork")
	writeFile(t, wt, "code.go", "package x // mine\n")
	commitAll(t, wt, "feat: mine")
	other := filepath.Join(t.TempDir(), "other")
	gitIn(t, filepath.Dir(other), "clone", "-q", e.remote, other)
	gitIn(t, other, "checkout", "-q", "-b", "feat/issue-2223-fork", "origin/main")
	writeFile(t, other, "code.go", "package x // theirs\n")
	commitAll(t, other, "feat: theirs")
	gitIn(t, other, "push", "-q", "origin", "feat/issue-2223-fork")
	got, _, code := e.rescue()
	if code != 0 || got["push"] != "false" || !strings.Contains(got["reason"], "no force push") {
		t.Fatalf("exit %d, got %v", code, got)
	}
}

func TestRescueRefusesABranchThatChangesWorkflows(t *testing.T) {
	e := newRescueEnv(t)
	wt := e.worktree("ci/issue-2223-wf")
	writeFile(t, wt, ".github/workflows/x.yml", "name: x\n")
	commitAll(t, wt, "ci: x")
	_, errs, code := e.rescue()
	if code != 1 || !strings.Contains(errs, ".github/workflows/x.yml") {
		t.Fatalf("exit %d, stderr %q", code, errs)
	}
}

// agent-fix names the PR's branch, and the agent works in the checkout itself.
func TestRescueForAFixTakesTheBranchTheCheckoutHasOut(t *testing.T) {
	e := newRescueEnv(t)
	gitIn(t, e.ws, "checkout", "-q", "-b", "feat/issue-9-pr")
	writeFile(t, e.ws, "code.go", "package x // pr\n")
	commitAll(t, e.ws, "feat: pr")
	gitIn(t, e.ws, "push", "-q", "origin", "feat/issue-9-pr")
	writeFile(t, e.ws, "code.go", "package x // pr, then the fix\n")
	got, errs, code := e.rescue("BRANCH=feat/issue-9-pr")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if got["push"] != "true" || got["dirty"] != "true" || got["branch"] != "feat/issue-9-pr" {
		t.Fatalf("got %v", got)
	}
}

// Edits and no branch at all: they land on a branch of their own.
func TestRescueSavesEditsWhenTheAgentNeverMadeABranch(t *testing.T) {
	e := newRescueEnv(t)
	writeFile(t, e.ws, "new.go", "package x\n")
	got, errs, code := e.rescue()
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if got["push"] != "true" || got["branch"] != "chore/issue-2223-agent-stopped" || got["ahead"] != "1" {
		t.Fatalf("got %v", got)
	}
}

func TestRescueFindsNothingWhenThereIsNothing(t *testing.T) {
	e := newRescueEnv(t)
	got, _, code := e.rescue()
	if code != 0 || got["push"] != "false" {
		t.Fatalf("exit %d, got %v", code, got)
	}
}

func TestRescueBodyIsADraftThatClosesTheIssueAndQuotesTheLastMessage(t *testing.T) {
	requireGit(t)
	script, err := filepath.Abs("agent-rescue.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script, "body")
	cmd.Stdin = strings.NewReader(`{"issue":2223,"branch":"feat/issue-2223-x","ahead":3,"dirty":true,"last_message":"Still to do:\n1. screenshots\n2. preflight"}`)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	body := string(out)
	for _, want := range []string{"Closes #2223", "3 commit(s) on `feat/issue-2223-x`", "`wip:` commit", "> Still to do:", "> 2. preflight", "not ready for review", "demo: none"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "\u2014") {
		t.Errorf("body has an em dash")
	}
}

// A push to a branch that has no PR must not start agent-review (or burn CI):
// the rescue and the agent's own step pushes land on a branch before its PR
// exists. This holds the workflows to that.
func TestAPushToABranchWithNoPRStartsNoReviewAndNoCI(t *testing.T) {
	entries, err := filepath.Glob(filepath.Join("..", ".github", "workflows", "*.yml"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("no workflows found: %v", err)
	}
	for _, f := range entries {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var wf struct {
			On map[string]yaml.Node `yaml:"on"`
		}
		if err := yaml.Unmarshal(b, &wf); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		name := filepath.Base(f)
		if name == "agent-review.yml" {
			for ev := range wf.On {
				switch ev {
				case "pull_request", "merge_group", "workflow_dispatch":
				default:
					t.Errorf("agent-review.yml starts on %q: a branch push would start a review before there is a PR", ev)
				}
			}
		}
		for _, ev := range []string{"create", "workflow_run"} {
			if _, ok := wf.On[ev]; ok && strings.HasPrefix(name, "agent-") {
				t.Errorf("%s starts on %q", name, ev)
			}
		}
		if n, ok := wf.On["push"]; ok {
			var push struct {
				Branches []string `yaml:"branches"`
			}
			if err := n.Decode(&push); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			for _, br := range push.Branches {
				if br != "main" && !strings.HasPrefix(br, "release/") {
					t.Errorf("%s runs on pushes to %q, so an agent's branch push would start it", name, br)
				}
			}
			if len(push.Branches) == 0 {
				t.Errorf("%s runs on a push to any branch", name)
			}
		}
	}
}

// The agent pushes per step and opens its PR last. A run that stops after a
// push with a clean tree has nothing to push, so the PR step must not hang on
// the push step. These run the real step script from agent-build.yml with a
// fake gh that answers from FAKE_* variables and logs what it was asked.

type prStep struct {
	t   *testing.T
	run string
	dir string
}

func loadPRStep(t *testing.T) *prStep {
	t.Helper()
	requireTools(t)
	b, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "agent-build.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				If   string `yaml:"if"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(b, &wf); err != nil {
		t.Fatal(err)
	}
	for _, st := range wf.Jobs["build"].Steps {
		if st.Name != "Open the draft PR the agent never opened" {
			continue
		}
		if strings.Contains(st.If, "outputs.push") {
			t.Errorf("the PR step is gated on the push step (%q): a branch pushed earlier would get no PR", st.If)
		}
		if !strings.Contains(st.If, "outputs.branch != ''") {
			t.Errorf("the PR step must run whenever a branch was found: %q", st.If)
		}
		dir := t.TempDir()
		script, _ := filepath.Abs("agent-rescue.sh")
		if err := os.Symlink(script, filepath.Join(dir, "agent-rescue.sh")); err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir, "rescue-last-message.txt", "Still to do: the docs\n")
		writeFile(t, dir, "bin/gh", `#!/usr/bin/env bash
echo "gh $*" >> "$RUNNER_TEMP/gh.log"
case "$1 $2" in
  "pr list") printf '%s' "${FAKE_PR:-}" ;;
  "api repos/o/r/compare/main...feat/issue-7-x") [ -n "${FAKE_AHEAD:-}" ] || exit 1; printf '%s' "$FAKE_AHEAD" ;;
  "issue view") echo "feat: the ticket" ;;
  "pr create") echo "https://github.com/o/r/pull/99" ;;
esac
`)
		if err := os.Chmod(filepath.Join(dir, "bin", "gh"), 0o755); err != nil {
			t.Fatal(err)
		}
		return &prStep{t: t, run: st.Run, dir: dir}
	}
	t.Fatal("agent-build.yml has no step 'Open the draft PR the agent never opened'")
	return nil
}

func (p *prStep) exec(env ...string) (string, int) {
	p.t.Helper()
	cmd := exec.Command("bash", "-c", p.run)
	cmd.Env = append(os.Environ(), "PATH="+filepath.Join(p.dir, "bin")+":"+os.Getenv("PATH"),
		"RUNNER_TEMP="+p.dir, "REPO=o/r", "NUMBER=7", "BRANCH=feat/issue-7-x", "DIRTY=false", "GH_TOKEN=x")
	cmd.Env = append(cmd.Env, env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			p.t.Fatal(err)
		}
		return string(out), exit.ExitCode()
	}
	log, _ := os.ReadFile(filepath.Join(p.dir, "gh.log"))
	return string(log), 0
}

func TestPRStepOpensADraftForABranchPushedEarlierWithNoPR(t *testing.T) {
	log, code := loadPRStep(t).exec("FAKE_AHEAD=3")
	if code != 0 || !strings.Contains(log, "pr create --repo o/r --draft --base main --head feat/issue-7-x --title feat: the ticket") {
		t.Fatalf("exit %d, gh calls:\n%s", code, log)
	}
}

func TestPRStepOpensNothingWhenAPRIsAlreadyOpen(t *testing.T) {
	log, code := loadPRStep(t).exec("FAKE_PR=41", "FAKE_AHEAD=3")
	if code != 0 || strings.Contains(log, "pr create") {
		t.Fatalf("exit %d, gh calls:\n%s", code, log)
	}
}

func TestPRStepOpensNothingWhenTheBranchIsNotAheadOfMain(t *testing.T) {
	log, code := loadPRStep(t).exec("FAKE_AHEAD=0")
	if code != 0 || strings.Contains(log, "pr create") {
		t.Fatalf("exit %d, gh calls:\n%s", code, log)
	}
}

func TestPRStepOpensNothingWhenTheBranchIsNotOnGitHub(t *testing.T) {
	log, code := loadPRStep(t).exec()
	if code != 0 || strings.Contains(log, "pr create") {
		t.Fatalf("exit %d, gh calls:\n%s", code, log)
	}
}
