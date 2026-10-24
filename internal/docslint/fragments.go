package docslint

// The fragment-directory lints (#1095). The build log, the decision log, and the
// adversarial-review anti-pattern catalog each keep one file per entry instead
// of one shared file every PR appends to, so two PRs adding entries add two
// files and never conflict. The docs build splices the logs back into their
// pages (docs/src/plugins/fragments.mjs); these lints hold the files to the
// shape that render needs, catch two PRs that took the same ADR number, and
// refuse an entry written into a page instead of its directory.
//
// ParseFragment reads the same line grammar as the docs build's reader, not
// YAML, so the two can never disagree about a file.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Fragment is one entry file of a fragment directory.
type Fragment struct {
	Name     string
	Fields   map[string]string // scalar values: a JSON or single-quoted string, or a bare word
	Lists    map[string][]int  // list values, like refs: [12, 34]
	Body     string            // the text after the frontmatter, leading blank lines and trailing space trimmed
	BodyLine int               // the 1-indexed file line the body starts on
}

var fragmentKeyLine = regexp.MustCompile(`^([a-z][a-z_]*):(?: (.*))?$`)

// ParseFragment parses one fragment file. Every error names the file.
func ParseFragment(name, text string) (Fragment, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	f := Fragment{Name: name, Fields: map[string]string{}, Lists: map[string][]int{}}
	if lines[0] != "---" {
		return f, fmt.Errorf("%s: a fragment must open with a --- frontmatter fence", name)
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return f, fmt.Errorf("%s: the frontmatter fence never closes", name)
	}
	for i := 1; i < end; i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := fragmentKeyLine.FindStringSubmatch(line)
		if m == nil {
			return f, fmt.Errorf("%s: line %d is not a \"key: value\" line: %s", name, i+1, line)
		}
		key, raw := m[1], strings.TrimSpace(m[2])
		if _, dup := f.Fields[key]; dup {
			return f, fmt.Errorf("%s: duplicate key %q", name, key)
		}
		if _, dup := f.Lists[key]; dup {
			return f, fmt.Errorf("%s: duplicate key %q", name, key)
		}
		switch {
		case strings.HasPrefix(raw, `"`):
			var s string
			if err := json.Unmarshal([]byte(raw), &s); err != nil {
				return f, fmt.Errorf("%s: %s is not a valid double-quoted string: %s", name, key, raw)
			}
			f.Fields[key] = s
		case strings.HasPrefix(raw, "'"):
			inner := ""
			if len(raw) >= 2 && strings.HasSuffix(raw, "'") {
				inner = raw[1 : len(raw)-1]
			}
			if len(raw) < 2 || !strings.HasSuffix(raw, "'") || strings.Contains(strings.ReplaceAll(inner, "''", ""), "'") {
				return f, fmt.Errorf("%s: %s is not a valid single-quoted string: %s", name, key, raw)
			}
			f.Fields[key] = strings.ReplaceAll(inner, "''", "'")
		case strings.HasPrefix(raw, "["):
			var ns []json.Number
			bad := json.Unmarshal([]byte(raw), &ns) != nil
			out := make([]int, 0, len(ns))
			for _, n := range ns {
				v, err := strconv.Atoi(n.String())
				if err != nil || v <= 0 {
					bad = true
					break
				}
				out = append(out, v)
			}
			if bad {
				return f, fmt.Errorf("%s: %s must be a list of positive integers, like [12, 34]", name, key)
			}
			f.Lists[key] = out
		default:
			f.Fields[key] = raw
		}
	}
	rest := lines[end+1:]
	start := 0
	for start < len(rest) && rest[start] == "" {
		start++
	}
	f.BodyLine = end + 2 + start
	f.Body = strings.TrimRight(strings.Join(rest[start:], "\n"), " \t\n")
	if f.Body == "" {
		return f, fmt.Errorf("%s: empty body", name)
	}
	return f, nil
}

// fragmentSource is one file of a fragment directory, unparsed.
type fragmentSource struct{ name, text string }

// readFragmentDir lists dir's .md files in code-point order, the order the docs
// build renders them in.
func readFragmentDir(dir string) ([]fragmentSource, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []fragmentSource
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, fragmentSource{e.Name(), string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// The fragment directories, beside the docs content root.
func buildLogDir() string  { return filepath.Join(DocsRoot, "..", "build-log") }
func decisionsDir() string { return filepath.Join(DocsRoot, "..", "decisions") }

// antiPatternDir and antiPatternMonolith are the adversarial-review catalog's
// directory and the single file it replaced.
var (
	antiPatternRefs     = filepath.Join(repoRoot, ".claude", "skills", "adversarial-review", "references")
	antiPatternDir      = filepath.Join(antiPatternRefs, "anti-patterns")
	antiPatternMonolith = filepath.Join(antiPatternRefs, "anti-patterns.md")
)

// fragmentRoots are walked by walkDocs beside DocsRoot, so every corpus lint
// (paths, tables, env coverage) still reads the log entries it read when they
// sat in their pages.
func fragmentRoots() []string { return []string{buildLogDir(), decisionsDir()} }

// isFragmentFile reports whether rel (relative to DocsRoot) is a log entry file.
func isFragmentFile(rel string) bool {
	rel = filepath.ToSlash(rel)
	return strings.HasPrefix(rel, "../build-log/") || strings.HasPrefix(rel, "../decisions/")
}

var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// checkFields reports the required fields a fragment lacks, the keys it carries
// that its log does not know, and a malformed date.
func checkFields(f Fragment, required, lists, optional []string) []string {
	known := map[string]bool{}
	for _, k := range append(append(append([]string{}, required...), lists...), optional...) {
		known[k] = true
	}
	var out []string
	var keys []string
	for k := range f.Fields {
		keys = append(keys, k)
	}
	for k := range f.Lists {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !known[k] {
			out = append(out, fmt.Sprintf("unknown key %q", k))
		}
	}
	for _, k := range required {
		if f.Fields[k] == "" {
			out = append(out, "needs a "+k+" in its frontmatter")
		}
	}
	for _, k := range lists {
		if _, ok := f.Lists[k]; !ok {
			out = append(out, "needs "+k+" in its frontmatter, a list like [1095] or []")
		}
	}
	if d := f.Fields["date"]; d != "" && !isoDate.MatchString(d) {
		out = append(out, fmt.Sprintf("date %q is not YYYY-MM-DD", d))
	}
	return out
}

// bodyHeading matches an h1 to h3 in an entry body: the render makes each entry
// one section under its own heading, so a heading inside a body would split it
// (and a ### ADR- heading would smuggle in a second decision).
var bodyHeading = regexp.MustCompile(`(?m)^#{1,3} `)

func checkBodyHeadings(f Fragment, rel string) []Finding {
	var out []Finding
	for _, loc := range bodyHeading.FindAllStringIndex(f.Body, -1) {
		line := f.BodyLine + strings.Count(f.Body[:loc[0]], "\n")
		out = append(out, Finding{rel, line, "a heading inside a body splits the entry: an entry is one section, titled by its frontmatter"})
	}
	return out
}

var (
	adrFileName = regexp.MustCompile(`^adr-(\d{4})-[a-z0-9][a-z0-9-]*\.md$`)
	adrBodyDate = regexp.MustCompile(`\*\*Date:\*\* (\d{4}-\d{2}-\d{2})`)
	adrDate     = regexp.MustCompile(`(?i)\*\*Date:?\*\*`)
	adrStatus   = regexp.MustCompile(`(?i)\*\*Status:?\*\*`)
)

// ScanDecisions lints the decision log's fragment directory.
func ScanDecisions() ([]Finding, error) { return scanDecisionsDir(decisionsDir()) }

// scanDecisionsDir checks every decision file: its name, its frontmatter (the
// index row's title, date, status, and summary), the Date and Status fields its
// body carries, that no two files hold one number, and that the numbers run
// from 0001 with no gap.
func scanDecisionsDir(dir string) ([]Finding, error) {
	srcs, err := readFragmentDir(dir)
	if err != nil {
		return nil, err
	}
	rel := func(name string) string { return filepath.ToSlash(filepath.Join("..", "decisions", name)) }
	var findings []Finding
	holders := map[int][]string{}
	for _, s := range srcs {
		m := adrFileName.FindStringSubmatch(s.name)
		if m == nil {
			findings = append(findings, Finding{rel(s.name), 1, "a decision is named adr-NNNN-<slug>.md (four digits, a lowercase slug)"})
			continue
		}
		n := atoi(m[1])
		holders[n] = append(holders[n], s.name)
		f, err := ParseFragment(s.name, s.text)
		if err != nil {
			findings = append(findings, Finding{rel(s.name), 1, err.Error()})
			continue
		}
		for _, msg := range checkFields(f, []string{"title", "date", "status", "summary"}, nil, nil) {
			findings = append(findings, Finding{rel(s.name), 1, msg})
		}
		if !adrDate.MatchString(f.Body) {
			findings = append(findings, Finding{rel(s.name), f.BodyLine, "the body has no **Date:** field"})
		} else if bd := adrBodyDate.FindStringSubmatch(f.Body); bd != nil && f.Fields["date"] != "" && bd[1] != f.Fields["date"] {
			findings = append(findings, Finding{rel(s.name), f.BodyLine, "the body says **Date:** " + bd[1] + " but the frontmatter says " + f.Fields["date"]})
		}
		if !adrStatus.MatchString(f.Body) {
			findings = append(findings, Finding{rel(s.name), f.BodyLine, "the body has no **Status:** field"})
		}
		findings = append(findings, checkBodyHeadings(f, rel(s.name))...)
	}
	max := 0
	for n := range holders {
		if n > max {
			max = n
		}
	}
	for n := 1; n <= max; n++ {
		names := holders[n]
		switch {
		case len(names) == 0:
			next := n + 1
			for len(holders[next]) == 0 {
				next++
			}
			findings = append(findings, Finding{rel(holders[next][0]), 1, fmt.Sprintf("ADR-%04d is missing before it: the numbers run from 0001 with no gap, so a new decision takes the next after the highest", n)})
		case len(names) > 1:
			findings = append(findings, Finding{rel(names[1]), 1, fmt.Sprintf("ADR-%04d is taken by both %s: renumber the one your branch added (make adr-renumber FROM=%04d TO=%04d) before merging main",
				n, strings.Join(names, " and "), n, max+1)})
		}
	}
	return findings, nil
}

// buildLogFileName: <YYYY-MM-DD>-<slug>.md, or the four-digit sequence the
// entries from before the split carry, which sorts ahead of every date and
// keeps them in their logged order.
var buildLogFileName = regexp.MustCompile(`^(?:(\d{4}-\d{2}-\d{2})|(\d{4}))-[a-z0-9][a-z0-9-]*\.md$`)

// buildLogSplit is the day the build log became a directory. The
// sequence-numbered entries are its history and are closed: a new entry is
// named for its date.
const buildLogSplit = "2026-10-24"

// ScanBuildLog lints the build log's fragment directory.
func ScanBuildLog() ([]Finding, error) { return scanBuildLogDir(buildLogDir()) }

func scanBuildLogDir(dir string) ([]Finding, error) {
	srcs, err := readFragmentDir(dir)
	if err != nil {
		return nil, err
	}
	rel := func(name string) string { return filepath.ToSlash(filepath.Join("..", "build-log", name)) }
	var findings []Finding
	seq := map[int]string{}
	maxSeq := 0
	for _, s := range srcs {
		m := buildLogFileName.FindStringSubmatch(s.name)
		if m == nil {
			findings = append(findings, Finding{rel(s.name), 1, "a build-log entry is named <YYYY-MM-DD>-<slug>.md (a lowercase slug)"})
			continue
		}
		f, err := ParseFragment(s.name, s.text)
		if err != nil {
			findings = append(findings, Finding{rel(s.name), 1, err.Error()})
			continue
		}
		for _, msg := range checkFields(f, []string{"title", "date"}, []string{"refs"}, []string{"form"}) {
			findings = append(findings, Finding{rel(s.name), 1, msg})
		}
		date := f.Fields["date"]
		if m[1] != "" && m[1] != date {
			findings = append(findings, Finding{rel(s.name), 1, "the file name names " + m[1] + " but its date is " + date})
		}
		if m[2] != "" {
			n := atoi(m[2])
			seq[n] = s.name
			if n > maxSeq {
				maxSeq = n
			}
			if date > buildLogSplit {
				findings = append(findings, Finding{rel(s.name), 1, "the sequence-numbered entries are the history before the split (" + buildLogSplit + "); name a new entry <YYYY-MM-DD>-<slug>.md"})
			}
		}
		switch form := f.Fields["form"]; form {
		case "":
		case "bullet":
			lead := "- **" + f.Fields["title"]
			if !strings.HasPrefix(strings.Join(strings.Fields(f.Body), " "), lead) {
				findings = append(findings, Finding{rel(s.name), f.BodyLine, "a bullet entry's body must open with \"" + lead + "\" (its bold lead is its title)"})
			}
		default:
			findings = append(findings, Finding{rel(s.name), 1, "form is \"bullet\" (the historical list entries) or absent"})
		}
		findings = append(findings, checkBodyHeadings(f, rel(s.name))...)
	}
	for n := 1; n <= maxSeq; n++ {
		if seq[n] == "" {
			next := n + 1
			for seq[next] == "" {
				next++
			}
			findings = append(findings, Finding{rel(seq[next]), 1, fmt.Sprintf("%04d is missing from the sequence-numbered history before it", n)})
		}
	}
	return findings, nil
}

// logHost is a page the docs build splices a fragment directory into: the
// directives it must carry, and the lines that would be an entry written into
// the page instead of its directory.
type logHost struct {
	page       string
	directives []string
	entry      *regexp.Regexp
}

var logHosts = []logHost{
	{filepath.Join("architecture", "build-log.md"), []string{"::build-log"}, regexp.MustCompile(`^(### |- )`)},
	{filepath.Join("architecture", "decisions.md"), []string{"::decision-index", "::decision-log"}, regexp.MustCompile(`^(### |\| \[ADR-)`)},
}

// ScanLogHosts is the monolith guard: the host pages carry their directives and
// no entries, and the anti-pattern catalog's old single file does not return.
func ScanLogHosts() ([]Finding, error) { return scanLogHosts(DocsRoot, antiPatternMonolith) }

func scanLogHosts(docsRoot, monolith string) ([]Finding, error) {
	var findings []Finding
	for _, h := range logHosts {
		b, err := os.ReadFile(filepath.Join(docsRoot, h.page))
		if err != nil {
			return nil, err
		}
		lines := strings.Split(string(b), "\n")
		seen := map[string]int{}
		inFrontmatter := len(lines) > 0 && lines[0] == "---"
		for i, line := range lines {
			if inFrontmatter {
				if i > 0 && line == "---" {
					inFrontmatter = false
				}
				continue
			}
			seen[strings.TrimSpace(line)]++
			if h.entry.MatchString(line) {
				findings = append(findings, Finding{h.page, i + 1, "an entry written into the page: each entry is one file in its fragment directory (see the page's frontmatter comment)"})
			}
		}
		for _, d := range h.directives {
			if seen[d] != 1 {
				findings = append(findings, Finding{h.page, 1, fmt.Sprintf("the page must carry %s exactly once on a line of its own (found %d), or its entries do not publish", d, seen[d])})
			}
		}
	}
	if _, err := os.Stat(monolith); err == nil {
		findings = append(findings, Finding{filepath.ToSlash(monolith), 1, "the anti-pattern catalog is one file per entry under anti-patterns/; add an entry as its own file"})
	}
	return findings, nil
}

// ScanAntiPatterns lints the adversarial-review catalog's directory.
func ScanAntiPatterns() ([]Finding, error) { return scanAntiPatternDir(antiPatternDir) }

var antiPatternHeading = regexp.MustCompile(`^## ([a-z0-9][a-z0-9-]*) \(.+\)$`)

func scanAntiPatternDir(dir string) ([]Finding, error) {
	srcs, err := readFragmentDir(dir)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for _, s := range srcs {
		if s.name == "README.md" {
			continue
		}
		id := strings.TrimSuffix(s.name, ".md")
		first, _, _ := strings.Cut(s.text, "\n")
		if m := antiPatternHeading.FindStringSubmatch(first); m == nil || m[1] != id {
			findings = append(findings, Finding{s.name, 1, "an anti-pattern entry must open with \"## " + id + " (<status>)\": the file is named for its id"})
		}
	}
	return findings, nil
}
