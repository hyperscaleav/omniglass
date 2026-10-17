package simnode

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

func TestParseCapEff(t *testing.T) {
	status := "Name:\tomniglass\nCapInh:\t0000000000000000\nCapEff:\t0000000000001400\nCapBnd:\t000001ffffffffff\n"
	eff, err := parseCapEff(status)
	if err != nil {
		t.Fatal(err)
	}
	// 0x1400 = bit 10 (NET_BIND_SERVICE) and bit 12 (NET_ADMIN).
	if eff&(1<<capNetBindService) == 0 || eff&(1<<capNetAdmin) == 0 {
		t.Fatalf("capEff = %#x, want bind and admin", eff)
	}
	if _, err := parseCapEff("Name:\tx\n"); err == nil {
		t.Fatal("a status with no CapEff parsed")
	}
}

func TestCapabilitiesFrom(t *testing.T) {
	for _, tc := range []struct {
		name        string
		capEff      uint64
		unprivStart int
		offset      int
		want        collection.SimCapabilities
	}{
		{"unprivileged host", 0, 1024, 0, collection.SimCapabilities{}},
		{"container sysctl", 0, 0, 0, collection.SimCapabilities{LowPorts: true}},
		{"setcap bind", 1 << capNetBindService, 1024, 0, collection.SimCapabilities{LowPorts: true}},
		{"sidecar with NET_ADMIN", 1 << capNetAdmin, 0, 0, collection.SimCapabilities{LowPorts: true, NetAdmin: true}},
		{"shifted ports", 0, 1024, 20000, collection.SimCapabilities{PortOffset: 20000}},
	} {
		if got := capabilitiesFrom(tc.capEff, tc.unprivStart, tc.offset); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestPlanServesKnownProductsOnLoopback(t *testing.T) {
	has := func(p string) bool { return p == "sim-dsp" || p == "sim-display" }
	snaps, skips := plan([]collection.SimDevice{
		{ComponentID: "c1", Name: "dsp", Product: "sim-dsp", Address: "127.30.0.1"},
		{ComponentID: "c2", Name: "laptop", Product: "generic-device", Address: "127.30.0.2"},
		{ComponentID: "c3", Name: "far", Product: "sim-display", Address: "10.20.4.12"},
		{ComponentID: "c4", Name: "named", Product: "sim-display", Address: "display.lan"},
		{ComponentID: "c5", Name: "bare", Product: "sim-display", Address: ""},
	}, has)
	if len(snaps) != 1 {
		t.Fatalf("snapshots = %+v, want only dsp", snaps)
	}
	s := snaps[0]
	if s.DeviceID != "c1" || s.Hostname != "dsp" || s.ProfileName != "sim-dsp" || s.LoopbackIP != "127.30.0.1" || s.State != nil {
		t.Fatalf("snapshot = %+v", s)
	}
	reasons := map[string]string{}
	for _, k := range skips {
		reasons[k.Name] = k.Reason
	}
	for name, want := range map[string]string{
		"laptop": "no simulator profile", "far": "not a loopback address",
		"named": "not an IP address", "bare": "no address",
	} {
		if !strings.Contains(reasons[name], want) {
			t.Errorf("%s skipped for %q, want %q", name, reasons[name], want)
		}
	}
}
