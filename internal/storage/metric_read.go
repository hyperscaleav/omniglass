package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The series read (#1572, ADR-1530): every reader of a metric series'
// samples reads them through one statement, so a reader answers the same
// whether a day of the series is raw rows or folded into blocks. A series-day
// of one lineage is read from its blocks where they cover it and from its raw
// rows where they do not, never both: blocks cover a lineage's day once the
// day fold has written it an hour or day block, or the week fold a week block
// of its ISO week, while the day is intact (blockIntact, the late merge's and
// the trend recompute's own test). Every sample of a covered day is in its
// blocks (a late one merges in with its raw row, ADR-1529), and a day that is
// no longer intact took no late sample into its block, so its raw rows hold
// what the block does not. The raw rows stay where they are; nothing here
// drops one.

// blockSpanEnd is the end of block b's span.
func blockSpanEnd(b string) string {
	return b + `.span_start + case ` + b + `.span when 'hour' then interval '1 hour' when 'day' then interval '24 hours'
		else interval '168 hours' end`
}

// blockLineageKey is a row's (alias a) lineage as three non-null columns,
// rule, version and rule id, so two lineages are equal by plain equality and
// a hash join can match them: an observed row's nulls read as the empty rule,
// version 0 and the nil uuid, which no calc rule carries.
func blockLineageKey(a string) string {
	return `coalesce(` + a + `.source_rule, '') as rule, coalesce(` + a + `.source_rule_version, 0) as version,
		coalesce(` + a + `.source_rule_id, '00000000-0000-0000-0000-000000000000') as rule_id`
}

// metricIntactFromSQL is the start of the window of series (an SQL
// expression) by blockIntactFrom: a day starting at or after it is intact.
// lane is the metric lane's days.
func metricIntactFromSQL(series, lane string) string {
	return `(select ` + blockIntactFrom(lane) + ` from metric_series rs join metric_type t on t.id = rs.metric_type_id
		where rs.id = ` + series + `)`
}

// metricReadSQL is series' samples in [from, to) (SQL expressions: series a
// lateral's column or a parameter, never under an alias of the read's own,
// each r-prefixed; lane the metric lane's days), as ts, value, the lineage
// columns and ord, a sample's landing order within its part: its raw row's
// id, or its place in its block. Unordered: a reader orders by
// metricSampleOrder. A raw row is read unless its day and lineage are in rc,
// the intact days the series' blocks in range cover (a week block every day
// of its week: the week fold took the days no hour or day block of its
// lineage holds), which the planner hashes once and anti-joins every row
// against. The week's days come from a list of seven, not generate_series,
// whose guess of a thousand rows a block turns the anti-join into a sort of
// every raw row in range. A block is found by the start of the longest span that can reach
// from, and gives the samples of its intact days.
func metricReadSQL(series, from, to, lane string) string {
	inRange := `rb.series_id = ` + series + ` and rb.span_start < ` + to + ` and rb.span_start > ` + from + ` - interval '168 hours'
			and ` + blockSpanEnd("rb") + ` > ` + from
	return `select rm.ts, rm.value, ` + blockLineage("rm") + `, rm.id as ord
		from metric rm where rm.series_id = ` + series + ` and rm.ts >= ` + from + ` and rm.ts < ` + to + `
			and not exists (select 1 from (
				select distinct g.day, ` + blockLineageKey("rb") + `
				from metric_block rb cross join lateral (select date_bin('1 day', rb.span_start, ` + trendEpoch + `) + n * interval '24 hours' as day
					from (values (0), (1), (2), (3), (4), (5), (6)) n(n) where n.n = 0 or rb.span = 'week') g
				where ` + inRange + ` and g.day >= ` + metricIntactFromSQL(series, lane) + `) rc
			where rc.day = date_bin('1 day', rm.ts, ` + trendEpoch + `) and rc.rule = coalesce(rm.source_rule, '')
				and rc.version = coalesce(rm.source_rule_version, 0)
				and rc.rule_id = coalesce(rm.source_rule_id, '00000000-0000-0000-0000-000000000000'))
		union all
		select ru.ts, ru.value, ` + blockLineage("rb") + `, ru.ord
		from metric_block rb cross join ` + blockSamplesOf("rb") + ` ru
		where ` + inRange + ` and ru.ts >= ` + from + ` and ru.ts < ` + to + `
			and date_bin('1 day', ru.ts, ` + trendEpoch + `) >= ` + metricIntactFromSQL(series, lane)
}

// metricSampleOrder is the series read's order over alias a, oldest first or
// newest first: by ts, then by lineage and landing order (metricLineageOrder).
func metricSampleOrder(a string, desc bool) string {
	if desc {
		return a + `.ts desc, ` + metricLineageOrder(a, desc)
	}
	return a + `.ts, ` + metricLineageOrder(a, desc)
}

// metricLineageOrder orders one instant's samples of a series (alias a): by
// lineage (rows of one instant under two rule versions are rare, and the older
// version wrote first), then in landing order. One instant's samples of one
// lineage are all raw rows or all in one block, which holds them in their
// rows' id order, so the order is the same before the fold and after.
func metricLineageOrder(a string, desc bool) string {
	if desc {
		return a + `.source_rule_version desc nulls last, ` + a + `.source_rule_id desc, ` + a + `.source_rule desc, ` + a + `.ord desc`
	}
	return a + `.source_rule_version nulls first, ` + a + `.source_rule_id, ` + a + `.source_rule, ` + a + `.ord`
}

// MetricSeriesSamples reads series seriesID's samples in [from, to) through
// the series read (metricReadSQL), oldest first, each with the lineage its raw
// row was written with, whether the raw rows or the blocks hold it.
func (p *PG) MetricSeriesSamples(ctx context.Context, seriesID int64, from, to time.Time) ([]BlockSample, error) {
	s, err := p.platformSettings(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `select r.ts, r.value, r.source_rule, r.source_rule_id::text, r.source_rule_version
		from (`+metricReadSQL("$1::bigint", "$2::timestamptz", "$3::timestamptz", "$4")+`) r
		order by `+metricSampleOrder("r", false), seriesID, from, to, s.Retention.MetricDays)
	if err != nil {
		return nil, fmt.Errorf("storage: read series %d: %w", seriesID, err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (BlockSample, error) {
		var s BlockSample
		var rule, ruleID *string
		var version *int64
		err := r.Scan(&s.TS, &s.Value, &rule, &ruleID, &version)
		s.Lineage = scanLineage(rule, ruleID, version)
		return s, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read series %d: %w", seriesID, err)
	}
	return out, nil
}
