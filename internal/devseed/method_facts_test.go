package devseed_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/devseed"
)

func methodFacts(t *testing.T) devseed.MethodFacts {
	t.Helper()
	raw, err := devseed.MethodFactsJSON()
	if err != nil {
		t.Fatal(err)
	}
	var facts devseed.MethodFacts
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	return facts
}

// TestMethodFactsListEveryAttachedMethod holds the generated table to the
// fixture it renders (#1419): every sim_endpoints entry is a row, by the
// driver it attaches or the transport it probes, naming the component it sits
// on, that component's place and the endpoint's label.
func TestMethodFactsListEveryAttachedMethod(t *testing.T) {
	t.Parallel()
	doc, err := devseed.Fixtures()
	if err != nil {
		t.Fatal(err)
	}
	facts := methodFacts(t)
	if len(facts.Methods) != len(doc.SimEndpoints) {
		t.Fatalf("the table has %d rows, the fixture %d sim_endpoints", len(facts.Methods), len(doc.SimEndpoints))
	}
	for _, se := range doc.SimEndpoints {
		name, kind := se.Driver, "driver"
		if name == "" {
			name, kind = se.Transport, "transport"
		}
		var row *devseed.MethodRow
		for i, r := range facts.Methods {
			if r.Method == name && r.Component == se.Component && r.Label == se.Label {
				row = &facts.Methods[i]
			}
		}
		if row == nil {
			t.Errorf("no row for %s %s on %s (%s)", kind, name, se.Component, se.Label)
			continue
		}
		if row.Kind != kind {
			t.Errorf("%s on %s: kind = %q, want %q", name, se.Component, row.Kind, kind)
		}
		if row.Place == "" {
			t.Errorf("%s on %s has no place", name, se.Component)
		}
		if row.Planted != (se.PlantedFault != "") {
			t.Errorf("%s on %s: planted = %v, fixture says %q", name, se.Component, row.Planted, se.PlantedFault)
		}
		if kind == "driver" && row.Transport == "" {
			t.Errorf("driver %s on %s names no transport it rides", name, se.Component)
		}
	}
}

// TestMethodFactsListEveryGap holds the table to devseed.MethodGaps: every
// listed gap is a row with its issue, so closing one changes the page.
func TestMethodFactsListEveryGap(t *testing.T) {
	t.Parallel()
	facts := methodFacts(t)
	if len(facts.Gaps) != len(devseed.MethodGaps) {
		t.Fatalf("the table has %d gaps, MethodGaps %d", len(facts.Gaps), len(devseed.MethodGaps))
	}
	for name, gap := range devseed.MethodGaps {
		var row *devseed.GapRow
		for i, g := range facts.Gaps {
			if g.Method == name {
				row = &facts.Gaps[i]
			}
		}
		if row == nil {
			t.Errorf("no gap row for %s", name)
			continue
		}
		if row.Issue != gap.Issue || row.Unbuilt != gap.Unbuilt || row.Why != gap.Why {
			t.Errorf("gap %s = %+v, MethodGaps says %+v", name, *row, gap)
		}
		if row.Why == "" {
			t.Errorf("gap %s says nothing of why it is a gap", name)
		}
	}
}

// TestMethodFactsMatchCommitted is the drift gate: a moved endpoint or a
// closed gap without make gen fails here.
func TestMethodFactsMatchCommitted(t *testing.T) {
	t.Parallel()
	fresh, err := devseed.MethodFactsJSON()
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("../../docs/src/generated/methods.json")
	if err != nil {
		t.Fatalf("read committed methods.json (run make gen and commit it): %v", err)
	}
	if string(committed) != string(fresh) {
		t.Error("docs/src/generated/methods.json drifted from the fixtures or MethodGaps: run make gen and commit the result")
	}
}
