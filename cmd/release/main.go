// Command release cuts the next release branch with no judgment in it
// (#2711): it reads the conventional commits on main since the newest release
// line branched, works out the bump (internal/releasecut), previews the
// release notes, and, once confirmed, creates release/<major>.<minor> at
// main's tip on the remote. A patch creates nothing: it lists the fixes on
// main that the line's branch does not carry yet, to cherry-pick.
//
// It never dispatches the release workflow and never touches RELEASES_ENABLED:
// the first candidate is still a dispatch on the new branch.
//
//	make release            # preview, then ask before creating the branch
//	make release DRY_RUN=1  # preview only
//	make release YES=1      # create without asking
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/releasecut"
	"github.com/hyperscaleav/omniglass/internal/releasenotes"
	"github.com/hyperscaleav/omniglass/internal/releaseversion"
)

// shownDrivers caps the commits listed as driving the bump; the notes below
// list them all.
const shownDrivers = 15

type options struct {
	dir    string // the repository; empty for the working directory
	remote string
	main   string
	repo   string // owner/name, for the notes' links
	dryRun bool
	yes    bool
}

func main() {
	var o options
	flag.StringVar(&o.remote, "remote", "origin", "the remote to read and to create the branch on")
	flag.StringVar(&o.main, "main", "main", "the branch releases are cut from")
	flag.StringVar(&o.repo, "repo", "hyperscaleav/omniglass", "owner/name, for the notes' links")
	flag.BoolVar(&o.dryRun, "dry-run", false, "preview only; create nothing")
	flag.BoolVar(&o.yes, "yes", false, "create the branch without asking")
	flag.Parse()
	if err := run(o, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func run(o options, in io.Reader, out io.Writer) error {
	g := gitRunner{dir: o.dir}
	if shallow, err := g.out("rev-parse", "--is-shallow-repository"); err != nil {
		return err
	} else if shallow == "true" {
		return errors.New("this clone is shallow; the bump needs main's history since the last release: git fetch --unshallow " + o.remote)
	}
	remoteRelease := "refs/remotes/" + o.remote + "/release/"
	// Prune only the release branches, so a deleted branch stops naming a
	// line. Tags are fetched apart and never pruned, so a local tag the remote
	// lacks survives.
	if _, err := g.out("fetch", "--quiet", "--no-tags", "--prune", o.remote,
		"+refs/heads/"+o.main+":refs/remotes/"+o.remote+"/"+o.main,
		"+refs/heads/release/*:"+remoteRelease+"*"); err != nil {
		return err
	}
	if _, err := g.out("fetch", "--quiet", "--no-prune", o.remote, "refs/tags/*:refs/tags/*"); err != nil {
		return err
	}
	mainRef := o.remote + "/" + o.main
	tip, err := g.out("rev-parse", "--verify", mainRef+"^{commit}")
	if err != nil {
		return err
	}
	branches, err := g.lines("for-each-ref", "--format=%(refname:strip=3)", remoteRelease)
	if err != nil {
		return err
	}
	tags, err := g.lines("tag", "--list", "v*")
	if err != nil {
		return err
	}

	line, found := releasecut.NewestLine(branches, tags)
	anchor := releasecut.Anchor(line, found, branches, tags)
	rng, since := tip, "the start of "+o.main
	if anchor != "" {
		ref := anchor
		if strings.HasPrefix(anchor, "release/") {
			ref = o.remote + "/" + anchor
		}
		base, err := g.out("merge-base", tip, ref)
		if err != nil {
			return fmt.Errorf("%s shares no history with %s: %w", anchor, mainRef, err)
		}
		rng, since = base+".."+tip, anchor+" ("+short(base)+")"
	}
	commits, err := g.log(rng)
	if err != nil {
		return err
	}
	plan, err := releasecut.Make(releasecut.Request{Branches: branches, Tags: tags, Commits: commits})
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "%d commits on %s since %s: %s.\n", len(commits), mainRef, since, tally(commits))
	switch plan.Bump {
	case releasecut.None:
		fmt.Fprintln(out, "Nothing to release: none of them is a feat, fix, perf or breaking change.")
		return nil
	case releasecut.Patch:
		return patch(o, g, out, plan, tags)
	}
	return newLine(o, g, in, out, plan, tags, tip)
}

// newLine previews and, once confirmed, creates a minor or major line.
func newLine(o options, g gitRunner, in io.Reader, out io.Writer, plan releasecut.Plan, tags []string, tip string) error {
	branch := plan.To.Branch()
	v, err := releaseversion.Next(releaseversion.Request{Branch: branch, Tags: tags})
	if err != nil {
		return err
	}
	final := fmt.Sprintf("v%d.%d.0", plan.To.Major, plan.To.Minor)

	fmt.Fprintf(out, "\nNext release: %s, a %s bump.\n", final, plan.Bump)
	if plan.Bump == releasecut.Minor && plan.From.Major == 0 {
		fmt.Fprintln(out, "Before 1.0 a breaking change bumps the minor, never the major.")
	}
	writeDrivers(out, plan.Drivers)

	rng := tip
	if v.Previous != "" {
		rng = v.Previous + ".." + tip
	}
	notesCommits, err := g.log(rng)
	if err != nil {
		return err
	}
	writeNotes(out, o.repo, final, v.Previous, notesCommits)

	fmt.Fprintf(out, "\nBranch: %s from %s at %s. The first dispatch on it cuts %s; the soaked candidate releases as %s.\n",
		branch, o.remote+"/"+o.main, short(tip), v.Tag, final)
	if o.dryRun {
		fmt.Fprintln(out, "Dry run: nothing created.")
		return nil
	}
	if !o.yes {
		fmt.Fprintf(out, "Create %s at %s on %s? [y/N] ", branch, short(tip), o.remote)
		answer, _ := bufio.NewReader(in).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Fprintln(out, "Nothing created.")
			return nil
		}
	}
	if _, err := g.out("push", "--quiet", o.remote, tip+":refs/heads/"+branch); err != nil {
		return err
	}
	fmt.Fprintf(out, "Created %s at %s. Dispatch the release workflow on it to cut %s.\n", branch, short(tip), v.Tag)
	return nil
}

// patch lists the fixes the line's branch does not carry yet. It creates
// nothing: each reaches the branch by a cherry-pick PR.
func patch(o options, g gitRunner, out io.Writer, plan releasecut.Plan, tags []string) error {
	branch := plan.To.Branch()
	branchRef := o.remote + "/" + branch
	tip, err := g.out("rev-parse", "--verify", branchRef+"^{commit}")
	if err != nil {
		return err
	}
	v, err := releaseversion.Next(releaseversion.Request{Branch: branch, Tags: tags})
	if err != nil {
		return err
	}
	base, err := g.out("merge-base", o.remote+"/"+o.main, tip)
	if err != nil {
		return err
	}
	onBranch, err := g.log(base + ".." + tip)
	if err != nil {
		return err
	}
	ids, err := g.patchIDs(base+".."+o.remote+"/"+o.main, base+".."+tip)
	if err != nil {
		return err
	}
	withIDs := func(cs []releasecut.Commit) []releasecut.Commit {
		for i := range cs {
			cs[i].PatchID = ids[cs[i].SHA]
		}
		return cs
	}
	missing := releasecut.Missing(withIDs(plan.Drivers), withIDs(onBranch))

	fmt.Fprintf(out, "\nNext release: a patch on %s, cut by its next dispatch as %s. No branch to create: a patch is a cherry-pick.\n", branch, v.Tag)
	if len(missing) == 0 {
		fmt.Fprintf(out, "Every fix on %s since %s branched is already on %s.\n", o.main, branch, branch)
	} else {
		fmt.Fprintf(out, "\nTo cherry-pick onto %s, oldest first (git cherry-pick -x):\n", branch)
		for i := len(missing) - 1; i >= 0; i-- {
			fmt.Fprintf(out, "  %s %s\n", short(missing[i].SHA), missing[i].Subject)
		}
	}
	rng := tip
	if v.Previous != "" {
		rng = v.Previous + ".." + tip
	}
	notesCommits, err := g.log(rng)
	if err != nil {
		return err
	}
	writeNotes(out, o.repo, v.Tag, v.Previous, append(missing, notesCommits...))
	return nil
}

func writeDrivers(out io.Writer, drivers []releasecut.Commit) {
	fmt.Fprintf(out, "Driven by %d commit(s):\n", len(drivers))
	for i, d := range drivers {
		if i == shownDrivers {
			fmt.Fprintf(out, "  ...and %d more, all in the notes below\n", len(drivers)-shownDrivers)
			break
		}
		fmt.Fprintf(out, "  %s %s\n", short(d.SHA), d.Subject)
	}
}

func writeNotes(out io.Writer, repo, tag, previous string, commits []releasecut.Commit) {
	in := make([]releasenotes.Commit, len(commits))
	for i, c := range commits {
		in[i] = c.Commit
	}
	notes := releasenotes.Render(releasenotes.Input{Repo: repo, Tag: tag, Previous: previous, Commits: in})
	since := "the start"
	if previous != "" {
		since = previous
	}
	rule := strings.Repeat("-", 72)
	fmt.Fprintf(out, "\nRelease notes preview (%s, since %s):\n%s\n%s%s\n", tag, since, rule, notes.Markdown, rule)
}

// tally counts the commits by what they ask of a release.
func tally(commits []releasecut.Commit) string {
	var breaking, feat, fix, perf, other int
	for _, c := range commits {
		ch, _ := releasenotes.Parse(c.Commit)
		switch {
		case ch.Breaking:
			breaking++
		case ch.Type == "feat":
			feat++
		case ch.Type == "fix":
			fix++
		case ch.Type == "perf":
			perf++
		default:
			other++
		}
	}
	return fmt.Sprintf("%d breaking, %d feat, %d fix, %d perf, %d other", breaking, feat, fix, perf, other)
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

type gitRunner struct{ dir string }

func (g gitRunner) raw(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return b, nil
}

func (g gitRunner) out(args ...string) (string, error) {
	b, err := g.raw(args...)
	return strings.TrimSpace(string(b)), err
}

func (g gitRunner) lines(args ...string) ([]string, error) {
	b, err := g.raw(args...)
	return strings.Fields(string(b)), err
}

// log reads a range along first parents, newest first, as the release
// workflow's notes read it. Unit and record separators keep the fields apart.
func (g gitRunner) log(rng string) ([]releasecut.Commit, error) {
	b, err := g.raw("log", "--first-parent", "--format=%H%x1f%s%x1f%b%x1e", rng)
	if err != nil {
		return nil, err
	}
	var cs []releasecut.Commit
	for _, rec := range strings.Split(string(b), "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.SplitN(rec, "\x1f", 3)
		for len(f) < 3 {
			f = append(f, "")
		}
		cs = append(cs, releasecut.Commit{SHA: f[0], Commit: releasenotes.Commit{Subject: f[1], Body: f[2]}})
	}
	return cs, nil
}

// patchIDs maps each commit in the ranges to its stable patch id.
func (g gitRunner) patchIDs(ranges ...string) (map[string]string, error) {
	ids := map[string]string{}
	for _, rng := range ranges {
		diff, err := g.raw("log", "--first-parent", "-p", "--format=commit %H", rng)
		if err != nil {
			return nil, err
		}
		cmd := exec.Command("git", "patch-id", "--stable")
		cmd.Dir = g.dir
		cmd.Stdin = strings.NewReader(string(diff))
		b, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("git patch-id: %w", err)
		}
		for _, l := range strings.Split(string(b), "\n") {
			if id, sha, ok := strings.Cut(l, " "); ok {
				ids[sha] = id
			}
		}
	}
	return ids, nil
}
