//go:build linux

// The simulator serves on 127.x addresses beyond 127.0.0.1, which Linux
// treats as local without configuration.

package node_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/nettest"
	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// runNodeReporting starts the node with its own collection report window, as
// runNode does with the platform's: a test cannot wait the minute.
func (s *gateStack) runNodeReporting(name string, every time.Duration) {
	if s.stop != nil {
		s.stop()
	}
	runCtx, cancel := context.WithCancel(s.ctx)
	ran := make(chan error, 1)
	go func() {
		_, err := node.Run(runCtx, node.Config{ServerURL: s.apiURL, Name: name, Token: s.token,
			HeartbeatEvery: 100 * time.Millisecond, ReportEvery: every})
		ran <- err
	}()
	s.stop = func() {
		cancel()
		<-ran
		s.stop = nil
	}
}

// waitMetric waits for the endpoint's latest key to satisfy ok.
func (s *gateStack) waitMetric(comp, instance, key string, ok func(float64) bool, what string) float64 {
	s.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if v, landed := s.metric(comp, instance, key); landed && ok(v) {
			return v
		}
		time.Sleep(25 * time.Millisecond)
	}
	v, landed := s.metric(comp, instance, key)
	s.t.Fatalf("%s on %s[%s] = %v (landed %v), want %s", key, comp, instance, v, landed, what)
	return 0
}

// TestEveryEndpointReportsItsOwnCollection drives #1369 end to end over the
// real wire: the shipped kestrel-api driver (polls, and a held event stream
// its listeners claim) attached to a simulated bar, and to a second bar
// whose device is gone. Each report window, the live bar's endpoint lands
// the mean time of its polls and the messages its listeners claimed, a
// quiet window as zero; the dead bar's endpoint lands its failed polls. The
// rows are the endpoint's own series (instance: the endpoint), owned by its
// component, and a push may not forge them.
func TestEveryEndpointReportsItsOwnCollection(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var barProfile string
	s := newHTTPStack(t, "edge-report", nil, func(store *profile.Store) []string {
		barProfile = driverProfile(t, store, "kestrel-api")
		return []string{barProfile}
	})
	bar := s.devices[barProfile]
	s.component("bar-live")
	s.component("bar-gone")
	s.secret("bar-login", "basic-auth", map[string]string{"username": "admin", "password": "kestrel"})
	live := s.attachHTTP("bar-live", "kestrel-api", "edge-report", map[string]string{
		"host": s.ips[barProfile], "port": fmt.Sprint(s.httpPort(barProfile)), "login": "bar-login"})
	// The gone bar's address refuses every connect for the test's life.
	goneHost, gonePort, err := net.SplitHostPort(nettest.RefusedAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	gone := s.attachHTTP("bar-gone", "kestrel-api", "edge-report", map[string]string{
		"host": goneHost, "port": gonePort, "login": "bar-login"})
	s.runNodeReporting("edge-report", time.Second)

	s.waitFor("bar-live", live.Name, "session-state", is("up"), "up: the stream held")

	// The live bar's polls answer: their mean time lands, and no failure.
	s.waitMetric("bar-live", live.Name, probekeys.PollDuration, func(v float64) bool { return v > 0 }, "a positive mean poll time")
	if v, landed := s.metric("bar-live", live.Name, probekeys.PollFailureCount); landed {
		t.Errorf("the live bar landed %v failed polls; a failure count lands only when nonzero", v)
	}

	// A change on the bar is a message the stream's listeners claim, counted
	// in its window; the next quiet window lands zero, not nothing.
	setVolume(t, bar, 41)
	s.waitMetric("bar-live", live.Name, probekeys.PushMessageCount, func(v float64) bool { return v >= 1 }, "the claimed change counted")
	s.waitMetric("bar-live", live.Name, probekeys.PushMessageCount, func(v float64) bool { return v == 0 }, "a quiet window as zero")

	// The gone bar's polls are refused at the port: each window counts them.
	s.waitMetric("bar-gone", gone.Name, probekeys.PollFailureCount, func(v float64) bool { return v >= 1 }, "the refused polls counted")
	s.waitMetric("bar-gone", gone.Name, probekeys.PollDuration, func(v float64) bool { return v >= 0 }, "a failed poll's time counted too")
}
