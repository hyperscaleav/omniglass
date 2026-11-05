//go:build linux

package sim

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
)

// The unclassified device answers what snmp-generic reads (#1372): MIB-II
// sysDescr and sysUpTime, resolved from the generic-device product.

func TestGenericDeviceServesSysDescrAndSysUpTime(t *testing.T) {
	ip := simtest.ClaimLoopback(t)
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	name, _, ok := store.ForProduct("generic-device")
	if !ok {
		t.Fatal("no profile resolves for the generic-device product")
	}
	e := NewEngine(store, profiles.Templates())
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	if err := e.Sync([]DeviceSnapshot{{DeviceID: "generic-1", Hostname: "generic-1", LoopbackIP: ip, ProfileName: name, Version: 1}}); err != nil {
		t.Fatal(err)
	}
	got := beamSNMP(t, deviceByProfile(t, e, name), "1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0")
	if got["1.3.6.1.2.1.1.1.0"] != "Generic SNMP Device" {
		t.Errorf("sysDescr = %q, want Generic SNMP Device", got["1.3.6.1.2.1.1.1.0"])
	}
	if _, ok := got["1.3.6.1.2.1.1.3.0"]; !ok {
		t.Errorf("sysUpTime missing from %v", got)
	}
}
