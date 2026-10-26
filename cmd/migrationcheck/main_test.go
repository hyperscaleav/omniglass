package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// git runs a git command in dir for a fixture repository.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=fixture", "-c", "user.email=fixture@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func touch(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("-- migrate:up\n\n-- migrate:down\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixtureRepo is a repository whose main holds two migrations, checked out on
// a branch that adds nothing yet: the shape a PR's checkout has against its
// base. Each case adds its migration to the working tree, as `make migration`
// would, without committing it.
func fixtureRepo(t *testing.T) (root, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root = t.TempDir()
	dir = filepath.Join(root, "db", "migrations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q", "-b", "main")
	touch(t, dir, "20260101090000_init.sql")
	touch(t, dir, "20260102090000_widget.sql")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "base")
	git(t, root, "checkout", "-q", "-b", "feature")
	return root, dir
}

// clock is a day after every fixture stamp.
var clock = time.Date(2026, 1, 4, 9, 0, 0, 0, time.UTC)

func TestRun(t *testing.T) {
	for _, tc := range []struct {
		what   string
		add    string
		base   string
		code   int
		stderr []string
	}{
		{"a valid tree passes", "20260103090000_gadget.sql", "main", 0, nil},
		{"nothing added passes", "", "main", 0, nil},
		{
			"a migration older than the base's newest fails",
			"20260101120000_gadget.sql", "main", 1,
			[]string{"20260101120000_gadget.sql", "20260102090000_widget.sql", "make migration NAME="},
		},
		{
			"a migration taking a version the base holds fails twice: a duplicate, and not newer",
			"20260102090000_gadget.sql", "main", 1,
			[]string{"is taken by both", "not newer than"},
		},
		{"an unknown base is a usage error, not a pass", "20260103090000_gadget.sql", "no-such-ref", 2, []string{"no-such-ref"}},
	} {
		t.Run(tc.what, func(t *testing.T) {
			root, dir := fixtureRepo(t)
			if tc.add != "" {
				touch(t, dir, tc.add)
			}
			var stdout, stderr bytes.Buffer
			code := run(options{Repo: root, Base: tc.base, Dir: "db/migrations", Now: clock}, &stdout, &stderr)
			if code != tc.code {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", code, tc.code, &stdout, &stderr)
			}
			for _, want := range tc.stderr {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr does not name %q:\n%s", want, &stderr)
				}
			}
			if tc.code == 0 && !strings.Contains(stdout.String(), "20260102090000") {
				t.Errorf("a pass does not name the base's newest version:\n%s", &stdout)
			}
		})
	}
}

// TestRunJudgesTheBaseTipNotTheForkPoint is the case the check exists for: the
// branch was cut, then another migration landed on main. Judged against the
// fork point the branch's migration is newer; against main's tip it is not.
func TestRunJudgesTheBaseTipNotTheForkPoint(t *testing.T) {
	root, dir := fixtureRepo(t)
	touch(t, dir, "20260103090000_gadget.sql")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "the branch's migration")

	git(t, root, "checkout", "-q", "main")
	touch(t, dir, "20260103120000_landed_first.sql")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "another PR merges first")
	git(t, root, "checkout", "-q", "feature")

	var stdout, stderr bytes.Buffer
	if code := run(options{Repo: root, Base: "main", Dir: "db/migrations", Now: clock}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit %d, want 1\nstdout: %s\nstderr: %s", code, &stdout, &stderr)
	}
	for _, want := range []string{"20260103090000_gadget.sql", "20260103120000_landed_first.sql"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr does not name %s:\n%s", want, &stderr)
		}
	}
}
