//go:build linux

// The simulator serves on 127.x addresses beyond 127.0.0.1, which Linux
// treats as local without configuration.

package node_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestARebootHoldsItsDevicesEndpointsUntilEachReadsUpAgain drives #1651 over
// the real wire: a simulated occupancy sensor carried by two endpoints of
// one component on one node, its http API (polled every second, and the
// reboot's carrier) and its raw-tcp feed (probed only). The reboot's
// delivery closes both gates with disrupted before it sends; the device
// goes away as a rebooting one does; nothing runs against either endpoint
// for the quiet period, and its due polls are held, counted apart from
// failures. Past quiet the device is still gone, so both stay held: a down
// verdict releases nothing. The device returns on the same address and
// each endpoint reopens on its own up verdict, the backstop never firing.
func TestARebootHoldsItsDevicesEndpointsUntilEachReadsUpAgain(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var sensorProfile string
	s := newHTTPStack(t, "edge-reboot", nil, func(store *profile.Store) []string {
		sensorProfile = driverProfile(t, store, "http-json")
		return []string{sensorProfile}
	})
	owner := s.ownerToken()
	s.rebootingDriver("http-json", "sensor-reboot")
	s.component("sensor-1")
	s.secret("census-key", "api-key", map[string]string{"key": "census-lab-key"})
	web := s.attachHTTP("sensor-1", "sensor-reboot", "edge-reboot", map[string]string{
		"host": s.ips[sensorProfile], "port": fmt.Sprint(s.httpPort(sensorProfile)), "key": "census-key"})
	feed := s.attachBareTCP("sensor-1", "edge-reboot", fmt.Sprintf("%s:%d", s.ips[sensorProfile], s.facePort(sensorProfile, "tcp")))
	// The seeded reboot's quiet is 15s and its hold 300s; the test shortens
	// both, keeping the hold far past the outage so only up verdicts release.
	const quiet = 4 * time.Second
	s.setDisruption("reboot", quiet, 120*time.Second)
	s.runNodeReporting("edge-reboot", time.Second)

	s.waitMetric("sensor-1", web.Name, probekeys.PollDuration, func(v float64) bool { return v > 0 }, "a positive mean poll time")
	s.waitFor("sensor-1", feed.Name, probekeys.EndpointReachable, is("up"), "up: the feed's port answered")

	issuedAt := time.Now().UTC()
	var cmd waitedCommand
	if code := s.api(owner, http.MethodPost, "/components/sensor-1/commands:issue",
		map[string]any{"command_type": "reboot"}, &cmd); code >= 300 {
		t.Fatalf("issue reboot = %d", code)
	}
	// A reboot is fire-and-forget, settled at its issue; the device goes
	// once the node has carried it.
	for deadline := time.Now().Add(20 * time.Second); cmd.ExecutedAt == nil; {
		if time.Now().After(deadline) {
			t.Fatalf("reboot = %+v, want executed within 20s", cmd)
		}
		time.Sleep(25 * time.Millisecond)
		s.api(owner, http.MethodGet, fmt.Sprintf("/components/sensor-1/commands/%d", cmd.ID), nil, &cmd)
	}
	// The device goes as a rebooting one does; it comes back later on the
	// very ports it served.
	ports := map[int]int{}
	for _, f := range s.devices[sensorProfile].Faces() {
		ports[f.Declared] = int(f.Addr.Port())
	}
	s.engine.SetPortMap(func(declared int) int { return ports[declared] })
	if err := s.engine.Sync(nil); err != nil {
		t.Fatal(err)
	}

	disrupted := func(v string) bool {
		return strings.HasPrefix(v, "disrupted ") && strings.Contains(v, fmt.Sprintf("command-%d", cmd.ID))
	}
	for _, ep := range []*storage.Endpoint{web, feed} {
		s.waitFor("sensor-1", ep.Name, probekeys.EndpointRejection, disrupted, "disrupted by the reboot")
	}
	s.waitMetric("sensor-1", web.Name, probekeys.PollHeldCount, func(float64) bool {
		return s.latestAt("sensor-1", web.Name, probekeys.PollHeldCount).After(issuedAt)
	}, "the reboot's held polls counted")

	// Past quiet with the device still gone: each probe reads it down, and a
	// down verdict releases nothing.
	time.Sleep(time.Until(issuedAt.Add(quiet + 1500*time.Millisecond)))
	for _, ep := range []*storage.Endpoint{web, feed} {
		if v := s.latest("sensor-1", ep.Name, probekeys.EndpointRejection); !disrupted(v) {
			t.Fatalf("%s's rejection = %q past quiet with the device gone, want still disrupted", ep.Name, v)
		}
	}

	// The device returns: each endpoint reopens on its own up verdict, and
	// the polls run again within a few seconds.
	if err := s.engine.Sync(s.snaps); err != nil {
		t.Fatal(err)
	}
	backAt := time.Now().UTC()
	for _, ep := range []*storage.Endpoint{web, feed} {
		s.waitFor("sensor-1", ep.Name, probekeys.EndpointRejection, ungated, "the gate open again")
	}
	deadline := backAt.Add(5 * time.Second)
	for !s.latestAt("sensor-1", web.Name, probekeys.PollDuration).After(backAt) {
		if time.Now().After(deadline) {
			t.Fatalf("no poll time landed within 5s of the device's return (latest %v)", s.latestAt("sensor-1", web.Name, probekeys.PollDuration))
		}
		time.Sleep(50 * time.Millisecond)
	}

	if failedAt := s.latestAt("sensor-1", web.Name, probekeys.PollFailureCount); failedAt.After(issuedAt) {
		t.Errorf("a poll failure landed at %v, after the reboot at %v; a held poll is not a failure", failedAt, issuedAt)
	}
	for _, ep := range []*storage.Endpoint{web, feed} {
		if at := s.latestAt("sensor-1", ep.Name, probekeys.HoldExpiryCount); !at.IsZero() {
			t.Errorf("%s landed a hold expiry at %v; its up verdict released it", ep.Name, at)
		}
	}
}

// rebootingDriver authors a copy of a shipped http driver whose polls run
// each second and which carries the sensor's reboot.
func (s *httpStack) rebootingDriver(shipped, name string) {
	s.t.Helper()
	d, err := s.gw.GetDriver(s.ctx, shipped)
	if err != nil {
		s.t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(d.Spec, &spec); err != nil {
		s.t.Fatal(err)
	}
	for _, p := range spec["polls"].([]any) {
		p.(map[string]any)["schedule"] = map[string]any{"every": "1s"}
	}
	spec["commands"] = []any{map[string]any{
		"command_type": "reboot",
		"request":      map[string]any{"method": "POST", "path": "/api/admin/reboot"},
	}}
	s.authorDriver(name, spec)
}

// attachBareTCP attaches a driverless tcp endpoint: its probe is all that
// runs against it.
func (s *httpStack) attachBareTCP(comp, nodeName, target string) *storage.Endpoint {
	s.t.Helper()
	ep, err := s.gw.CreateEndpoint(s.ctx, "", storage.EndpointSpec{Transport: "tcp", Component: &comp, Node: &nodeName,
		Params: []byte(`{"target":"` + target + `"}`)}, s.all)
	if err != nil {
		s.t.Fatalf("attach bare tcp: %v", err)
	}
	return ep
}

// setDisruption shortens a seeded disruptive type's quiet and hold, which
// the API refuses on an official type.
func (s *httpStack) setDisruption(typ string, quiet, hold time.Duration) {
	s.t.Helper()
	conn, err := pgx.Connect(s.ctx, s.dsn)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = conn.Close(s.ctx) }()
	if _, err := conn.Exec(s.ctx, `update command_type set quiet_seconds = $2, hold_seconds = $3 where name = $1 and disruptive`,
		typ, int(quiet.Seconds()), int(hold.Seconds())); err != nil {
		s.t.Fatal(err)
	}
}
