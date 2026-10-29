//go:build linux

package node_test

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestNodePolarisAmpReadsItsOperatingState (#898): the shipped
// polaris-amp-snmp driver on the simulated amplifier's SNMP face lands the
// amplifier's own state beside its identity and uptime: power, protect,
// temperature and fan. Standby entered over the amplifier's HTTP API reads
// standby on the next poll.
func TestNodePolarisAmpReadsItsOperatingState(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var amp string
	s := newHTTPStack(t, "edge-amp", nil, func(store *profile.Store) []string {
		amp = driverProfile(t, store, "polaris-amp-snmp")
		return []string{amp}
	})
	s.community("amp-community", "public")
	s.component("amp-1")
	dev := s.devices[amp]
	var snmpPort, httpPort int
	for _, f := range dev.Faces() {
		switch f.Protocol {
		case "snmp":
			snmpPort = int(f.Addr.Port())
		case "http":
			httpPort = int(f.Addr.Port())
		}
	}
	ep := s.attachHTTP("amp-1", "polaris-amp-snmp", "edge-amp", map[string]string{
		"host": s.ips[amp], "port": fmt.Sprint(snmpPort), "community": "amp-community"})
	s.runNode("edge-amp")

	s.waitFor("amp-1", ep.Name, "endpoint-responsive", is("up"), "up: the agent answered")
	s.waitFor("amp-1", ep.Name, "model-number", is(fmt.Sprint(dev.ReadPath("configuration.device.model"))), "the model, sysDescr")
	s.waitFor("amp-1", ep.Name, "serial-number", is(fmt.Sprint(dev.ReadPath("configuration.device.serial"))), "the serial, off the Polaris arc")
	s.waitFor("amp-1", ep.Name, "firmware-version", is(fmt.Sprint(dev.ReadPath("configuration.firmware.version"))), "the firmware")
	s.waitFor("amp-1", ep.Name, "power-state", is("on"), "on at rest")
	s.waitFor("amp-1", ep.Name, "protect-state", is("normal"), "normal at rest")
	for _, key := range []string{"temperature", "fan-speed", "uptime"} {
		deadline := time.Now().Add(20 * time.Second)
		for {
			// uptime reads 0 on a poll in the agent's first second.
			if v, ok := s.metric("amp-1", ep.Name, key); ok && (v > 0 || key == "uptime") {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("the amplifier's poll landed no %s", key)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	// The heatsink drifts on its own tick, so the reading is held to the
	// profile's band rather than to one instant's value.
	if temp, _ := s.metric("amp-1", ep.Name, "temperature"); temp < 34 || temp > 52 {
		t.Errorf("temperature = %v, want within the amplifier's 34 to 52 C", temp)
	}

	// Standby over the control face, read back over the monitoring face.
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s:%d/api/power", s.ips[amp], httpPort), bytes.NewBufferString(`{"power": "standby"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Polaris-Token", "polaris-lab-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("standby: status %d", resp.StatusCode)
	}
	s.runNode("edge-amp") // a restarted node polls at once, rather than in 30s
	s.waitFor("amp-1", ep.Name, "power-state", is("standby"), "standby, entered over HTTP")
}
