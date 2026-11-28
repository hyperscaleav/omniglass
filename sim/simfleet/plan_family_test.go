package simfleet

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

// The planner wires a Go family's product (#2829): an address, and one http
// endpoint to probe, for each product and with or without a driver.
func TestPlanWiresAFamilyProduct(t *testing.T) {
	cat, err := spectest.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, product := range spectest.Products {
		for _, driver := range []string{"", "a-stub"} {
			got, err := Plan([]Component{{ID: "c1", Product: product}}, nil,
				Catalog{ProfileFor: cat.ProfileFor, DriverOf: func(string) string { return driver }, PortOffset: 10000})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || len(got[0].Endpoints) != 1 || got[0].Endpoints[0].Transport != "http" {
				t.Fatalf("%s (driver %q): plan = %+v, want one http probe", product, driver, got)
			}
		}
	}
	// A product no family simulates is skipped.
	got, err := Plan([]Component{{ID: "c1", Product: "nobody"}}, nil, Catalog{ProfileFor: cat.ProfileFor, DriverOf: func(string) string { return "" }})
	if err != nil || len(got) != 0 {
		t.Fatalf("plan of an unsimulated product = %+v, %v", got, err)
	}
}
