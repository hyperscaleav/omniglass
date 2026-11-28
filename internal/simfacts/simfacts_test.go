package simfacts

import (
	"os"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/device"
)

// TestFactsMatchCommitted is the drift gate: a family added, a model row added
// or a product accounted for changes the generated file, and a stale committed
// one fails here until make gen is run and the result committed.
func TestFactsMatchCommitted(t *testing.T) {
	want, err := JSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../docs/src/generated/simulation.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Error("docs/src/generated/simulation.json drifted from the family specs: run make gen and commit the result")
	}
}

// Every family and every model row the registry holds is in the facts, by
// package, and a model the seed ships carries the driver and component type the
// seed gives it.
func TestFactsListEveryFamilyAndModel(t *testing.T) {
	f, err := Of()
	if err != nil {
		t.Fatal(err)
	}
	fams := device.Families()
	if len(f.Families) != len(fams) {
		t.Fatalf("facts list %d families, the registry holds %d", len(f.Families), len(fams))
	}
	byName := map[string]Family{}
	for _, fam := range f.Families {
		byName[fam.Name] = fam
		if fam.Package == "" || fam.Summary == "" || fam.DeviceType == "" {
			t.Errorf("%s: package %q, summary %q, type %q: a family is named, summarized and typed", fam.Name, fam.Package, fam.Summary, fam.DeviceType)
		}
	}
	models, driven := 0, 0
	for _, s := range fams {
		fam, ok := byName[s.Name]
		if !ok {
			t.Errorf("family %s is not in the facts", s.Name)
			continue
		}
		if len(fam.Models) != len(s.Models) || len(fam.Interfaces) != len(s.Interfaces) || len(fam.Behaviors) != len(s.Ticks) {
			t.Errorf("%s: facts hold %d models, %d faces and %d behaviors; the spec has %d, %d and %d",
				s.Name, len(fam.Models), len(fam.Interfaces), len(fam.Behaviors), len(s.Models), len(s.Interfaces), len(s.Ticks))
		}
		for i, m := range fam.Models {
			models++
			if m.Product != s.Models[i].Product {
				t.Errorf("%s: model %d is %s, the spec's row is %s", s.Name, i, m.Product, s.Models[i].Product)
			}
			if m.Driver != "" {
				driven++
				if m.ComponentType == "" {
					t.Errorf("%s: %s has driver %s and no component type", s.Name, m.Product, m.Driver)
				}
			}
		}
	}
	if models == 0 || driven == 0 {
		t.Fatalf("facts hold %d models, %d of them with a driver", models, driven)
	}
	if strings.Count(string(mustJSON(t)), `"package"`) != len(fams) {
		t.Error("the rendered file does not hold one entry per family")
	}
}

// The coverage tables list the accounting as the code holds it, sorted.
func TestFactsListTheCoverageAccounting(t *testing.T) {
	f, err := Of()
	if err != nil {
		t.Fatal(err)
	}
	cov := device.Accounted()
	if len(f.Coverage.Unsimulated) != len(cov.Unsimulated) || len(f.Coverage.Passive) != len(cov.Passive) {
		t.Fatalf("facts hold %d unsimulated and %d passive, the accounting %d and %d",
			len(f.Coverage.Unsimulated), len(f.Coverage.Passive), len(cov.Unsimulated), len(cov.Passive))
	}
	for i := 1; i < len(f.Coverage.Passive); i++ {
		if f.Coverage.Passive[i-1].Product >= f.Coverage.Passive[i].Product {
			t.Errorf("passive products are not sorted: %v", f.Coverage.Passive)
		}
	}
	for _, a := range f.Coverage.Passive {
		if cov.Passive[a.Product] == "" || a.Why == "" {
			t.Errorf("passive %q: why %q", a.Product, a.Why)
		}
	}
}

func mustJSON(t *testing.T) []byte {
	t.Helper()
	b, err := JSON()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The facts are stable run to run: nothing a map's order or a clock decides.
func TestFactsAreStable(t *testing.T) {
	if string(mustJSON(t)) != string(mustJSON(t)) {
		t.Fatal("two renders differ")
	}
}
