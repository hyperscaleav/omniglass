package releasenotes

import (
	"reflect"
	"strings"
	"testing"
)

const repo = "hyperscaleav/omniglass"

func TestRenderGroupsByType(t *testing.T) {
	got := Render(Input{
		Repo:     repo,
		Tag:      "v0.1.0",
		Previous: "v0.0.0",
		Commits: []Commit{
			{Subject: "feat: omnibot in the console (#2136)"},
			{Subject: "fix(lab): a reused pid reads as gone (#1911)"},
			{Subject: "perf: the chart reads hourly trends (#2002)"},
			{Subject: "docs: the Helm guide gains a production section (#1906)"},
			{Subject: "feat: a second feature (#2131)"},
		},
	})
	want := `## Features

- omnibot in the console ([#2136](https://github.com/hyperscaleav/omniglass/pull/2136))
- a second feature ([#2131](https://github.com/hyperscaleav/omniglass/pull/2131))

## Fixes

- a reused pid reads as gone ([#1911](https://github.com/hyperscaleav/omniglass/pull/1911))

## Performance

- the chart reads hourly trends ([#2002](https://github.com/hyperscaleav/omniglass/pull/2002))

## Other changes

- docs: the Helm guide gains a production section ([#1906](https://github.com/hyperscaleav/omniglass/pull/1906))

**Full changelog:** https://github.com/hyperscaleav/omniglass/compare/v0.0.0...v0.1.0
`
	if got.Markdown != want {
		t.Fatalf("markdown:\n%s\nwant:\n%s", got.Markdown, want)
	}
	if wantPRs := []int{2136, 1911, 2002, 1906, 2131}; !reflect.DeepEqual(got.PRs, wantPRs) {
		t.Fatalf("PRs = %v, want %v", got.PRs, wantPRs)
	}
}

func TestRenderBreaking(t *testing.T) {
	got := Render(Input{Repo: repo, Tag: "v0.1.0", Commits: []Commit{
		{Subject: "refactor!: the principal becomes the user (#2130)"},
		{Subject: "feat(api): health replaces verdict (#2102)", Body: "Long body.\n\nBREAKING CHANGE: verdict is gone"},
		{Subject: "fix: mentions BREAKING CHANGE: inline only (#5)", Body: "not a footer BREAKING CHANGE: here"},
	}}).Markdown
	for _, s := range []string{
		"## Breaking changes\n\n- refactor: the principal becomes the user",
		"- feat(api): health replaces verdict",
		"## Fixes\n\n- mentions BREAKING CHANGE: inline only",
	} {
		if !strings.Contains(got, s) {
			t.Errorf("missing %q in:\n%s", s, got)
		}
	}
	if strings.Contains(got, "## Features") {
		t.Errorf("a breaking feat is listed under Breaking changes only:\n%s", got)
	}
}

func TestRenderOddSubjects(t *testing.T) {
	got := Render(Input{Repo: repo, Tag: "v0.1.1-rc.1", Commits: []Commit{
		{Subject: "Wait for the first SNMP datagram to settle (#1765)"},
		{Subject: "fix: a direct push with no PR"},
		{Subject: "fix: cherry-picked twice (#7)"},
		{Subject: "fix: cherry-picked twice (#7)"},
		{Subject: "   "},
	}})
	for _, s := range []string{
		"## Other changes\n\n- Wait for the first SNMP datagram to settle ([#1765]",
		"- a direct push with no PR\n",
	} {
		if !strings.Contains(got.Markdown, s) {
			t.Errorf("missing %q in:\n%s", s, got.Markdown)
		}
	}
	if strings.Contains(got.Markdown, "Full changelog") {
		t.Errorf("no previous release, no compare link:\n%s", got.Markdown)
	}
	if want := []int{1765, 7}; !reflect.DeepEqual(got.PRs, want) {
		t.Fatalf("PRs = %v, want %v", got.PRs, want)
	}
}

func TestRenderEmpty(t *testing.T) {
	got := Render(Input{Repo: repo, Tag: "v0.1.0-rc.2", Previous: "v0.0.0"})
	if !strings.HasPrefix(got.Markdown, "No changes since the previous release.") || len(got.PRs) != 0 {
		t.Fatalf("got %q %v", got.Markdown, got.PRs)
	}
}
