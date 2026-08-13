package label

import (
	"encoding/json"
	"os"
	"testing"
	"text/template"
)

// The generated rule-language artifact and the invariants that let it be
// generated at all (#701).
//
// The set of function names a rule may spell used to be transcribed four times:
// into the FuncMap, into FuncNames, into the AST allowlist, and into the prose
// that teaches the language. Three of those are now derived from [ruleFuncs] and
// the fourth is rendered from this artifact, so the tests below pin the
// derivation rather than the copies.

// TestAllowlistIsTheFuncMapPlusTheDocumentedBuiltins is the safety property
// ADR-0098 argues for, stated as a test rather than as a habit: a name is usable
// only if the FuncMap defines it AND the grammar admits it, and the two sets may
// differ only by the builtins the parser would accept whatever the FuncMap says.
//
// It is checked in BOTH directions, which the transcribed version never was. A
// FuncMap entry the allowlist omits parses nowhere (a function published and
// unreachable); an allowlist entry the FuncMap omits is a hole that widens the
// grammar the moment text/template grows a builtin by that name.
func TestAllowlistIsTheFuncMapPlusTheDocumentedBuiltins(t *testing.T) {
	builtins := map[string]bool{
		"and": true, "or": true, "not": true,
		"eq": true, "ne": true, "lt": true, "le": true, "gt": true, "ge": true,
	}
	funcs := New(nil).funcs

	for name := range funcs {
		if !allowedFuncs[name] {
			t.Errorf("%q is in the FuncMap but not in the AST allowlist: a rule naming it is refused by checkTree", name)
		}
	}
	for name := range allowedFuncs {
		if _, ours := funcs[name]; ours {
			continue
		}
		if !builtins[name] {
			t.Errorf("%q is in the AST allowlist but is neither a FuncMap entry nor one of the admitted builtins", name)
		}
	}
	for name := range builtins {
		if !allowedFuncs[name] {
			t.Errorf("builtin %q is documented as admitted but the allowlist refuses it", name)
		}
	}
}

// TestFuncNamesIsTheFuncMapsOwnKeys pins the other half of the derivation: the
// published order is a list, and a list can go stale against the map it claims
// to name. Order is a documentation choice and is not asserted here; membership
// is not.
func TestFuncNamesIsTheFuncMapsOwnKeys(t *testing.T) {
	funcs := New(nil).funcs
	published := FuncNames()
	if len(published) != len(funcs) {
		t.Fatalf("FuncNames() has %d names, the FuncMap has %d entries: %v vs %v", len(published), len(funcs), published, keysOf(funcs))
	}
	seen := map[string]bool{}
	for _, name := range published {
		if seen[name] {
			t.Errorf("FuncNames() publishes %q twice", name)
		}
		seen[name] = true
		if _, ok := funcs[name]; !ok {
			t.Errorf("FuncNames() publishes %q, which the FuncMap does not define", name)
		}
	}
}

func keysOf(m template.FuncMap) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestEveryDocumentedExampleIsWhatTheFunctionDoes closes the last way this table
// could lie. The name set is derived, so it cannot go stale; the worked example
// beside each name is prose somebody typed, so it can. The artifact renders the
// output through the engine and this asserts the declared output is the one the
// engine produced, which means changing what a function DOES fails here rather
// than shipping a table that teaches the old behaviour.
func TestEveryDocumentedExampleIsWhatTheFunctionDoes(t *testing.T) {
	for _, f := range ruleFuncs {
		got, err := renderExample(f)
		if err != nil {
			t.Errorf("%s: rendering the documented example failed: %v", f.Name, err)
			continue
		}
		if got != f.ExampleOut {
			t.Errorf("%s(%q) = %q, the docs table says %q", f.Name, f.ExampleIn, got, f.ExampleOut)
		}
	}
}

// TestFactsCarryEveryPublishedFunction is the artifact's ground truth: the docs
// table is rendered from this JSON, so a function missing here is a function the
// rule-language page does not teach, and a summary is what makes the row worth
// reading rather than a bare name.
func TestFactsCarryEveryPublishedFunction(t *testing.T) {
	raw, err := FactsJSON()
	if err != nil {
		t.Fatalf("FactsJSON: %v", err)
	}
	var doc struct {
		Functions []struct {
			Name    string `json:"name"`
			Summary string `json:"summary"`
		} `json:"functions"`
		Builtins []string `json:"builtins"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("label facts JSON does not parse: %v", err)
	}
	if len(doc.Functions) != len(FuncNames()) {
		t.Fatalf("facts carry %d functions, FuncNames publishes %d", len(doc.Functions), len(FuncNames()))
	}
	for i, name := range FuncNames() {
		if doc.Functions[i].Name != name {
			t.Errorf("facts function %d is %q, FuncNames publishes %q", i, doc.Functions[i].Name, name)
		}
		if doc.Functions[i].Summary == "" {
			t.Errorf("facts function %q has no summary; the docs table would render an empty cell", name)
		}
	}
	for _, b := range doc.Builtins {
		if !allowedFuncs[b] {
			t.Errorf("facts publish builtin %q, which the allowlist refuses", b)
		}
	}
}

// TestLabelFactsMatchesCommitted is the drift gate, the same shape as the seed
// and schema artifacts: the committed JSON the docs import must equal a fresh
// in-process render, or a function was added without make gen and the published
// rule language is a release behind the engine.
func TestLabelFactsMatchesCommitted(t *testing.T) {
	fresh, err := FactsJSON()
	if err != nil {
		t.Fatalf("FactsJSON: %v", err)
	}
	committed, err := os.ReadFile("../../docs/src/generated/label.json")
	if err != nil {
		t.Fatalf("read committed label.json (run make gen and commit it): %v", err)
	}
	if string(committed) != string(fresh) {
		t.Error("docs/src/generated/label.json drifted from the engine's function set: run make gen and commit the result")
	}
}
