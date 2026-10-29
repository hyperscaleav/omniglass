//go:build linux

package node_test

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestNodeBorealBeamReadsItsProjectorState (#899): the shipped
// boreal-beam-snmp driver on the simulated projector's SNMP face lands the
// projector's own state beside its identity: power, light-source hours, the
// shutter and the input. A shutter closed and an input switched on the
// projector read back on the next poll.
func TestNodeBorealBeamReadsItsProjectorState(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var beam string
	s := newHTTPStack(t, "edge-beam", nil, func(store *profile.Store) []string {
		beam = driverProfile(t, store, "boreal-beam-snmp")
		return []string{beam}
	})
	s.community("beam-community", "public")
	s.component("projector-1")
	dev := s.devices[beam]
	var snmpPort int
	for _, f := range dev.Faces() {
		if f.Protocol == "snmp" {
			snmpPort = int(f.Addr.Port())
		}
	}
	ep := s.attachHTTP("projector-1", "boreal-beam-snmp", "edge-beam", map[string]string{
		"host": s.ips[beam], "port": fmt.Sprint(snmpPort), "community": "beam-community"})
	s.runNode("edge-beam")

	s.waitFor("projector-1", ep.Name, "endpoint-responsive", is("up"), "up: the agent answered")
	s.waitFor("projector-1", ep.Name, "model-number", is(fmt.Sprint(dev.ReadPath("configuration.device.model"))), "the model, sysDescr")
	s.waitFor("projector-1", ep.Name, "serial-number", is(fmt.Sprint(dev.ReadPath("configuration.device.serial"))), "the serial, off the Boreal arc")
	s.waitFor("projector-1", ep.Name, "firmware-version", is(fmt.Sprint(dev.ReadPath("configuration.firmware.version"))), "the firmware")
	s.waitFor("projector-1", ep.Name, "power-state", is("on"), "on at rest")
	s.waitFor("projector-1", ep.Name, "shutter-state", is("open"), "open at rest")
	s.waitFor("projector-1", ep.Name, "video-input", is("hdmi1"), "hdmi1 at rest")
	// The light source ages only by the minute, so the first poll reads the
	// profile's starting hours, give or take a tick.
	start, _ := strconv.ParseFloat(fmt.Sprint(dev.ReadPath("status.projector.light_source_hours")), 64)
	deadline := time.Now().Add(20 * time.Second)
	for {
		if h, ok := s.metric("projector-1", ep.Name, "light-source-hours"); ok && h >= start-0.1 && h <= start+0.1 {
			break
		}
		if time.Now().After(deadline) {
			h, ok := s.metric("projector-1", ep.Name, "light-source-hours")
			t.Fatalf("light-source-hours = %v (%v), want about %v", h, ok, start)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The shutter closed and the input switched on the projector's web admin.
	for _, op := range []sim.Operation{
		{Kind: "http", Method: "POST", Path: "/api/projector/shutter", Body: `{"shutter": "closed"}`},
		{Kind: "http", Method: "POST", Path: "/api/projector/input", Body: `{"input": "hdmi2"}`},
	} {
		if res := dev.Operate(op); res.Status != 200 {
			t.Fatalf("%s: %+v", op.Path, res)
		}
	}
	s.runNode("edge-beam") // a restarted node polls at once, rather than in 30s
	s.waitFor("projector-1", ep.Name, "shutter-state", is("closed"), "closed, set over HTTP")
	s.waitFor("projector-1", ep.Name, "video-input", is("hdmi2"), "hdmi2, set over HTTP")
}
