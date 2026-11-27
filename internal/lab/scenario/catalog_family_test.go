package scenario

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

// The scenario's catalog answers for a Go family's product exactly as for the
// YAML twin's (#2829): simulated, the profile that serves it, its firmware,
// and that neither pushes nor sends.
func TestCatalogResolvesAFamilyProductAsItResolvesItsYAMLTwin(t *testing.T) {
	cat, err := spectest.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	c := shippedOver(cat)
	for _, p := range spectest.Pairs {
		if !c.Simulated(p.Go) || !c.Simulated(p.YAML) {
			t.Errorf("%s / %s: not both simulated", p.Go, p.YAML)
		}
		if name, ok := c.SimProfile(p.Go); !ok || name != spectest.Name {
			t.Errorf("%s: sim profile = %q, %v", p.Go, name, ok)
		}
		if name, ok := c.SimProfile(p.YAML); !ok || name != spectest.TwinName {
			t.Errorf("%s: sim profile = %q, %v", p.YAML, name, ok)
		}
		if g, y := c.Firmware(p.Go), c.Firmware(p.YAML); g != y || g != "1.2.3" {
			t.Errorf("%s: firmware = %q, twin %q", p.Go, g, y)
		}
	}
	for _, name := range []string{spectest.Name, spectest.TwinName} {
		if c.Pusher(name) || c.Sender(name) {
			t.Errorf("%s: a display that pushes or sends", name)
		}
	}
	if c.Simulated("nobody") {
		t.Error("an unknown product is simulated")
	}
}
