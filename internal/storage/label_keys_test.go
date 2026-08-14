package storage

import (
	"encoding/json"
	"os"
	"testing"
)

// The data map's key set is declared once (#729), and these pin the derivation
// rather than the copies: the map a rule executes over, the artifact the docs
// render, and the API descriptions that name the keys are all readers of
// componentLabelKeys and its two siblings.

// TestTheBuiltMapIsExactlyTheDeclaredKeys is the divergence gate the issue asks
// for. It is structural today (labelData ranges the declaration), and that is
// the point: it fails the moment somebody adds a key back into a map literal, or
// gives a kind an accessor the declaration does not carry, which is how all three
// previous changes to this map were made.
func TestTheBuiltMapIsExactlyTheDeclaredKeys(t *testing.T) {
	built := map[string][]string{
		"component": keysOfData(componentLabelData(&Component{Name: "display-1"}, componentLabelInputs{}, componentPlacement{})),
		"system":    keysOfData(systemLabelData(&System{Name: "boardroom"}, systemLabelInputs{}, systemPlacement{})),
		"location":  keysOfData(locationLabelData(&Location{Name: "hq"}, locationLabelInputs{})),
	}
	for _, kind := range LabelDataKinds {
		declared := LabelDataKeys(kind)
		if len(declared) == 0 {
			t.Errorf("%s declares no keys", kind)
			continue
		}
		got := built[kind]
		if len(got) != len(declared) {
			t.Errorf("%s builds %d keys and declares %d: %v vs %v", kind, len(got), len(declared), got, declared)
			continue
		}
		have := map[string]bool{}
		for _, k := range got {
			have[k] = true
		}
		for _, k := range declared {
			if !have[k] {
				t.Errorf("%s declares %q, which the built map does not carry", kind, k)
			}
		}
	}
}

func keysOfData(d map[string]string) []string {
	out := make([]string, 0, len(d))
	for k := range d {
		out = append(out, k)
	}
	return out
}

// TestEveryDeclaredKeyCarriesASummary keeps the render worth reading: the docs
// table has a description column, so a key with no summary is an empty cell on a
// published page. One of these keys (Ordinal) reports something subtler than its
// name suggests, which is the reason the render is a table rather than a list.
func TestEveryDeclaredKeyCarriesASummary(t *testing.T) {
	raw, err := LabelDataFactsJSON()
	if err != nil {
		t.Fatalf("LabelDataFactsJSON: %v", err)
	}
	var doc labelDataDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("label data facts JSON does not parse: %v", err)
	}
	if len(doc.Kinds) != len(LabelDataKinds) {
		t.Fatalf("facts carry %d kinds, %d are declared", len(doc.Kinds), len(LabelDataKinds))
	}
	for i, k := range doc.Kinds {
		if k.Kind != LabelDataKinds[i] {
			t.Errorf("facts kind %d is %q, want %q (the order is the documented one)", i, k.Kind, LabelDataKinds[i])
		}
		for _, key := range k.Keys {
			if key.Summary == "" {
				t.Errorf("%s.%s has no summary; the docs table would render an empty cell", k.Kind, key.Name)
			}
		}
	}
}

// TestLabelDataFactsMatchesCommitted is the drift gate, the same shape as the
// seed, schema and rule-language artifacts: the committed JSON the docs import
// must equal a fresh in-process render, or a key was added without make gen and
// the published data map is a release behind the engine.
func TestLabelDataFactsMatchesCommitted(t *testing.T) {
	fresh, err := LabelDataFactsJSON()
	if err != nil {
		t.Fatalf("LabelDataFactsJSON: %v", err)
	}
	committed, err := os.ReadFile("../../docs/src/generated/labeldata.json")
	if err != nil {
		t.Fatalf("read committed labeldata.json (run make gen and commit it): %v", err)
	}
	if string(committed) != string(fresh) {
		t.Error("docs/src/generated/labeldata.json drifted from the declared data map: run make gen and commit the result")
	}
}
