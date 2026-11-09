package platformmetric_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/platformmetric"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// TestPoolReadingsAreTheWindowsDeltas: the pool's occupancy reads as it
// stands at the flush, its waits as what the window added to the pool's
// running totals.
func TestPoolReadingsAreTheWindowsDeltas(t *testing.T) {
	t.Parallel()
	prev := storage.PoolStat{Acquired: 1, Idle: 3, Total: 4, Max: 10, EmptyAcquireCount: 5, EmptyAcquireWait: 40 * time.Millisecond}
	cur := storage.PoolStat{Acquired: 6, Idle: 0, Total: 6, Max: 10, EmptyAcquireCount: 9, EmptyAcquireWait: 140 * time.Millisecond}
	want := map[string]float64{
		writerclass.DBPoolAcquired:     6,
		writerclass.DBPoolIdle:         0,
		writerclass.DBPoolTotal:        6,
		writerclass.DBPoolMax:          10,
		writerclass.DBPoolWaitCount:    4,
		writerclass.DBPoolWaitDuration: 100,
	}
	got := platformmetric.PoolReadings(prev, cur)
	if len(got) != len(want) {
		t.Fatalf("readings = %+v, want %d", got, len(want))
	}
	for _, r := range got {
		if v, ok := want[r.Key]; !ok || v != r.Value {
			t.Errorf("%s = %v, want %v", r.Key, r.Value, v)
		}
	}
}

// A pool whose running totals went backwards (a pool rebuilt under the
// sampler) reads no negative wait.
func TestPoolReadingsNeverGoNegative(t *testing.T) {
	t.Parallel()
	prev := storage.PoolStat{EmptyAcquireCount: 9, EmptyAcquireWait: time.Second}
	for _, r := range platformmetric.PoolReadings(prev, storage.PoolStat{Max: 2}) {
		if r.Value < 0 {
			t.Errorf("%s = %v, want no negative", r.Key, r.Value)
		}
	}
}

// fakeStore keeps what the reporter wrote.
type fakeStore struct {
	mu   sync.Mutex
	rows [][]storage.PlatformMetric
	err  error
}

func (s *fakeStore) RecordPlatformMetrics(_ context.Context, rows []storage.PlatformMetric) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, rows)
	return s.err
}

func (s *fakeStore) writes() [][]storage.PlatformMetric {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]storage.PlatformMetric(nil), s.rows...)
}

// TestReporterSamplesThenFlushesOneWindow: a report runs its samplers into
// the recorder, then lands the window on the component, and a window with
// nothing to say writes nothing.
func TestReporterSamplesThenFlushesOneWindow(t *testing.T) {
	t.Parallel()
	rec := platformmetric.NewRecorder(time.Now)
	store := &fakeStore{}
	sampled := 0
	rp := platformmetric.NewReporter(rec, store, "server-1", func(context.Context) time.Duration { return time.Minute }, nil,
		func(_ context.Context, r *platformmetric.Recorder) {
			sampled++
			if sampled == 1 {
				r.Gauge(writerclass.DBPoolMax, "", 4)
			}
		})
	rec.Count(writerclass.HTTPRequestCount, "GET /x 2xx", 1)
	if err := rp.Report(context.Background()); err != nil {
		t.Fatalf("report: %v", err)
	}
	w := store.writes()
	if len(w) != 1 || len(w[0]) != 2 {
		t.Fatalf("writes = %+v, want one write of the count and the sampled gauge", w)
	}
	for _, row := range w[0] {
		if row.ComponentID != "server-1" {
			t.Errorf("%s landed on %q, want server-1", row.Key, row.ComponentID)
		}
	}
	// The count's one zero, then a window with nothing at all.
	_ = rp.Report(context.Background())
	if err := rp.Report(context.Background()); err != nil {
		t.Fatalf("report: %v", err)
	}
	if w := store.writes(); len(w) != 2 {
		t.Fatalf("writes = %d, want the empty window to write nothing", len(w))
	}
}

// TestReporterRunsEachWindowUntilCancelled: the loop reports once a window
// and stops with its context, and a failed write costs only its window.
func TestReporterRunsEachWindowUntilCancelled(t *testing.T) {
	t.Parallel()
	rec := platformmetric.NewRecorder(time.Now)
	store := &fakeStore{err: errors.New("database away")}
	rp := platformmetric.NewReporter(rec, store, "server-1", func(context.Context) time.Duration { return 5 * time.Millisecond }, nil,
		func(_ context.Context, r *platformmetric.Recorder) { r.Gauge(writerclass.DBPoolMax, "", 4) })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { rp.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for len(store.writes()) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if n := len(store.writes()); n < 3 {
		t.Fatalf("reported %d windows, want the loop to keep going past failed writes", n)
	}
}
