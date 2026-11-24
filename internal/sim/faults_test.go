package sim

import (
	"net"
	"reflect"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

func faultDevice(t *testing.T) *Device {
	t.Helper()
	dev := NewDevice("c1", "host", net.ParseIP("127.60.0.1"), "test", 1, map[string]any{
		"status": map[string]any{"device": map[string]any{"power": "on"}, "display": map[string]any{"input": "hdmi1"}},
	})
	dev.SetDefaults(map[string]any{
		"status": map[string]any{"device": map[string]any{"power": "on"}, "display": map[string]any{"input": "hdmi1"}},
	})
	for _, r := range []*profile.Route{
		{Match: "POST /api/display/power", Mutations: []profile.Mutation{{Path: "status.device.power", Value: "{{ .Request.Body.power }}", Enum: []string{"on", "off"}}}},
		{Match: "POST /api/display/input", Mutations: []profile.Mutation{{Path: "status.display.input", Value: "{{ .Request.Body.input }}", Enum: []string{"hdmi1", "hdmi2"}}}},
	} {
		if err := dev.AddRouteFromProfile(r, `{}`); err != nil {
			t.Fatal(err)
		}
	}
	return dev
}

// A device reads healthy until a quick set drives a path off its default, and
// healthy again when the default is set back: the read the console's chip
// shows (#2462), taken from the device, not from the browser.
func TestFaultsAreThePathsSetAwayFromTheirDefaults(t *testing.T) {
	dev := faultDevice(t)
	if got := dev.Faults(); len(got) != 0 {
		t.Fatalf("a fresh device has faults %+v", got)
	}
	if res := dev.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/display/power", Body: `{"power":"off"}`}); res.Status != 200 {
		t.Fatalf("set power off: %+v", res)
	}
	want := []Fault{{Path: "status.device.power", Value: "off", Default: "on"}}
	if got := dev.Faults(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Faults() = %+v, want %+v", got, want)
	}
	dev.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/display/power", Body: `{"power":"on"}`})
	if got := dev.Faults(); len(got) != 0 {
		t.Fatalf("after setting the default back, Faults() = %+v", got)
	}
}
