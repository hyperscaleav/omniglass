//go:build linux

package node_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestNodeLyraMicsReadMuteLevelAndBattery (#894): the shipped Lyra drivers
// on the simulated microphones' SNMP faces land mute and the input level for
// both, and the wireless system's battery as a ratio, beside identity. A
// mute set on a microphone reads back on the next poll.
func TestNodeLyraMicsReadMuteLevelAndBattery(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var arc, hx string
	s := newHTTPStack(t, "edge-lyra", nil, func(store *profile.Store) []string {
		arc, hx = driverProfile(t, store, "lyra-arc-snmp"), driverProfile(t, store, "lyra-hx-snmp")
		return []string{arc, hx}
	})
	s.community("lyra-community", "public")
	attach := func(comp, prof, driver string) string {
		s.component(comp)
		var port int
		for _, f := range s.devices[prof].Faces() {
			if f.Protocol == "snmp" {
				port = int(f.Addr.Port())
			}
		}
		return s.attachHTTP(comp, driver, "edge-lyra", map[string]string{
			"host": s.ips[prof], "port": fmt.Sprint(port), "community": "lyra-community"}).Name
	}
	arcEP, hxEP := attach("mic-ceiling", arc, "lyra-arc-snmp"), attach("mic-wireless", hx, "lyra-hx-snmp")
	s.runNode("edge-lyra")

	metric := func(comp, ep, key string, ok func(float64) bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for {
			if v, has := s.metric(comp, ep, key); has && ok(v) {
				return
			}
			if time.Now().After(deadline) {
				v, has := s.metric(comp, ep, key)
				t.Fatalf("%s on %s = %v (%v)", key, comp, v, has)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	for _, m := range []struct{ comp, ep, prof, model string }{
		{"mic-ceiling", arcEP, arc, "Arc A2"}, {"mic-wireless", hxEP, hx, "HX-1"},
	} {
		s.waitFor(m.comp, m.ep, "model-number", is(m.model), "the model")
		s.waitFor(m.comp, m.ep, "serial-number", is(fmt.Sprint(s.devices[m.prof].ReadPath("configuration.device.serial"))), "the serial")
		s.waitFor(m.comp, m.ep, "mute", is("false"), "unmuted at rest")
		metric(m.comp, m.ep, "audio-level", func(v float64) bool { return v >= -70 && v <= -20 })
	}
	metric("mic-wireless", hxEP, "battery-charge", func(v float64) bool { return v > 0 && v <= 1 })
	if _, has := s.metric("mic-ceiling", arcEP, "battery-charge"); has {
		t.Error("the ceiling array landed a battery")
	}

	if res := s.devices[hx].Operate(sim.Operation{Kind: "http", Method: "POST", Path: "/api/mute", Body: `{"mute": true}`}); res.Status != 200 {
		t.Fatalf("mute: %+v", res)
	}
	s.runNode("edge-lyra") // a restarted node polls at once, rather than in 30s
	s.waitFor("mic-wireless", hxEP, "mute", is("true"), "muted on the microphone")
}
