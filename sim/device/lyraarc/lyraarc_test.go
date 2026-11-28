package lyraarc

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
	if len(Rows) != 1 || Rows[0].Product != "lyra-arc-a2" {
		t.Fatalf("the table holds %+v, want the lyra-arc-a2 row", Rows)
	}
	res, ok := cat.ForProduct("lyra-arc-a2")
	if !ok || res.Definition.ID() != Name {
		t.Fatalf("lyra-arc-a2 resolves to %+v, want the %s family", res.Definition, Name)
	}
	if res.Identity.Model != "Arc A2" || res.Identity.Manufacturer != "Lyra" || res.Identity.DeviceType != "ceiling-mic" {
		t.Errorf("identity = %+v", res.Identity)
	}
	f := spectest.NewFake(Spec(), "lyra-arc-a2")
	if f.Read("configuration.device.model") != "Arc A2" || f.Read("configuration.firmware.version") != Rows[0].Firmware {
		t.Errorf("the device reports model %v firmware %v, its row says %s %s", f.Read("configuration.device.model"), f.Read("configuration.firmware.version"), "Arc A2", Rows[0].Firmware)
	}
}
