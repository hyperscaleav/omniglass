//go:build linux

package node_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestNodeNewtronProcessorAndSchedulerReadTheirClassState (#896): the
// shipped newtron-core-nvp and newtron-slate-nvp drivers over the simulated
// control processor's and scheduling panel's NVP faces. The processor lands
// its program state and the input beside it, the scheduling panel its
// room's booking; a change on the device reaches the series.
func TestNodeNewtronProcessorAndSchedulerReadTheirClassState(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var core, slate string
	s := newHTTPStack(t, "edge-nvp", nil, func(store *profile.Store) []string {
		core, slate = driverProfile(t, store, "newtron-core-nvp"), driverProfile(t, store, "newtron-slate-nvp")
		return []string{core, slate}
	})
	nvpPort := func(prof string) int {
		for _, f := range s.devices[prof].Faces() {
			if f.Name == "nvp" {
				return int(f.Addr.Port())
			}
		}
		t.Fatalf("%s serves no nvp face: %+v", prof, s.devices[prof].Faces())
		return 0
	}
	attach := func(comp, prof, driver string) string {
		s.component(comp)
		return s.attachHTTP(comp, driver, "edge-nvp", map[string]string{
			"host": s.ips[prof], "port": fmt.Sprint(nvpPort(prof))}).Name
	}
	coreEP, slateEP := attach("processor-1", core, "newtron-core-nvp"), attach("scheduler-1", slate, "newtron-slate-nvp")
	s.runNode("edge-nvp")

	s.waitFor("processor-1", coreEP, "program-state", is("running"), "a running program at rest")
	s.waitFor("processor-1", coreEP, "video-input", is("hdmi1"), "the input, which a processor answers as the panel does")
	s.waitFor("processor-1", coreEP, "scene-state", is("idle"), "the room's scene, which a processor answers as the panel does (#1494)")
	s.waitFor("scheduler-1", slateEP, "booking-state", is("free"), "a free room at rest")
	if v := s.latest("scheduler-1", slateEP, "video-input"); v != "" {
		t.Errorf("the scheduling panel landed a video input (%q)", v)
	}

	// The device's own change reaches the series.
	lx := collection.NewLineExchanger()
	for _, c := range []struct{ prof, line string }{{core, "SET PROGRAM stopped"}, {core, "SET SCENE presentation"}, {slate, "BOOK"}} {
		addr := fmt.Sprintf("%s:%d", s.ips[c.prof], nvpPort(c.prof))
		if _, err := lx.Exchange(context.Background(), addr, c.line, 2*time.Second); err != nil {
			t.Fatalf("%s: %v", c.line, err)
		}
	}
	s.runNode("edge-nvp") // a restarted node polls at once, rather than at the interval
	s.waitFor("processor-1", coreEP, "program-state", is("stopped"), "the stopped program")
	s.waitFor("processor-1", coreEP, "scene-state", is("presentation"), "the scene the room entered")
	s.waitFor("scheduler-1", slateEP, "booking-state", is("booked"), "the booked room")
}
