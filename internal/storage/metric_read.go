package storage

import (
	"context"
	"fmt"
	"strconv"
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
// columns, ord, a sample's landing order within its part (its raw row's id,
// or its place in its block), and folded, whether a block gave it: a block
// keeps no arrival order, so a reader that judges samples in the order they
// arrived (the rule preview) tells the two apart. Unordered: a reader orders
// by metricSampleOrder. A raw row is read unless its day and lineage are in rc,
// the intact days the series' blocks in range cover (a week block every day
// of its week: the week fold took the days no hour or day block of its
// lineage holds), which the planner hashes once and anti-joins every row
// against. The week's days come from a list of seven, not generate_series,
// whose guess of a thousand rows a block turns the anti-join into a sort of
// every raw row in range. A block is found by the start of the longest span that can reach
// from, and gives the samples of its intact days.
func metricReadSQL(series, from, to, lane string) string {
	return metricReadFromSQL(series, from, to, lane, "true")
}

// metricReadFromSQL is metricReadSQL with the blocks behind a switch: blocks
// is an SQL condition, and where it is false the read takes every sample in
// range from the raw rows and none from a block. It is for a reader that
// knows a range's raw rows hold what its blocks may not (the trend
// recompute's raw_only hour, #1560); every other reader takes metricReadSQL.
func metricReadFromSQL(series, from, to, lane, blocks string) string {
	inRange := `rb.series_id = ` + series + ` and rb.span_start < ` + to + ` and rb.span_start > ` + from + ` - interval '168 hours'
			and ` + blockSpanEnd("rb") + ` > ` + from
	return `select rm.ts, rm.value, ` + blockLineage("rm") + `, rm.id as ord, false as folded
		from metric rm where rm.series_id = ` + series + ` and rm.ts >= ` + from + ` and rm.ts < ` + to + `
			and not exists (select 1 from (
				select distinct g.day, ` + blockLineageKey("rb") + `
				from metric_block rb cross join lateral (select date_bin('1 day', rb.span_start, ` + trendEpoch + `) + n * interval '24 hours' as day
					from (values (0), (1), (2), (3), (4), (5), (6)) n(n) where n.n = 0 or rb.span = 'week') g
				where ` + blocks + ` and ` + inRange + ` and g.day >= ` + metricIntactFromSQL(series, lane) + `) rc
			where rc.day = date_bin('1 day', rm.ts, ` + trendEpoch + `) and rc.rule = coalesce(rm.source_rule, '')
				and rc.version = coalesce(rm.source_rule_version, 0)
				and rc.rule_id = coalesce(rm.source_rule_id, '00000000-0000-0000-0000-000000000000'))
		union all
		select ru.ts, ru.value, ` + blockLineage("rb") + `, ru.ord, true
		from metric_block rb cross join ` + blockSamplesOf("rb") + ` ru
		where ` + blocks + ` and ` + inRange + ` and ru.ts >= ` + from + ` and ru.ts < ` + to + `
			and date_bin('1 day', ru.ts, ` + trendEpoch + `) >= ` + metricIntactFromSQL(series, lane)
}

// metricNewestSQL is a metric series' newest sample (ts, id, value and the
// lineage columns; series an SQL expression, a lateral's column, and lane
// the metric lane's days), the series read's newest sample (#1573): its
// newest raw row, unless its blocks' intact days hold a sample newer still.
// Every sample of an intact covered day is in its blocks and its raw rows
// alike, so the raw rows answer while they hold a series' newest sample, by
// an index probe of each day newest first (metricNewestRawSQL), and the
// blocks are read only where one could hold a newer one: those that end
// after the newest raw row (none, while the newest raw row is newer than
// every folded day, as it is for a series still sampled), or for a series
// with no raw row, its newest block start and the blocks starting within a
// week before it (no older one can end after it). A late sample's raw row in
// a folded day whose other raw rows are gone is then not taken for the
// series' newest. A block sample's id is its place in its block, which
// orders it only against its own block's samples; one at the newest raw
// row's instant is that row's own sample, and the raw row answers.
func metricNewestSQL(series, lane string) string {
	return `select n.ts, n.id, n.value, n.source_rule, n.source_rule_version, n.source_rule_id
		from (select 1) o left join lateral (` + metricNewestRawSQL(series) + `) nr on true
		cross join lateral (
			select nr.ts, nr.id, nr.value, nr.source_rule, nr.source_rule_version, nr.source_rule_id where nr.ts is not null
			union all
			(select nb.ts, nb.ord, nb.value, nb.source_rule, nb.source_rule_version, nb.source_rule_id from (
				select nu.ts, nu.ord, nu.value, ` + blockLineage("nk") + `
				from metric_block nk cross join ` + blockSamplesOf("nk") + ` nu
				where nk.series_id = ` + series + `
					and nk.span_start > coalesce(nr.ts, (select max(nx.span_start) from metric_block nx
						where nx.series_id = ` + series + `)) - interval '168 hours'
					and (nr.ts is null or ` + blockSpanEnd("nk") + ` > nr.ts)
					and nu.ts > coalesce(nr.ts, '-infinity')
					and date_bin('1 day', nu.ts, ` + trendEpoch + `) >= ` + metricIntactFromSQL(series, lane) + `) nb
			order by ` + metricSampleOrder("nb", true) + ` limit 1)) n
		order by n.ts desc limit 1`
}

// metricNewestRawSQL is a metric series' newest raw row (series an SQL
// expression): an index probe of metric_series_ts_idx in each day, newest
// day first, stopping at the first day that holds one
// (TestMetricNewestReadsTheNewestDayFirst).
func metricNewestRawSQL(series string) string {
	return `select m.ts, m.id, m.value, m.source_rule, m.source_rule_version, m.source_rule_id
		from metric m where m.series_id = ` + series + `
		order by m.ts desc, m.id desc limit 1`
}

// metricStoredSQL is the predicate that a metric series (series an SQL
// expression) holds a stored sample: a raw row, or a block, each one probe
// of an index that leads with the series. A series whose raw rows a fold
// covered holds its samples still, so a block counts as a raw row does. It
// is no read of a value and applies no window, as a raw row's existence
// never did.
func metricStoredSQL(series string) string {
	return `(exists (select 1 from metric hm where hm.series_id = ` + series + `)
		or exists (select 1 from metric_block hb where hb.series_id = ` + series + `))`
}

// metricLane is the metric lane's days, the window of a series whose type
// sets none of its own, as the SQL literal the series read takes: the
// platform setting resolved through q. It is an int the settings schema
// bounds, so a statement can carry it inline.
func (p *PG) metricLane(ctx context.Context, q querier) (string, error) {
	s, err := p.platformSettings(ctx, q)
	if err != nil {
		return "", err
	}
	return strconv.Itoa(s.Retention.MetricDays), nil
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
