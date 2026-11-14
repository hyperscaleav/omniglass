package scripts

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These drive scripts/branch-owned.sh, the ownership test the autopilot runs
// before it resumes or adopts an existing branch (#1112). A live session's
// unpushed branch was once copied, gated and shipped as a second PR for the same
// issue; the script says whether another session still holds a branch. The
// tests build a real temporary git repo with real worktrees (no fake git), and
// age commits and files explicitly so the activity window is deterministic.

// ownedRepo is a throwaway repo whose main branch holds one old commit.
type ownedRepo struct {
	t    *testing.T
	root string
}

func (r *ownedRepo) git(dir string, env []string, args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func newOwnedRepo(t *testing.T) *ownedRepo {
	t.Helper()
	requireGit(t)
	r := &ownedRepo{t: t, root: filepath.Join(t.TempDir(), "repo")}
	if err := os.MkdirAll(r.root, 0o755); err != nil {
		t.Fatal(err)
	}
	r.git(r.root, nil, "init", "-q", "-b", "main")
	r.commit(r.root, "base", 90*24*time.Hour)
	return r
}

// commit makes an empty-tree-change commit in dir dated age ago.
func (r *ownedRepo) commit(dir, msg string, age time.Duration) {
	r.t.Helper()
	when := time.Now().Add(-age).Format(time.RFC3339)
	if err := os.WriteFile(filepath.Join(dir, strings.ReplaceAll(msg, "/", "_")+".txt"), []byte(msg), 0o644); err != nil {
		r.t.Fatal(err)
	}
	r.git(dir, nil, "add", "-A")
	r.git(dir, []string{"GIT_AUTHOR_DATE=" + when, "GIT_COMMITTER_DATE=" + when}, "commit", "-q", "-m", msg)
}

// branch cuts a branch off main with one commit dated age ago, no worktree.
func (r *ownedRepo) branch(name string, age time.Duration) {
	r.t.Helper()
	wt := filepath.Join(r.t.TempDir(), "cut")
	r.git(r.root, nil, "worktree", "add", "-q", "-b", name, wt, "main")
	r.commit(wt, name+"-work", age)
	r.git(r.root, nil, "worktree", "remove", "--force", wt)
}

// worktree checks name out in a new worktree, optionally locked, and ages every
// file in it to age (a fresh checkout otherwise reads as activity right now).
func (r *ownedRepo) worktree(name string, locked bool, age time.Duration) string {
	r.t.Helper()
	wt := filepath.Join(r.t.TempDir(), strings.ReplaceAll(name, "/", "_"))
	args := []string{"worktree", "add", "-q"}
	if locked {
		args = append(args, "--lock", "--reason", "live session")
	}
	r.git(r.root, nil, append(args, wt, name)...)
	old := time.Now().Add(-age)
	err := filepath.Walk(wt, func(p string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(p, old, old)
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return wt
}

func requireGit(t *testing.T) {
	t.Helper()
	requireTools(t)
	if _, err := exec.LookPath("git"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("git is not on PATH: scripts/branch-owned.sh and its tests need it")
		}
		t.Skipf("git is not on PATH: scripts/branch-owned.sh needs it")
	}
}

// owned runs the script in the repo; env adds to the environment.
func (r *ownedRepo) owned(branch string, env ...string) (string, string, int) {
	r.t.Helper()
	script, err := filepath.Abs("branch-owned.sh")
	if err != nil {
		r.t.Fatal(err)
	}
	args := []string{script}
	if branch != "" {
		args = append(args, branch)
	}
	cmd := exec.Command("bash", args...)
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "AUTOPILOT_OWNED_WINDOW_HOURS=")
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
	return strings.TrimSpace(out.String()), errb.String(), code
}

func (r *ownedRepo) expect(branch, want string, wantCode int, env ...string) {
	r.t.Helper()
	got, errb, code := r.owned(branch, env...)
	if got != want || code != wantCode {
		r.t.Fatalf("branch-owned %s: got %q exit %d (stderr %q), want %q exit %d", branch, got, code, errb, want, wantCode)
	}
}

const (
	stale = 72 * time.Hour
	fresh = 2 * time.Hour
)

func TestBranchOwnedLockedWorktree(t *testing.T) {
	r := newOwnedRepo(t)
	r.branch("feat/udp-transport", stale)
	r.worktree("feat/udp-transport", true, stale)
	// Old commits and old files: only the lock says a session is alive.
	r.expect("feat/udp-transport", "owned locked", 0)
}

func TestBranchOwnedRecentCommit(t *testing.T) {
	r := newOwnedRepo(t)
	r.branch("feat/recent", fresh) // no worktree at all
	r.expect("feat/recent", "owned active", 0)
}

func TestBranchOwnedFreeWhenOldAndUnchecked(t *testing.T) {
	r := newOwnedRepo(t)
	r.branch("feat/old", stale)
	r.expect("feat/old", "free", 1)
}

func TestBranchOwnedUnlockedWorktreeAges(t *testing.T) {
	r := newOwnedRepo(t)
	r.branch("feat/idle", stale)
	r.worktree("feat/idle", false, stale)
	r.expect("feat/idle", "free", 1)
}

func TestBranchOwnedRecentFileInUnlockedWorktree(t *testing.T) {
	r := newOwnedRepo(t)
	r.branch("feat/dirty", stale)
	wt := r.worktree("feat/dirty", false, stale)
	// An uncommitted edit an hour ago: the commits are old but someone is typing.
	if err := os.WriteFile(filepath.Join(wt, "wip.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.expect("feat/dirty", "owned active", 0)
}

func TestBranchOwnedIgnoresGitDirMtime(t *testing.T) {
	r := newOwnedRepo(t)
	r.branch("feat/gitdir", stale)
	wt := r.worktree("feat/gitdir", false, stale)
	// Bookkeeping inside a worktree's .git (a status refresh) is not activity.
	now := time.Now()
	if err := os.Chtimes(filepath.Join(wt, ".git"), now, now); err != nil {
		t.Fatal(err)
	}
	r.expect("feat/gitdir", "free", 1)
}

func TestBranchOwnedWindowIsHonoured(t *testing.T) {
	r := newOwnedRepo(t)
	r.branch("feat/thirty", 30*time.Hour)
	r.expect("feat/thirty", "free", 1)                                            // default 24h
	r.expect("feat/thirty", "owned active", 0, "AUTOPILOT_OWNED_WINDOW_HOURS=48") // widened
	r.branch("feat/two", fresh)
	r.expect("feat/two", "free", 1, "AUTOPILOT_OWNED_WINDOW_HOURS=1") // narrowed
}

func TestBranchOwnedClaimFile(t *testing.T) {
	r := newOwnedRepo(t)
	r.branch("feat/claimed", stale)
	wt := r.worktree("feat/claimed", false, stale)
	r.expect("feat/claimed", "free", 1) // a missing claim is no claim
	if err := os.WriteFile(wt+".claim", []byte("session 42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.expect("feat/claimed", "owned claimed", 0)
}

func TestBranchOwnedLockBeatsOtherReasons(t *testing.T) {
	r := newOwnedRepo(t)
	r.branch("feat/all", fresh)
	wt := r.worktree("feat/all", true, fresh)
	if err := os.WriteFile(wt+".claim", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r.expect("feat/all", "owned locked", 0)
}

func TestBranchOwnedUsage(t *testing.T) {
	r := newOwnedRepo(t)
	if _, errb, code := r.owned(""); code != 64 || !strings.Contains(errb, "usage") {
		t.Fatalf("no branch: exit %d, %q", code, errb)
	}
	if _, errb, code := r.owned("feat/missing"); code != 2 || !strings.Contains(errb, "feat/missing") {
		t.Fatalf("unknown branch: exit %d, %q", code, errb)
	}
	if _, errb, code := r.owned("main", "AUTOPILOT_OWNED_WINDOW_HOURS=soon"); code != 64 || !strings.Contains(errb, "AUTOPILOT_OWNED_WINDOW_HOURS") {
		t.Fatalf("bad window: exit %d, %q", code, errb)
	}
}
