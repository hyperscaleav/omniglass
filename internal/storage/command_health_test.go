package storage

import (
	"slices"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// TestCommandQueueMetricsWritesTheGaugesAndTheWindowsCounts maps one health
// read: the gauges every read, an idle queue's age at zero (never absent, so
// a drained queue's last age is not left standing as its latest sample), and
// the window's counts every read with a window, a quiet window's as zero.
func TestCommandQueueMetricsWritesTheGaugesAndTheWindowsCounts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	oldest := now.Add(-90 * time.Second)
	read := CommandQueueRead{Now: now, Nodes: []CommandQueue{
		{Node: "edge-a", ComponentID: "pa", Pending: 3, Oldest: &oldest, Stuck: 1, Redelivered: 2, ExpiredUnexecuted: 1},
		{Node: "edge-b", ComponentID: "pb"},
	}}
	at := func(c, key, instance string, v float64) PlatformMetric {
		return PlatformMetric{ComponentID: c, Key: key, Instance: instance, Value: v, TS: now}
	}
	gauges := []PlatformMetric{
		at("pa", writerclass.CommandPendingCount, "", 3),
		at("pa", writerclass.CommandStuckCount, "", 1),
		at("pa", writerclass.CommandPendingAge, "", 90),
	}
	idle := []PlatformMetric{
		at("pb", writerclass.CommandPendingCount, "", 0),
		at("pb", writerclass.CommandStuckCount, "", 0),
		at("pb", writerclass.CommandPendingAge, "", 0),
	}

	first := CommandQueueMetrics(read, time.Time{})
	if want := slices.Concat(gauges, idle); !slices.Equal(first, want) {
		t.Errorf("first read = %+v\nwant %+v: no window, so no counts", first, want)
	}

	windowed := CommandQueueMetrics(read, now.Add(-15*time.Second))
	want := slices.Concat(gauges, []PlatformMetric{
		at("pa", writerclass.CommandRedeliveryCount, "", 2),
		at("pa", writerclass.CommandExpiredCount, writerclass.CommandExpiredUndelivered, 0),
		at("pa", writerclass.CommandExpiredCount, writerclass.CommandExpiredUnexecuted, 1),
	}, idle, []PlatformMetric{
		at("pb", writerclass.CommandRedeliveryCount, "", 0),
		at("pb", writerclass.CommandExpiredCount, writerclass.CommandExpiredUndelivered, 0),
		at("pb", writerclass.CommandExpiredCount, writerclass.CommandExpiredUnexecuted, 0),
	})
	if !slices.Equal(windowed, want) {
		t.Errorf("windowed read = %+v\nwant %+v: a zero count is written as zero", windowed, want)
	}
}

// TestCommandQueueCountsReturnToZeroAfterTheirWindow: a window with a
// redelivery and an expiry is followed by a quiet one, and the quiet window
// writes 0 for both keys, so each key's latest value is 0 again rather than
// the event's count frozen as current.
func TestCommandQueueCountsReturnToZeroAfterTheirWindow(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	t1, t2 := t0.Add(15*time.Second), t0.Add(30*time.Second)
	event := CommandQueueMetrics(CommandQueueRead{Now: t1, Nodes: []CommandQueue{
		{Node: "edge-a", ComponentID: "pa", Redelivered: 1, ExpiredUndelivered: 1, ExpiredUnexecuted: 1},
	}}, t0)
	quiet := CommandQueueMetrics(CommandQueueRead{Now: t2, Nodes: []CommandQueue{
		{Node: "edge-a", ComponentID: "pa"},
	}}, t1)

	type series struct{ key, instance string }
	latest := map[series]PlatformMetric{}
	for _, m := range slices.Concat(event, quiet) {
		latest[series{m.Key, m.Instance}] = m
	}
	for _, s := range []series{
		{writerclass.CommandRedeliveryCount, ""},
		{writerclass.CommandExpiredCount, writerclass.CommandExpiredUndelivered},
		{writerclass.CommandExpiredCount, writerclass.CommandExpiredUnexecuted},
	} {
		m, ok := latest[s]
		if !ok || !m.TS.Equal(t2) || m.Value != 0 {
			t.Errorf("%s %q latest = %+v (written %v), want 0 at the quiet window %v", s.key, s.instance, m, ok, t2)
		}
	}
}
