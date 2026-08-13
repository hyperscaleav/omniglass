package accesspath_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/accesspath"
)

// The tree walk, tested without a database. The documents below are the shapes
// Postgres really emits (trimmed of the fields nothing here reads), including
// the two that a name-only assertion would wave through.

const indexScanDoc = `[{"Plan": {
	"Node Type": "Hash Join", "Disabled": false,
	"Plans": [
		{"Node Type": "Index Scan", "Subplan Name": "InitPlan 2", "Index Name": "property_type_handle_key",
		 "Relation Name": "property_type", "Alias": "property_type", "Disabled": false,
		 "Index Cond": "(name = 'health'::text)"},
		{"Node Type": "Merge Join", "Disabled": false, "Plans": [
			{"Node Type": "Index Scan", "Index Name": "property_system_owner_idx",
			 "Relation Name": "property", "Alias": "v", "Disabled": false,
			 "Index Cond": "(property_type_id = (InitPlan 2).col1)"}
		]}
	]}}]`

const disabledSeqScanDoc = `[{"Plan": {
	"Node Type": "Hash Join", "Disabled": false,
	"Plans": [
		{"Node Type": "Seq Scan", "Relation Name": "property", "Alias": "v", "Disabled": true,
		 "Filter": "(property_type_id = (InitPlan 2).col1)"}
	]}}]`

const walkedNotSearchedDoc = `[{"Plan": {
	"Node Type": "Nested Loop", "Disabled": false,
	"Plans": [
		{"Node Type": "Index Scan", "Index Name": "property_system_owner_idx",
		 "Relation Name": "property", "Alias": "v", "Disabled": false,
		 "Filter": "((property_type_id)::text = ((InitPlan 2).col1)::text)"}
	]}}]`

func TestParseCollectsEveryScanInTreeOrder(t *testing.T) {
	plan, err := accesspath.Parse([]byte(indexScanDoc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(plan) != 2 {
		t.Fatalf("parsed %d scans (%s), want the two relations the plan scans", len(plan), plan)
	}
	if plan[0].Relation != "property_type" || plan[1].Relation != "property" {
		t.Errorf("scans came back as %s, want them in tree order", plan)
	}
	got := plan[1]
	if got.Node != "Index Scan" || got.Index != "property_system_owner_idx" || got.Alias != "v" {
		t.Errorf("property scan = %+v, want the index scan's node, index and alias", got)
	}
	if got.Cond != "(property_type_id = (InitPlan 2).col1)" {
		t.Errorf("index condition = %q, want the condition the plan carries", got.Cond)
	}
	if got.Disabled {
		t.Errorf("scan reported disabled: %s", got)
	}
	if s := plan.Scans("property"); len(s) != 1 || s[0] != got {
		t.Errorf("Scans(property) = %v, want exactly the one scan of it", s)
	}
	if s := plan.Scans("nothing_here"); len(s) != 0 {
		t.Errorf("Scans of a relation the plan never touches = %v, want none", s)
	}
}

func TestMustReachAcceptsASearchedIndex(t *testing.T) {
	plan, err := accesspath.Parse([]byte(indexScanDoc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ok, why := plan.MustReach("property", "property_system_owner_idx"); !ok {
		t.Errorf("MustReach refused a searched index scan: %s", why)
	}
}

// The three ways the answer is no, each of which a `pg_indexes` catalog test
// answers yes to.
func TestMustReachRefusesTheThreeSilentBreaks(t *testing.T) {
	cases := []struct {
		name, doc, want string
	}{
		{"the index is gone, so the only path left is a disabled sequential scan", disabledSeqScanDoc, "not by"},
		{"the index is named but walked rather than searched", walkedNotSearchedDoc, "no index condition"},
		{"the relation is not scanned at all", `[{"Plan": {"Node Type": "Result", "Disabled": false}}]`, "never scans"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan, err := accesspath.Parse([]byte(c.doc))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			ok, why := plan.MustReach("property", "property_system_owner_idx")
			if ok {
				t.Fatalf("MustReach accepted %s", c.name)
			}
			if !strings.Contains(why, c.want) {
				t.Errorf("refusal said %q, want it to name the defect (%q)", why, c.want)
			}
		})
	}
}

// A disabled node is refused even when it IS the right index: the planner
// reaching an index only because everything else was priced out of the market
// is not the same as the index being usable.
func TestMustReachRefusesADisabledIndexScan(t *testing.T) {
	const doc = `[{"Plan": {"Node Type": "Index Scan", "Index Name": "property_system_owner_idx",
		"Relation Name": "property", "Disabled": true, "Index Cond": "(property_type_id = $1)"}}]`
	plan, err := accesspath.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ok, why := plan.MustReach("property", "property_system_owner_idx"); ok {
		t.Errorf("MustReach accepted a disabled node (%s)", why)
	}
}

func TestParseRejectsWhatIsNotAPlan(t *testing.T) {
	if _, err := accesspath.Parse([]byte(`not json`)); err == nil {
		t.Error("Parse accepted a document that is not EXPLAIN output; a silent empty plan would pass every assertion built on it")
	}
}
