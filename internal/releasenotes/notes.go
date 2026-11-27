// Package releasenotes renders a release's notes from the commits it ships,
// grouped by conventional-commit type (#2142). The repo squash-merges, so each
// commit subject on main is a PR title, `<type>(<scope>)!: <description> (#<n>)`.
// release-please writes the published notes (ADR-2652); this renders the same
// grouping for a local preview and lists the PRs a release ships. Nothing here
// touches git or the network: the command reads the log and hands the commits in.
package releasenotes

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Commit is one commit in the release's range.
type Commit struct {
	Subject string
	Body    string
}

// Input is everything the notes need.
type Input struct {
	Repo     string // owner/name, for links
	Tag      string // the tag being released
	Previous string // the previous final release; empty for none
	Commits  []Commit
}

// Notes is the rendered body and the PRs it names.
type Notes struct {
	Markdown string
	PRs      []int // each PR once, in the order first seen
}

var (
	subjectRe  = regexp.MustCompile(`^([a-zA-Z]+)(\([^)]*\))?(!)?:\s*(.+)$`)
	prSuffixRe = regexp.MustCompile(`\s*\(#(\d+)\)\s*$`)
	breakingRe = regexp.MustCompile(`(?m)^BREAKING[ -]CHANGE:`)
)

// Change is one commit read as a conventional commit.
type Change struct {
	// Type is the lower-cased conventional-commit type, empty when the
	// subject is not a conventional commit.
	Type string
	// Scope is the parenthesized scope as written, "(lab)", or empty.
	Scope string
	// Breaking is a `!` after the type or a `BREAKING CHANGE:` footer.
	Breaking bool
	// Description is the subject after the type, or the whole subject when
	// it is not a conventional commit, without the PR suffix.
	Description string
	// PR is the squash merge's PR number, 0 when the subject names none.
	PR int
}

// Parse reads a commit as a conventional commit, the shape a squash merge on
// main has: `<type>(<scope>)!: <description> (#<n>)`. It reports false for an
// empty subject. The notes and the release cut (#2711) both read commits here,
// so the two always agree on what is breaking.
func Parse(c Commit) (Change, bool) {
	subject := strings.TrimSpace(c.Subject)
	if subject == "" {
		return Change{}, false
	}
	ch := Change{}
	if m := prSuffixRe.FindStringSubmatch(subject); m != nil {
		ch.PR, _ = strconv.Atoi(m[1])
		subject = strings.TrimSpace(subject[:len(subject)-len(m[0])])
	}
	ch.Description = subject
	if m := subjectRe.FindStringSubmatch(subject); m != nil {
		ch.Type = strings.ToLower(m[1])
		ch.Scope = m[2]
		ch.Breaking = m[3] == "!" || breakingRe.MatchString(c.Body)
		ch.Description = strings.TrimSpace(m[4])
	}
	return ch, true
}

type group struct {
	title string
	lines []string
}

// Render groups the commits into sections: breaking changes, features, fixes,
// performance, then everything else. An empty section is left out.
func Render(in Input) Notes {
	const (
		breaking = iota
		features
		fixes
		perf
		other
	)
	groups := []group{
		{title: "Breaking changes"},
		{title: "Features"},
		{title: "Fixes"},
		{title: "Performance"},
		{title: "Other changes"},
	}
	var prs []int
	seen := map[int]bool{}

	for _, c := range in.Commits {
		ch, ok := Parse(c)
		if !ok {
			continue
		}
		pr := ch.PR
		if pr > 0 && !seen[pr] {
			seen[pr] = true
			prs = append(prs, pr)
		}

		section, desc := other, ch.Description
		if ch.Type != "" {
			switch {
			case ch.Breaking:
				section = breaking
			case ch.Type == "feat":
				section = features
			case ch.Type == "fix":
				section = fixes
			case ch.Type == "perf":
				section = perf
			}
			if section == other || section == breaking {
				// Keep the type where the section does not already say it.
				desc = ch.Type + ch.Scope + ": " + desc
			}
		}
		line := "- " + desc
		if pr > 0 {
			line += fmt.Sprintf(" ([#%d](https://github.com/%s/pull/%d))", pr, in.Repo, pr)
		}
		groups[section].lines = append(groups[section].lines, line)
	}

	var b strings.Builder
	for _, g := range groups {
		if len(g.lines) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", g.title, strings.Join(g.lines, "\n"))
	}
	if b.Len() == 0 {
		b.WriteString("No changes since the previous release.\n\n")
	}
	if in.Previous != "" {
		fmt.Fprintf(&b, "**Full changelog:** https://github.com/%s/compare/%s...%s\n", in.Repo, in.Previous, in.Tag)
	}
	return Notes{Markdown: strings.TrimRight(b.String(), "\n") + "\n", PRs: prs}
}
