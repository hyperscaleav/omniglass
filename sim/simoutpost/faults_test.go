package simoutpost

import (
	"reflect"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/wire"
)

// powerDevice is a device of a one-control family: a power route that sets the
// path from a body field to on or off.
func powerDevice(t *testing.T, id string) *sim.Device {
	t.Helper()
	fam := spec.Spec{
		Name:       "powerbox",
		State:      map[string]any{"status": map[string]any{"device": map[string]any{"power": "on"}}},
		Interfaces: []wire.Interface{{Name: "web", Protocol: "http", Port: 80}},
		HTTP: []spec.HTTPRoute{{
			Match: "POST /api/display/power",
			Sets:  []wire.Set{{Path: "status.device.power", Field: "power", Values: []string{"on", "off"}}},
			Handle: func(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
				v, _ := r.Body["power"].(string)
				if v != "on" && v != "off" {
					return spec.HTTPResponse{Status: 400, Body: "refused"}
				}
				_ = d.Write("status.device.power", v)
				return spec.HTTPResponse{Body: map[string]any{"ok": true}}
			},
		}},
	}
	cat, err := spec.NewCatalog(fam)
	if err != nil {
		t.Fatal(err)
	}
	e := sim.NewEngineOver(cat)
	if err := e.LoadDevices([]sim.DeviceSnapshot{{DeviceID: id, Hostname: "h", LoopbackIP: "127.60.0.1", ProfileName: "powerbox", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	return e.Devices()[0]
}

// The batch carries only the devices holding a fault, by component id.
func TestFaultsOfNamesOnlyTheFaultedDevices(t *testing.T) {
	a, b := powerDevice(t, "a"), powerDevice(t, "b")
	b.Operate(sim.Operation{Kind: "http", Method: "POST", Path: "/api/display/power", Body: `{"power":"off"}`})
	want := map[string][]collection.SimFault{"b": {{Path: "status.device.power", Value: "off", Default: "on"}}}
	if got := faultsOf([]*sim.Device{a, b}); !reflect.DeepEqual(got, want) {
		t.Fatalf("faultsOf = %+v, want %+v", got, want)
	}
	if got := faultsOf([]*sim.Device{a}); got != nil {
		t.Fatalf("a healthy fleet reported %+v", got)
	}
}
