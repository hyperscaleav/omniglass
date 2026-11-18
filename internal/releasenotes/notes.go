// Package releasenotes renders a release's notes from the commits it ships,
// grouped by conventional-commit type (#2142). The repo squash-merges, so each
// commit subject on main is a PR title, `<type>(<scope>)!: <description> (#<n>)`,
// and a cherry-pick onto a release branch keeps it. Nothing here touches git or
// the network: the command reads the log and hands the commits in.
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
		subject := strings.TrimSpace(c.Subject)
		if subject == "" {
			continue
		}
		pr := 0
		if m := prSuffixRe.FindStringSubmatch(subject); m != nil {
			pr, _ = strconv.Atoi(m[1])
			subject = strings.TrimSpace(subject[:len(subject)-len(m[0])])
		}
		if pr > 0 && !seen[pr] {
			seen[pr] = true
			prs = append(prs, pr)
		}

		section, desc := other, subject
		if m := subjectRe.FindStringSubmatch(subject); m != nil {
			desc = strings.TrimSpace(m[4])
			switch {
			case m[3] == "!" || breakingRe.MatchString(c.Body):
				section = breaking
			case strings.EqualFold(m[1], "feat"):
				section = features
			case strings.EqualFold(m[1], "fix"):
				section = fixes
			case strings.EqualFold(m[1], "perf"):
				section = perf
			}
			if section == other || section == breaking {
				// Keep the type where the section does not already say it.
				desc = strings.ToLower(m[1]) + m[2] + ": " + desc
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
