package storage

import (
	"context"
	"time"

	"github.com/hyperscaleav/omniglass/internal/tail"
)

// ExportBeginSampleWrite opens a sample write the way sampleWriteOnce does,
// holding a pooled connection until its commit, which passes the tail's
// fence: for a test that parks a writer between the two.
func (p *PG) ExportBeginSampleWrite(ctx context.Context) (commit func(rows []tail.Row) error, err error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return func(rows []tail.Row) error {
		return p.tail.Commit(func() error { return tx.Commit(ctx) }, rows)
	}, nil
}

// ExportInsertMetricTrend seeds one trend-tier bucket directly, bypassing the
// compaction pipeline. The metric_series must already exist (from a prior
// WriteMetricSamples call). ownerName is the component's name, not its uuid.
func (p *PG) ExportInsertMetricTrend(ctx context.Context, ownerKind, ownerName, key, instance string, hour time.Time, min, max, sum, last float64, count int64) error {
	_, err := p.pool.Exec(ctx, `
		insert into metric_trend (series_id, hour, count, min, max, sum, first, last)
		select s.id, $5, $6, $7, $8, $9, $7, $10
		from metric_series s
		join metric_type mt on mt.id = s.metric_type_id
		where mt.name = $1 and s.owner_kind = $2
		  and s.owner_id = (select id from component where name = $3)
		  and s.instance = $4
		  and s.provenance = 'observed'
		on conflict (series_id, hour) do nothing`,
		key, ownerKind, ownerName, instance, hour, count, min, max, sum, last)
	return err
}
