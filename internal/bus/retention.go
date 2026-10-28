package bus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hyperscaleav/omniglass/internal/settings"
)

// The sample retention pass (#417): how often it runs, how long one pass may
// take, and how much one pass removes. A pass deletes a lane in chunks of
// sampleRetentionChunk rows, each its own short statement, and stops after
// sampleRetentionMaxChunks so a first pass over a large backlog cannot hold the
// database for long.
//
// A pass that kept up runs again in sampleRetentionEvery. Its last chunk comes
// back short, and finding that out reads every page the ts BRIN cannot rule out;
// once vacuum frees pruned pages and ingest reuses them, those ranges widen
// toward the whole table and never narrow, until ts partitioning (#420) drops
// whole partitions instead. So the steady state runs rarely. A pass that left a
// lane behind runs again in sampleRetentionCatchUp, so a backlog drains at up to
// 200,000 rows a lane a minute rather than an hour.
const (
	sampleRetentionEvery     = time.Hour
	sampleRetentionCatchUp   = time.Minute
	sampleRetentionTimeout   = 5 * time.Minute // a lane's deadline within a pass
	sampleRetentionChunk     = 5000
	sampleRetentionMaxChunks = 40
)

// nextRetentionWait is how long the loop waits after a pass: an hour when the
// pass kept up, a minute while a lane is behind.
func nextRetentionWait(rep retentionReport) time.Duration {
	if len(rep.Behind) > 0 {
		return sampleRetentionCatchUp
	}
	return sampleRetentionEvery
}

// retentionStore is what a retention pass reads and prunes through.
type retentionStore interface {
	RetentionWindows(ctx context.Context) (settings.RetentionSettings, error)
	PruneSamples(ctx context.Context, lane string, before time.Time, limit int) (int64, error)
}

// retentionReport is what one pass removed per lane, and the lanes it left
// with more to remove when it reached its chunk budget.
type retentionReport struct {
	Deleted map[string]int64
	Behind  []string
}

// retentionPass prunes each sample lane to its window once: the metric lane to
// metric_days, the property lane to property_days, each counted back from now.
// A lane is deleted chunk by chunk until a chunk comes back short (nothing more
// to find) or the pass has spent maxChunks on it, each lane under a deadline of
// its own (laneTimeout, none when zero), so a slow lane never starts the next on
// an expired context. A lane that fails, its deadline included, is reported and
// named behind, so the next pass comes in a minute; the other lane is still
// pruned. A lane whose window is out of range (stored past the settings
// validator) is refused, not pruned: its cutoff could overflow the timestamp
// encoding and match every row. It holds no clock and no database of its own,
// so it is tested with neither.
func retentionPass(ctx context.Context, store retentionStore, now time.Time, chunk, maxChunks int, laneTimeout time.Duration) (retentionReport, error) {
	rep := retentionReport{Deleted: map[string]int64{}}
	bounded := func() (context.Context, context.CancelFunc) {
		if laneTimeout > 0 {
			return context.WithTimeout(ctx, laneTimeout)
		}
		return context.WithCancel(ctx)
	}
	wctx, done := bounded()
	w, err := store.RetentionWindows(wctx)
	done()
	if err != nil {
		return rep, err
	}
	var errs []error
	for _, lane := range []struct {
		name string
		days int
	}{{"metric", w.MetricDays}, {"property", w.PropertyDays}} {
		if lane.days <= 0 || lane.days > settings.RetentionMaxDays {
			errs = append(errs, fmt.Errorf("prune %s: a window of %d days is outside 1 to %d; not pruned", lane.name, lane.days, settings.RetentionMaxDays))
			continue
		}
		before := now.AddDate(0, 0, -lane.days)
		lctx, done := bounded()
		for i := 0; ; i++ {
			if i == maxChunks {
				rep.Behind = append(rep.Behind, lane.name)
				break
			}
			n, err := store.PruneSamples(lctx, lane.name, before, chunk)
			rep.Deleted[lane.name] += n
			if err != nil {
				errs = append(errs, fmt.Errorf("prune %s: %w", lane.name, err))
				rep.Behind = append(rep.Behind, lane.name)
				break
			}
			if n < int64(chunk) {
				break
			}
		}
		done()
	}
	return rep, errors.Join(errs...)
}

// startSampleRetention keeps the sample tables to their retention windows
// (#417): once at start and again after nextRetentionWait, on a goroutine of
// its own, as the receipt prune does. A failed pass is logged and retried at the
// next; a sample kept a little longer only costs its row.
func (s *Server) startSampleRetention() {
	ctx, cancel := context.WithCancel(context.Background())
	s.stopRetention = cancel
	s.sweepDone.Add(1)
	go func() {
		defer s.sweepDone.Done()
		for {
			rep, err := retentionPass(ctx, s.store, time.Now(), sampleRetentionChunk, sampleRetentionMaxChunks, sampleRetentionTimeout)
			if err != nil && ctx.Err() == nil {
				slog.Warn("sample retention pass failed", "facility", "retention", "error", err.Error())
			}
			if rep.Deleted["metric"]+rep.Deleted["property"] > 0 || len(rep.Behind) > 0 {
				slog.Info("samples pruned past their retention window", "facility", "retention",
					"metric", rep.Deleted["metric"], "property", rep.Deleted["property"], "behind", rep.Behind)
			}
			t := time.NewTimer(nextRetentionWait(rep))
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
		}
	}()
}
