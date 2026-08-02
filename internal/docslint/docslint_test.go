package docslint

import (
	"os"
	"strings"
	"testing"
)

// enforceVocabulary makes the vocabulary lint a red gate. It was warn-only
// while the wave-2 sweep (#428) cleared the pre-existing findings.
const enforceVocabulary = true

// enforceDecisions makes the decisions-format lint a red gate. It was
// warn-only while the wave-3 hygiene pass (#428) cleared the backlog.
const enforceDecisions = true

// TestVocabulary scans the docs for retired vocabulary (the Banned denylist).
// Every ADR that retires a term appends its entry to Banned in the same PR,
// so a retirement can never half-land again the way ADR-0065 did (the audit
// found `datapoint` surviving on twelve pages).
func TestVocabulary(t *testing.T) {
	findings, err := ScanVocabulary()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "vocabulary", findings, enforceVocabulary)
}

// TestDecisionsFormat checks the decision log's mechanical structure: unique,
// ordered ADR numbers, an index row per entry, and Date and Status fields.
// The audit found a duplicate ADR-0036, sixteen missing index rows, and
// entries with no status at all.
func TestDecisionsFormat(t *testing.T) {
	findings, err := ScanDecisions()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "decisions-format", findings, enforceDecisions)
}

// TestBannedPatternsStaySane pins the denylist entries against over-matching:
// current vocabulary must never trip a banned pattern.
func TestBannedPatternsStaySane(t *testing.T) {
	current := []string{
		"property_type", "the property catalog", "a sample lands in metric",
		"the current value cache", "source_event_id", "omniglass node run",
		"ListShell", "FlatList", "TreeList", "UpsertPropertyType",
		"value_type on the variable", // a live column, distinct from the retired data-type usage
		// ADR-0071 redefines "template" rather than retiring the word, so the prose
		// form must stay legal: an operator really does start from a template.
		"system templates you can import",
		"a component template to clone",
		"start from a template",
	}
	for _, s := range current {
		for _, term := range Banned {
			if m := term.Pattern.FindString(s); m != "" {
				t.Errorf("banned pattern for %q matches current vocabulary %q (hit %q)", term.Replacement, s, m)
			}
		}
	}
}

func report(t *testing.T, name string, findings []Finding, enforce bool) {
	t.Helper()
	if len(findings) == 0 {
		return
	}
	var b strings.Builder
	for _, f := range findings {
		b.WriteString("\n  " + f.File + ":" + itoa(f.Line) + ": " + f.Text)
	}
	if enforce {
		t.Errorf("%d %s finding(s):%s", len(findings), name, b.String())
		return
	}
	t.Logf("WARN (not yet enforced, #437): %d %s finding(s):%s", len(findings), name, b.String())
}

// TestScanLineSkipsLinkTargets pins scanner fix 1. Six of the eight
// property_value hits in the corpus are the ADR-0047 anchor slug
// (#adr-0047-the-fields-fold-product_property-and-property_value), cited from
// six pages. That slug is an immutable historical ADR title: citing it is
// correct, and a lint that fires on it would make the corpus unfixable without
// breaking its own cross-references.
func TestScanLineSkipsLinkTargets(t *testing.T) {
	cases := []struct {
		name string
		line string
		want int
	}{
		{
			"the ADR-0047 anchor slug is a link target, not prose",
			"see [the fields fold](/architecture/decisions/#adr-0047-the-fields-fold-product_property-and-property_value)",
			0,
		},
		{
			"a bare markdown target is skipped too",
			"[x](#adr-0063-property_value-was-the-store)",
			0,
		},
		{
			"but the same term in prose still fires",
			"the `property_value` table holds the latest value",
			1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := len(scanLine(c.line)); got != c.want {
				t.Fatalf("scanLine(%q) = %d findings, want %d", c.line, got, c.want)
			}
		})
	}
}

// TestScanLineAllowsRetirementProse pins scanner fix 2. Every remaining hit for
// component_type, system_type, field_definition, and field_value is prose that
// names the term precisely in order to retire it. Without this escape, adding
// those entries turns `make test` red on the sentences that do the teaching,
// which would push authors toward deleting the explanation instead of keeping it.
func TestScanLineAllowsRetirementProse(t *testing.T) {
	retiring := []string{
		"(the `component_type` registry retired with the product catalog)",
		"replaces the old `component_type`-as-shape notion",
		"was `property_value`, built",
		"Replaces the retired `field_definition`",
		"`system_type` is superseded by the standard",
		"the former `field_value` store",
	}
	for _, line := range retiring {
		if got := scanLine(line); len(got) != 0 {
			t.Errorf("retirement prose fired the lint: %q -> %v", line, got)
		}
	}
	// The escape must be narrow: a nearby retirement word does not license an
	// unrelated current-tense claim later on the same line.
	live := "the `field_definition` registry stores the contract"
	if got := scanLine(live); len(got) == 0 {
		t.Errorf("current-tense claim did not fire: %q", live)
	}
}

// TestScanLineFirstMatchWins pins scanner fix 3: a log_datapoint line must
// report its own entry (which points at the log_line lane) and not also the
// generic *datapoint* entry, whose replacement text is wrong for it.
func TestScanLineFirstMatchWins(t *testing.T) {
	got := scanLine("the `log_datapoint` table holds a component's own words")
	if len(got) != 1 {
		t.Fatalf("got %d findings, want exactly 1: %v", len(got), got)
	}
	if !strings.Contains(got[0], "log_line") {
		t.Fatalf("finding %q does not point at the log_line lane", got[0])
	}
}

// TestScanLineEscapeIsProximityScoped pins that the retirement escape is scoped
// to the neighbourhood of the match, not the whole line. A long line can mention
// some unrelated thing that "was replaced" while still making a current-tense
// claim about a retired term, and a whole-line escape waved exactly that through.
func TestScanLineEscapeIsProximityScoped(t *testing.T) {
	drift := "The `field_definition` registry stores the contract, unlike the old alarm model which was replaced."
	if got := scanLine(drift); len(got) == 0 {
		t.Errorf("a distant retirement word exempted current-tense drift: %q", drift)
	}
	near := "the `field_definition` registry retired with the fields fold"
	if got := scanLine(near); len(got) != 0 {
		t.Errorf("an adjacent retirement marker failed to exempt: %q -> %v", near, got)
	}
}

// TestOperatorStrings enforces the denylist on the text an operator actually
// reads outside the docs tree. This is the lint that would have caught "the
// lineage cause of that intended datapoint" sitting in a seeded event_type
// description, upserted at every boot and rendered verbatim on the Event Types
// console page and in the API payload, right through a vocabulary migration
// that renamed the noun everywhere else.
func TestOperatorStrings(t *testing.T) {
	findings, err := ScanOperatorStrings()
	if err != nil {
		t.Fatal(err)
	}
	report(t, "operator-string vocabulary", findings, true)
}

// TestOperatorStringPathsResolve guards the failure mode that makes
// TestOperatorStrings meaningless: it reports zero findings both when the text
// is clean and when it read nothing at all. A reviewer read the ../../ prefixes
// as repo-relative and called the lint dead; the prefixes are right (a Go test
// runs with its package directory as the working directory, which is the same
// convention DocsRoot has always used) but nothing proved it. Now something does.
func TestOperatorStringPathsResolve(t *testing.T) {
	if len(operatorStrings) == 0 {
		t.Fatal("operatorStrings is empty, so ScanOperatorStrings can never find anything")
	}
	for _, p := range operatorStrings {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("operator-string path does not resolve from the package directory: %s (%v)", p, err)
		}
	}
}

// TestNearRetirementMarkerIsRuneSafe pins the window against multi-byte text:
// byte slicing at start-30 could split a rune and hand the matcher invalid UTF-8.
func TestNearRetirementMarkerIsRuneSafe(t *testing.T) {
	// Curly quotes and arrows are three bytes each, so a byte window would cut
	// through one of them here.
	line := "the “shape” arrow → ✓ marker sits here, and the `field_definition` registry stores it"
	if got := scanLine(line); len(got) == 0 {
		t.Errorf("multi-byte line did not fire: %q", line)
	}
	near := "“retired” → the `field_definition` contract"
	if got := scanLine(near); len(got) != 0 {
		t.Errorf("multi-byte retirement prose fired: %q -> %v", near, got)
	}
}

// TestLinkTargetHandlesNestedParens pins the stripper against a URL carrying
// parentheses, which a naive [^)]* truncates, leaving a tail that reads as prose.
func TestLinkTargetHandlesNestedParens(t *testing.T) {
	line := "see [the fold](/architecture/decisions/#adr-0047-the-fields-fold-product_property-and-property_value)"
	if got := scanLine(line); len(got) != 0 {
		t.Errorf("plain link target fired: %v", got)
	}
	nested := "see [a note](https://example.test/x_(property_value)_y) for context"
	if got := scanLine(nested); len(got) != 0 {
		t.Errorf("nested-paren link target fired: %v", got)
	}
}

// TestADR0073EntriesFire proves the two ADR-0073 denylist entries match the
// prose they were written to retire: the real clause from ADR-0067 and the real
// phrase that described interface_type before it was named the transport.
func TestADR0073EntriesFire(t *testing.T) {
	cases := []struct{ name, line, wantOrigin string }{
		{"adr-0067 clause", "exposing a `calendar` **interface whose `interface_type` is a driver** (`graph-calendar`)", "ADR-0073"},
		{"as-a variant", "an interface_type as a driver is the wrong seam", "ADR-0073"},
		{"stale registry phrase", "the protocol-and-style registry (`ssh`, `http`, `snmp`)", "ADR-0073"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var hit bool
			for _, b := range Banned {
				if b.Origin == c.wantOrigin && b.Pattern.MatchString(c.line) {
					hit = true
				}
			}
			if !hit {
				t.Fatalf("no %s entry matched: %q", c.wantOrigin, c.line)
			}
		})
	}
}

// TestCLIReferenceDescriptions fails on any generated CLI flag published with
// an empty Description cell (#472): the blank traces to a missing `doc:` tag
// on the Huma input field, which this gate makes non-recurring.
func TestCLIReferenceDescriptions(t *testing.T) {
	findings, err := ScanCLIReference()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "cli-reference-descriptions", findings, true)
}
