package simfleet

import (
	"reflect"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

// The planner wires a Go family's product exactly as it wires the YAML twin's
// (#2829): the same address, and the same endpoint to probe, for each product
// and with or without a driver.
func TestPlanWiresAFamilyProductAsItWiresItsYAMLTwin(t *testing.T) {
	cat, err := spectest.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range spectest.Pairs {
		for _, driver := range []string{"", "a-stub"} {
			plan := func(product string) []Device {
				got, err := Plan([]Component{{ID: "c1", Product: product}}, nil,
					Catalog{ProfileFor: cat.ProfileFor, DriverOf: func(string) string { return driver }, PortOffset: 10000})
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			g, y := plan(p.Go), plan(p.YAML)
			if len(g) != 1 || len(g[0].Endpoints) != 1 || g[0].Endpoints[0].Transport != "http" {
				t.Fatalf("%s: plan = %+v, want one http probe", p.Go, g)
			}
			if !reflect.DeepEqual(g, y) {
				t.Errorf("%s (driver %q): plan = %+v, twin %+v", p.Go, driver, g, y)
			}
		}
	}
	// A product neither kind simulates is skipped alike.
	got, err := Plan([]Component{{ID: "c1", Product: "nobody"}}, nil, Catalog{ProfileFor: cat.ProfileFor, DriverOf: func(string) string { return "" }})
	if err != nil || len(got) != 0 {
		t.Fatalf("plan of an unsimulated product = %+v, %v", got, err)
	}
}
