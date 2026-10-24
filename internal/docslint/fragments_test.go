package docslint

import (
	"os"
	"os/exec"
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
		{"a list with a decimal", "---\nrefs: [1.0]\n---\nBody", "refs must be a list of positive integers"},
		{"a list with an exponent", "---\nrefs: [1e3]\n---\nBody", "refs must be a list of positive integers"},
		{"a zero in a list", "---\nrefs: [0]\n---\nBody", "refs must be a list of positive integers"},
		{"a list with a leading zero", "---\nrefs: [01]\n---\nBody", "refs must be a list of positive integers"},
		{"a number too long to be an issue", "---\nrefs: [1234567890]\n---\nBody", "refs must be a list of positive integers"},
		{"a line separator in a value", "---\ntitle: A\u2028B\n---\nBody", "line 2 carries a control character"},
		{"a lone carriage return in a value", "---\ntitle: A\rB\n---\nBody", "line 2 carries a control character"},
		{"a newline escaped into a string", "---\ntitle: \"First\\nline two\"\n---\nBody", "title carries a control character"},
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
		{"a directive in a body", "adr-0004-x.md", "---\ntitle: T\ndate: 2026-06-27\nstatus: Accepted\nsummary: S\n---\n\n- **Date:** 2026-06-27 | **Status:** Accepted\n\n::walkthrough{#x}\n", "cannot carry a directive"},
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

// TestScanDecisionsIssueNumbers pins the numbering rule (#1108): the legacy
// range runs from 0001 with no gap, and every later decision takes its issue's
// number, named in its refs, a second decision on the issue a suffix from .2.
func TestScanDecisionsIssueNumbers(t *testing.T) {
	legacy := map[string]string{}
	for _, d := range [][2]string{{"0001", "first"}, {"0002", "second"}, {"0003", "third"}} {
		name, text := adrFile(d[0], d[1], d[1], "2026-06-27", "")
		legacy[name] = text
	}
	with := func(extra map[string]string) map[string]string {
		f := map[string]string{}
		for k, v := range legacy {
			f[k] = v
		}
		for k, v := range extra {
			f[k] = v
		}
		return f
	}
	byIssue := func(num, slug, refs string) (string, string) {
		return adrFile(num, slug, slug, "2026-10-24", "refs: ["+refs+"]\n")
	}
	clean := map[string]string{}
	for _, d := range [][3]string{{"1100", "by-issue", "1100, 1095"}, {"1100.2", "second-on-the-issue", "1100"}, {"1200", "a-gap-above-the-legacy-range-is-fine", "1200"}, {"10000", "five-digits", "10000"}} {
		name, text := byIssue(d[0], d[1], d[2])
		clean[name] = text
	}
	if fs, err := scanDecisionsDir(decisionsFixture(t, with(clean))); err != nil || len(fs) != 0 {
		t.Fatalf("legacy plus issue numbers: err %v, findings:\n%s", err, texts(fs))
	}

	for _, c := range []struct {
		why   string
		files [][3]string // number, slug, refs ("-" for no refs line)
		want  []string
	}{
		{"refs that do not name the number", [][3]string{{"1100", "mismatch", "1099"}}, []string{"adr-1100-mismatch.md", "numbered by its issue, so its refs must include 1100"}},
		{"no refs at all", [][3]string{{"1100", "no-refs", "-"}}, []string{"adr-1100-no-refs.md", "refs must include 1100"}},
		{"two files holding one suffixed number", [][3]string{{"1100", "first", "1100"}, {"1100.2", "mine", "1100"}, {"1100.2", "theirs", "1100"}}, []string{"ADR-1100.2 is taken by both adr-1100.2-mine.md and adr-1100.2-theirs.md", "make adr-renumber FROM=1100.2 TO=1100.3 FILE="}},
		{"two files holding one issue number", [][3]string{{"1100", "mine", "1100"}, {"1100", "theirs", "1100"}}, []string{"ADR-1100 is taken by both adr-1100-mine.md and adr-1100-theirs.md", "make adr-renumber FROM=1100 TO=1100.2 FILE="}},
		{"a suffix with no plain decision before it", [][3]string{{"1100.2", "orphan", "1100"}}, []string{"adr-1100.2-orphan.md", "ADR-1100.2 has no ADR-1100 before it"}},
		{"a suffix gap", [][3]string{{"1100", "first", "1100"}, {"1100.3", "third", "1100"}}, []string{"adr-1100.3-third.md", "ADR-1100.3 has no ADR-1100.2 before it"}},
		{"a suffix on a legacy number", [][3]string{{"0003.2", "legacy-suffix", "-"}}, []string{"adr-0003.2-legacy-suffix.md", "a legacy number takes no suffix"}},
		{"two numbers that share an anchor", [][3]string{{"1100", "first", "1100"}, {"1100.2", "second", "1100"}, {"11002", "issue", "11002"}}, []string{"ADR-11002 and ADR-1100.2 share the anchor prefix #adr-11002-"}},
		{"a number not zero-padded to four digits", [][3]string{{"163", "short", "-"}}, []string{"adr-163-short.md", "adr-NNNN-<slug>.md"}},
		{"an issue number padded past four digits", [][3]string{{"01100", "padded", "1100"}}, []string{"adr-01100-padded.md", "adr-NNNN-<slug>.md"}},
		{"a suffix of one", [][3]string{{"1100.1", "one", "1100"}}, []string{"adr-1100.1-one.md", "adr-NNNN-<slug>.md"}},
	} {
		t.Run(c.why, func(t *testing.T) {
			extra := map[string]string{}
			for _, d := range c.files {
				if d[2] == "-" {
					name, text := adrFile(d[0], d[1], d[1], "2026-10-24", "")
					extra[name] = text
				} else {
					name, text := byIssue(d[0], d[1], d[2])
					extra[name] = text
				}
			}
			fs, _ := scanDecisionsDir(decisionsFixture(t, with(extra)))
			wantFinding(t, fs, c.want...)
		})
	}

	t.Run("a gap below the legacy ceiling", func(t *testing.T) {
		name, text := adrFile("0005", "skipped", "skipped", "2026-06-28", "")
		fs, _ := scanDecisionsDir(decisionsFixture(t, with(map[string]string{name: text})))
		wantFinding(t, fs, "ADR-0004 is missing")
	})
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
		"2026-10-25-shell.md": buildLogEntry("Shell (#5)", "2026-10-25", "", "Run:\n\n```bash\n# a comment is not a heading\nmake gen\n```\n\n```markdown\n::screenshot{#shown-not-embedded}\n```"),
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
		{"a new dated entry in the bullet form", "2026-10-26-x.md", "---\ntitle: \"T\"\ndate: 2026-10-26\nrefs: []\nform: bullet\n---\n\n- **T.** Text.\n", "form: bullet is the sequence-numbered history's"},
		{"a directive in a body", "2026-10-25-x.md", buildLogEntry("T", "2026-10-25", "", "Text.\n\n::screenshot{#console-map}"), "cannot carry a directive"},
		{"a design fence in a body", "2026-10-25-x.md", buildLogEntry("T", "2026-10-25", "", ":::design\nUnowned.\n:::"), "cannot carry a directive"},
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
	wantFinding(t, host(t, cleanLog, cleanDecisions+"\n### ADR-0003: Mine\n\nBody.\n"), "decisions.md", "an entry written into the page")
	wantFinding(t, host(t, cleanLog, strings.Replace(cleanDecisions, "::decision-index\n", "::decision-index\n| [ADR-0003](#adr-0003-mine) | 2026-10-24 | Accepted | Mine |\n", 1)), "decisions.md", "an entry written into the page")
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
	writeFile(t, filepath.Join(root, "proto", "og", "v1", "telemetry.proto"), "// Per ADR-0003.\n")
	writeFile(t, filepath.Join(root, "internal", "foo", "blob.bin"), "ADR-0003\x00\x01binary")

	changed, err := RenumberADR(root, "3", "4", RenumberOptions{})
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
	if got := read("proto/og/v1/telemetry.proto"); got != "// Per ADR-0004.\n" {
		t.Errorf("a .proto comment was not rewritten: %q", got)
	}
	for _, rel := range []string{"node_modules/pkg/x.md", ".claude/worktrees/other/x.md", "docs/public/shot.png", "internal/foo/blob.bin"} {
		if got := read(rel); !strings.Contains(got, "ADR-0003") {
			t.Errorf("%s was rewritten: %q", rel, got)
		}
	}
	want := []string{
		".claude/skills/s/SKILL.md",
		"docs/src/content/decisions/adr-0004-mine.md",
		"docs/src/content/docs/architecture/x.md",
		"internal/foo/foo.go",
		"proto/og/v1/telemetry.proto",
	}
	if !reflect.DeepEqual(changed, want) {
		t.Errorf("changed = %v, want %v", changed, want)
	}

	t.Run("refuses a number that is taken", func(t *testing.T) {
		if _, err := RenumberADR(root, "4", "2", RenumberOptions{}); err == nil || !strings.Contains(err.Error(), "ADR-0002 is taken by adr-0002-second.md") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("refuses a number no file holds", func(t *testing.T) {
		if _, err := RenumberADR(root, "9", "10", RenumberOptions{}); err == nil || !strings.Contains(err.Error(), "no decision file holds ADR-0009") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("refuses a number two files hold", func(t *testing.T) {
		_, theirs := adrFile("0004", "theirs", "Theirs", "2026-10-24", "")
		writeFile(t, filepath.Join(dec, "adr-0004-theirs.md"), theirs)
		if _, err := RenumberADR(root, "4", "5", RenumberOptions{}); err == nil || !strings.Contains(err.Error(), "adr-0004-mine.md") || !strings.Contains(err.Error(), "adr-0004-theirs.md") || !strings.Contains(err.Error(), "FILE=") {
			t.Errorf("err = %v", err)
		}
	})
}

// TestWalkDocsReadsLogEntries pins the corpus lints' coverage of the history:
// walkDocs yields the log entry files beside the docs root, and they keep the
// historical-record exemption the log pages carried.
func TestWalkDocsReadsLogEntries(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "content", "docs")
	writeFile(t, filepath.Join(docs, "architecture", "build-log.md"), "---\ntitle: Build log\n---\n\n::build-log\n")
	writeFile(t, filepath.Join(root, "content", "build-log", "2026-10-24-x.md"), "---\ntitle: X\ndate: 2026-10-24\nrefs: []\n---\n\nBody.\n")
	writeFile(t, filepath.Join(root, "content", "decisions", "adr-0001-x.md"), "---\ntitle: X\n---\n\nBody.\n")
	old := DocsRoot
	DocsRoot = docs
	defer func() { DocsRoot = old }()
	var got []string
	if err := walkDocs(func(rel, _ string) error {
		got = append(got, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"architecture/build-log.md", "../build-log/2026-10-24-x.md", "../decisions/adr-0001-x.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("walkDocs = %v, want %v", got, want)
	}
	for _, rel := range want[1:] {
		if !vocabularyAllowed(filepath.FromSlash(rel)) {
			t.Errorf("%s lost the historical-record exemption", rel)
		}
	}
	if vocabularyAllowed(filepath.Join("architecture", "ui.md")) {
		t.Error("a current page is exempt")
	}
}

// git runs a git command in dir for a fixture repository.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=fixture", "-c", "user.email=fixture@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestRenumberADRRepairsTwoHolders covers the state the plain mode refuses: two
// PRs' decisions both landed (on main, or on a branch after merging main). With
// FILE naming the decision to move, only the lines the commits after SINCE wrote
// are rewritten, so the other decision's references keep their number.
func TestRenumberADRRepairsTwoHolders(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	dec := filepath.Join(root, "docs", "src", "content", "decisions")
	page := filepath.Join(root, "docs", "src", "content", "docs", "architecture", "x.md")
	git(t, root, "init", "-q", "-b", "main")
	name, text := adrFile("0001", "first", "First", "2026-06-27", "")
	writeFile(t, filepath.Join(dec, name), text)
	writeFile(t, page, "Intro.\n")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "base")

	// PR A lands first: its decision 0002 and a reference to it.
	_, a := adrFile("0002", "theirs", "Theirs", "2026-10-24", "")
	writeFile(t, filepath.Join(dec, "adr-0002-theirs.md"), a)
	writeFile(t, page, "Intro.\nTheirs is ADR-0002.\n")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "PR A")
	since := git(t, root, "rev-parse", "HEAD")

	// PR B lands second, also taking 0002, with references in the same page
	// and in a code comment.
	_, b := adrFile("0002", "mine", "Mine", "2026-10-24", "")
	writeFile(t, filepath.Join(dec, "adr-0002-mine.md"), strings.Replace(b, "- **Decision:** Mine.", "- **Decision:** Mine, see [ADR-0002](#adr-0002-mine).", 1))
	writeFile(t, page, "Intro.\nTheirs is ADR-0002.\nMine is [ADR-0002](/architecture/decisions/#adr-0002-mine).\n")
	writeFile(t, filepath.Join(root, "internal", "foo", "foo.go"), "package foo\n\n// ADR-0002 (mine).\n")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "PR B")

	if _, err := RenumberADR(root, "2", "3", RenumberOptions{File: "adr-0002-theirs.md", Since: since}); err == nil {
		t.Error("moving a decision the commits after SINCE did not add was accepted")
	}
	changed, err := RenumberADR(root, "2", "3", RenumberOptions{File: "adr-0002-mine.md", Since: since})
	if err != nil {
		t.Fatal(err)
	}
	read := func(path string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if got, want := read(page), "Intro.\nTheirs is ADR-0002.\nMine is [ADR-0003](/architecture/decisions/#adr-0003-mine).\n"; got != want {
		t.Errorf("page:\n got %q\nwant %q", got, want)
	}
	if got := read(filepath.Join(root, "internal", "foo", "foo.go")); !strings.Contains(got, "// ADR-0003 (mine).") {
		t.Errorf("code comment: %q", got)
	}
	if got := read(filepath.Join(dec, "adr-0003-mine.md")); !strings.Contains(got, "see [ADR-0003](#adr-0003-mine)") {
		t.Errorf("the moved decision's own reference: %q", got)
	}
	if got := read(filepath.Join(dec, "adr-0002-theirs.md")); got != a {
		t.Errorf("the other decision changed:\n%s", got)
	}
	want := []string{
		"docs/src/content/decisions/adr-0003-mine.md",
		"docs/src/content/docs/architecture/x.md",
		"internal/foo/foo.go",
	}
	if !reflect.DeepEqual(changed, want) {
		t.Errorf("changed = %v, want %v", changed, want)
	}
}

// TestRenumberADRSuffix moves a suffixed decision (the rare clash between two
// PRs adding a second decision to one issue, #1108): the dot form follows in
// mentions and file names, the anchor form (the dot dropped) in links, and a
// neighbour on the same issue keeps its number.
func TestRenumberADRSuffix(t *testing.T) {
	root := t.TempDir()
	dec := filepath.Join(root, "docs", "src", "content", "decisions")
	for _, d := range [][2]string{{"1100", "first"}, {"1100.2", "mine"}, {"1200", "plain"}} {
		name, text := adrFile(d[0], d[1], d[1], "2026-10-24", "refs: ["+strings.Split(d[0], ".")[0]+"]\n")
		writeFile(t, filepath.Join(dec, name), text)
	}
	page := filepath.Join(root, "docs", "src", "content", "docs", "architecture", "x.md")
	writeFile(t, page, "[ADR-1100.2](/architecture/decisions/#adr-11002-mine) amends ADR-1100 (#adr-1100-first), not ADR-1100.25; adr-1100.2-mine.md.\n")
	changed, err := RenumberADR(root, "1100.2", "1100.3", RenumberOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(page)
	if got, want := string(b), "[ADR-1100.3](/architecture/decisions/#adr-11003-mine) amends ADR-1100 (#adr-1100-first), not ADR-1100.25; adr-1100.3-mine.md.\n"; got != want {
		t.Errorf("page:\n got %q\nwant %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(dec, "adr-1100.3-mine.md")); err != nil {
		t.Errorf("the suffixed file did not move: %v", err)
	}
	if !reflect.DeepEqual(changed, []string{"docs/src/content/decisions/adr-1100.3-mine.md", "docs/src/content/docs/architecture/x.md"}) {
		t.Errorf("changed = %v", changed)
	}

	// A plain number moved to a suffix: the anchor drops the dot.
	writeFile(t, page, "See [ADR-1200](/architecture/decisions/#adr-1200-plain), not ADR-1200.5.\n")
	if _, err := RenumberADR(root, "1200", "1100.2", RenumberOptions{}); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(page)
	if got, want := string(b), "See [ADR-1100.2](/architecture/decisions/#adr-11002-plain), not ADR-1200.5.\n"; got != want {
		t.Errorf("plain to suffix:\n got %q\nwant %q", got, want)
	}
	if _, err := RenumberADR(root, "1100.2", "1100.1", RenumberOptions{}); err == nil {
		t.Error("a suffix of one was accepted")
	}
}
