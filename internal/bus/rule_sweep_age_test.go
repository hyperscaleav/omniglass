package bus

import (
	"slices"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// The rule sweep age (#1986) is how long ago the event rule sweep last
// completed: a server's own from a clock the sweep sets, a peer's from the
// stamp it holds in the shared database.

func TestSweepClockAgesFromTheLastCompletedSweep(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	c := newSweepClock(start)
	if got := c.Age(start.Add(40 * time.Second)); got != 40*time.Second {
		t.Errorf("age before any sweep = %v, want the time since the clock began (40s)", got)
	}
	c.Done(start.Add(50 * time.Second))
	if got := c.Age(start.Add(53 * time.Second)); got != 3*time.Second {
		t.Errorf("age 3s after a sweep = %v, want 3s", got)
	}
	if got := c.Age(start.Add(10 * time.Second)); got != 0 {
		t.Errorf("age read before the last sweep = %v, want 0, never negative", got)
	}
}

func TestPeerSweepAgesSkipTheServersOwnComponent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	got := peerSweepAges([]storage.ServerSweepAge{
		{ComponentID: "mine", Age: 2 * time.Second},
		{ComponentID: "peer", Age: 90 * time.Second},
	}, "mine", now)
	want := []storage.PlatformMetric{{ComponentID: "peer", Key: writerclass.RuleSweepAge, Value: 90, TS: now}}
	if !slices.Equal(got, want) {
		t.Errorf("peer sweep ages = %+v, want %+v", got, want)
	}
	if got := peerSweepAges(nil, "mine", now); len(got) != 0 {
		t.Errorf("peer sweep ages of no servers = %+v, want none", got)
	}
}
