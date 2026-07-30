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
			for _, term := range Banned {
				if m := term.Pattern.FindString(line); m != "" {
					findings = append(findings, Finding{
						File: rel,
						Line: i + 1,
						Text: m + " is retired (" + term.Origin + "); write " + term.Replacement,
					})
				}
			}
		}
		return nil
	})
	return findings, err
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
