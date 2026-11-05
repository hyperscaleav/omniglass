//go:build linux

package node_test

import (
	"fmt"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestNodeBorealEdgeReadsItsDisplayState (#1372): the shipped
// boreal-edge-snmp driver on the simulated display's SNMP face lands the
// display's own state beside its identity: power and the selected input.
// Power cycled and the input switched on the display's web admin read back on
// the next poll.
func TestNodeBorealEdgeReadsItsDisplayState(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var edge string
	s := newHTTPStack(t, "edge-display", nil, func(store *profile.Store) []string {
		edge = driverProfile(t, store, "boreal-edge-snmp")
		return []string{edge}
	})
	s.community("display-community", "public")
	s.component("display-1")
	dev := s.devices[edge]
	var snmpPort int
	for _, f := range dev.Faces() {
		if f.Protocol == "snmp" {
			snmpPort = int(f.Addr.Port())
		}
	}
	ep := s.attachHTTP("display-1", "boreal-edge-snmp", "edge-display", map[string]string{
		"host": s.ips[edge], "port": fmt.Sprint(snmpPort), "community": "display-community"})
	s.runNode("edge-display")

	s.waitFor("display-1", ep.Name, "endpoint-responsive", is("up"), "up: the agent answered")
	s.waitFor("display-1", ep.Name, "model-number", is(fmt.Sprint(dev.ReadPath("configuration.device.model"))), "the model, sysDescr")
	s.waitFor("display-1", ep.Name, "serial-number", is(fmt.Sprint(dev.ReadPath("configuration.device.serial"))), "the serial, off the Boreal arc")
	s.waitFor("display-1", ep.Name, "power-state", is("on"), "on at rest")
	s.waitFor("display-1", ep.Name, "video-input", is("hdmi1"), "hdmi1 at rest")

	// Power cycled and the input switched on the display's web admin.
	for _, op := range []sim.Operation{
		{Kind: "http", Method: "POST", Path: "/api/display/power", Body: `{"power": "standby"}`},
		{Kind: "http", Method: "POST", Path: "/api/display/input", Body: `{"input": "dp1"}`},
	} {
		if res := dev.Operate(op); res.Status != 200 {
			t.Fatalf("%s: %+v", op.Path, res)
		}
	}
	s.runNode("edge-display") // a restarted node polls at once, rather than in 30s
	s.waitFor("display-1", ep.Name, "power-state", is("standby"), "standby, set over HTTP")
	s.waitFor("display-1", ep.Name, "video-input", is("dp1"), "dp1, set over HTTP")
}
