package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixture is a throwaway remote and a clone of it, so the command is driven
// end to end against real git with nothing reaching a real remote.
type fixture struct {
	t      *testing.T
	remote string
	clone  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	f := &fixture{t: t, remote: filepath.Join(root, "remote.git"), clone: filepath.Join(root, "clone")}
	f.git(root, "init", "--quiet", "--bare", "--initial-branch=main", f.remote)
	f.git(root, "clone", "--quiet", f.remote, f.clone)
	f.commit("chore: the first commit")
	return f
}

func (f *fixture) git(dir string, args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit lands a squash-merge-shaped commit on the clone's current branch,
// touching its own file so cherry-picks apply cleanly.
func (f *fixture) commit(subject string) string {
	f.t.Helper()
	name := strings.NewReplacer(" ", "-", ":", "", "(", "", ")", "", "#", "", "!", "").Replace(subject)
	if err := os.WriteFile(filepath.Join(f.clone, name), []byte(subject), 0o644); err != nil {
		f.t.Fatal(err)
	}
	f.git(f.clone, "add", name)
	f.git(f.clone, "commit", "--quiet", "-m", subject)
	return f.git(f.clone, "rev-parse", "HEAD")
}

func (f *fixture) run(o options, stdin string) (string, error) {
	f.t.Helper()
	o.dir, o.remote, o.main, o.repo = f.clone, "origin", "main", "hyperscaleav/omniglass"
	var out strings.Builder
	err := run(o, strings.NewReader(stdin), &out)
	return out.String(), err
}

func (f *fixture) remoteBranches() string {
	return f.git(f.remote, "for-each-ref", "--format=%(refname:short)", "refs/heads/release/")
}

// Today's shape: v0.0.0 on main, then features and breaking changes. A dry run
// previews release/0.1 and v0.1.0 with the notes, and creates nothing.
func TestDryRunPreviewsTheMinorAndCreatesNothing(t *testing.T) {
	f := newFixture(t)
	f.git(f.clone, "tag", "v0.0.0")
	f.commit("feat!: the principal becomes the user (#8)")
	f.commit("feat: omnibot in the console (#9)")
	f.commit("fix: a reused pid reads as gone (#10)")
	f.git(f.clone, "push", "--quiet", "origin", "main", "--tags")

	out, err := f.run(options{dryRun: true}, "")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, s := range []string{
		"3 commits on origin/main since v0.0.0",
		"1 breaking, 1 feat, 1 fix, 0 perf, 0 other",
		"Next release: v0.1.0, a minor bump.",
		"Before 1.0 a breaking change bumps the minor, never the major.",
		"Driven by 2 commit(s):",
		"feat!: the principal becomes the user (#8)",
		"Release notes preview (v0.1.0, since v0.0.0):",
		"## Breaking changes\n\n- feat: the principal becomes the user ([#8](https://github.com/hyperscaleav/omniglass/pull/8))",
		"## Fixes\n\n- a reused pid reads as gone",
		"Branch: release/0.1 from origin/main",
		"The first dispatch on it cuts v0.1.0-rc.1; the soaked candidate releases as v0.1.0.",
		"Dry run: nothing created.",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
	if b := f.remoteBranches(); b != "" {
		t.Fatalf("a dry run created %q", b)
	}
}

// The fetch prunes deleted release branches but never a local tag.
func TestFetchPrunesReleaseBranchesNotTags(t *testing.T) {
	f := newFixture(t)
	f.git(f.clone, "tag", "v0.0.0")
	f.git(f.clone, "push", "--quiet", "origin", "main", "main:refs/heads/release/0.4", "--tags")
	f.git(f.clone, "fetch", "--quiet", "origin")
	f.git(f.remote, "branch", "-D", "release/0.4")
	f.git(f.clone, "tag", "local-only")
	f.commit("feat: a feature (#9)")
	f.git(f.clone, "push", "--quiet", "origin", "main")

	out, err := f.run(options{dryRun: true}, "")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Branch: release/0.1") {
		t.Errorf("the deleted release/0.4 still names a line:\n%s", out)
	}
	if got := f.git(f.clone, "tag", "--list", "local-only"); got != "local-only" {
		t.Fatalf("the local tag was pruned")
	}
}

func TestConfirmedRunCreatesTheBranchAtMainsTip(t *testing.T) {
	f := newFixture(t)
	f.git(f.clone, "tag", "v0.0.0")
	tip := f.commit("feat: a feature (#9)")
	f.git(f.clone, "push", "--quiet", "origin", "main", "--tags")

	out, err := f.run(options{}, "n\n")
	if err != nil || !strings.Contains(out, "Nothing created.") || f.remoteBranches() != "" {
		t.Fatalf("a declined run must create nothing: err %v, branches %q\n%s", err, f.remoteBranches(), out)
	}

	out, err = f.run(options{}, "y\n")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if b := f.remoteBranches(); b != "release/0.1" {
		t.Fatalf("remote release branches = %q, want release/0.1\n%s", b, out)
	}
	if got := f.git(f.remote, "rev-parse", "release/0.1"); got != tip {
		t.Fatalf("release/0.1 at %s, want main's tip %s", got, tip)
	}
	if !strings.Contains(out, "Created release/0.1") {
		t.Errorf("no confirmation line:\n%s", out)
	}

	// Main moves on with features: the next cut is release/0.2, past the open 0.1.
	f.commit("feat: another (#11)")
	f.git(f.clone, "push", "--quiet", "origin", "main")
	out, err = f.run(options{yes: true}, "")
	if err != nil || f.remoteBranches() != "release/0.1\nrelease/0.2" {
		t.Fatalf("err %v, branches %q\n%s", err, f.remoteBranches(), out)
	}
	if !strings.Contains(out, "1 commits on origin/main since release/0.1") {
		t.Errorf("the bump reads only main since release/0.1 branched:\n%s", out)
	}
}

// After 1.0 a breaking change opens a new major line.
func TestBreakingFromOneOpensTheMajor(t *testing.T) {
	f := newFixture(t)
	f.git(f.clone, "tag", "v1.2.0")
	f.commit("refactor!: the config moves (#5)")
	f.git(f.clone, "push", "--quiet", "origin", "main", "--tags")
	out, err := f.run(options{dryRun: true}, "")
	if err != nil || !strings.Contains(out, "Next release: v2.0.0, a major bump.") || !strings.Contains(out, "Branch: release/2.0") {
		t.Fatalf("err %v:\n%s", err, out)
	}
}

// Fixes only: no branch, and the fixes the line's branch lacks, to cherry-pick.
func TestPatchListsTheFixesToCherryPick(t *testing.T) {
	f := newFixture(t)
	f.git(f.clone, "push", "--quiet", "origin", "main")
	f.git(f.clone, "push", "--quiet", "origin", "main:refs/heads/release/0.1")
	picked := f.commit("fix: already picked (#20)")
	f.commit("docs: a page (#21)")
	f.commit("fix: still to pick (#22)")
	f.git(f.clone, "push", "--quiet", "origin", "main")

	f.git(f.clone, "checkout", "--quiet", "-b", "pick", "origin/release/0.1")
	f.git(f.clone, "cherry-pick", "-x", picked)
	f.git(f.clone, "push", "--quiet", "origin", "pick:release/0.1")
	f.git(f.clone, "checkout", "--quiet", "main")

	out, err := f.run(options{}, "")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, s := range []string{
		"Next release: a patch on release/0.1, cut by its next dispatch as v0.1.0-rc.1. No branch to create",
		"To cherry-pick onto release/0.1, oldest first",
		"fix: still to pick (#22)",
		"Release notes preview (v0.1.0-rc.1",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
	if strings.Contains(out, "  "+picked[:7]+" fix: already picked") {
		t.Errorf("an already-picked fix is listed to pick:\n%s", out)
	}
	if strings.Contains(out, "Create ") {
		t.Errorf("a patch asks to create nothing:\n%s", out)
	}
}

func TestNothingToRelease(t *testing.T) {
	f := newFixture(t)
	f.git(f.clone, "tag", "v0.1.0")
	f.commit("docs: a page (#1)")
	f.git(f.clone, "push", "--quiet", "origin", "main", "--tags")
	out, err := f.run(options{}, "")
	if err != nil || !strings.Contains(out, "Nothing to release") {
		t.Fatalf("err %v:\n%s", err, out)
	}
}

func TestShallowCloneIsRefused(t *testing.T) {
	f := newFixture(t)
	f.commit("feat: a (#1)")
	f.git(f.clone, "push", "--quiet", "origin", "main")
	shallow := filepath.Join(t.TempDir(), "shallow")
	f.git(f.clone, "clone", "--quiet", "--depth=1", "file://"+f.remote, shallow)
	var out strings.Builder
	err := run(options{dir: shallow, remote: "origin", main: "main", repo: "x/y", dryRun: true}, strings.NewReader(""), &out)
	if err == nil || !strings.Contains(err.Error(), "shallow") {
		t.Fatalf("err = %v", err)
	}
}
