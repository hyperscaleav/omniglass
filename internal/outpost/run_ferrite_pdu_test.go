//go:build linux

package node_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestNodeFerritePDUReadsOutletsCurrentAndVoltage (#1368): the shipped
// ferrite-pdu-snmp driver on the simulated power conditioner's SNMP face lands
// the state of each outlet, the total current in amperes and the input voltage
// in volts, beside its identity and uptime.
func TestNodeFerritePDUReadsOutletsCurrentAndVoltage(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var pdu string
	s := newHTTPStack(t, "edge-pdu", nil, func(store *profile.Store) []string {
		pdu = driverProfile(t, store, "ferrite-pdu-snmp")
		return []string{pdu}
	})
	s.community("pdu-community", "public")
	s.component("power-1")
	dev := s.devices[pdu]
	var snmpPort int
	for _, f := range dev.Faces() {
		if f.Protocol == "snmp" {
			snmpPort = int(f.Addr.Port())
		}
	}
	ep := s.attachHTTP("power-1", "ferrite-pdu-snmp", "edge-pdu", map[string]string{
		"host": s.ips[pdu], "port": fmt.Sprint(snmpPort), "community": "pdu-community"})
	s.runNode("edge-pdu")

	s.waitFor("power-1", ep.Name, "endpoint-responsive", is("up"), "up: the agent answered")
	s.waitFor("power-1", ep.Name, "model-number", is(fmt.Sprint(dev.ReadPath("configuration.device.model"))), "the model, sysDescr")
	s.waitFor("power-1", ep.Name, "serial-number", is(fmt.Sprint(dev.ReadPath("configuration.device.serial"))), "the serial, off the Ferrite arc")
	s.waitFor("power-1", ep.Name, "firmware-version", is(fmt.Sprint(dev.ReadPath("configuration.firmware.version"))), "the firmware")
	for n := 1; n <= 4; n++ {
		want := fmt.Sprint(dev.ReadPath(fmt.Sprintf("status.outlets.%d.state", n-1)))
		s.waitFor("power-1", ep.Name, fmt.Sprintf("outlet-%d-state", n), is(want), fmt.Sprintf("outlet %d as the PDU holds it", n))
	}

	metric := func(key string, ok func(float64) bool) float64 {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for {
			if v, has := s.metric("power-1", ep.Name, key); has && ok(v) {
				return v
			}
			if time.Now().After(deadline) {
				v, has := s.metric("power-1", ep.Name, key)
				t.Fatalf("%s = %v (%v)", key, v, has)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	// The agent reports millivolts and milliamps; the driver lands the canon's
	// volts and amperes.
	metric("voltage", func(v float64) bool { return v >= 226 && v <= 234 })
	metric("current", func(v float64) bool { return v > 0.1 && v < 5 })
	metric("uptime", func(v float64) bool { return v >= 0 })
}
