package bus

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// retentionSamples is one retention pass as platform metrics on component,
// the server's own (#1629), each at now: per lane the pass pruned, the rows
// it removed and whether it stopped behind, keyed by the lane's name so a lane
// added later reports with no change here; a behind flag per compaction when
// the pass ran them, with the metric window; the pass's duration, failures
// and partition roll; each compaction's mark lag behind now, when it has a
// mark; and, from the status read after the pass (nil when that read
// failed), the partitions held, the oldest day's age and the sizes. A server
// with no platform component writes none.
func retentionSamples(component string, rep retentionReport, status *storage.RetentionStatus, now time.Time) []storage.PlatformMetric {
	if component == "" {
		return nil
	}
	var rows []storage.PlatformMetric
	add := func(key, instance string, v float64) {
		rows = append(rows, storage.PlatformMetric{ComponentID: component, Key: key, Instance: instance, Value: v, TS: now})
	}
	behind := func(lane string) float64 {
		if slices.Contains(rep.Behind, lane) {
			return 1
		}
		return 0
	}
	for _, lane := range slices.Sorted(maps.Keys(rep.Deleted)) {
		add(writerclass.RetentionDeletedCount, lane, float64(rep.Deleted[lane]))
		add(writerclass.RetentionBehind, lane, behind(lane))
	}
	if rep.Compacted {
		for _, c := range []string{"metric_trend compaction", "metric_block compaction"} {
			add(writerclass.RetentionBehind, c, behind(c))
		}
		add(writerclass.RetentionWindowDays, "metric", float64(rep.MetricDays))
	}
	add(writerclass.RetentionPassDuration, "", rep.Duration.Seconds())
	add(writerclass.RetentionPassFailureCount, "", float64(rep.Failures))
	add(writerclass.MetricPartitionCreatedCount, "", float64(len(rep.Created)))
	add(writerclass.MetricPartitionDroppedCount, "", float64(len(rep.Dropped)))
	if !rep.Trends.Through.IsZero() {
		add(writerclass.MetricTrendMarkLag, "", now.Sub(rep.Trends.Through).Hours())
	}
	if !rep.Blocks.Through.IsZero() {
		add(writerclass.MetricBlockMarkLag, "", now.Sub(rep.Blocks.Through).Hours()/24)
	}
	if status != nil {
		add(writerclass.MetricPartitionCount, "", float64(len(status.Partitions)))
		if len(status.Partitions) > 0 {
			add(writerclass.MetricOldestDayAge, "", now.Sub(status.Partitions[0].From).Hours()/24)
		}
		for _, table := range slices.Sorted(maps.Keys(status.TableBytes)) {
			add(writerclass.TableBytes, table, float64(status.TableBytes[table]))
		}
		add(writerclass.DatabaseBytes, "", float64(status.DatabaseBytes))
	}
	return rows
}

// retentionRecorder is the store half recordRetention needs.
type retentionRecorder interface {
	RetentionStatus(ctx context.Context) (storage.RetentionStatus, error)
	RecordPlatformMetrics(ctx context.Context, rows []storage.PlatformMetric) error
}

// recordRetention reads the storage status after a pass and lands the pass's
// figures on component as the process (#1629). A failed status read loses
// only the status rows; the pass's own still land. No component, no write.
func recordRetention(ctx context.Context, store retentionRecorder, component string, rep retentionReport, now time.Time) error {
	if component == "" {
		return nil
	}
	var errs []error
	var status *storage.RetentionStatus
	if st, err := store.RetentionStatus(ctx); err != nil {
		errs = append(errs, fmt.Errorf("read retention status: %w", err))
	} else {
		status = &st
	}
	if err := store.RecordPlatformMetrics(ctx, retentionSamples(component, rep, status, now)); err != nil {
		errs = append(errs, fmt.Errorf("record retention figures: %w", err))
	}
	return errors.Join(errs...)
}
