package storage

import "context"

// ExportLatestMetricValue is settlement's read of a metric series' latest
// value (latestMetricValue), the observation a command settles against, so a
// test can hold it to the series read (#1573).
func (p *PG) ExportLatestMetricValue(ctx context.Context, ownerKind, ownerID, key, instance, provenance string) (*CurrentValue, error) {
	return p.latestMetricValue(ctx, p.pool, ownerKind, ownerID, key, instance, provenance)
}
