package devseed

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestPlanSimFleet(t *testing.T) {
	profiles := map[string]*profile.DeviceProfile{
		"x-display": {Interfaces: []profile.Interface{{Protocol: "http", Port: 80}, {Protocol: "snmp", Port: 161}}},
		"x-panel":   {Interfaces: []profile.Interface{{Protocol: "tcp", Port: 51325}}},
		"x-bar":     {Interfaces: []profile.Interface{{Protocol: "http", Port: 443, TLS: true}}},
		"x-sensor":  {Interfaces: []profile.Interface{{Protocol: "http", Port: 80}, {Protocol: "tcp", Port: 5000}}},
	}
	profileFor := func(p string) (*profile.DeviceProfile, bool) { d, ok := profiles[p]; return d, ok }
	driverOf := map[string]string{"x-display": "snmp-generic", "x-panel": "newtron-nvp", "x-bar": "kestrel-api", "x-sensor": "http-json"}

	got, err := planSimFleet([]simComponent{
		{ID: "c3", Product: "x-panel"},
		{ID: "c1", Product: "x-display"},
		{ID: "c9", Product: "x-unsimulated"},
		{ID: "c4", Product: "x-bar"},
		{ID: "c5", Product: "x-sensor"},
	}, nil, profileFor, func(p string) string { return driverOf[p] })
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 4 {
		t.Fatalf("planned %d devices, want the four the simulator serves: %+v", len(got), got)
	}
	// Ordered by id, addresses handed out in that order.
	want := []struct{ id, addr string }{{"c1", "127.60.0.1"}, {"c3", "127.60.0.2"}, {"c4", "127.60.0.3"}, {"c5", "127.60.0.4"}}
	for i, w := range want {
		if got[i].ComponentID != w.id || got[i].Address != w.addr {
			t.Fatalf("device %d = %s at %s, want %s at %s", i, got[i].ComponentID, got[i].Address, w.id, w.addr)
		}
	}
	display := got[0].Endpoints
	if len(display) != 1 || display[0].Driver != "snmp-generic" ||
		display[0].Inputs["host"] != "127.60.0.1" || display[0].Inputs["port"] != "10161" || display[0].Inputs["community"] != simCommunitySecret {
		t.Errorf("display endpoints = %+v, want an snmp-generic attach at the shifted port", display)
	}
	if panel := got[1].Endpoints; len(panel) != 1 || panel[0].Driver != "newtron-nvp" || panel[0].Inputs["host"] != "127.60.0.2" {
		t.Errorf("panel endpoints = %+v, want a newtron-nvp attach", panel)
	}
	// Drivers with no spec yet get a reachability probe: plain HTTP where the
	// device serves it, a TCP port check on a TLS-only device.
	if bar := got[2].Endpoints; len(bar) != 1 || bar[0].Transport != "tcp" || bar[0].Target != "127.60.0.3:10443" {
		t.Errorf("TLS-only device endpoints = %+v, want a tcp probe at the shifted port", bar)
	}
	if sensor := got[3].Endpoints; len(sensor) != 1 || sensor[0].Transport != "http" || sensor[0].Target != "127.60.0.4:10080" {
		t.Errorf("plain-HTTP device endpoints = %+v, want an http probe", sensor)
	}
}

func TestSimFleetAddressesStayInTheBlock(t *testing.T) {
	for i, want := range map[int]string{0: "127.60.0.1", 249: "127.60.0.250", 250: "127.60.1.1", 600: "127.60.2.101"} {
		if got := simAddress(i); got != want {
			t.Errorf("simAddress(%d) = %s, want %s", i, got, want)
		}
	}
}

// An address already in use stays its owner's: a component wired on an
// earlier run keeps its address, and a new one takes the lowest free one,
// whatever was deleted or added in between.
func TestPlanSimFleetNeverHandsOutAnAddressInUse(t *testing.T) {
	prof := &profile.DeviceProfile{Interfaces: []profile.Interface{{Protocol: "http", Port: 80}}}
	profileFor := func(string) (*profile.DeviceProfile, bool) { return prof, true }
	driverOf := func(string) string { return "" }
	// 01a and 01c were wired on an earlier run at .1 and .3; 01b (which held
	// .2) was deleted; 02b is new.
	used := map[string]bool{"127.60.0.1": true, "127.60.0.3": true}
	got, err := planSimFleet([]simComponent{{ID: "02b", Product: "x"}}, used, profileFor, driverOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Address != "127.60.0.2" {
		t.Fatalf("plan = %+v, want 02b at the free 127.60.0.2", got)
	}
}

// The simulated block is finite; a plan past it says so rather than
// wrapping into addresses outside the block.
func TestPlanSimFleetRefusesPastTheBlock(t *testing.T) {
	prof := &profile.DeviceProfile{}
	used := map[string]bool{}
	for i := 0; i < simAddressCap; i++ {
		used[simAddress(i)] = true
	}
	_, err := planSimFleet([]simComponent{{ID: "late", Product: "x"}}, used,
		func(string) (*profile.DeviceProfile, bool) { return prof, true }, func(string) string { return "" })
	if err == nil {
		t.Fatal("a full block planned an address")
	}
}

// A driver that has a spec and simulated products is one the seed attaches,
// not probes: when a stub driver gains its spec, this fails until simAttach
// learns its inputs.
func TestSpecdDriversWithSimulatedProductsAreAttached(t *testing.T) {
	if testing.Short() {
		t.Skip("needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	drivers, err := gw.ListDrivers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	specd := map[string]bool{}
	for _, d := range drivers {
		if len(d.Spec) > 0 {
			specd[d.Name] = true
		}
	}
	products, err := gw.ListProducts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, p := range products {
		if p.DriverName == nil || !specd[*p.DriverName] {
			continue
		}
		if _, _, ok := store.ForProduct(p.Name); !ok {
			continue
		}
		checked++
		if _, ok := simAttach[*p.DriverName]; !ok {
			t.Errorf("%s is simulated and its driver %s has a spec, but the dev seed would only probe it", p.Name, *p.DriverName)
		}
	}
	if checked == 0 {
		t.Fatal("no simulated product on a spec'd driver; the check checked nothing")
	}
}
