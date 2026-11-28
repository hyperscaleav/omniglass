package sim

import (
	"net"
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/wire"
)

func faultDevice(t *testing.T) *Device {
	t.Helper()
	dev := NewDevice("c1", "host", net.ParseIP("127.60.0.1"), "test", 1, map[string]any{
		"status": map[string]any{"device": map[string]any{"power": "on"}, "display": map[string]any{"input": "hdmi1"}},
	})
	dev.SetDefaults(map[string]any{
		"status": map[string]any{"device": map[string]any{"power": "on"}, "display": map[string]any{"input": "hdmi1"}},
	})
	// Two controls that set a path from a body field to one of a fixed list,
	// as a family's httpapi.Control declares them.
	control := func(match, path, field string, values ...string) spec.HTTPRoute {
		return spec.HTTPRoute{
			Match: match,
			Sets:  []wire.Set{{Path: path, Field: field, Values: values}},
			Handle: func(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
				v, _ := r.Body[field].(string)
				if !slices.Contains(values, v) {
					return spec.HTTPResponse{Status: http.StatusBadRequest, Body: map[string]any{"error": "refused"}}
				}
				if err := d.Write(path, v); err != nil {
					return spec.HTTPResponse{Status: http.StatusInternalServerError, Body: err.Error()}
				}
				return spec.HTTPResponse{Body: map[string]any{"ok": true}}
			},
		}
	}
	if err := dev.addHandlers(spec.Handlers{HTTP: []spec.HTTPRoute{
		control("POST /api/display/power", "status.device.power", "power", "on", "off"),
		control("POST /api/display/input", "status.display.input", "input", "hdmi1", "hdmi2"),
	}}, spec.Model{}); err != nil {
		t.Fatal(err)
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
