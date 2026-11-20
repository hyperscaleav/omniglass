package storage

import (
	"context"
	"time"
)

// ListMetricBucketsFromSamples is ListMetricBuckets with every bucket read
// through the series read, no hour from the trends (#1566): the answer a
// trend-served bucket must equal.
func (p *PG) ListMetricBucketsFromSamples(ctx context.Context, ownerKind, ownerRef, key string, instance *string, window, resolution time.Duration) (MetricBuckets, error) {
	return p.listMetricBuckets(ctx, ownerKind, ownerRef, key, instance, window, resolution, false)
}

// FoldMetricTrendHours folds every hour in [from, to) into its trend rows as
// the compaction does (foldTrendHour), whatever the marks say, so a test that
// stands the compacted mark past its samples also holds their trends.
func (p *PG) FoldMetricTrendHours(ctx context.Context, from, to time.Time) error {
	for h := from.UTC().Truncate(time.Hour); h.Before(to); h = h.Add(time.Hour) {
		if err := p.foldTrendHour(ctx, h); err != nil {
			return err
		}
	}
	return nil
}
