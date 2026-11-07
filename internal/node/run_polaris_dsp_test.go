//go:build linux

package node_test

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestNodePolarisDSPReadsItsProgramOutput (#1493): the shipped
// polaris-dsp-snmp driver on the simulated DSP's SNMP face lands the program
// output's level and mute beside the DSP's identity and uptime, the class
// signals the lab's DSPs read nothing but identity without. A mute set over
// the DSP's HTTP API reads back as muted on the next poll.
func TestNodePolarisDSPReadsItsProgramOutput(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var prof string
	s := newHTTPStack(t, "edge-dsp", nil, func(store *profile.Store) []string {
		prof = driverProfile(t, store, "polaris-dsp-snmp")
		return []string{prof}
	})
	s.community("dsp-community", "public")
	s.component("dsp-1")
	dev := s.devices[prof]
	var snmpPort, httpPort int
	for _, f := range dev.Faces() {
		switch f.Protocol {
		case "snmp":
			snmpPort = int(f.Addr.Port())
		case "http":
			httpPort = int(f.Addr.Port())
		}
	}
	ep := s.attachHTTP("dsp-1", "polaris-dsp-snmp", "edge-dsp", map[string]string{
		"host": s.ips[prof], "port": fmt.Sprint(snmpPort), "community": "dsp-community"})
	s.runNode("edge-dsp")

	s.waitFor("dsp-1", ep.Name, "endpoint-responsive", is("up"), "up: the agent answered")
	s.waitFor("dsp-1", ep.Name, "model-number", is(fmt.Sprint(dev.ReadPath("configuration.device.model"))), "the model, sysDescr")
	s.waitFor("dsp-1", ep.Name, "serial-number", is(fmt.Sprint(dev.ReadPath("configuration.device.serial"))), "the serial, off the Polaris arc")
	s.waitFor("dsp-1", ep.Name, "firmware-version", is(fmt.Sprint(dev.ReadPath("configuration.firmware.version"))), "the firmware")
	s.waitFor("dsp-1", ep.Name, "mute", is("false"), "unmuted at rest")
	s.waitMetric("dsp-1", ep.Name, "uptime", func(v float64) bool { return v >= 0 }, "an uptime")
	// The level drifts on its own tick, so it is held to the band the
	// profile runs it in rather than to one instant's value.
	s.waitMetric("dsp-1", ep.Name, "audio-level", func(v float64) bool { return v >= -40 && v <= -6 }, "a program level inside the DSP's band")

	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s:%d/api/output/mute", s.ips[prof], httpPort), bytes.NewBufferString(`{"mute": true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Polaris-Token", "polaris-lab-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mute: status %d", resp.StatusCode)
	}
	s.runNode("edge-dsp") // a restarted node polls at once, rather than in 30s
	s.waitFor("dsp-1", ep.Name, "mute", is("true"), "muted, set over HTTP")
}
