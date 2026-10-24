package simfleet

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

func TestPlan(t *testing.T) {
	profiles := map[string]*profile.DeviceProfile{
		"x-display": {Interfaces: []profile.Interface{{Protocol: "http", Port: 80}, {Protocol: "snmp", Port: 161}}},
		"x-panel":   {Interfaces: []profile.Interface{{Protocol: "tcp", Port: 51325}}},
		"x-bar":     {Interfaces: []profile.Interface{{Protocol: "http", Port: 443, TLS: true}}},
		"x-sensor":  {Interfaces: []profile.Interface{{Protocol: "http", Port: 80}, {Protocol: "tcp", Port: 5000}}},
		"x-cam":     {Interfaces: []profile.Interface{{Protocol: "http", Port: 443, TLS: true}}},
		"x-player":  {Interfaces: []profile.Interface{{Protocol: "http", Port: 80}}},
	}
	profileFor := func(p string) (*profile.DeviceProfile, bool) { d, ok := profiles[p]; return d, ok }
	driverOf := map[string]string{"x-display": "snmp-generic", "x-panel": "newtron-nvp", "x-bar": "kestrel-api", "x-sensor": "http-json",
		"x-cam": "a-stub", "x-player": "a-stub"}

	got, err := Plan([]Component{
		{ID: "c3", Product: "x-panel"},
		{ID: "c1", Product: "x-display"},
		{ID: "c9", Product: "x-unsimulated"},
		{ID: "c4", Product: "x-bar"},
		{ID: "c5", Product: "x-sensor"},
		{ID: "c6", Product: "x-cam"},
		{ID: "c7", Product: "x-player"},
	}, nil, Catalog{ProfileFor: profileFor, DriverOf: func(p string) string { return driverOf[p] }, PortOffset: 10000})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 6 {
		t.Fatalf("planned %d devices, want the six the simulator serves: %+v", len(got), got)
	}
	// Ordered by id, addresses handed out in that order.
	want := []struct{ id, addr string }{{"c1", "127.60.0.1"}, {"c3", "127.60.0.2"}, {"c4", "127.60.0.3"}, {"c5", "127.60.0.4"}, {"c6", "127.60.0.5"}, {"c7", "127.60.0.6"}}
	for i, w := range want {
		if got[i].ComponentID != w.id || got[i].Address != w.addr {
			t.Fatalf("device %d = %s at %s, want %s at %s", i, got[i].ComponentID, got[i].Address, w.id, w.addr)
		}
	}
	display := got[0].Endpoints
	if len(display) != 1 || display[0].Driver != "snmp-generic" ||
		display[0].Inputs["host"] != "127.60.0.1" || display[0].Inputs["port"] != "10161" || display[0].Inputs["community"] != CommunitySecret {
		t.Errorf("display endpoints = %+v, want an snmp-generic attach at the shifted port", display)
	}
	if panel := got[1].Endpoints; len(panel) != 1 || panel[0].Driver != "newtron-nvp" || panel[0].Inputs["host"] != "127.60.0.2" {
		t.Errorf("panel endpoints = %+v, want a newtron-nvp attach", panel)
	}
	// The http drivers attach with the credential the simulator's face
	// demands (#902): the bar's https on the shifted 443, the sensor's http on
	// the shifted 80.
	if bar := got[2].Endpoints; len(bar) != 1 || bar[0].Driver != "kestrel-api" || bar[0].Inputs["port"] != "10443" || bar[0].Inputs["login"] != KestrelLoginSecret {
		t.Errorf("bar endpoints = %+v, want a kestrel-api attach at the shifted 443", bar)
	}
	if sensor := got[3].Endpoints; len(sensor) != 1 || sensor[0].Driver != "http-json" || sensor[0].Inputs["port"] != "10080" || sensor[0].Inputs["key"] != CensusKeySecret {
		t.Errorf("sensor endpoints = %+v, want an http-json attach at the shifted 80", sensor)
	}
	// Drivers with no spec yet get a reachability probe: plain HTTP where the
	// device serves it, a TCP port check on a TLS-only device.
	if cam := got[4].Endpoints; len(cam) != 1 || cam[0].Transport != "tcp" || cam[0].Target != "127.60.0.5:10443" {
		t.Errorf("TLS-only device endpoints = %+v, want a tcp probe at the shifted port", cam)
	}
	if player := got[5].Endpoints; len(player) != 1 || player[0].Transport != "http" || player[0].Target != "127.60.0.6:10080" {
		t.Errorf("plain-HTTP device endpoints = %+v, want an http probe", player)
	}
}

func TestAddressesStayInTheBlock(t *testing.T) {
	for i, want := range map[int]string{0: "127.60.0.1", 249: "127.60.0.250", 250: "127.60.1.1", 600: "127.60.2.101"} {
		if got := Address(i); got != want {
			t.Errorf("Address(%d) = %s, want %s", i, got, want)
		}
	}
}

// An address already in use stays its owner's: a component wired on an
// earlier run keeps its address, and a new one takes the lowest free one,
// whatever was deleted or added in between.
func TestPlanNeverHandsOutAnAddressInUse(t *testing.T) {
	prof := &profile.DeviceProfile{Interfaces: []profile.Interface{{Protocol: "http", Port: 80}}}
	profileFor := func(string) (*profile.DeviceProfile, bool) { return prof, true }
	driverOf := func(string) string { return "" }
	// 01a and 01c were wired on an earlier run at .1 and .3; 01b (which held
	// .2) was deleted; 02b is new.
	used := map[string]bool{"127.60.0.1": true, "127.60.0.3": true}
	got, err := Plan([]Component{{ID: "02b", Product: "x"}}, used, Catalog{ProfileFor: profileFor, DriverOf: driverOf, PortOffset: 10000})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Address != "127.60.0.2" {
		t.Fatalf("plan = %+v, want 02b at the free 127.60.0.2", got)
	}
}

// The simulated block is finite; a plan past it says so rather than
// wrapping into addresses outside the block.
func TestPlanRefusesPastTheBlock(t *testing.T) {
	prof := &profile.DeviceProfile{}
	used := map[string]bool{}
	for i := 0; i < AddressCap; i++ {
		used[Address(i)] = true
	}
	_, err := Plan([]Component{{ID: "late", Product: "x"}}, used, Catalog{
		ProfileFor: func(string) (*profile.DeviceProfile, bool) { return prof, true }, DriverOf: func(string) string { return "" }})
	if err == nil {
		t.Fatal("a full block planned an address")
	}
}

// A driver that cannot be attached here (an SNMP attach whose community
// secret is missing) falls back to a probe instead of an endpoint that
// cannot resolve.
func TestPlanProbesWhenTheDriverCannotBeAttached(t *testing.T) {
	prof := &profile.DeviceProfile{Interfaces: []profile.Interface{{Protocol: "http", Port: 80}, {Protocol: "snmp", Port: 161}}}
	got, err := Plan([]Component{{ID: "c1", Product: "x"}}, nil, Catalog{
		ProfileFor: func(string) (*profile.DeviceProfile, bool) { return prof, true },
		DriverOf:   func(string) string { return "snmp-generic" },
		Attachable: func(string) bool { return false },
	})
	if err != nil {
		t.Fatal(err)
	}
	if e := got[0].Endpoints; len(e) != 1 || e[0].Driver != "" || e[0].Transport != "http" || e[0].Target != "127.60.0.1:80" {
		t.Fatalf("endpoints = %+v, want an http probe at the unshifted port", e)
	}
}

func TestFreeAddressIsTheLowestUnused(t *testing.T) {
	if a, ok := FreeAddress(map[string]bool{"127.60.0.1": true, "127.60.0.3": true}); !ok || a != "127.60.0.2" {
		t.Fatalf("FreeAddress = %s %v, want 127.60.0.2", a, ok)
	}
}
