//go:build linux

// The simulator serves on 127.x addresses beyond 127.0.0.1, which Linux
// treats as local without configuration.

package node_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestAGoneDeviceHoldsItsPollsUntilItReturns drives #1650 over the real
// wire: the shipped http-json driver, its polls every second, against the
// simulated sensor. The device goes away; after two down verdicts the
// endpoint's gate holds unreachable, so its due polls are held, counted
// apart from failures, with no backoff booked, and a run now answers at
// once, sending nothing. The device comes back on the same address; the
// probe's next up verdict opens the gate and the polls run again within a
// few seconds, not after a backoff.
func TestAGoneDeviceHoldsItsPollsUntilItReturns(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var sensorProfile string
	s := newHTTPStack(t, "edge-held", nil, func(store *profile.Store) []string {
		sensorProfile = driverProfile(t, store, "http-json")
		return []string{sensorProfile}
	})
	owner := s.ownerToken()
	s.fastDriver("http-json", "sensor-fast")
	s.component("sensor-1")
	s.secret("census-key", "api-key", map[string]string{"key": "census-lab-key"})
	ep := s.attachHTTP("sensor-1", "sensor-fast", "edge-held", map[string]string{
		"host": s.ips[sensorProfile], "port": fmt.Sprint(s.httpPort(sensorProfile)), "key": "census-key"})
	s.runNodeReporting("edge-held", time.Second)

	s.waitMetric("sensor-1", ep.Name, probekeys.PollDuration, func(v float64) bool { return v > 0 }, "a positive mean poll time")

	// The device goes; it comes back later on the very ports it served.
	ports := map[int]int{}
	for _, f := range s.devices[sensorProfile].Faces() {
		ports[f.Declared] = int(f.Addr.Port())
	}
	s.engine.SetPortMap(func(declared int) int { return ports[declared] })
	if err := s.engine.Sync(nil); err != nil {
		t.Fatal(err)
	}

	s.waitFor("sensor-1", ep.Name, "endpoint-rejection", func(v string) bool { return strings.HasPrefix(v, "unreachable ") }, "the gate holding unreachable")
	heldAt := time.Now().UTC()
	s.waitMetric("sensor-1", ep.Name, probekeys.PollHeldCount, func(float64) bool {
		return s.latestAt("sensor-1", ep.Name, probekeys.PollHeldCount).After(heldAt.Add(time.Second))
	}, "held polls counted a window after the gate closed")
	if failedAt := s.latestAt("sensor-1", ep.Name, probekeys.PollFailureCount); failedAt.After(heldAt) {
		t.Errorf("a poll failure landed at %v, after the gate held unreachable at %v; a held poll is not a failure", failedAt, heldAt)
	}

	// A run now answers at once with the reason, sending nothing.
	path := "/endpoints/sensor-1%2F" + ep.Name + "/polls/status:run"
	if code := s.api(owner, http.MethodPost, path, nil, nil); code != http.StatusConflict {
		t.Errorf("run now on a held endpoint = %d, want 409", code)
	}

	// The device returns: the next up verdict opens the gate, and the due
	// polls run within a few seconds, no backoff booked against them.
	if err := s.engine.Sync(s.snaps); err != nil {
		t.Fatal(err)
	}
	backAt := time.Now().UTC()
	s.waitFor("sensor-1", ep.Name, "endpoint-rejection", ungated, "the gate open again")
	deadline := backAt.Add(5 * time.Second)
	for !s.latestAt("sensor-1", ep.Name, probekeys.PollDuration).After(backAt) {
		if time.Now().After(deadline) {
			t.Fatalf("no poll time landed within 5s of the device's return (latest %v); a held poll must not back off",
				s.latestAt("sensor-1", ep.Name, probekeys.PollDuration))
		}
		time.Sleep(50 * time.Millisecond)
	}
}
