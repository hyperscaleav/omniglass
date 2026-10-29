//go:build linux

package node_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestNodeNewtronCoreAndSlate (#896): the shipped Newtron Core driver on the
// simulated processor's NVP face lands the panel's video input and the
// processor's own program state and connected panels; the Slate driver lands
// the scheduling panel's booking state. A program stopped and a space booked
// on the components arrive as pushes, with no poll.
func TestNodeNewtronCoreAndSlate(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var core, slate string
	s := newHTTPStack(t, "edge-newtron", nil, func(store *profile.Store) []string {
		core, slate = driverProfile(t, store, "newtron-core-nvp"), driverProfile(t, store, "newtron-slate-nvp")
		return []string{core, slate}
	})
	attach := func(comp, prof, driver string) string {
		s.component(comp)
		var port int
		for _, f := range s.devices[prof].Faces() {
			if f.Name == "nvp" {
				port = int(f.Addr.Port())
			}
		}
		return s.attachHTTP(comp, driver, "edge-newtron", map[string]string{"host": s.ips[prof], "port": fmt.Sprint(port)}).Name
	}
	coreEP, slateEP := attach("processor-1", core, "newtron-core-nvp"), attach("scheduler-1", slate, "newtron-slate-nvp")
	s.runNode("edge-newtron")

	s.waitFor("processor-1", coreEP, "video-input", is("hdmi1"), "the panel's input, on the processor")
	s.waitFor("processor-1", coreEP, "program-state", is("running"), "the program running")
	deadline := time.Now().Add(20 * time.Second)
	for {
		if v, ok := s.metric("processor-1", coreEP, "panel-count"); ok && v == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the processor's poll landed no panel-count of 2")
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.waitFor("scheduler-1", slateEP, "booking-state", is("free"), "free at rest")

	// The held sessions' subscriptions carry the changes, with no poll due.
	if res := s.devices[core].Operate(sim.Operation{Kind: "http", Method: "POST", Path: "/api/program", Body: `{"state": "stopped"}`}); res.Status != 200 {
		t.Fatalf("stop the program: %+v", res)
	}
	if res := s.devices[slate].Operate(sim.Operation{Kind: "http", Method: "POST", Path: "/api/booking", Body: `{"state": "booked"}`}); res.Status != 200 {
		t.Fatalf("book now: %+v", res)
	}
	s.waitFor("processor-1", coreEP, "program-state", is("stopped"), "stopped, pushed")
	s.waitFor("scheduler-1", slateEP, "booking-state", is("booked"), "booked, pushed")
}
