package docslint

import (
	"strings"
	"testing"
)

// enforceVocabulary flips the vocabulary lint from warn to red. It stays warn
// until the wave-2 vocabulary sweep lands (#428); flipping it is that PR's
// last commit. TODO(#437): flip to true.
const enforceVocabulary = false

// enforceDecisions flips the decisions-format lint from warn to red. It stays
// warn until the wave-3 decision-log hygiene PR lands (#428).
// TODO(#437): flip to true.
const enforceDecisions = false

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
