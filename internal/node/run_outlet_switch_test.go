//go:build linux

package node_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestSwitchingAnOutletOffHoldsWhatItFeeds drives #1653 over the real wire:
// a simulated Ferrite PDU read by ferrite-pdu-snmp and switched by
// ferrite-pdu-api, and a sensor fed from its outlet 2 through a power
// dependency, carried by two endpoints (its polled http API and a probed
// raw-tcp feed). outlet-off on outlet 2, issued through the API, switches
// the simulated outlet; its delivery closes both of the sensor's gates with
// disrupted before it sends, so a run now on the sensor is answered with that
// reason and sends nothing; and the PDU's next status poll reads
// outlet-2-state off. The PDU's own endpoints are not held: it switched the
// outlet and stays up.
func TestSwitchingAnOutletOffHoldsWhatItFeeds(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var pduProfile, sensorProfile string
	s := newHTTPStack(t, "edge-outlet", nil, func(store *profile.Store) []string {
		pduProfile = driverProfile(t, store, "ferrite-pdu-snmp")
		sensorProfile = driverProfile(t, store, "http-json")
		return []string{pduProfile, sensorProfile}
	})
	owner := s.ownerToken()
	pdu := s.devices[pduProfile]
	state := func(n int) string { return fmt.Sprint(pdu.ReadPath(fmt.Sprintf("status.outlets.%d.state", n-1))) }
	before := map[int]string{1: state(1), 3: state(3), 4: state(4)}

	s.fastDriver("ferrite-pdu-snmp", "pdu-snmp-fast")
	s.community("pdu-community", "public")
	s.secret("pdu-login", "basic-auth", map[string]string{"username": "ferrite", "password": "ferrite"})
	s.component("power-1")
	outlets := s.attachHTTP("power-1", "pdu-snmp-fast", "edge-outlet", map[string]string{
		"host": s.ips[pduProfile], "port": fmt.Sprint(s.facePort(pduProfile, "snmp")), "community": "pdu-community"})
	switcher := s.attachHTTP("power-1", "ferrite-pdu-api", "edge-outlet", map[string]string{
		"host": s.ips[pduProfile], "port": fmt.Sprint(s.httpPort(pduProfile)), "login": "pdu-login"})

	s.fastDriver("http-json", "sensor-fast")
	s.component("sensor-1")
	s.secret("census-key", "api-key", map[string]string{"key": "census-lab-key"})
	web := s.attachHTTP("sensor-1", "sensor-fast", "edge-outlet", map[string]string{
		"host": s.ips[sensorProfile], "port": fmt.Sprint(s.httpPort(sensorProfile)), "key": "census-key"})
	feed := s.attachBareTCP("sensor-1", "edge-outlet", fmt.Sprintf("%s:%d", s.ips[sensorProfile], s.facePort(sensorProfile, "tcp")))
	if code := s.api(owner, http.MethodPost, "/components/sensor-1/dependencies",
		map[string]any{"upstream": "power-1", "kind": "power", "outlet": "2"}, nil); code >= 300 {
		t.Fatalf("depend sensor-1 on power-1 outlet 2 = %d", code)
	}
	// The seeded outlet-off's quiet is 15s and its hold 300s. The simulated
	// sensor draws no power from the outlet, so its probe reads it up past
	// quiet and releases the hold; the quiet is set long enough to observe the
	// hold inside it.
	s.setDisruption("outlet-off", 60*time.Second, 120*time.Second)
	s.runNodeReporting("edge-outlet", time.Second)

	s.waitFor("power-1", outlets.Name, "outlet-2-state", is("on"), "outlet 2 on, as the PDU starts")
	s.waitMetric("sensor-1", web.Name, probekeys.PollDuration, func(v float64) bool { return v > 0 }, "a positive mean poll time")
	s.waitFor("sensor-1", feed.Name, probekeys.EndpointReachable, is("up"), "up: the feed's port answered")

	var cmd waitedCommand
	if code := s.api(owner, http.MethodPost, "/components/power-1/commands:issue",
		map[string]any{"command_type": "outlet-off", "params": map[string]any{"outlet": 2}}, &cmd); code >= 300 {
		t.Fatalf("issue outlet-off = %d", code)
	}
	for deadline := time.Now().Add(20 * time.Second); cmd.ExecutedAt == nil; {
		if time.Now().After(deadline) {
			t.Fatalf("outlet-off = %+v, want executed within 20s", cmd)
		}
		time.Sleep(25 * time.Millisecond)
		s.api(owner, http.MethodGet, fmt.Sprintf("/components/power-1/commands/%d", cmd.ID), nil, &cmd)
	}
	if got := state(2); got != "off" {
		t.Fatalf("simulated outlet 2 = %q after outlet-off executed, want off", got)
	}
	for n, want := range before {
		if got := state(n); got != want {
			t.Errorf("simulated outlet %d = %q after outlet-off on 2, want it unmoved at %s", n, got, want)
		}
	}

	disrupted := func(v string) bool {
		return strings.HasPrefix(v, "disrupted ") && strings.Contains(v, fmt.Sprintf("command-%d", cmd.ID))
	}
	for _, ep := range []*storage.Endpoint{web, feed} {
		s.waitFor("sensor-1", ep.Name, probekeys.EndpointRejection, disrupted, "disrupted by the outlet-off")
	}
	code, body := s.runNow(owner, "sensor-1", web.Name, "status")
	if want := fmt.Sprintf("disrupted by outlet-off (command %d)", cmd.ID); code != http.StatusConflict || !strings.Contains(body, want) {
		t.Errorf("run now on the fed sensor = %d %s, want 409 naming %q", code, body, want)
	}

	s.waitFor("power-1", outlets.Name, "outlet-2-state", is("off"), "outlet 2 off, as the next status poll reads it")
	for _, ep := range []*storage.Endpoint{outlets, switcher} {
		if v := s.latest("power-1", ep.Name, probekeys.EndpointRejection); strings.HasPrefix(v, "disrupted ") {
			t.Errorf("the PDU's %s rejection = %q; the PDU switched the outlet and is not held", ep.Name, v)
		}
	}
}

// runNow runs an endpoint's poll now through the API and returns the status
// and the raw answer, whose refusal text names the gate's reason.
func (s *httpStack) runNow(tok, comp, instance, poll string) (int, string) {
	s.t.Helper()
	req, err := http.NewRequestWithContext(s.ctx, http.MethodPost,
		s.apiURL+"/api/v1/endpoints/"+comp+"%2F"+instance+"/polls/"+poll+":run", nil)
	if err != nil {
		s.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatalf("run now %s/%s: %v", comp, instance, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}
