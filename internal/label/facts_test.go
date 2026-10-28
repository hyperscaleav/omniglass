package label

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sandbox"
)

// The generated rule-language artifact and the invariants that let it be
// generated at all (#701).
//
// The set of function names a rule may spell used to be transcribed four times:
// into the FuncMap, into FuncNames, into the AST allowlist, and into the prose
// that teaches the language. Three of those are now derived from [ruleFuncs] and
// the fourth is rendered from this artifact, so the tests below pin the
// derivation rather than the copies. The set itself lives in internal/sandbox
// since #618, where its allowlist invariants are pinned.

// TestEveryDocumentedExampleIsWhatTheFunctionDoes closes the last way this table
// could lie. The name set is derived, so it cannot go stale; the worked example
// beside each name is prose somebody typed, so it can. The artifact renders the
// output through the engine and this asserts the declared output is the one the
// engine produced, which means changing what a function DOES fails here rather
// than shipping a table that teaches the old behaviour.
func TestEveryDocumentedExampleIsWhatTheFunctionDoes(t *testing.T) {
	for _, f := range sandbox.Funcs() {
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
	if !slices.Equal(doc.Builtins, sandbox.Builtins()) {
		t.Errorf("facts publish builtins %v, the grammar admits %v", doc.Builtins, sandbox.Builtins())
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
