//go:build linux

// The simulator serves on 127.x addresses beyond 127.0.0.1, which Linux
// treats as local without configuration.

package node_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
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
		barProfile = driverProfile(t, store, "kestrel-api", "websocket") // the bar (only it has the control socket)
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

// fastDriver authors a copy of a shipped driver whose every poll runs each
// second, so a test sees a gate refuse due polls window after window.
func (s *httpStack) fastDriver(shipped, name string) {
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
	s.authorDriver(name, spec)
}

// latestAt is the time of the endpoint's latest key, zero when none landed.
func (s *gateStack) latestAt(comp, instance, key string) time.Time {
	m, err := s.gw.LatestMetricInstance(s.ctx, comp, key, instance)
	if err != nil || m == nil {
		return time.Time{}
	}
	return m.TS
}

// TestAGatedPollAndARunNowCountInTheReport drives the two poll paths beside
// the scheduled run (#1369's review): a due poll the endpoint's gate refuses
// is a failure every window the gate holds, with no time, so a credential
// lockout's failure count does not fall silent after its first window; and
// a run now is a poll run, timed in its window like a scheduled one.
func TestAGatedPollAndARunNowCountInTheReport(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	var barProfile string
	s := newHTTPStack(t, "edge-gate-report", nil, func(store *profile.Store) []string {
		barProfile = driverProfile(t, store, "kestrel-api", "websocket") // the bar (only it has the control socket)
		return []string{barProfile}
	})
	owner := s.ownerToken()
	s.fastDriver("kestrel-api", "bar-fast")
	s.streamDriver("kestrel-api", "bar-slow") // its one poll runs every five minutes
	s.component("bar-locked")
	s.component("bar-run")
	s.secret("bar-wrong", "basic-auth", map[string]string{"username": "admin", "password": "wrong"})
	s.secret("bar-login", "basic-auth", map[string]string{"username": "admin", "password": "kestrel"})
	at := map[string]string{"host": s.ips[barProfile], "port": fmt.Sprint(s.httpPort(barProfile))}
	with := func(login string) map[string]string {
		return map[string]string{"host": at["host"], "port": at["port"], "login": login}
	}
	locked := s.attachHTTP("bar-locked", "bar-fast", "edge-gate-report", with("bar-wrong"))
	run := s.attachHTTP("bar-run", "bar-slow", "edge-gate-report", with("bar-login"))
	s.runNodeReporting("edge-gate-report", time.Second)

	// The wrong password gates the endpoint. Two windows on, every due poll
	// is still refused: the window counts failures and times none.
	s.waitFor("bar-locked", locked.Name, "endpoint-rejection", func(v string) bool { return !ungated(v) }, "the endpoint gated")
	gatedAt := time.Now().UTC()
	s.waitMetric("bar-locked", locked.Name, probekeys.PollFailureCount, func(float64) bool {
		return s.latestAt("bar-locked", locked.Name, probekeys.PollFailureCount).After(gatedAt.Add(2 * time.Second))
	}, "refused polls counted two windows after the gate")
	if failedAt, timedAt := s.latestAt("bar-locked", locked.Name, probekeys.PollFailureCount), s.latestAt("bar-locked", locked.Name, probekeys.PollDuration); !timedAt.Before(failedAt) {
		t.Errorf("the gated window landed a poll time (%v, failures at %v); a refused poll sent nothing", timedAt, failedAt)
	}

	// The slow bar's poll ran at the start and is not due again for five
	// minutes, so its windows land no poll time until a run now.
	s.waitMetric("bar-run", run.Name, probekeys.PollDuration, func(v float64) bool { return v > 0 }, "the first scheduled run timed")
	time.Sleep(2500 * time.Millisecond)
	ranAt := time.Now().UTC()
	if !s.latestAt("bar-run", run.Name, probekeys.PollDuration).Before(ranAt.Add(-time.Second)) {
		t.Fatal("a poll time landed in a window with no run; the run now below would prove nothing")
	}
	path := "/endpoints/bar-run%2F" + run.Name + "/polls/system:run"
	if code := s.api(owner, http.MethodPost, path, nil, nil); code != http.StatusOK {
		t.Fatalf("run now = %d", code)
	}
	s.waitMetric("bar-run", run.Name, probekeys.PollDuration, func(float64) bool {
		return s.latestAt("bar-run", run.Name, probekeys.PollDuration).After(ranAt)
	}, "the run now timed in its window")
}
