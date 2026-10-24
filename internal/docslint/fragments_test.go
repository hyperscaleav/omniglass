package docslint

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func texts(fs []Finding) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(f.File + ":" + itoa(f.Line) + ": " + f.Text + "\n")
	}
	return b.String()
}

func wantFinding(t *testing.T, fs []Finding, parts ...string) {
	t.Helper()
	for _, f := range fs {
		line := f.File + ": " + f.Text
		ok := true
		for _, p := range parts {
			if !strings.Contains(line, p) {
				ok = false
				break
			}
		}
		if ok {
			return
		}
	}
	t.Errorf("no finding carries all of %q; got:\n%s", parts, texts(fs))
}

// TestParseFragment pins the frontmatter grammar shared with the docs build
// (docs/src/plugins/fragments.mjs): the two readers must agree on every file.
func TestParseFragment(t *testing.T) {
	f, err := ParseFragment("x.md", "---\ntitle: \"A `code` title: with a colon\"\ndate: 2026-10-24\nrefs: [12, 34]\nsummary: 'It''s quoted'\n---\n\n\nBody line.\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Fields; !reflect.DeepEqual(got, map[string]string{"title": "A `code` title: with a colon", "date": "2026-10-24", "summary": "It's quoted"}) {
		t.Errorf("fields = %v", got)
	}
	if got := f.Lists["refs"]; !reflect.DeepEqual(got, []int{12, 34}) {
		t.Errorf("refs = %v", got)
	}
	if f.Body != "Body line." || f.BodyLine != 9 {
		t.Errorf("body = %q at line %d", f.Body, f.BodyLine)
	}
	if f, err := ParseFragment("crlf.md", "---\r\ntitle: T\r\n---\r\nBody\r\n"); err != nil || f.Fields["title"] != "T" || f.Body != "Body" {
		t.Errorf("CRLF: %v %+v", err, f)
	}
	for _, c := range []struct{ why, text, want string }{
		{"no opening fence", "title: T\n---\nBody", "must open with a --- frontmatter fence"},
		{"an unclosed fence", "---\ntitle: T\nBody", "never closes"},
		{"a line that is not key: value", "---\ntitle T\n---\nBody", `line 2 is not a "key: value" line`},
		{"a duplicate key", "---\ntitle: A\ntitle: B\n---\nBody", `duplicate key "title"`},
		{"a list of non-integers", "---\nrefs: [\"a\"]\n---\nBody", "refs must be a list of positive integers"},
		{"a bad single-quoted string", "---\ntitle: 'it's'\n---\nBody", "not a valid single-quoted string"},
		{"an empty body", "---\ntitle: T\n---\n\n", "empty body"},
	} {
		if _, err := ParseFragment("bad.md", c.text); err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "bad.md") {
			t.Errorf("%s: err = %v, want it to name bad.md and say %q", c.why, err, c.want)
		}
	}
}

func adrFile(n, slug, title, date, extra string) (string, string) {
	return "adr-" + n + "-" + slug + ".md",
		"---\ntitle: \"" + title + "\"\ndate: " + date + "\nstatus: Accepted\nsummary: \"One line\"\n" + extra + "---\n\n" +
			"- **Date:** " + date + " | **Status:** Accepted | **Pages:** none\n- **Decision:** " + title + ".\n"
}

func decisionsFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		writeFile(t, filepath.Join(dir, name), text)
	}
	return dir
}

// TestScanDecisionsDir pins the decision log's lint over its fragment
// directory: required fields, one number per file, numbers without a gap.
func TestScanDecisionsDir(t *testing.T) {
	files := map[string]string{}
	for _, d := range [][2]string{{"0001", "first"}, {"0002", "second"}, {"0003", "third"}} {
		name, text := adrFile(d[0], d[1], d[1], "2026-06-27", "")
		files[name] = text
	}
	if fs, err := scanDecisionsDir(decisionsFixture(t, files)); err != nil || len(fs) != 0 {
		t.Fatalf("a clean log: err %v, findings:\n%s", err, texts(fs))
	}

	t.Run("two PRs took the same number", func(t *testing.T) {
		f := map[string]string{}
		for k, v := range files {
			f[k] = v
		}
		name, text := adrFile("0003", "theirs", "theirs", "2026-06-28", "")
		f[name] = text
		fs, _ := scanDecisionsDir(decisionsFixture(t, f))
		wantFinding(t, fs, "ADR-0003", "adr-0003-third.md", "adr-0003-theirs.md", "make adr-renumber FROM=0003 TO=0004")
	})

	t.Run("a gap", func(t *testing.T) {
		f := map[string]string{}
		for k, v := range files {
			f[k] = v
		}
		name, text := adrFile("0005", "skipped", "skipped", "2026-06-28", "")
		f[name] = text
		fs, _ := scanDecisionsDir(decisionsFixture(t, f))
		wantFinding(t, fs, "ADR-0004 is missing")
	})

	for _, c := range []struct{ why, name, text, want string }{
		{"a misnamed file", "adr-4-x.md", "---\ntitle: T\n---\nB", "adr-NNNN-<slug>.md"},
		{"a missing summary", "adr-0004-x.md", "---\ntitle: T\ndate: 2026-06-27\nstatus: Accepted\n---\n\n- **Date:** 2026-06-27 | **Status:** Accepted\n", "needs a summary"},
		{"an unknown key", "adr-0004-x.md", "---\ntitle: T\ndate: 2026-06-27\nstatus: Accepted\nsummary: S\nnumber: 4\n---\n\n- **Date:** 2026-06-27 | **Status:** Accepted\n", `unknown key "number"`},
		{"a body with no Status field", "adr-0004-x.md", "---\ntitle: T\ndate: 2026-06-27\nstatus: Accepted\nsummary: S\n---\n\n- **Date:** 2026-06-27\n", "no **Status:** field"},
		{"a body date that disagrees", "adr-0004-x.md", "---\ntitle: T\ndate: 2026-06-27\nstatus: Accepted\nsummary: S\n---\n\n- **Date:** 2026-06-28 | **Status:** Accepted\n", "body says **Date:** 2026-06-28 but the frontmatter says 2026-06-27"},
		{"a second entry smuggled into a body", "adr-0004-x.md", "---\ntitle: T\ndate: 2026-06-27\nstatus: Accepted\nsummary: S\n---\n\n- **Date:** 2026-06-27 | **Status:** Accepted\n\n### ADR-0005: Another\n", "a heading inside a body"},
	} {
		t.Run(c.why, func(t *testing.T) {
			f := map[string]string{}
			for k, v := range files {
				f[k] = v
			}
			f[c.name] = c.text
			fs, _ := scanDecisionsDir(decisionsFixture(t, f))
			wantFinding(t, fs, c.name, c.want)
		})
	}
}

func buildLogEntry(title, date, extra, body string) string {
	return "---\ntitle: \"" + title + "\"\ndate: " + date + "\nrefs: [1]\n" + extra + "---\n\n" + body + "\n"
}

// TestScanBuildLogDir pins the build log's lint over its fragment directory.
func TestScanBuildLogDir(t *testing.T) {
	good := map[string]string{
		"0001-first.md":       "---\ntitle: \"First\"\ndate: 2026-06-27\nrefs: []\nform: bullet\n---\n\n- **First.** Text.\n",
		"0002-second.md":      buildLogEntry("Second (#2)", "2026-06-28", "", "Body."),
		"2026-10-25-later.md": buildLogEntry("Later (#3)", "2026-10-25", "", "Body."),
		"2026-10-25-again.md": buildLogEntry("Again (#4)", "2026-10-25", "", "Body.\n\n#### A sub-heading is fine\n\nMore."),
	}
	if fs, err := scanBuildLogDir(decisionsFixture(t, good)); err != nil || len(fs) != 0 {
		t.Fatalf("a clean log: err %v, findings:\n%s", err, texts(fs))
	}
	for _, c := range []struct{ why, name, text, want string }{
		{"a name with no date", "later.md", buildLogEntry("T", "2026-10-25", "", "B"), "<YYYY-MM-DD>-<slug>.md"},
		{"a date prefix that disagrees", "2026-10-25-x.md", buildLogEntry("T", "2026-10-26", "", "B"), "names 2026-10-25 but its date is 2026-10-26"},
		{"missing refs", "2026-10-25-x.md", "---\ntitle: T\ndate: 2026-10-25\n---\n\nB\n", "needs refs"},
		{"an unknown key", "2026-10-25-x.md", buildLogEntry("T", "2026-10-25", "author: me\n", "B"), `unknown key "author"`},
		{"a heading in the body", "2026-10-25-x.md", buildLogEntry("T", "2026-10-25", "", "### Another entry\n\nB"), "a heading inside a body"},
		{"a bullet whose lead is not its title", "0003-x.md", "---\ntitle: \"Third\"\ndate: 2026-06-29\nrefs: []\nform: bullet\n---\n\n- **Something else.** Text.\n", `must open with "- **Third"`},
		{"a new sequence-numbered entry", "0003-x.md", "---\ntitle: \"Third\"\ndate: 2026-10-30\nrefs: []\nform: bullet\n---\n\n- **Third.** Text.\n", "the sequence-numbered entries are the history before the split"},
		{"a sequence gap", "0004-x.md", "---\ntitle: \"Fourth\"\ndate: 2026-06-29\nrefs: []\nform: bullet\n---\n\n- **Fourth.** Text.\n", "0003 is missing"},
	} {
		t.Run(c.why, func(t *testing.T) {
			f := map[string]string{}
			for k, v := range good {
				f[k] = v
			}
			f[c.name] = c.text
			fs, _ := scanBuildLogDir(decisionsFixture(t, f))
			wantFinding(t, fs, c.name, c.want)
		})
	}
}

// TestScanLogHostsRefusesAnEntryInThePage is the monolith guard: an entry or
// an index row written into a host page instead of its fragment directory.
func TestScanLogHostsRefusesAnEntryInThePage(t *testing.T) {
	host := func(t *testing.T, buildLog, decisions string) []Finding {
		t.Helper()
		root := t.TempDir()
		docs := filepath.Join(root, "docs")
		writeFile(t, filepath.Join(docs, "architecture", "build-log.md"), buildLog)
		writeFile(t, filepath.Join(docs, "architecture", "decisions.md"), decisions)
		fs, err := scanLogHosts(docs, filepath.Join(root, "anti-patterns.md"))
		if err != nil {
			t.Fatal(err)
		}
		return fs
	}
	cleanLog := "---\ntitle: Build log\n---\n\nIntro.\n\n::build-log\n"
	cleanDecisions := "---\ntitle: Decision log\n---\n\n## How it works\n\n- **One entry per decision.**\n\n## Index\n\n::decision-index\n\n## Entries\n\n::decision-log\n"
	if fs := host(t, cleanLog, cleanDecisions); len(fs) != 0 {
		t.Fatalf("clean hosts: %s", texts(fs))
	}
	wantFinding(t, host(t, cleanLog+"\n### A new slice (#1)\n\nBody.\n", cleanDecisions), "build-log.md", "an entry written into the page")
	wantFinding(t, host(t, cleanLog+"\n- **A new slice.** Body.\n", cleanDecisions), "build-log.md", "an entry written into the page")
	wantFinding(t, host(t, cleanLog, cleanDecisions+"\n### ADR-0160: Mine\n\nBody.\n"), "decisions.md", "an entry written into the page")
	wantFinding(t, host(t, cleanLog, strings.Replace(cleanDecisions, "::decision-index\n", "::decision-index\n| [ADR-0160](#adr-0160-mine) | 2026-10-24 | Accepted | Mine |\n", 1)), "decisions.md", "an entry written into the page")
	wantFinding(t, host(t, "---\ntitle: Build log\n---\n\nIntro.\n", cleanDecisions), "build-log.md", "::build-log")

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs", "architecture", "build-log.md"), cleanLog)
	writeFile(t, filepath.Join(root, "docs", "architecture", "decisions.md"), cleanDecisions)
	writeFile(t, filepath.Join(root, "anti-patterns.md"), "# The anti-pattern catalog\n")
	fs, _ := scanLogHosts(filepath.Join(root, "docs"), filepath.Join(root, "anti-patterns.md"))
	wantFinding(t, fs, "anti-patterns.md", "one file per entry")
}

// TestScanAntiPatternDir pins the catalog's shape: one file per entry, named
// for the id its heading carries.
func TestScanAntiPatternDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "README.md"), "# The anti-pattern catalog\n")
	writeFile(t, filepath.Join(dir, "missing-audit-row.md"), "## missing-audit-row (live)\n\n- **Cue:** x\n")
	if fs, err := scanAntiPatternDir(dir); err != nil || len(fs) != 0 {
		t.Fatalf("clean catalog: %v %s", err, texts(fs))
	}
	writeFile(t, filepath.Join(dir, "wrong-name.md"), "## another-id (live)\n\n- **Cue:** x\n")
	writeFile(t, filepath.Join(dir, "no-status.md"), "## no-status\n\n- **Cue:** x\n")
	fs, _ := scanAntiPatternDir(dir)
	wantFinding(t, fs, "wrong-name.md", `must open with "## wrong-name (<status>)"`)
	wantFinding(t, fs, "no-status.md", `must open with "## no-status (<status>)"`)
}

// TestRenumberADR drives the renumber tool over a fixture tree: the file moves,
// every reference follows (docs, anchors, code comments, skills), and nothing
// else is touched.
func TestRenumberADR(t *testing.T) {
	root := t.TempDir()
	dec := filepath.Join(root, "docs", "src", "content", "decisions")
	for _, d := range [][2]string{{"0001", "first"}, {"0002", "second"}} {
		name, text := adrFile(d[0], d[1], d[1], "2026-06-27", "")
		writeFile(t, filepath.Join(dec, name), text)
	}
	_, mine := adrFile("0003", "mine", "Mine", "2026-10-24", "")
	writeFile(t, filepath.Join(dec, "adr-0003-mine.md"), strings.Replace(mine, "status: Accepted", "status: \"Amends [ADR-0002](#adr-0002-second)\"", 1))
	page := "See [ADR-0003](/architecture/decisions/#adr-0003-mine) and adr-0003-mine.md, not ADR-0030 or ADR-00031 or ADR-0002.\n"
	writeFile(t, filepath.Join(root, "docs", "src", "content", "docs", "architecture", "x.md"), page)
	writeFile(t, filepath.Join(root, "internal", "foo", "foo.go"), "package foo\n\n// Per ADR-0003 (and adr-0003), not ADR-0030.\n")
	writeFile(t, filepath.Join(root, ".claude", "skills", "s", "SKILL.md"), "Read ADR-0003.\n")
	writeFile(t, filepath.Join(root, "node_modules", "pkg", "x.md"), "ADR-0003\n")
	writeFile(t, filepath.Join(root, ".claude", "worktrees", "other", "x.md"), "ADR-0003\n")
	writeFile(t, filepath.Join(root, "docs", "public", "shot.png"), "ADR-0003")

	changed, err := RenumberADR(root, 3, 4)
	if err != nil {
		t.Fatal(err)
	}
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(b)
	}
	if _, err := os.Stat(filepath.Join(dec, "adr-0003-mine.md")); !os.IsNotExist(err) {
		t.Errorf("the old file is still there: %v", err)
	}
	if got := read("docs/src/content/decisions/adr-0004-mine.md"); !strings.Contains(got, "status: \"Amends [ADR-0002](#adr-0002-second)\"") {
		t.Errorf("the moved file lost its content or rewrote another number:\n%s", got)
	}
	if got, want := read("docs/src/content/docs/architecture/x.md"), "See [ADR-0004](/architecture/decisions/#adr-0004-mine) and adr-0004-mine.md, not ADR-0030 or ADR-00031 or ADR-0002.\n"; got != want {
		t.Errorf("page:\n got %q\nwant %q", got, want)
	}
	if got, want := read("internal/foo/foo.go"), "package foo\n\n// Per ADR-0004 (and adr-0004), not ADR-0030.\n"; got != want {
		t.Errorf("code comment:\n got %q\nwant %q", got, want)
	}
	if got := read(".claude/skills/s/SKILL.md"); got != "Read ADR-0004.\n" {
		t.Errorf("skill: %q", got)
	}
	for _, rel := range []string{"node_modules/pkg/x.md", ".claude/worktrees/other/x.md", "docs/public/shot.png"} {
		if got := read(rel); !strings.Contains(got, "ADR-0003") {
			t.Errorf("%s was rewritten: %q", rel, got)
		}
	}
	want := []string{
		".claude/skills/s/SKILL.md",
		"docs/src/content/decisions/adr-0004-mine.md",
		"docs/src/content/docs/architecture/x.md",
		"internal/foo/foo.go",
	}
	if !reflect.DeepEqual(changed, want) {
		t.Errorf("changed = %v, want %v", changed, want)
	}

	t.Run("refuses a number that is taken", func(t *testing.T) {
		if _, err := RenumberADR(root, 4, 2); err == nil || !strings.Contains(err.Error(), "ADR-0002 is taken by adr-0002-second.md") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("refuses a number no file holds", func(t *testing.T) {
		if _, err := RenumberADR(root, 9, 10); err == nil || !strings.Contains(err.Error(), "no decision file holds ADR-0009") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("refuses a number two files hold", func(t *testing.T) {
		_, theirs := adrFile("0004", "theirs", "Theirs", "2026-10-24", "")
		writeFile(t, filepath.Join(dec, "adr-0004-theirs.md"), theirs)
		if _, err := RenumberADR(root, 4, 5); err == nil || !strings.Contains(err.Error(), "adr-0004-mine.md") || !strings.Contains(err.Error(), "adr-0004-theirs.md") || !strings.Contains(err.Error(), "before merging main") {
			t.Errorf("err = %v", err)
		}
	})
}
