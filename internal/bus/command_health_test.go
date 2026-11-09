package bus

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// healthStore answers the command queue health reads in turn and keeps what
// the timer asked and wrote.
type healthStore struct {
	fakeStore
	reads  []storage.CommandQueueRead
	errs   []error
	asked  *[]time.Time
	landed *[][]storage.PlatformMetric
}

func (h *healthStore) CommandQueueHealth(_ context.Context, since time.Time) (storage.CommandQueueRead, error) {
	*h.asked = append(*h.asked, since)
	i := len(*h.asked) - 1
	return h.reads[i], h.errs[i]
}

func (h *healthStore) RecordPlatformMetrics(_ context.Context, rows []storage.PlatformMetric) error {
	*h.landed = append(*h.landed, rows)
	return nil
}

// TestCommandHealthCarriesTheWindowBetweenPasses runs three passes: the
// first reads with no window and writes the gauges alone; a failed read keeps
// the window's start, so the next pass's window covers it; that pass writes
// the window's counts.
func TestCommandHealthCarriesTheWindowBetweenPasses(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	t2 := t0.Add(30 * time.Second)
	q := storage.CommandQueue{Node: "edge-a", ComponentID: "pa", Pending: 2, Stuck: 1, ExpiredUndelivered: 1}
	var asked []time.Time
	var landed [][]storage.PlatformMetric
	s := &Server{store: &healthStore{
		reads:  []storage.CommandQueueRead{{Now: t0, Nodes: []storage.CommandQueue{q}}, {}, {Now: t2, Nodes: []storage.CommandQueue{q}}},
		errs:   []error{nil, errors.New("database away"), nil},
		asked:  &asked,
		landed: &landed,
	}}

	var since time.Time
	for i := range 3 {
		next, err := s.recordCommandQueueHealth(context.Background(), since)
		if (err != nil) != (i == 1) {
			t.Fatalf("pass %d: err = %v", i, err)
		}
		since = next
	}

	if want := []time.Time{{}, t0, t0}; !slices.Equal(asked, want) {
		t.Errorf("windows asked = %v, want %v: the failed read keeps its start", asked, want)
	}
	gauges := []storage.PlatformMetric{
		{ComponentID: "pa", Key: writerclass.CommandPendingCount, Value: 2, TS: t0},
		{ComponentID: "pa", Key: writerclass.CommandStuckCount, Value: 1, TS: t0},
	}
	if len(landed) != 2 || !slices.Equal(landed[0], gauges) {
		t.Fatalf("writes = %+v, want the first pass's gauges %+v and one more", landed, gauges)
	}
	expired := storage.PlatformMetric{ComponentID: "pa", Key: writerclass.CommandExpiredCount,
		Instance: writerclass.CommandExpiredUndelivered, Value: 1, TS: t2}
	if !slices.Contains(landed[1], expired) {
		t.Errorf("third pass wrote %+v, want it to carry %+v", landed[1], expired)
	}
	if since != t2 {
		t.Errorf("since after the third pass = %v, want %v", since, t2)
	}
}
