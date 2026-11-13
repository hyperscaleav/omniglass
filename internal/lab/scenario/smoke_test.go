package scenario_test

// An external test package: the smoke size lives in package lab, which
// imports scenario to resolve a scale lab, so a test in package scenario
// cannot import it back.

import (
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/lab"
	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
)

// missingProducts is every product the manifest's standards name that the
// plan seeds none of, in name order.
func missingProducts(m *scenario.Manifest, p *scenario.Plan) []string {
	seeded := map[string]bool{}
	for _, c := range p.Totals.Products {
		if c.Count > 0 {
			seeded[c.Name] = true
		}
	}
	named := map[string]bool{}
	for _, st := range m.Standards {
		for _, ref := range st.Products {
			named[ref.Name] = true
		}
	}
	var missing []string
	for name := range named {
		if !seeded[name] {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	return missing
}

// The smoke rung of the measuring ladder (#1719) is corporate at
// lab.SmokeSystems: the smallest round size past the last one that drops a
// product, so the smoke run exercises every simulator profile the scale
// levels do. At 16 and 17 the headquarters is too small to place a divisible
// conference room, and its microphone, DSP and amplifier are never seeded;
// from 18 on, through every rung of the ladder, every product is.
func TestCorporateSmokeSizeSeedsEveryProduct(t *testing.T) {
	m, err := scenario.Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := scenario.ShippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	at := func(n int) []string {
		t.Helper()
		p, err := scenario.Resolve(m, cat, n)
		if err != nil {
			t.Fatal(err)
		}
		return missingProducts(m, p)
	}
	for _, n := range []int{lab.SmokeSystems, 300, 1000, 3000} {
		if missing := at(n); len(missing) > 0 {
			t.Errorf("corporate at %d seeds none of %v", n, missing)
		}
	}
	for n := 18; n < lab.SmokeSystems; n++ {
		if missing := at(n); len(missing) > 0 {
			t.Errorf("corporate at %d seeds none of %v: the last size that drops a product moved, so revisit the smoke size", n, missing)
		}
	}
	for _, n := range []int{16, 17} {
		if missing := at(n); len(missing) == 0 {
			t.Errorf("corporate at %d seeds every product: the gap the smoke size clears closed, so revisit it", n)
		}
	}
}
