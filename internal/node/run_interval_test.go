//go:build linux

// The simulator serves on 127.x addresses beyond 127.0.0.1, which Linux
// treats as local without configuration.

package node_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestAPollRunsOnItsOwnIntervalBesideADeadDevice drives #1634 over the real
// wire: a node that checks in every 30s, the shipped http-json driver
// re-authored so each poll runs every 10s, against the simulated sensor, and
// a second component whose device accepts a connection and never answers.
// The live sensor's samples land 10s apart, within a second, though the
// heartbeat is three times that: a poll runs on its own interval, on the
// dispatcher's timer. The dead device holds only its own endpoint's lane,
// so it delays nothing on the live one.
func TestAPollRunsOnItsOwnIntervalBesideADeadDevice(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	const every = 10 * time.Second
	var sensorProfile string
	s := newHTTPStack(t, "edge-interval", nil, func(store *profile.Store) []string {
		sensorProfile = driverProfile(t, store, "http-json")
		return []string{sensorProfile}
	})
	s.everyDriver("http-json", "sensor-10s", every.String())
	s.secret("census-key", "api-key", map[string]string{"key": "census-lab-key"})
	s.component("sensor-live")
	s.component("sensor-dead")
	s.attachHTTP("sensor-live", "sensor-10s", "edge-interval", map[string]string{
		"host": s.ips[sensorProfile], "port": fmt.Sprint(s.httpPort(sensorProfile)), "key": "census-key"})
	dead, accepted := silentListener(t)
	deadHost, deadPort, err := net.SplitHostPort(dead)
	if err != nil {
		t.Fatal(err)
	}
	s.attachHTTP("sensor-dead", "sensor-10s", "edge-interval", map[string]string{
		"host": deadHost, "port": deadPort, "key": "census-key"})

	runCtx, cancel := context.WithCancel(s.ctx)
	ran := make(chan error, 1)
	go func() {
		_, err := node.Run(runCtx, node.Config{ServerURL: s.apiURL, Name: "edge-interval", Token: s.token,
			HeartbeatEvery: 30 * time.Second})
		ran <- err
	}()
	t.Cleanup(func() {
		cancel()
		<-ran
	})

	// The first run is at once and the next lands on the task's phase, the
	// first slot at least half an interval on (so between half and one and a
	// half intervals later); from there each is an interval apart. Four
	// samples is at most 35s and change.
	const want = 4
	var at []time.Time
	deadline := time.Now().Add(55 * time.Second)
	for time.Now().Before(deadline) {
		series, err := s.gw.ListMetricSeries(s.ctx, "component", "sensor-live", "uptime", 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		at = at[:0]
		for _, m := range series {
			at = append(at, m.TS)
		}
		if len(at) >= want {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if len(at) < want {
		t.Fatalf("the live sensor landed %d uptime samples in 55s at a 30s heartbeat, want %d at its 10s interval", len(at), want)
	}
	sort.Slice(at, func(i, j int) bool { return at[i].Before(at[j]) })
	if first := at[1].Sub(at[0]); first < every/2-time.Second || first > every+every/2+time.Second {
		t.Errorf("the second sample landed %v after the first, want between half and one and a half %v intervals (samples at %v)", first, every, at)
	}
	for i := 2; i < len(at); i++ {
		gap := at[i].Sub(at[i-1])
		if gap < every-time.Second || gap > every+time.Second {
			t.Errorf("uptime samples %d and %d landed %v apart, want %v within 1s (samples at %v)", i-1, i, gap, every, at)
		}
	}
	if accepted() == 0 {
		t.Error("the dead device was never asked; the test proves nothing about its delay")
	}
}

// everyDriver authors a copy of a shipped driver whose every poll runs at
// the given interval.
func (s *httpStack) everyDriver(shipped, name, every string) {
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
		p.(map[string]any)["schedule"] = map[string]any{"every": every}
	}
	s.authorDriver(name, spec)
}

// silentListener is a device that accepts every connection and never
// answers: a request to it waits out its whole timeout. accepted counts the
// connections it took.
func silentListener(t *testing.T) (addr string, accepted func() int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			go func() { _, _ = io.Copy(io.Discard, c) }()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return ln.Addr().String(), func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(conns)
	}
}
