package simoutpost

import (
	"net"
	"reflect"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

func powerDevice(t *testing.T, id string) *sim.Device {
	t.Helper()
	state := func() map[string]any {
		return map[string]any{"status": map[string]any{"device": map[string]any{"power": "on"}}}
	}
	d := sim.NewDevice(id, "h", net.ParseIP("127.60.0.1"), "p", 1, state())
	d.SetDefaults(state())
	r := &profile.Route{Match: "POST /api/display/power", Mutations: []profile.Mutation{{Path: "status.device.power", Value: "{{ .Request.Body.power }}", Enum: []string{"on", "off"}}}}
	if err := d.AddRouteFromProfile(r, `{}`); err != nil {
		t.Fatal(err)
	}
	return d
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
