//go:build linux

package node_test

import (
	"fmt"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestNodeMeridianReadsRouteLinkAndSignal (#895): the shipped Meridian
// drivers on the simulated switcher's and extender's SNMP faces land each
// device's own signals beside its identity and uptime: the switcher's routed
// input and the signal on it, the extender's link and the signal on its
// input. A route set over the switcher's HTTP API reads back as the routed
// input on the next poll.
func TestNodeMeridianReadsRouteLinkAndSignal(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var sw, ext string
	s := newHTTPStack(t, "edge-meridian", nil, func(store *profile.Store) []string {
		sw, ext = driverProfile(t, store, "meridian-mx-snmp"), driverProfile(t, store, "meridian-xl-snmp")
		return []string{sw, ext}
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
	swEP, extEP := attach("switcher-1", sw, "meridian-mx-snmp"), attach("extender-1", ext, "meridian-xl-snmp")
	s.runNode("edge-meridian")

	for _, m := range []struct{ comp, ep, prof, model string }{
		{"switcher-1", swEP, sw, "MX 84"}, {"extender-1", extEP, ext, "XL-1"},
	} {
		s.waitFor(m.comp, m.ep, "model-number", is(m.model), "the model")
		s.waitFor(m.comp, m.ep, "serial-number", is(fmt.Sprint(s.devices[m.prof].ReadPath("configuration.device.serial"))), "the serial")
		s.waitFor(m.comp, m.ep, "firmware-version", is(fmt.Sprint(s.devices[m.prof].ReadPath("configuration.firmware.version"))), "the firmware")
		s.waitFor(m.comp, m.ep, "signal-state", is("present"), "a signal at rest")
		// uptime reads 0 on a poll in the agent's first second, so landing is the check.
		s.waitMetric(m.comp, m.ep, "uptime", func(v float64) bool { return v >= 0 }, "an uptime")
	}
	s.waitFor("switcher-1", swEP, "video-input", is("hdmi1"), "hdmi1 routed at rest")
	s.waitFor("extender-1", extEP, "link-state", is("up"), "the link up at rest")

	// The route changed over the switcher's HTTP API: dp1, which has no
	// source, so the routed input and its signal both move.
	if res := s.devices[sw].Operate(sim.Operation{Kind: "http", Method: "POST", Path: "/api/route", Body: `{"input": "dp1"}`}); res.Status != 200 {
		t.Fatalf("route: %+v", res)
	}
	s.runNode("edge-meridian") // a restarted node polls at once, rather than in 30s
	s.waitFor("switcher-1", swEP, "video-input", is("dp1"), "dp1 routed over HTTP")
	s.waitFor("switcher-1", swEP, "signal-state", is("absent"), "no signal on the routed dp1")
}
