// Package releasecut works out which release branch to cut next from the
// conventional commits on main (#2711), so cutting a release takes no
// judgment. It picks the branch only: the tag a dispatch on that branch cuts
// still comes from the branch name and the tags (internal/releaseversion).
//
// The bump is read from the squash merges on main since the newest release
// line branched. A breaking change bumps the minor while the major is 0 and
// the major from 1.0 on, so no 0.x commit can ever number a release 1.0. A
// feat bumps the minor, a fix or perf the patch, and anything else alone
// releases nothing. A minor or major bump opens a new release/<major>.<minor>
// from main; a patch opens nothing, because a patch is a cherry-pick onto the
// line's existing branch. Nothing here touches git: the command reads the
// refs and the log and hands them in.
package releasecut

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/releasenotes"
)

// Bump is how far a release moves the version.
type Bump int

const (
	// None is no release: nothing since the line branched asks for one.
	None Bump = iota
	// Patch is a fix on the newest line, cherry-picked onto its branch.
	Patch
	// Minor is a new line, release/<major>.<minor+1>.
	Minor
	// Major is a new major line, release/<major+1>.0. Never before 1.0.
	Major
)

func (b Bump) String() string {
	switch b {
	case Patch:
		return "patch"
	case Minor:
		return "minor"
	case Major:
		return "major"
	}
	return "none"
}

// Line is a release line, the <major>.<minor> a release branch is named for.
type Line struct {
	Major, Minor int
}

// Branch is the line's release branch, release/<major>.<minor>.
func (l Line) Branch() string { return fmt.Sprintf("release/%d.%d", l.Major, l.Minor) }

// after orders lines: is l newer than o?
func (l Line) after(o Line) bool {
	if l.Major != o.Major {
		return l.Major > o.Major
	}
	return l.Minor > o.Minor
}

var (
	branchPattern = regexp.MustCompile(`^release/(\d+)\.(\d+)$`)
	tagPattern    = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(?:-rc\.[1-9]\d*)?$`)
)

func atoi(s string) int {
	n, _ := strconv.Atoi(s) // the patterns admit only digits
	return n
}

// NewestLine is the newest release line any release branch or release tag
// names, and false when there has never been a release.
func NewestLine(branches, tags []string) (Line, bool) {
	var newest Line
	found := false
	consider := func(l Line) {
		if !found || l.after(newest) {
			newest, found = l, true
		}
	}
	for _, b := range branches {
		if m := branchPattern.FindStringSubmatch(b); m != nil {
			consider(Line{atoi(m[1]), atoi(m[2])})
		}
	}
	for _, t := range tags {
		if m := tagPattern.FindStringSubmatch(t); m != nil {
			consider(Line{atoi(m[1]), atoi(m[2])})
		}
	}
	return newest, found
}

// Anchor is the ref whose merge base with main is where the line branched:
// its release branch when it has one, else its newest tag. Empty when there
// has never been a release, meaning all of main.
func Anchor(line Line, found bool, branches, tags []string) string {
	if !found {
		return ""
	}
	for _, b := range branches {
		if b == line.Branch() {
			return b
		}
	}
	newest, best := "", [2]int{-1, -1} // patch, rc (a final sorts above its candidates)
	for _, t := range tags {
		m := tagPattern.FindStringSubmatch(t)
		if m == nil || atoi(m[1]) != line.Major || atoi(m[2]) != line.Minor {
			continue
		}
		rank := [2]int{atoi(m[3]), 1 << 30}
		if _, rc, ok := strings.Cut(t, "-rc."); ok {
			rank[1] = atoi(rc)
		}
		if rank[0] > best[0] || (rank[0] == best[0] && rank[1] > best[1]) {
			newest, best = t, rank
		}
	}
	return newest
}

// BumpOf is the bump one change asks of a release on a line whose major is
// major. Before 1.0 a breaking change asks only a minor.
func BumpOf(ch releasenotes.Change, major int) Bump {
	switch {
	case ch.Breaking && major >= 1:
		return Major
	case ch.Breaking, ch.Type == "feat":
		return Minor
	case ch.Type == "fix", ch.Type == "perf":
		return Patch
	}
	return None
}

// Commit is a commit on main or on a release branch.
type Commit struct {
	SHA string
	releasenotes.Commit
	// PatchID is the commit's `git patch-id --stable`, read only when a patch
	// needs it to tell which fixes a branch already has.
	PatchID string
}

// Request is everything the plan depends on.
type Request struct {
	// Branches is every release branch on the remote, release/<major>.<minor>.
	Branches []string
	// Tags is every tag in the repository.
	Tags []string
	// Commits is main since the newest line branched, as git log lists it.
	Commits []Commit
}

// Plan is the release the commits ask for.
type Plan struct {
	Bump Bump
	// From is the newest line, and HasFrom false when there has never been
	// a release.
	From    Line
	HasFrom bool
	// To is the line the release lands on: a new line for a minor or major,
	// From for a patch.
	To Line
	// NewBranch is true when To's branch is to be created from main.
	NewBranch bool
	// Drivers is every commit that asks for Bump, in log order.
	Drivers []Commit
}

// Make works out the plan. A patch with no branch to cherry-pick onto is an
// error.
func Make(req Request) (Plan, error) {
	from, found := NewestLine(req.Branches, req.Tags)
	p := Plan{From: from, HasFrom: found}
	for _, c := range req.Commits {
		ch, ok := releasenotes.Parse(c.Commit)
		if !ok {
			continue
		}
		switch b := BumpOf(ch, from.Major); {
		case b > p.Bump:
			p.Bump, p.Drivers = b, []Commit{c}
		case b == p.Bump && b != None:
			p.Drivers = append(p.Drivers, c)
		}
	}
	// With no release to patch, the first release is a minor: 0.1.0.
	if !found && p.Bump == Patch {
		p.Bump = Minor
	}

	switch p.Bump {
	case None:
		return p, nil
	case Patch:
		p.To = from
		if !hasBranch(req.Branches, from.Branch()) {
			return p, fmt.Errorf("the fixes on main belong on %s, which does not exist: create it from its newest tag before cherry-picking", from.Branch())
		}
		return p, nil
	case Minor:
		p.To = Line{from.Major, from.Minor + 1}
	case Major:
		p.To = Line{from.Major + 1, 0}
	}
	// To is past the newest line any branch or tag names, so its branch is new.
	p.NewBranch = true
	return p, nil
}

func hasBranch(branches []string, name string) bool {
	for _, b := range branches {
		if b == name {
			return true
		}
	}
	return false
}

// Missing is the fixes on main that a release branch does not carry yet, in
// log order. A fix counts as carried when a branch commit has its patch id,
// says it was cherry-picked from it (`git cherry-pick -x`), names its PR, or
// has its description.
func Missing(fixes, onBranch []Commit) []Commit {
	var out []Commit
	for _, f := range fixes {
		if !carried(f, onBranch) {
			out = append(out, f)
		}
	}
	return out
}

func carried(f Commit, onBranch []Commit) bool {
	fc, _ := releasenotes.Parse(f.Commit)
	var prRe *regexp.Regexp
	if fc.PR > 0 {
		prRe = regexp.MustCompile(`#` + strconv.Itoa(fc.PR) + `\b`)
	}
	for _, b := range onBranch {
		if f.PatchID != "" && b.PatchID == f.PatchID {
			return true
		}
		text := b.Subject + "\n" + b.Body
		if f.SHA != "" && strings.Contains(text, "cherry picked from commit "+f.SHA) {
			return true
		}
		if prRe != nil && prRe.MatchString(text) {
			return true
		}
		if bc, ok := releasenotes.Parse(b.Commit); ok && fc.Description != "" && bc.Description == fc.Description {
			return true
		}
	}
	return false
}
