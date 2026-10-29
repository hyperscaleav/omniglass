//go:build linux

package node_test

import (
	"fmt"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestNodeMeridianReadsRouteSignalAndLink (#895): the shipped Meridian
// drivers on the simulated switcher's and extender's SNMP faces land the
// switcher's main output route and its signal, and the extender's link and
// signal, beside identity. An input routed over the switcher's HTTP API
// reads back on the next poll.
func TestNodeMeridianReadsRouteSignalAndLink(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var mx, xl string
	s := newHTTPStack(t, "edge-meridian", nil, func(store *profile.Store) []string {
		mx, xl = driverProfile(t, store, "meridian-mx-snmp"), driverProfile(t, store, "meridian-xl-snmp")
		return []string{mx, xl}
	})
	s.community("meridian-community", "public")
	attach := func(comp, prof, driver string) string {
		s.component(comp)
		var port int
		for _, f := range s.devices[prof].Faces() {
			if f.Protocol == "snmp" {
				port = int(f.Addr.Port())
			}
		}
		return s.attachHTTP(comp, driver, "edge-meridian", map[string]string{
			"host": s.ips[prof], "port": fmt.Sprint(port), "community": "meridian-community"}).Name
	}
	mxEP, xlEP := attach("switcher-1", mx, "meridian-mx-snmp"), attach("extender-1", xl, "meridian-xl-snmp")
	s.runNode("edge-meridian")

	s.waitFor("switcher-1", mxEP, "model-number", is("MX 84"), "the switcher's model")
	s.waitFor("switcher-1", mxEP, "serial-number", is(fmt.Sprint(s.devices[mx].ReadPath("configuration.device.serial"))), "its serial")
	s.waitFor("switcher-1", mxEP, "video-input", is("hdmi1"), "output 1 shows the room PC")
	s.waitFor("switcher-1", mxEP, "signal-state", is("present"), "with a signal")
	s.waitFor("extender-1", xlEP, "model-number", is("XL-1"), "the extender's model")
	s.waitFor("extender-1", xlEP, "link-state", is("up"), "its link up")
	s.waitFor("extender-1", xlEP, "signal-state", is("present"), "a signal over it")

	if res := s.devices[mx].Operate(sim.Operation{Kind: "http", Method: "POST", Path: "/api/route", Body: `{"output": 1, "input": "hdmi3"}`}); res.Status != 200 {
		t.Fatalf("route: %+v", res)
	}
	s.runNode("edge-meridian") // a restarted node polls at once, rather than in 30s
	s.waitFor("switcher-1", mxEP, "video-input", is("hdmi3"), "hdmi3, routed over HTTP")
	s.waitFor("switcher-1", mxEP, "signal-state", is("absent"), "a table input with no laptop")
}
