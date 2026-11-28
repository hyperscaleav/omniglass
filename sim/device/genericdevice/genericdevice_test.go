package genericdevice

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

// The device answers the two objects every agent does and nothing else: no
// vendor arc, no route, no command, no tick.
func TestItAnswersSysDescrAndSysUpTimeAndNothingElse(t *testing.T) {
	s := Spec()
	f := spectest.NewFake(s, Name)
	oids := s.Surface(f).OIDs
	if len(oids) != 2 || oids[0].OID != "1.3.6.1.2.1.1.1.0" || oids[1].OID != "1.3.6.1.2.1.1.3.0" {
		t.Fatalf("OIDs = %+v, want sysDescr and sysUpTime", oids)
	}
	if got := oids[0].Get(); got != "Generic SNMP Device" {
		t.Errorf("sysDescr = %v, want the model", got)
	}
	if len(s.HTTP) != 0 || len(s.Commands) != 0 || len(s.Ticks) != 0 {
		t.Errorf("the device serves %d routes, %d commands and %d ticks, want none", len(s.HTTP), len(s.Commands), len(s.Ticks))
	}
}

// sysUpTime is the uptime in hundredths of a second, read from the boot anchor.
func TestSysUpTimeCountsFromTheBootAnchor(t *testing.T) {
	f := spectest.NewFake(Spec(), Name)
	_ = f.Write(pathUptime, int64(90))
	if got := Spec().Surface(f).OIDs[1].Get(); got != uint32(9000) {
		t.Errorf("sysUpTime = %v, want 9000 ticks", got)
	}
}

// A family of one is named for its product, so the product resolves to the
// family itself and carries the row's identity.
func TestTheFamilyOfOneIsItsOwnProduct(t *testing.T) {
	cat, err := spec.NewCatalog(nil, Spec())
	if err != nil {
		t.Fatal(err)
	}
	r, ok := cat.ForProduct(Name)
	if !ok || r.Definition.ID() != Name || r.Identity.Model != "Generic SNMP Device" || r.Model.Product != Name {
		t.Fatalf("%s resolves to %+v", Name, r)
	}
}
