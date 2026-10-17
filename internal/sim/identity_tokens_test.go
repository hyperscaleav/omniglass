package sim

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestLoadDevicesExpandsIdentityTokensPerRoom verifies the simulator expands
// {site}/{building}/{room} and {room-device:<profile>} tokens in a profile's
// defaults per device. Reset-to-defaults reads {{ .Defaults.* }}, so without
// this the raw template ("room-{site}-...") would be written back on a
// factory reset. The two rooms also prove cross-device resolution picks the
// paired panel in the SAME room even when the panel's hostname suffix differs
// per system (TP1 vs touch-panel1), the case a fixed-suffix template can't
// handle.
func TestLoadDevicesExpandsIdentityTokensPerRoom(t *testing.T) {
	store := &profile.Store{Devices: map[string]*profile.DeviceProfile{
		"vbar": {
			Name: "vbar",
			Defaults: map[string]any{
				"status": map[string]any{
					"app":         map[string]any{"signin": "room-{site}-{building}-{room}@shinra.lan"},
					"peripherals": map[string]any{"pair": "{room-device:panel}"},
				},
			},
		},
		"panel": {Name: "panel"},
	}}
	e := NewEngine(store, nil)

	snaps := []DeviceSnapshot{
		{DeviceID: "vb-300", Hostname: "BOI-17C-300-COD1", LoopbackIP: "127.0.0.1", ProfileName: "vbar", SiteCode: "BOI", Building: "17C", Room: "300", State: map[string]any{}},
		{DeviceID: "tp-300", Hostname: "BOI-17C-300-TP1", LoopbackIP: "127.0.0.2", ProfileName: "panel", SiteCode: "BOI", Building: "17C", Room: "300", State: map[string]any{}},
		{DeviceID: "vb-301", Hostname: "BOI-17C-301-COD1", LoopbackIP: "127.0.0.3", ProfileName: "vbar", SiteCode: "BOI", Building: "17C", Room: "301", State: map[string]any{}},
		{DeviceID: "tp-301", Hostname: "BOI-17C-301-touch-panel1", LoopbackIP: "127.0.0.4", ProfileName: "panel", SiteCode: "BOI", Building: "17C", Room: "301", State: map[string]any{}},
	}
	if err := e.LoadDevices(snaps); err != nil {
		t.Fatalf("LoadDevices: %v", err)
	}

	byID := make(map[string]*Device)
	for _, d := range e.devices {
		byID[d.DeviceID] = d
	}

	cases := []struct{ id, signin, pair string }{
		{"vb-300", "room-BOI-17C-300@shinra.lan", "BOI-17C-300-TP1"},
		{"vb-301", "room-BOI-17C-301@shinra.lan", "BOI-17C-301-touch-panel1"},
	}
	for _, c := range cases {
		d := byID[c.id]
		if d == nil {
			t.Fatalf("device %s not loaded", c.id)
		}
		if got := digDefault(d, "status", "app", "signin"); got != c.signin {
			t.Errorf("%s signin = %q, want %q", c.id, got, c.signin)
		}
		if got := digDefault(d, "status", "peripherals", "pair"); got != c.pair {
			t.Errorf("%s pair = %q, want %q (paired panel in same room)", c.id, got, c.pair)
		}
	}
}

func digDefault(d *Device, path ...string) string {
	cur := any(d.defaults)
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[k]
	}
	s, _ := cur.(string)
	return s
}
