package storage

import (
	"context"
	"time"
)

// ExportPruneSamplesSQL is one lane's prune statements by owner arc, for the
// external test that plans each and holds its probe to an index (#417).
func ExportPruneSamplesSQL(lane string) map[string]string {
	out := map[string]string{}
	for _, arc := range sampleArcs {
		out[arc] = pruneSamplesSQL(lane, arc, false)
	}
	return out
}

// ExportPruneLogsSQL is one log table's prune statements, "rated" (by the
// levels in $3) and "unrated", for the external access-path test (#417).
func ExportPruneLogsSQL(table string) map[string]string {
	return map[string]string{"rated": pruneLogsSQL(table, "rated"), "unrated": pruneLogsSQL(table, "unrated")}
}

// ExportPruneSamplesOfTypeSQL is one lane's typed prune statements by owner
// arc ($3 one type's id), for the same access-path test (#417).
func ExportPruneSamplesOfTypeSQL(lane string) map[string]string {
	out := map[string]string{}
	for _, arc := range sampleArcs {
		out[arc] = pruneSamplesSQL(lane, arc, true)
	}
	return out
}

// ExportPruneMetricSampleSQL is the metric lane's prune statement over one
// partition (a sanitized name), the lane's or a typed one, for the
// access-path test (#1275).
func ExportPruneMetricSampleSQL(part string, typed bool) string {
	return pruneMetricSampleSQL(part, typed)
}

// MakeMetricMonths makes the metric partition of each instant's month, as the
// ingest does for a sample whose month has none, so a test that plants rows
// in the past through SQL has somewhere to land them.
func (p *PG) MakeMetricMonths(ctx context.Context, at ...time.Time) error {
	_, err := p.ensureMetricMonths(ctx, at)
	return err
}

// ExportMetricNewestSQL is one metric series' newest-sample read ($1 the
// series id), for the test that plans it (#1275).
func ExportMetricNewestSQL() string { return metricNewestSQL("$1::bigint") }

// ExportIsPartitionRoutingMiss is the classifier the ingest retries a write by
// when no metric partition covers a row (#420).
func ExportIsPartitionRoutingMiss(err error) bool { return isPartitionRoutingMiss(err) }

// SetMetricTrendMark sets how far the trend compaction has closed hours
// (#1276), so a test of the metric prune, which never passes the mark, can
// stand the mark where it wants it.
func (p *PG) SetMetricTrendMark(ctx context.Context, through time.Time) error {
	return setMetricTrendMark(ctx, p.pool, through)
}
