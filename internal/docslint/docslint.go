// Package docslint holds the documentation lint suite: mechanical checks that
// keep the hand-written docs from drifting away from the code, run as ordinary
// Go tests so the `make test` gate (and `/ship-slice`) inherits them.
//
// The suite grew out of the 2026-07-30 drift audit, which found that the two
// fully generated doc artifacts (the ERD, the CLI reference) were accurate
// while nearly every hand-written claim about a built surface had drifted.
// Each lint here turns one audited drift class into a check.
//
// Slice 1 (#437): the banned-vocabulary lint and the decisions-format lint.
// Later slices add route, permission, make-target, env-var, and file-path
// lints, and a storage-table lint over the generated schema facts.
package docslint

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DocsRoot is the content root the lints scan, relative to this package.
var DocsRoot = filepath.Join("..", "..", "docs", "src", "content", "docs")

// BannedTerm is a retired identifier or vocabulary item that must not appear
// in current-tense documentation. Every ADR that retires a term appends an
// entry here in the same PR (the rename ripple).
type BannedTerm struct {
	// Pattern matches the retired term. Word-ish boundaries are the entry's
	// own responsibility so each term can decide how strict to be.
	Pattern *regexp.Regexp
	// Replacement names what to write instead.
	Replacement string
	// Origin is the decision that retired the term.
	Origin string
}

// Banned is the denylist. Order is presentation only.
var Banned = []BannedTerm{
	{
		// Split out of the generic datapoint entry below and placed BEFORE it,
		// because first-match-wins and the generic replacement is wrong here: a
		// log_datapoint did not become a property or a sample, it was retired
		// into a different lane entirely (ADR-0066).
		Pattern:     regexp.MustCompile(`(?i)log_datapoints?`),
		Replacement: "log_line (the raw ingest lane) or event (the derived occurrence)",
		Origin:      "ADR-0066",
	},
	{
		// The datapoint noun family: datapoint, datapoints, datapoint_type,
		// metric_datapoint, state_datapoint.
		Pattern:     regexp.MustCompile(`(?i)[a-z_]*datapoint[a-z_]*`),
		Replacement: "property (the signal), sample (the observation), or current value (the latest)",
		Origin:      "ADR-0065",
	},
	{
		Pattern:     regexp.MustCompile(`caused_by_event_id`),
		Replacement: "source_event_id",
		Origin:      "ADR-0066",
	},
	{
		Pattern:     regexp.MustCompile(`--mode[ =]node`),
		Replacement: "omniglass node run",
		Origin:      "ADR-0058",
	},
	{
		Pattern:     regexp.MustCompile(`\bListView\b`),
		Replacement: "ListShell with a FlatList or TreeList body",
		Origin:      "the list-surface split (status.mdx)",
	},
	{
		Pattern:     regexp.MustCompile(`\bUpsertOfficial\b`),
		Replacement: "the per-registry upserts (UpsertPropertyType and siblings)",
		Origin:      "internal/storage/registries.go",
	},

	// The estate-model wave. Each of these was retired by an ADR that shipped
	// without its denylist entry, which is why the terms survived in the docs
	// long enough for the 2026-07-30 audit to find them.
	{
		Pattern:     regexp.MustCompile(`\bcomponent_type\b`),
		Replacement: "product (the shape a component points at)",
		Origin:      "ADR-0047",
	},
	{
		Pattern:     regexp.MustCompile(`\bsystem_type\b`),
		Replacement: "standard",
		Origin:      "ADR-0048",
	},
	{
		Pattern:     regexp.MustCompile(`\bfield_definition\b`),
		Replacement: "product_property (the declared-property contract)",
		Origin:      "ADR-0047",
	},
	{
		Pattern:     regexp.MustCompile(`\bfield_value\b`),
		Replacement: "property (the value store)",
		Origin:      "ADR-0047",
	},

	// The ADR-0063 name foundation: the registry takes the _type suffix, the
	// bare noun holds the data.
	{
		Pattern:     regexp.MustCompile(`\bproperty_value\b`),
		Replacement: "property (the latest-value store)",
		Origin:      "ADR-0063",
	},
	{
		// Safe against property_type_id, which does not contain property_id.
		Pattern:     regexp.MustCompile(`\bproperty_id\b`),
		Replacement: "property_type_id",
		Origin:      "ADR-0063",
	},
}

// vocabularyAllowed lists files (relative to DocsRoot) exempt from the
// banned-vocabulary lint: historical records where a retired term was true at
// the time it was written, and generated files whose fix belongs in the
// generator's source.
var vocabularyAllowed = map[string]bool{
	filepath.Join("architecture", "decisions.md"): true,
	filepath.Join("architecture", "status.mdx"):   true,
	filepath.Join("reference", "cli", "index.md"): true,
}

// operatorStrings are the non-docs files whose text reaches an OPERATOR: seed
// descriptions are upserted into registries at every boot and render verbatim
// on catalog pages and in API payloads, the console nav labels and hints are
// read on every page, and the README is the first thing a reader of the public
// repo sees. The docs lint cannot see any of them (DocsRoot is the content
// tree), which is how "intended datapoint" survived on a live console page
// through a whole vocabulary migration.
//
// Deliberately a short explicit list rather than a tree walk: the goal is the
// text an operator reads, not every identifier in the codebase. Renaming
// internal identifiers is a code change with its own ripple, not a lint.
var operatorStrings = []string{
	filepath.Join("..", "..", "internal", "seed", "event_types.yaml"),
	filepath.Join("..", "..", "internal", "seed", "properties.yaml"),
	filepath.Join("..", "..", "internal", "seed", "command_types.yaml"),
	filepath.Join("..", "..", "internal", "seed", "interface_types.yaml"),
	filepath.Join("..", "..", "internal", "seed", "roles.yaml"),
	filepath.Join("..", "..", "web", "src", "lib", "nav.ts"),
	filepath.Join("..", "..", "README.md"),
}

// ScanOperatorStrings applies the same denylist to the operator-visible text
// outside the docs tree. Reported separately from ScanVocabulary so a failure
// says which surface drifted.
func ScanOperatorStrings() ([]Finding, error) {
	var findings []Finding
	for _, path := range operatorStrings {
		b, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue // a seed file that has not been added yet is not a lint failure
		}
		if err != nil {
			return nil, err
		}
		for i, line := range strings.Split(string(b), "\n") {
			for _, text := range scanLine(line) {
				findings = append(findings, Finding{File: filepath.Base(path), Line: i + 1, Text: text})
			}
		}
	}
	return findings, nil
}

// Finding is one lint hit.
type Finding struct {
	File string // path relative to DocsRoot
	Line int    // 1-indexed
	Text string // what and why
}

// walkDocs yields every .md/.mdx file under DocsRoot as (relative path, content).
func walkDocs(fn func(rel string, content string) error) error {
	return filepath.Walk(DocsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext != ".md" && ext != ".mdx" {
			return nil
		}
		rel, err := filepath.Rel(DocsRoot, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return fn(rel, string(b))
	})
}

// ScanVocabulary returns every banned-term hit in the docs content, excluding
// the allowlisted historical and generated files.
func ScanVocabulary() ([]Finding, error) {
	var findings []Finding
	err := walkDocs(func(rel, content string) error {
		if vocabularyAllowed[rel] {
			return nil
		}
		for i, line := range strings.Split(content, "\n") {
			for _, text := range scanLine(line) {
				findings = append(findings, Finding{File: rel, Line: i + 1, Text: text})
			}
		}
		return nil
	})
	return findings, err
}

// linkTarget matches a markdown link destination: the (...) of [text](dest).
// A destination is an address, not prose, and an ADR anchor slug is the ADR's
// own immutable historical title, so citing one must never trip the lint that
// retired the term the title contains.
// The inner alternation tolerates one level of nested parentheses, which real
// URLs do carry (a wiki link ending in "(disambiguation)"). A naive [^)]* stops
// at the first inner ")" and leaves a dangling tail that can look like prose.
var linkTarget = regexp.MustCompile(`\]\((?:[^()]|\([^()]*\))*\)`)

// retiringProse marks prose that names a retired term in order to RETIRE it,
// which is the one context where writing the dead word is the correct thing to
// do. Without this the denylist fires on the very sentences that teach the
// rename, and the cheapest way to green the build becomes deleting the
// explanation.
var retiringProse = regexp.MustCompile(`(?i)\b(retire[ds]?|retirement|was|were|old|former(ly)?|replaces?d?|supersed(e|es|ed)|no longer|instead of|renamed)\b`)

// retirementWindow is how far either side of a banned term the retirement
// marker must sit for the exemption to apply. Scoped rather than whole-line on
// purpose: a long line can easily mention some unrelated thing that "was
// replaced" while still making a current-tense claim about the retired term,
// and a whole-line escape would wave that through. Wide enough for the real
// phrasings ("the `component_type` registry retired with the product catalog"),
// narrow enough that a clause about something else does not license it. Every
// real phrasing in the corpus puts the marker within ~25 characters ("the
// `component_type` registry retired with...", "replaces the old `component_type`",
// "was `property_value`, built"), so 30 fits them all while a mention two
// clauses away does not reach.
const retirementWindow = 30

// nearRetirementMarker reports whether a retirement word sits within
// retirementWindow CHARACTERS of the match at byte offsets [start, end).
//
// The window is counted in runes, not bytes, so a multi-byte character near the
// boundary cannot split mid-rune and hand the matcher invalid UTF-8. The docs
// carry plenty of them (curly quotes, arrows), so this is a live concern rather
// than a theoretical one.
func nearRetirementMarker(prose string, start, end int) bool {
	before := []rune(prose[:start])
	if len(before) > retirementWindow {
		before = before[len(before)-retirementWindow:]
	}
	after := []rune(prose[end:])
	if len(after) > retirementWindow {
		after = after[:retirementWindow]
	}
	return retiringProse.MatchString(string(before)) || retiringProse.MatchString(string(after))
}

// scanLine returns one message per banned term found in a single line of docs
// prose, after the link-target and retirement-prose exemptions. Pure: no I/O,
// which is what makes the exemptions testable without a corpus.
//
// First match wins: the entries are ordered specific-before-generic, so a
// log_datapoint line reports the log_line lane rather than also reporting the
// generic datapoint entry, whose replacement text is wrong for it.
func scanLine(line string) []string {
	prose := linkTarget.ReplaceAllString(line, "")
	for _, term := range Banned {
		loc := term.Pattern.FindStringIndex(prose)
		if loc == nil {
			continue
		}
		if nearRetirementMarker(prose, loc[0], loc[1]) {
			continue // named in order to retire it
		}
		return []string{prose[loc[0]:loc[1]] + " is retired (" + term.Origin + "); write " + term.Replacement}
	}
	return nil
}

var (
	adrHeading  = regexp.MustCompile(`(?m)^### ADR-(\d{4})\b`)
	adrIndexRow = regexp.MustCompile(`(?m)^\|\s*\[ADR-(\d{4})\]`)
	adrDate     = regexp.MustCompile(`(?i)\*\*Date:?\*\*`)
	adrStatus   = regexp.MustCompile(`(?i)\*\*Status:?\*\*`)
)

// ScanDecisions checks the decision log's structure: unique ADR numbers in
// ascending order, an index-table row per entry, and Date and Status fields on
// each entry. (The status vocabulary itself is prose; uniqueness and presence
// are what rot mechanically.)
func ScanDecisions() ([]Finding, error) {
	path := filepath.Join(DocsRoot, "architecture", "decisions.md")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(b)
	rel := filepath.Join("architecture", "decisions.md")
	var findings []Finding

	indexed := map[string]bool{}
	for _, m := range adrIndexRow.FindAllStringSubmatch(content, -1) {
		indexed[m[1]] = true
	}

	locs := adrHeading.FindAllStringSubmatchIndex(content, -1)
	seen := map[string]int{}
	prev := 0
	for i, loc := range locs {
		num := content[loc[2]:loc[3]]
		line := 1 + strings.Count(content[:loc[0]], "\n")
		if firstLine, dup := seen[num]; dup {
			findings = append(findings, Finding{rel, line, "duplicate ADR-" + num + " (first at line " + itoa(firstLine) + ")"})
		}
		seen[num] = line
		n := atoi(num)
		if n < prev {
			findings = append(findings, Finding{rel, line, "ADR-" + num + " is out of numeric order"})
		}
		if n > prev {
			prev = n
		}
		if !indexed[num] {
			findings = append(findings, Finding{rel, line, "ADR-" + num + " has no index-table row"})
		}
		end := len(content)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		entry := content[loc[0]:end]
		if !adrDate.MatchString(entry) {
			findings = append(findings, Finding{rel, line, "ADR-" + num + " has no Date field"})
		}
		if !adrStatus.MatchString(entry) {
			findings = append(findings, Finding{rel, line, "ADR-" + num + " has no Status field"})
		}
	}
	return findings, nil
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
