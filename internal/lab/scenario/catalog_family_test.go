package scenario

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

// The scenario's catalog answers for a Go family's product (#2829): simulated,
// the family that serves it, its firmware, and that neither pushes nor sends.
func TestCatalogResolvesAFamilyProduct(t *testing.T) {
	cat, err := spectest.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	c := shippedOver(cat)
	for _, product := range spectest.Products {
		if !c.Simulated(product) {
			t.Errorf("%s: not simulated", product)
		}
		if name, ok := c.SimProfile(product); !ok || name != spectest.Name {
			t.Errorf("%s: sim profile = %q, %v", product, name, ok)
		}
		if got := c.Firmware(product); got != "1.2.3" {
			t.Errorf("%s: firmware = %q", product, got)
		}
	}
	if c.Pusher(spectest.Name) || c.Sender(spectest.Name) {
		t.Errorf("%s: a display that pushes or sends", spectest.Name)
	}
	if c.Simulated("nobody") {
		t.Error("an unknown product is simulated")
	}
}
