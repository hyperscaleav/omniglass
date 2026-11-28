package lyrahx

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

// The model table is the line, each row a product the catalog resolves to the
// family with the identity its row gives.
func TestEveryRowIsAProductThatReportsItself(t *testing.T) {
	cat, err := spec.NewCatalog(nil, Spec())
	if err != nil {
		t.Fatal(err)
	}
	if len(Rows) != 1 || Rows[0].Product != "lyra-hx-1" {
		t.Fatalf("the table holds %+v, want the lyra-hx-1 row", Rows)
	}
	res, ok := cat.ForProduct("lyra-hx-1")
	if !ok || res.Definition.ID() != Name {
		t.Fatalf("lyra-hx-1 resolves to %+v, want the %s family", res.Definition, Name)
	}
	if res.Identity.Model != "HX-1" || res.Identity.Manufacturer != "Lyra" || res.Identity.DeviceType != "wireless-mic" {
		t.Errorf("identity = %+v", res.Identity)
	}
	f := spectest.NewFake(Spec(), "lyra-hx-1")
	if f.Read("configuration.device.model") != "HX-1" || f.Read("configuration.firmware.version") != Rows[0].Firmware {
		t.Errorf("the device reports model %v firmware %v, its row says %s %s", f.Read("configuration.device.model"), f.Read("configuration.firmware.version"), "HX-1", Rows[0].Firmware)
	}
}
