package node

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// countingDialer is the tcp probe primitive, counting its probes. Workers
// dial from their own goroutines, so the count is locked.
type countingDialer struct {
	mu    sync.Mutex
	dials int
}

func (d *countingDialer) Dial(context.Context, string, time.Duration) (float64, collection.Reachability, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dials++
	return 1, collection.Responded, nil
}

func (d *countingDialer) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dials
}

func tcpProbeTask(fingerprint string) collection.TaskSpec {
	return collection.TaskSpec{ID: "t-tcp", Mode: "poll", Transport: "tcp", Fingerprint: fingerprint,
		EndpointParams: json.RawMessage(`{"target":"127.0.0.1:80"}`)}
}

// A node probes each endpoint on its probe interval (#1273), from the
// dispatcher's own timer, never the heartbeat (#1634): unset, the interval is
// DefaultProbeEvery however often the node checks in, and a set interval is
// honored as given. The run loop here passes every second.
func TestAProbeRunsOnItsProbeIntervalNotTheHeartbeat(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		probeEvery time.Duration
		every      time.Duration
		ticks      int
		want       []time.Duration
	}{
		{"unset probes at the default", 0, DefaultProbeEvery, 91,
			[]time.Duration{0, 30 * time.Second, 60 * time.Second, 90 * time.Second}},
		{"a 3s interval probes every 3s", 3 * time.Second, 3 * time.Second, 10,
			[]time.Duration{0, 3 * time.Second, 6 * time.Second, 9 * time.Second}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dialer := &countingDialer{}
			l := newReasonLoop(t, &collection.Runner{TCP: dialer})
			l.probeEvery = tc.probeEvery
			start := aligned(gateT0, "t-tcp", tc.every)
			l.now = start
			var got []time.Duration
			for range tc.ticks {
				at, before := l.now, dialer.count()
				l.tick(tcpProbeTask("fp"))
				if dialer.count() > before {
					got = append(got, at.Sub(start))
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("probed at %v, want %v", got, tc.want)
			}
		})
	}
}

// A probe whose endpoint's connection fingerprint moves (an address edited in
// place, a secret rotated, an operator's :retry) runs at once, as a driver
// poll does (#1061): its booking belongs to inputs that are gone, and an
// operator who fixed the endpoint never waits out the probe interval.
func TestProbeRunsAtOnceWhenItsFingerprintMoves(t *testing.T) {
	t.Parallel()
	dialer := &countingDialer{}
	l := newReasonLoop(t, &collection.Runner{TCP: dialer})
	l.probeEvery = 10 * time.Second
	l.now = aligned(gateT0, "t-tcp", l.probeEvery)
	l.tick(tcpProbeTask("fp-wrong-port"))
	l.tick(tcpProbeTask("fp-wrong-port"))
	if n := dialer.count(); n != 1 {
		t.Fatalf("an unchanged probe inside its interval: %d probes, want 1", n)
	}
	l.tick(tcpProbeTask("fp-fixed-port"))
	if n := dialer.count(); n != 2 {
		t.Fatalf("a moved fingerprint: %d probes, want the probe run at once (2)", n)
	}
	l.tick(tcpProbeTask("fp-fixed-port"))
	if n := dialer.count(); n != 2 {
		t.Fatalf("after the rerun: %d probes, want it booked on its interval again (2)", n)
	}
}

// A task that leaves the worklist leaves nothing behind in the schedule, even
// one that was never booked: retain sweeps every map by its own keys.
func TestRetainForgetsATaskThatWasNeverBooked(t *testing.T) {
	t.Parallel()
	s := newPollSchedule()
	s.rebook("p1", "fp")
	s.refusedRun("p1")
	s.retain(map[string]bool{})
	if len(s.fingerprint) != 0 || len(s.refused) != 0 {
		t.Fatalf("a task gone from the worklist left fingerprint %v refused %v", s.fingerprint, s.refused)
	}
}
