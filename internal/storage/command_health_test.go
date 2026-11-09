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
// the window's counts only when nonzero and only for a read with a window.
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
		at("pa", writerclass.CommandExpiredCount, writerclass.CommandExpiredUnexecuted, 1),
	}, idle)
	if !slices.Equal(windowed, want) {
		t.Errorf("windowed read = %+v\nwant %+v: a zero count writes nothing", windowed, want)
	}
}
