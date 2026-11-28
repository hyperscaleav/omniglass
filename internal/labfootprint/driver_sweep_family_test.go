package labfootprint

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

// The driver sweep counts a Go family's product as simulated (#2829): a driver
// on it is swept on it.
func TestSweepSimulatesAFamilyProduct(t *testing.T) {
	cat, err := spectest.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	simulated := simulatedBy(cat)
	for _, product := range spectest.Products {
		if !simulated(product) {
			t.Errorf("%s: not simulated", product)
		}
		targets, skips := PlanDriverSweep(map[string][]time.Duration{"d": {time.Minute}},
			[]seed.ShippedProduct{{Name: product, Driver: "d"}}, simulated, func(string) bool { return true })
		if len(targets) != 1 || len(skips) != 0 || targets[0].Product != product {
			t.Fatalf("%s: targets %+v skips %+v", product, targets, skips)
		}
	}
	if simulated("nobody") {
		t.Error("an unknown product is simulated")
	}
}
