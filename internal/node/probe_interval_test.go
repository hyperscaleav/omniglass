package node

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// countingDialer is the tcp probe primitive, counting its probes.
type countingDialer struct{ dials int }

func (d *countingDialer) Dial(context.Context, string, time.Duration) (float64, collection.Reachability, error) {
	d.dials++
	return 1, collection.Responded, nil
}

// TestProbeIntervalGatesReachabilityProbes is #1273: a node probes each
// endpoint at its probe interval, not on every heartbeat tick, so a lab can
// check in every 2 s for responsiveness and probe every 10 s. Unset, the
// interval is the heartbeat and every tick probes, as before.
func TestProbeIntervalGatesReachabilityProbes(t *testing.T) {
	wl := collection.WorklistReply{Tasks: []collection.TaskSpec{{
		ID: "t-tcp", Mode: "poll", Transport: "tcp",
		EndpointParams: json.RawMessage(`{"target":"127.0.0.1:80"}`),
	}}}
	start := time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)
	ticks := 6 // one second apart

	for _, tc := range []struct {
		name       string
		probeEvery time.Duration
		want       int
	}{
		{"unset probes every tick", 0, 6},
		{"a 3s interval probes on ticks 0 and 3", 3 * time.Second, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dialer := &countingDialer{}
			runner := &collection.Runner{TCP: dialer}
			spool := newTelemetrySpool("og.v1.telemetry.edge", func(string, []byte, string) (ackWait, error) {
				return func(context.Context) error { return nil }, nil
			})
			sessions := newSessionManager(nil, sessionHooks{})
			sched := newPollSchedule(time.Second)
			sched.probeEvery = tc.probeEvery
			verdicts, faultseen := map[string]string{}, map[string]string{}
			for i := 0; i < ticks; i++ {
				now := start.Add(time.Duration(i) * time.Second)
				if err := runTasks(t.Context(), spool, "edge", wl, runner, sessions, sched, verdicts, faultseen, nil, now); err != nil {
					t.Fatalf("tick %d: %v", i, err)
				}
			}
			if dialer.dials != tc.want {
				t.Fatalf("%d ticks probed %d times, want %d", ticks, dialer.dials, tc.want)
			}
		})
	}
}

// A probe whose endpoint's connection fingerprint moves (an address edited in
// place, a secret rotated, an operator's :retry) runs at the next tick, as a
// driver poll does (#1061): its booking belongs to inputs that are gone, and
// an operator who fixed the endpoint never waits out the probe interval.
func TestProbeRunsAtOnceWhenItsFingerprintMoves(t *testing.T) {
	task := collection.TaskSpec{
		ID: "t-tcp", Mode: "poll", Transport: "tcp", Fingerprint: "fp-wrong-port",
		EndpointParams: json.RawMessage(`{"target":"127.0.0.1:80"}`),
	}
	dialer := &countingDialer{}
	runner := &collection.Runner{TCP: dialer}
	spool := newTelemetrySpool("og.v1.telemetry.edge", func(string, []byte, string) (ackWait, error) {
		return func(context.Context) error { return nil }, nil
	})
	sessions := newSessionManager(nil, sessionHooks{})
	sched := newPollSchedule(time.Second)
	sched.probeEvery = 10 * time.Second
	verdicts, faultseen := map[string]string{}, map[string]string{}
	now := time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)
	tick := func(fp string) {
		t.Helper()
		task.Fingerprint = fp
		wl := collection.WorklistReply{Tasks: []collection.TaskSpec{task}}
		if err := runTasks(t.Context(), spool, "edge", wl, runner, sessions, sched, verdicts, faultseen, nil, now); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
	tick("fp-wrong-port")
	tick("fp-wrong-port")
	if dialer.dials != 1 {
		t.Fatalf("an unchanged probe inside its interval: %d probes, want 1", dialer.dials)
	}
	tick("fp-fixed-port")
	if dialer.dials != 2 {
		t.Fatalf("a moved fingerprint: %d probes, want the probe run at once (2)", dialer.dials)
	}
	tick("fp-fixed-port")
	if dialer.dials != 2 {
		t.Fatalf("after the rerun: %d probes, want it booked a full interval again (2)", dialer.dials)
	}
}

// A probe whose interval equals the heartbeat runs on every tick, however the
// work before it in each tick (the heartbeat publish, the worklist pull)
// jitters the time it is judged at. The schedule books from that judged time,
// so without a tolerance a tick that reached the probe a few milliseconds
// sooner than the last found it not yet due and skipped it, doubling the gap.
func TestAProbeAtTheHeartbeatRunsEveryJitteredTick(t *testing.T) {
	wl := collection.WorklistReply{Tasks: []collection.TaskSpec{{
		ID: "t-tcp", Mode: "poll", Transport: "tcp", Fingerprint: "fp",
		EndpointParams: json.RawMessage(`{"target":"127.0.0.1:80"}`),
	}}}
	dialer := &countingDialer{}
	runner := &collection.Runner{TCP: dialer}
	spool := newTelemetrySpool("og.v1.telemetry.edge", func(string, []byte, string) (ackWait, error) {
		return func(context.Context) error { return nil }, nil
	})
	sessions := newSessionManager(nil, sessionHooks{})
	sched := newPollSchedule(2 * time.Second)
	sched.probeEvery = 2 * time.Second
	verdicts, faultseen := map[string]string{}, map[string]string{}
	start := time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)
	offsets := []time.Duration{12, 9, 14, 8, 15, 11} // ms of work before the probe, per tick
	for i, off := range offsets {
		now := start.Add(time.Duration(i)*2*time.Second + off*time.Millisecond)
		if err := runTasks(t.Context(), spool, "edge", wl, runner, sessions, sched, verdicts, faultseen, nil, now); err != nil {
			t.Fatal(err)
		}
	}
	if dialer.dials != len(offsets) {
		t.Fatalf("%d jittered ticks at a 2s heartbeat probed %d times, want every tick", len(offsets), dialer.dials)
	}
}

// A task that leaves the worklist leaves nothing behind in the schedule, even
// one that was never booked (a probe at the heartbeat, a poll with no
// schedule): retain sweeps every map by its own keys.
func TestRetainForgetsATaskThatWasNeverBooked(t *testing.T) {
	s := newPollSchedule(2 * time.Second)
	s.rebook("p1", "fp")
	s.refusedRun("p1")
	s.retain(map[string]bool{})
	if len(s.fingerprint) != 0 || len(s.refused) != 0 {
		t.Fatalf("a task gone from the worklist left fingerprint %v refused %v", s.fingerprint, s.refused)
	}
}
