package platformmetric

import (
	"context"
	"log/slog"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// Store lands a report window's samples on their component.
type Store interface {
	RecordPlatformMetrics(ctx context.Context, rows []storage.PlatformMetric) error
}

// Sampler reads a figure at the window's close into the recorder: a gauge of
// something the process holds (its pool, its runtime) rather than an event it
// counts as it happens.
type Sampler func(ctx context.Context, r *Recorder)

// reportTimeout bounds one window's write, so a stalled database costs that
// window and not the loop.
const reportTimeout = 10 * time.Second

// Reporter lands a recorder's windows on one platform component (#1627): each
// window it runs its samplers, flushes the recorder and writes what the flush
// read. The window is re-read every pass, so a change to the report-window
// setting takes at the next window without a restart.
type Reporter struct {
	rec         *Recorder
	store       Store
	componentID string
	window      func(context.Context) time.Duration
	log         *slog.Logger
	samplers    []Sampler
}

// NewReporter reports rec's windows onto componentID through store, every
// window, after the samplers. A nil log is slog.Default.
func NewReporter(rec *Recorder, store Store, componentID string, window func(context.Context) time.Duration, log *slog.Logger, samplers ...Sampler) *Reporter {
	if log == nil {
		log = slog.Default()
	}
	return &Reporter{rec: rec, store: store, componentID: componentID, window: window, log: log, samplers: samplers}
}

// Run reports once a window until ctx ends. A failed write is logged and
// costs only its window.
func (rp *Reporter) Run(ctx context.Context) {
	for {
		t := time.NewTimer(rp.window(ctx))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		if err := rp.Report(ctx); err != nil && ctx.Err() == nil {
			rp.log.Warn("platform metrics: report the window", "component", rp.componentID, "err", err)
		}
	}
}

// Report closes one window: samples, flushes and writes. A window with
// nothing to say writes nothing.
func (rp *Reporter) Report(ctx context.Context) error {
	for _, s := range rp.samplers {
		s(ctx, rp.rec)
	}
	rows := rp.rec.Flush(rp.componentID)
	if len(rows) == 0 {
		return nil
	}
	wctx, cancel := context.WithTimeout(ctx, reportTimeout)
	defer cancel()
	return rp.store.RecordPlatformMetrics(wctx, rows)
}

// PoolReadings are a database pool's platform metrics over the window between
// prev and cur: its connections in use, idle and open and its most as they
// stand at cur, and the acquires that waited on it exhausted and the time
// they waited as what the window added to the pool's running totals. A total
// that went backwards (a pool rebuilt between reads) reads zero, never less.
func PoolReadings(prev, cur storage.PoolStat) []Reading {
	return []Reading{
		{Key: writerclass.DBPoolAcquired, Value: float64(cur.Acquired)},
		{Key: writerclass.DBPoolIdle, Value: float64(cur.Idle)},
		{Key: writerclass.DBPoolTotal, Value: float64(cur.Total)},
		{Key: writerclass.DBPoolMax, Value: float64(cur.Max)},
		{Key: writerclass.DBPoolWaitCount, Value: float64(max(cur.EmptyAcquireCount-prev.EmptyAcquireCount, 0))},
		{Key: writerclass.DBPoolWaitDuration, Value: Millis(max(cur.EmptyAcquireWait-prev.EmptyAcquireWait, 0))},
	}
}

// PoolSampler gauges a pool each window from read, keeping the last read so
// each window's waits are its own.
func PoolSampler(read func() storage.PoolStat) Sampler {
	prev := read()
	return func(_ context.Context, r *Recorder) {
		cur := read()
		for _, rd := range PoolReadings(prev, cur) {
			r.Gauge(rd.Key, "", rd.Value)
		}
		prev = cur
	}
}
