package releasecut

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/releasenotes"
)

func commit(subject string) Commit {
	return Commit{Commit: releasenotes.Commit{Subject: subject}}
}

func change(t *testing.T, subject, body string) releasenotes.Change {
	t.Helper()
	ch, ok := releasenotes.Parse(releasenotes.Commit{Subject: subject, Body: body})
	if !ok {
		t.Fatalf("Parse(%q) not ok", subject)
	}
	return ch
}

// The pre-1.0 rule: a breaking change on 0.x asks for a minor, never a major.
func TestBreakingBeforeOneBumpsTheMinor(t *testing.T) {
	for _, c := range []struct{ subject, body string }{
		{"feat!: the principal becomes the user (#2130)", ""},
		{"refactor(api)!: verdict is gone (#2102)", ""},
		{"fix: a renamed key (#9)", "Body.\n\nBREAKING CHANGE: the old key is gone"},
	} {
		if got := BumpOf(change(t, c.subject, c.body), 0); got != Minor {
			t.Errorf("on 0.x, BumpOf(%q) = %s, want minor", c.subject, got)
		}
		if got := BumpOf(change(t, c.subject, c.body), 1); got != Major {
			t.Errorf("on 1.x, BumpOf(%q) = %s, want major", c.subject, got)
		}
	}
}

func TestBumpOfByType(t *testing.T) {
	for subject, want := range map[string]Bump{
		"feat: omnibot in the console (#2136)":   Minor,
		"feat(ui): a scoped feature (#1)":        Minor,
		"FEAT: shouting still counts (#2)":       Minor,
		"fix(lab): a reused pid reads gone (#3)": Patch,
		"perf: hourly trends (#4)":               Patch,
		"docs: the Helm guide (#5)":              None,
		"chore(deps): bump x (#6)":               None,
		"ci: faster lint (#7)":                   None,
		"Wait for the first datagram (#8)":       None,
	} {
		for _, major := range []int{0, 1} {
			if got := BumpOf(change(t, subject, ""), major); got != want {
				t.Errorf("BumpOf(%q, %d) = %s, want %s", subject, major, got, want)
			}
		}
	}
}

func TestNewestLine(t *testing.T) {
	cases := []struct {
		name     string
		branches []string
		tags     []string
		want     Line
		found    bool
	}{
		{"nothing", nil, nil, Line{}, false},
		{"a tag", nil, []string{"v0.0.0"}, Line{0, 0}, true},
		{"a branch beats an older tag", []string{"release/0.1"}, []string{"v0.0.0"}, Line{0, 1}, true},
		{"a candidate names its line", nil, []string{"v0.0.0", "v0.2.0-rc.1"}, Line{0, 2}, true},
		{"numeric, not lexical", []string{"release/0.9", "release/0.10"}, nil, Line{0, 10}, true},
		{"the major orders first", []string{"release/0.12", "release/1.0"}, nil, Line{1, 0}, true},
		{"strays ignored", []string{"release/x", "release/0.1.2"}, []string{"v1", "latest", "v0.3.0-rc.0"}, Line{}, false},
	}
	for _, c := range cases {
		got, found := NewestLine(c.branches, c.tags)
		if got != c.want || found != c.found {
			t.Errorf("%s: NewestLine = %v %t, want %v %t", c.name, got, found, c.want, c.found)
		}
	}
}

func TestAnchor(t *testing.T) {
	tags := []string{"v0.0.0", "v0.1.0-rc.1", "v0.1.0-rc.2", "v0.1.0", "v0.1.1-rc.1", "v0.2.0-rc.1"}
	if got := Anchor(Line{0, 1}, true, []string{"release/0.1"}, tags); got != "release/0.1" {
		t.Errorf("with a branch, anchor = %q", got)
	}
	if got := Anchor(Line{0, 1}, true, nil, tags); got != "v0.1.1-rc.1" {
		t.Errorf("newest tag of the line = %q, want v0.1.1-rc.1", got)
	}
	if got := Anchor(Line{0, 1}, true, nil, []string{"v0.1.0-rc.2", "v0.1.0", "v0.1.0-rc.10"}); got != "v0.1.0" {
		t.Errorf("a final sorts above its candidates, anchor = %q", got)
	}
	if got := Anchor(Line{}, false, nil, nil); got != "" {
		t.Errorf("no release, anchor = %q, want all of main", got)
	}
}

// Today's main: v0.0.0 and a throwaway release/0.0, then hundreds of commits,
// some breaking. The cut is release/0.1, never 1.0.
func TestMakeFirstMinorFromBreakingChanges(t *testing.T) {
	p, err := Make(Request{
		Branches: []string{"release/0.0"},
		Tags:     []string{"v0.0.0"},
		Commits: []Commit{
			commit("docs: a page (#10)"),
			commit("fix: a bug (#9)"),
			commit("feat!: the principal becomes the user (#8)"),
			commit("feat: a feature (#7)"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Bump != Minor || p.To != (Line{0, 1}) || !p.NewBranch || p.To.Branch() != "release/0.1" {
		t.Fatalf("plan = %+v, want a minor to release/0.1", p)
	}
	var subjects []string
	for _, d := range p.Drivers {
		subjects = append(subjects, d.Subject)
	}
	if want := []string{"feat!: the principal becomes the user (#8)", "feat: a feature (#7)"}; !reflect.DeepEqual(subjects, want) {
		t.Fatalf("drivers = %v, want %v", subjects, want)
	}
}

func TestMakeMajorFromOne(t *testing.T) {
	p, err := Make(Request{
		Branches: []string{"release/1.3"},
		Tags:     []string{"v1.3.2"},
		Commits:  []Commit{commit("feat: x (#2)"), commit("fix!: y (#1)")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Bump != Major || p.To != (Line{2, 0}) || len(p.Drivers) != 1 || p.Drivers[0].Subject != "fix!: y (#1)" {
		t.Fatalf("plan = %+v, want a major to release/2.0 driven by the breaking fix", p)
	}
}

func TestMakePatchStaysOnTheLine(t *testing.T) {
	p, err := Make(Request{
		Branches: []string{"release/0.1"},
		Tags:     []string{"v0.0.0", "v0.1.0"},
		Commits:  []Commit{commit("chore: x (#3)"), commit("perf: y (#2)"), commit("fix: z (#1)")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Bump != Patch || p.To != (Line{0, 1}) || p.NewBranch || len(p.Drivers) != 2 {
		t.Fatalf("plan = %+v, want a patch on release/0.1 with two fixes", p)
	}
}

func TestMakePatchWithNoBranchIsRefused(t *testing.T) {
	_, err := Make(Request{Tags: []string{"v0.1.0"}, Commits: []Commit{commit("fix: z (#1)")}})
	if err == nil || !strings.Contains(err.Error(), "release/0.1, which does not exist") {
		t.Fatalf("err = %v", err)
	}
}

func TestMakeNothingToRelease(t *testing.T) {
	p, err := Make(Request{Tags: []string{"v0.1.0"}, Commits: []Commit{commit("docs: a (#2)"), commit("ci: b (#1)")}})
	if err != nil || p.Bump != None || p.NewBranch || len(p.Drivers) != 0 {
		t.Fatalf("plan = %+v, err %v, want nothing to release", p, err)
	}
}

func TestMakeFirstReleaseEver(t *testing.T) {
	p, err := Make(Request{Commits: []Commit{commit("fix: only a fix (#1)")}})
	if err != nil || p.Bump != Minor || p.To != (Line{0, 1}) || p.HasFrom {
		t.Fatalf("plan = %+v, err %v, want the first release to be release/0.1", p, err)
	}
}

func TestMissing(t *testing.T) {
	fixes := []Commit{
		{SHA: "aaa1", PatchID: "p1", Commit: releasenotes.Commit{Subject: "fix: by patch id (#11)"}},
		{SHA: "bbb2", PatchID: "p2", Commit: releasenotes.Commit{Subject: "fix: by -x trailer (#12)"}},
		{SHA: "ccc3", PatchID: "p3", Commit: releasenotes.Commit{Subject: "fix: by PR number (#13)"}},
		{SHA: "ddd4", PatchID: "p4", Commit: releasenotes.Commit{Subject: "fix(lab): by description (#14)"}},
		{SHA: "eee5", PatchID: "p5", Commit: releasenotes.Commit{Subject: "fix: not there yet (#15)"}},
		{SHA: "fff6", PatchID: "p6", Commit: releasenotes.Commit{Subject: "perf: nor this (#150)"}},
	}
	onBranch := []Commit{
		{PatchID: "p1", Commit: releasenotes.Commit{Subject: "fix: renamed in the pick (#21)"}},
		{PatchID: "q2", Commit: releasenotes.Commit{Subject: "fix: other words (#22)", Body: "(cherry picked from commit bbb2)"}},
		{PatchID: "q3", Commit: releasenotes.Commit{Subject: "fix: cherry-pick #13 onto 0.1 (#23)"}},
		{PatchID: "q4", Commit: releasenotes.Commit{Subject: "fix(lab): by description (#24)"}},
		{PatchID: "q5", Commit: releasenotes.Commit{Subject: "fix: names #1500 and #15x only (#25)"}},
	}
	var got []string
	for _, m := range Missing(fixes, onBranch) {
		got = append(got, m.SHA)
	}
	if want := []string{"eee5", "fff6"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("missing = %v, want %v", got, want)
	}
}
