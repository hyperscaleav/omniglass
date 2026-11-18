package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGuardMainBranchHook exercises the PreToolUse hook that blocks write
// operations when the checked-out branch is main.
//
// The hook's contract:
//   - git commit, merge, rebase, and push on main are blocked (exit 2).
//   - The same operations on a feature branch are allowed (exit 0).
//   - git push --force / -f targeting main is blocked from any branch.
//   - Read-only git commands (status, log, diff, show) pass on main.
//
// The test spins up a fresh temporary git repo for each case so the hook's
// `git rev-parse --abbrev-ref HEAD` reads a real branch, not a mock.
func TestGuardMainBranchHook(t *testing.T) {
	hookScript := filepath.Join("..", "..", ".claude", "hooks", "guard-main-branch.sh")
	if _, err := os.Stat(hookScript); os.IsNotExist(err) {
		t.Fatalf("hook script not found at %s; create it first", hookScript)
	}

	cases := []struct {
		name    string
		branch  string // "main" or any other name
		command string
		blocked bool
	}{
		{
			name:    "commit on main is blocked",
			branch:  "main",
			command: "git commit -m 'test'",
			blocked: true,
		},
		{
			name:    "merge on main is blocked",
			branch:  "main",
			command: "git merge feature/x",
			blocked: true,
		},
		{
			name:    "rebase on main is blocked",
			branch:  "main",
			command: "git rebase origin/main",
			blocked: true,
		},
		{
			name:    "push on main is blocked",
			branch:  "main",
			command: "git push -u origin main",
			blocked: true,
		},
		{
			name:    "commit on feature branch passes",
			branch:  "feat/my-feature",
			command: "git commit -m 'wip'",
			blocked: false,
		},
		{
			name:    "push on feature branch passes",
			branch:  "feat/my-feature",
			command: "git push -u origin feat/my-feature",
			blocked: false,
		},
		{
			name:    "force push to main from a feature branch is blocked",
			branch:  "feat/my-feature",
			command: "git push --force origin main",
			blocked: true,
		},
		{
			name:    "force push shorthand to main from a feature branch is blocked",
			branch:  "feat/my-feature",
			command: "git push -f origin main",
			blocked: true,
		},
		{
			name:    "force-with-lease to main is blocked",
			branch:  "feat/my-feature",
			command: "git push --force-with-lease origin main",
			blocked: true,
		},
		{
			name:    "git status on main passes",
			branch:  "main",
			command: "git status",
			blocked: false,
		},
		{
			name:    "git log on main passes",
			branch:  "main",
			command: "git log --oneline",
			blocked: false,
		},
		{
			name:    "git diff on main passes",
			branch:  "main",
			command: "git diff HEAD",
			blocked: false,
		},
		{
			name:    "git show on main passes",
			branch:  "main",
			command: "git show HEAD",
			blocked: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repoDir := makeTestRepo(t, c.branch)
			stdin := fmt.Sprintf(`{"tool_name":"Bash","tool_input":{"command":%q}}`, c.command)
			blocked := runHook(t, hookScript, repoDir, stdin)
			if blocked != c.blocked {
				if c.blocked {
					t.Errorf("command %q on branch %q: expected blocked, got allowed", c.command, c.branch)
				} else {
					t.Errorf("command %q on branch %q: expected allowed, got blocked", c.command, c.branch)
				}
			}
		})
	}
}

// makeTestRepo creates an ephemeral git repository checked out on branch and
// returns its path. The repo has one commit so HEAD is valid.
func makeTestRepo(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run("git", "init", "--initial-branch=main")
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "test")
	run("git", "commit", "--allow-empty", "-m", "init")
	if branch != "main" {
		run("git", "checkout", "-b", branch)
	}
	return dir
}

// runHook executes hookScript with stdin as its standard input, using repoDir
// as the working directory. It returns true when the hook exits with a
// blocking code (non-zero), false when it exits 0.
func runHook(t *testing.T, hookScript, repoDir, stdin string) bool {
	t.Helper()
	absScript, err := filepath.Abs(hookScript)
	if err != nil {
		t.Fatalf("abs hook path: %v", err)
	}
	cmd := exec.Command("bash", absScript)
	cmd.Dir = repoDir
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = os.Environ()
	err = cmd.Run()
	return err != nil
}
