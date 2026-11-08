package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The compacted tier's blocks (#1528, ADR-1528): each closed UTC day of every
// observed or calculated metric series folds into lossless metric_block rows
// of millisecond offsets and values, in landing order, at the span its own
// sample count earns. A calculated series' block carries the one calc rule
// lineage its samples were written under, so its day splits at every lineage
// change and each lineage earns its span alone (#1534, ADR-1534). A dense day is cut into hour blocks, a moderate one is one day block,
// and a sparse one waits for its ISO week, whose leftover days fold into one
// week block. Every block crosses Postgres's 2 KB compression threshold or is
// the only row its sparse week costs. The blocks are written beside the raw
// rows, which stay in place: the series read (metricReadSQL, #1572,
// ADR-1530) takes a covered day from its blocks and the rest from raw rows,
// the trend's recompute of a dirtied hour reads them, and no prune waits on
// one. A late sample for
// a folded day lands raw and merges into the block that covers it, in its
// own batch's transaction (#1529, ADR-1529). Properties and declared rows are
// other tables.

const (
	// BlockHourMin is the samples a series-day holds at which it is cut into
	// hour blocks: 170 an hour on average.
	BlockHourMin = 24 * BlockDayMin
	// BlockDayMin is the samples a series-day holds at which it is one day
	// block: 170 int4 offsets and float8 values cross the 2 KB threshold at
	// which Postgres compresses a row. A sparser day waits for its week.
	BlockDayMin = 170
)

// blockWeekEpoch anchors the week buckets on ISO weeks: 2000-01-03 was a
// Monday. The day and hour buckets share the trend's epoch.
const blockWeekEpoch = `timestamptz '2000-01-03 00:00:00+00'`

// blockSeriesChunk is how many series one fold transaction blocks.
const blockSeriesChunk = 200

// blockLockKey names the advisory lock between the folds and the late merge:
// a fold holds it exclusive from after it takes the mark row to its commit,
// and a batch merging late samples holds it shared from before it reads the
// marks to its own, so the merge reads the marks of every fold that has
// written and no fold writes past a merge in flight. An on-time batch never
// takes it.
const blockLockKey = "metric_block:fold"

// blockOffset is a sample's (alias m) millisecond offset from the start of
// its block. Metric timestamps are whole milliseconds (#1532) and a week is
// 604,800,000 of them, inside an int4.
func blockOffset(start string) string {
	return `round((extract(epoch from m.ts) - extract(epoch from ` + start + `)) * 1000)::int4`
}

// blockSeries is the provenance condition on a series (alias s) whose
// samples fold into blocks.
const blockSeries = `s.provenance in ('observed', 'calculated')`

// blockLineage is a row's (alias a) lineage columns, as the metric table and
// the block key order them: null for an observed series' rows, the calc rule
// that wrote them for a calculated one's (ADR-1534).
func blockLineage(a string) string {
	return a + `.source_rule, ` + a + `.source_rule_version, ` + a + `.source_rule_id`
}

// sameLineage is the condition that rows a and b carry the same lineage, no
// lineage matching no lineage.
func sameLineage(a, b string) string {
	return a + `.source_rule is not distinct from ` + b + `.source_rule and ` +
		a + `.source_rule_version is not distinct from ` + b + `.source_rule_version and ` +
		a + `.source_rule_id is not distinct from ` + b + `.source_rule_id`
}

// blockInsert is the column list a block is written by.
const blockInsert = `insert into metric_block (series_id, span_start, span, source_rule, source_rule_version, source_rule_id, n, offset_ms, value)`

// blockConflict is the block key a write lands on: its series, span and
// lineage (the metric_block_lineage migration).
const blockConflict = `on conflict on constraint metric_block_key`

// blockUpsert replaces a refolded block whole: a fold writes every sample
// its span holds, never an increment.
const blockUpsert = blockConflict + ` do update set n = excluded.n,
	offset_ms = excluded.offset_ms, value = excluded.value`

// blockSamplesOf unnests block b's samples in landing order, as ts, value and
// ord (its place in the block).
func blockSamplesOf(b string) string {
	return `lateral (select ` + b + `.span_start + u.o * interval '1 millisecond' as ts, u.v as value, u.ord
		from unnest(` + b + `.offset_ms, ` + b + `.value) with ordinality as u(o, v, ord))`
}

// blockIntact is the condition that a series' day (t its metric type, day the
// day's start) is still wholly inside the series' window ($lane the metric
// lane's days): a day the prune has begun is not folded, since what remains
// would be only part of it, and it goes with the next prune. It is also the
// one test of whether a block covers a late sample: the late merge folds a
// sample into a day's block only while the day is intact, and the trend
// recompute and the series read (metricReadSQL) read a day from its blocks
// only while it is, so none of them disagree about a sample (#1546,
// ADR-1529, ADR-1530).
func blockIntact(day, lane string) string {
	return day + ` >= ` + blockIntactFrom(lane)
}

// blockIntactFrom is the start of a series' window (t its metric type, lane
// the metric lane's days): a day is intact when it starts at or after it.
func blockIntactFrom(lane string) string {
	return `now() - make_interval(days => coalesce(t.retention_days, ` + lane + `::int))`
}

// blockDaysSQL folds series-days into hour and day blocks: pairs yields
// (series_id, day) rows, bound narrows the metric scan to its days where the
// caller knows them (so the plan prunes partitions), and lane is the metric
// lane's days. Each lineage of a series-day is counted and folded alone, so
// a calculated day splits at its rule's every change (ADR-1534). A lineage
// under BlockDayMin in its day writes nothing here: its week fold takes it.
func blockDaysSQL(pairs, bound, lane string) string {
	return fmt.Sprintf(`with sd as (%[1]s),
	d as (
		select m.series_id, sd.day, m.ts, m.id, m.value, %[2]s,
			count(*) over (partition by m.series_id, sd.day, %[2]s) as n
		from sd join metric_series s on s.id = sd.series_id and %[3]s
		join metric_type t on t.id = s.metric_type_id
		join metric m on m.series_id = sd.series_id and m.ts >= sd.day and m.ts < sd.day + interval '24 hours'%[4]s
		where %[5]s),
	b as (
		select d.series_id, d.ts, d.id, d.value, %[6]s,
			case when d.n >= %[7]d then date_bin('1 hour', d.ts, %[8]s) else d.day end as span_start,
			case when d.n >= %[7]d then 'hour' else 'day' end as span
		from d where d.n >= %[9]d)
	%[10]s
	select m.series_id, m.span_start, m.span, %[11]s, count(*), array_agg(%[12]s order by m.ts, m.id),
		array_agg(m.value order by m.ts, m.id)
	from b m group by m.series_id, m.span_start, m.span, %[11]s
	%[13]s`, pairs, blockLineage("m"), blockSeries, bound, blockIntact("sd.day", lane), blockLineage("d"),
		BlockHourMin, trendEpoch, BlockDayMin, blockInsert, blockLineage("m"), blockOffset("m.span_start"), blockUpsert)
}

// blockWeekSQL folds each series' leftover days of an ISO week into one week
// block for each lineage: pairs yields (series_id, week) rows, bound narrows
// the metric scan as blockDaysSQL's does, and lane is the metric lane's days.
// A lineage's leftover days are those of the week where it has no hour or
// day block: an observed series' whole days, and a calculated series' rows
// of a lineage too sparse in its day, beside a block of another lineage there
// or not. There is no minimum: a week block of one sample is still the only
// row its week costs. A leftover day the prune has begun is left out.
func blockWeekSQL(pairs, bound, lane string) string {
	return fmt.Sprintf(`with sw as (%[1]s),
	sd as (
		select sw.series_id, sw.week, g.day, s.provenance = 'calculated' as calc
		from sw join metric_series s on s.id = sw.series_id and %[2]s
		join metric_type t on t.id = s.metric_type_id
		cross join lateral generate_series(sw.week, sw.week + interval '144 hours', interval '24 hours') as g(day)
		where %[3]s
			and (s.provenance = 'calculated' or not exists (select 1 from metric_block b where b.series_id = sw.series_id
				and b.span <> 'week' and b.span_start >= g.day and b.span_start < g.day + interval '24 hours')))
	%[4]s
	select sd.series_id, sd.week, 'week', %[5]s, count(*), array_agg(%[6]s order by m.ts, m.id),
		array_agg(m.value order by m.ts, m.id)
	from sd join metric m on m.series_id = sd.series_id and m.ts >= sd.day and m.ts < sd.day + interval '24 hours'%[7]s
	where not sd.calc or not exists (select 1 from metric_block b where b.series_id = sd.series_id and b.span <> 'week'
		and b.span_start >= sd.day and b.span_start < sd.day + interval '24 hours' and %[8]s)
	group by sd.series_id, sd.week, %[5]s
	%[9]s`, pairs, blockSeries, blockIntact("g.day", lane), blockInsert, blockLineage("m"), blockOffset("sd.week"), bound,
		sameLineage("b", "m"), blockUpsert)
}

// BlockFold is how far one block pass may go.
type BlockFold struct {
	// MaxDays is how many UTC days the day fold may close in the pass.
	MaxDays int
	// LaneDays is the metric lane's window, the window of a series whose
	// type sets none of its own.
	LaneDays int
	// SeriesChunk, when set, is how many series one fold transaction blocks
	// in place of blockSeriesChunk.
	SeriesChunk int
	// Verify, when set, holds every block inside its window to the raw rows
	// it was folded from once the pass has written, and fails the pass on the
	// first that differs (VerifyMetricBlocks).
	Verify bool
}

// BlockCompaction is what one block pass did.
type BlockCompaction struct {
	// Days is how many UTC days the day fold closed, and Through its mark:
	// every series-day before Through is folded.
	Days    int
	Through time.Time
	// Weeks is how many ISO weeks the week fold closed, and WeekThrough its
	// mark.
	Weeks       int
	WeekThrough time.Time
	// Blocks is how many blocks the two folds wrote.
	Blocks int64
	// Behind is true when closed days remain unfolded (the pass reached
	// MaxDays), so the next pass should come soon.
	Behind bool
}

// BlockKey names one block.
type BlockKey struct {
	SeriesID  int64
	SpanStart time.Time
	Span      string
	// Lineage is the calc rule a calculated series' block was written by,
	// nil for an observed series'.
	Lineage *BlockLineage
}

// BlockLineage is the calc rule that wrote a calculated sample, as its raw
// row and the block holding it carry it: the rule's name and id and its
// version when it wrote (ADR-1534).
type BlockLineage struct {
	Rule    string
	RuleID  string
	Version int64
}

// BlockSample is one sample as a block holds it: its instant, its value, and
// its lineage, nil for an observed sample.
type BlockSample struct {
	TS      time.Time
	Value   float64
	Lineage *BlockLineage
}

// scanLineage is the lineage of a row's nullable source_rule, source_rule_id
// and source_rule_version, nil when all three are null.
func scanLineage(rule, ruleID *string, version *int64) *BlockLineage {
	if rule == nil && ruleID == nil && version == nil {
		return nil
	}
	var l BlockLineage
	if rule != nil {
		l.Rule = *rule
	}
	if ruleID != nil {
		l.RuleID = *ruleID
	}
	if version != nil {
		l.Version = *version
	}
	return &l
}

// blockMarks is the two folds' cursors (the metric_block_mark migration):
// the day fold has folded every series-day before blocked, and the series of
// the day at blocked through blockedSeries by id; the week fold likewise.
type blockMarks struct {
	blocked       time.Time
	blockedSeries int64
	week          time.Time
	weekSeries    int64
}

const blockMarksSelect = `select blocked_through, blocked_series, week_through, week_series from metric_block_mark`

// metricBlockMarks reads the folds' marks, ok false before the first pass;
// lock takes the mark row for update, which orders every block transaction
// behind any other server's.
func metricBlockMarks(ctx context.Context, q querier, lock bool) (blockMarks, bool, error) {
	var m blockMarks
	sql := blockMarksSelect
	if lock {
		sql += ` for update`
	}
	err := q.QueryRow(ctx, sql).Scan(&m.blocked, &m.blockedSeries, &m.week, &m.weekSeries)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, false, nil
	}
	if err != nil {
		return m, false, fmt.Errorf("storage: read the block marks: %w", err)
	}
	return m, true, nil
}

// setMetricBlockMarks stands both folds' marks at through, a Monday, with no
// series of its day or week folded.
func setMetricBlockMarks(ctx context.Context, q txQuerier, through time.Time) error {
	if _, err := q.Exec(ctx, `insert into metric_block_mark (id, blocked_through, week_through) values (true, $1, $1)
		on conflict (id) do update set blocked_through = excluded.blocked_through, blocked_series = 0,
			week_through = excluded.week_through, week_series = 0`, through); err != nil {
		return fmt.Errorf("storage: set the block marks: %w", err)
	}
	return nil
}

// isoWeek is the Monday, at 00:00 UTC, that starts t's ISO week.
func isoWeek(t time.Time) time.Time {
	day := t.UTC().Truncate(24 * time.Hour)
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
}

// CompactMetricBlocks folds closed metric days into blocks: up to f.MaxDays
// UTC days, oldest first, up to the last day whose every hour the trend
// compaction has closed; then every ISO week whose days are all folded.
//
// The day fold rides on the trend compaction's marks (#1276) rather than a
// drain of its own: a day closes only once the trend's compacted mark has
// passed its every hour, so every metric insert into it either committed
// before the trend's drain, and the fold sees it, or read an announced mark
// past it and was late: such a sample merges into its block if the day has
// folded by the time it does (mergeLateBlocks), and lands raw for the fold
// to read if not, the block lock ordering the two. Each batch of series folds in
// a transaction of its own that moves the block mark past it, so a pass that
// stops anywhere leaves each series-day either folded whole or not at all,
// and the next pass goes on from the mark.
func (p *PG) CompactMetricBlocks(ctx context.Context, f BlockFold) (BlockCompaction, error) {
	var out BlockCompaction
	if f.MaxDays <= 0 {
		return out, fmt.Errorf("storage: compact metric blocks: MaxDays %d, want at least 1", f.MaxDays)
	}
	if f.SeriesChunk <= 0 {
		f.SeriesChunk = blockSeriesChunk
	}
	now, err := dbNow(ctx, p.pool)
	if err != nil {
		return out, err
	}
	trend, ok, err := metricTrendMarks(ctx, p.pool)
	if err != nil || !ok {
		// No trend pass yet: no day is closed.
		return out, err
	}
	target := trend.compacted
	if horizon := now.Add(-MetricTrendHorizon); horizon.Before(target) {
		target = horizon
	}
	target = target.UTC().Truncate(24 * time.Hour)
	marks, ok, err := metricBlockMarks(ctx, p.pool, false)
	if err != nil {
		return out, err
	}
	if !ok {
		if marks, err = p.seedBlockMarks(ctx, now, target, f.LaneDays); err != nil {
			return out, err
		}
	}
	out.Through, out.WeekThrough = marks.blocked, marks.week
	for marks.blocked.Before(target) && out.Days < f.MaxDays {
		day := marks.blocked
		if marks, err = p.foldBlockBatch(ctx, f, target, &out); err != nil {
			return out, err
		}
		if marks.blocked.After(day) {
			out.Days++
		}
		out.Through = marks.blocked
	}
	out.Behind = marks.blocked.Before(target)
	for !marks.week.Add(7 * 24 * time.Hour).After(marks.blocked) {
		week := marks.week
		if marks, err = p.foldWeekBatch(ctx, f, &out); err != nil {
			return out, err
		}
		if marks.week.After(week) {
			out.Weeks++
		}
		out.WeekThrough = marks.week
	}
	if !f.Verify {
		return out, nil
	}
	bad, err := p.VerifyMetricBlocks(ctx, f.LaneDays)
	if err != nil {
		return out, err
	}
	if len(bad) > 0 {
		return out, fmt.Errorf("storage: verify metric blocks: %d blocks differ from their raw rows, the first series %d's %s from %s",
			len(bad), bad[0].SeriesID, bad[0].Span, bad[0].SpanStart.Format(time.RFC3339))
	}
	return out, nil
}

// seedBlockMarks stands both marks where the first pass starts: the Monday
// that starts the week of the oldest day the hot tier holds, or of the
// longest window back from now when that is later (seedTrendMarks' rule), so
// the week fold always starts on a week's first day. Never past target's
// week. A second server seeding at once keeps the first's.
func (p *PG) seedBlockMarks(ctx context.Context, now, target time.Time, laneDays int) (blockMarks, error) {
	var days int
	if err := p.pool.QueryRow(ctx, `select greatest($1::int, coalesce(max(retention_days), 0)) from metric_type`, laneDays).Scan(&days); err != nil {
		return blockMarks{}, fmt.Errorf("storage: seed the block marks: %w", err)
	}
	parts, err := metricPartitions(ctx, p.pool)
	if err != nil {
		return blockMarks{}, err
	}
	from := now.Add(-time.Duration(days) * 24 * time.Hour)
	if len(parts) > 0 && parts[0].From.After(from) {
		from = parts[0].From
	}
	if from.After(target) {
		from = target
	}
	if _, err := p.pool.Exec(ctx, `insert into metric_block_mark (id, blocked_through, week_through)
		values (true, $1, $1) on conflict (id) do nothing`, isoWeek(from)); err != nil {
		return blockMarks{}, fmt.Errorf("storage: seed the block marks: %w", err)
	}
	marks, _, err := metricBlockMarks(ctx, p.pool, false)
	return marks, err
}

// nextBlockSeries is up to n observed or calculated series after the id
// after, by id: the next batch a fold transaction blocks.
func nextBlockSeries(ctx context.Context, tx pgx.Tx, after int64, n int) ([]int64, error) {
	var ids []int64
	if err := tx.QueryRow(ctx, `select coalesce(array_agg(id order by id), '{}') from (
		select id from metric_series s where `+blockSeries+` and id > $1 order by id limit $2) s`, after, n).Scan(&ids); err != nil {
		return nil, fmt.Errorf("storage: list the series to block: %w", err)
	}
	return ids, nil
}

// foldBlockBatch folds the next batch of series of the day at the block mark
// into hour and day blocks and moves the mark past them, in one transaction;
// the batch that finds the day's last series moves the mark to the next day.
// It returns the marks it left (another server's, should one have moved them
// first and the day reached target).
func (p *PG) foldBlockBatch(ctx context.Context, f BlockFold, target time.Time, out *BlockCompaction) (blockMarks, error) {
	tx, err := p.pool.BeginTx(ctx, trendTxOptions)
	if err != nil {
		return blockMarks{}, fmt.Errorf("storage: fold metric blocks: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	m, _, err := metricBlockMarks(ctx, tx, true)
	if err != nil || !m.blocked.Before(target) {
		return m, err
	}
	if err := lockBlockFolds(ctx, tx); err != nil {
		return m, err
	}
	day := m.blocked
	ids, err := nextBlockSeries(ctx, tx, m.blockedSeries, f.SeriesChunk)
	if err != nil {
		return m, err
	}
	var written int64
	if len(ids) > 0 {
		tag, err := tx.Exec(ctx, blockDaysSQL(`select unnest($1::bigint[]) as series_id, $2::timestamptz as day`,
			` and m.ts >= $2 and m.ts < $3`, "$4"), ids, day, day.Add(24*time.Hour), f.LaneDays)
		if err != nil {
			return m, fmt.Errorf("storage: block the day from %s: %w", day.Format(time.RFC3339), err)
		}
		written = tag.RowsAffected()
		m.blockedSeries = ids[len(ids)-1]
	}
	if len(ids) < f.SeriesChunk {
		m.blocked, m.blockedSeries = day.Add(24*time.Hour), 0
	}
	if _, err := tx.Exec(ctx, `update metric_block_mark set blocked_through = $1, blocked_series = $2`, m.blocked, m.blockedSeries); err != nil {
		return m, fmt.Errorf("storage: block the day from %s: %w", day.Format(time.RFC3339), err)
	}
	if err := tx.Commit(ctx); err != nil {
		return m, fmt.Errorf("storage: block the day from %s: %w", day.Format(time.RFC3339), err)
	}
	out.Blocks += written
	return m, nil
}

// foldWeekBatch folds the next batch of series' leftover days of the week at
// the week mark into week blocks and moves the mark past them, in one
// transaction, as foldBlockBatch does for days. The week is closed: the day
// fold has passed its last day.
func (p *PG) foldWeekBatch(ctx context.Context, f BlockFold, out *BlockCompaction) (blockMarks, error) {
	tx, err := p.pool.BeginTx(ctx, trendTxOptions)
	if err != nil {
		return blockMarks{}, fmt.Errorf("storage: fold metric week blocks: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	m, _, err := metricBlockMarks(ctx, tx, true)
	if err != nil || m.week.Add(7*24*time.Hour).After(m.blocked) {
		return m, err
	}
	if err := lockBlockFolds(ctx, tx); err != nil {
		return m, err
	}
	week := m.week
	ids, err := nextBlockSeries(ctx, tx, m.weekSeries, f.SeriesChunk)
	if err != nil {
		return m, err
	}
	var written int64
	if len(ids) > 0 {
		tag, err := tx.Exec(ctx, blockWeekSQL(`select unnest($1::bigint[]) as series_id, $2::timestamptz as week`,
			` and m.ts >= $2 and m.ts < $3`, "$4"), ids, week, week.Add(7*24*time.Hour), f.LaneDays)
		if err != nil {
			return m, fmt.Errorf("storage: block the week from %s: %w", week.Format(time.RFC3339), err)
		}
		written = tag.RowsAffected()
		m.weekSeries = ids[len(ids)-1]
	}
	if len(ids) < f.SeriesChunk {
		m.week, m.weekSeries = week.Add(7*24*time.Hour), 0
	}
	if _, err := tx.Exec(ctx, `update metric_block_mark set week_through = $1, week_series = $2`, m.week, m.weekSeries); err != nil {
		return m, fmt.Errorf("storage: block the week from %s: %w", week.Format(time.RFC3339), err)
	}
	if err := tx.Commit(ctx); err != nil {
		return m, fmt.Errorf("storage: block the week from %s: %w", week.Format(time.RFC3339), err)
	}
	out.Blocks += written
	return m, nil
}

// lockBlockFolds takes the block lock exclusive in a fold's transaction,
// waiting for every batch merging late samples to commit, and holds it until
// the fold's commit; a merge that starts meanwhile waits behind it.
func lockBlockFolds(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, blockLockKey); err != nil {
		return fmt.Errorf("storage: wait for the late block merges: %w", err)
	}
	return nil
}

// lateSamples is the samples a batch landed in hours the trend compaction had
// announced, as their raw rows landed, and the lineage the batch wrote them
// under (null for observed; one writer writes a batch): the late merge's
// input.
type lateSamples struct {
	series []int64
	ts     []time.Time
	id     []int64
	value  []float64
	// rule, version and ruleID are the batch's source_rule,
	// source_rule_version and source_rule_id.
	rule, version, ruleID any
}

// mergeBlocksSQL merges late samples ($1..$4 their raw rows' series, ts, id
// and value; $5 the metric lane's days) into the blocks that cover them. A
// sample's day is covered once the day fold has passed its series there and
// the day is still intact: by the hour block of its hour when the day was cut
// into hours (a new one when the hour had none), else by the day's block,
// else, once the week fold has passed the series' week, by the week block.
// A sample of a day the fold has not reached is covered by nothing and waits
// as a raw row for the fold. Each block is written once, in key order: the
// samples it gains, in ts then id order, join those it holds after every
// sample at the same instant, so it unnests as it would have folded had they
// landed on time. The block's span is never revisited, however many samples
// it gains (ADR-1529). No mark row (no pass yet) covers nothing.
var mergeBlocksSQL = fmt.Sprintf(`with l as (
		select u.series_id, u.ts, u.id, u.value, date_bin('1 day', u.ts, %[1]s) as day
		from unnest($1::bigint[], $2::timestamptz[], $3::bigint[], $4::float8[]) as u(series_id, ts, id, value)),
	x as (select $6::text as source_rule, $7::bigint as source_rule_version, $8::uuid as source_rule_id),
	k as (
		select l.series_id, l.ts, l.id, l.value, l.day, date_bin('7 days', l.day, %[2]s) as week, mk.week_through, mk.week_series,
			exists (select 1 from metric_block b where b.series_id = l.series_id and b.span = 'hour'
				and b.span_start >= l.day and b.span_start < l.day + interval '24 hours' and %[5]s) as hours,
			exists (select 1 from metric_block b where b.series_id = l.series_id and b.span = 'day' and b.span_start = l.day
				and %[5]s) as whole
		from l cross join x join metric_series s on s.id = l.series_id and %[6]s
		join metric_type t on t.id = s.metric_type_id
		cross join metric_block_mark mk
		where (l.day < mk.blocked_through or l.day = mk.blocked_through and l.series_id <= mk.blocked_series)
			and %[3]s),
	m as (
		select k.series_id, k.ts, k.id, k.value,
			case when k.hours then date_bin('1 hour', k.ts, %[1]s) when k.whole then k.day else k.week end as span_start,
			case when k.hours then 'hour' when k.whole then 'day' else 'week' end as span
		from k
		where k.hours or k.whole or k.week < k.week_through or k.week = k.week_through and k.series_id <= k.week_series)
	%[7]s
	select m.series_id, m.span_start, m.span, $6::text, $7::bigint, $8::uuid, count(*), array_agg(%[4]s order by m.ts, m.id),
		array_agg(m.value order by m.ts, m.id)
	from m group by m.series_id, m.span_start, m.span
	order by m.series_id, m.span_start, m.span
	%[8]s do update set (n, offset_ms, value) = (
		select count(*), array_agg(x.o order by x.o, x.src, x.ord), array_agg(x.v order by x.o, x.src, x.ord)
		from (
			select u.o, u.v, 0 as src, u.ord from unnest(metric_block.offset_ms, metric_block.value) with ordinality as u(o, v, ord)
			union all
			select u.o, u.v, 1, u.ord from unnest(excluded.offset_ms, excluded.value) with ordinality as u(o, v, ord)) x)`,
	trendEpoch, blockWeekEpoch, blockIntact("l.day", "$5"), blockOffset("m.span_start"), sameLineage("b", "x"), blockSeries,
	blockInsert, blockConflict)

// mergeLateBlocks merges a batch's late samples into the blocks that cover
// them (mergeBlocksSQL), in the batch's transaction, after their raw rows
// have landed: the block lock taken shared in a statement of its own, so the
// merge's own statement reads the marks every fold that wrote before it
// committed, and no fold commits past them before the batch does.
//
// The wait for the lock has no lock_timeout, on purpose: the consumer Terms a
// message after maxTelemetryDeliveries redeliveries (about 15 s), so a fold
// chunk longer than that would drop the batch, its on-time rows included. The
// stall a long wait can cause on a multi-server install is bounded where it
// bites, by trendDrainTimeout failing the compaction (ADR-1529, decision 9).
func mergeLateBlocks(ctx context.Context, tx pgx.Tx, late lateSamples, laneDays int) error {
	if len(late.series) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock_shared(hashtextextended($1, 0))`, blockLockKey); err != nil {
		return fmt.Errorf("storage: merge late samples into their blocks: %w", err)
	}
	if _, err := tx.Exec(ctx, mergeBlocksSQL, late.series, late.ts, late.id, late.value, laneDays,
		late.rule, late.version, late.ruleID); err != nil {
		return fmt.Errorf("storage: merge late samples into their blocks: %w", err)
	}
	return nil
}

// verifyBlocksSQL holds every block whose span starts inside its series'
// window ($1 the lane's days) to the raw rows it was folded from: unnested,
// its samples are exactly the span's rows of its lineage (a week's, those of
// its lineage's days with no hour or day block of it), timestamps and value
// bits alike, in ts then id order, so a sample read back under a lineage its
// raw row was not written with is missing from one block and extra in
// another. It yields each block that differs, at most 100. Each block is
// paired with its rows by bk, a key of its own for the pass: a full join
// cannot match the lineage's nulls.
var verifyBlocksSQL = fmt.Sprintf(`with b as (
		select row_number() over () as bk, b.series_id, b.span_start, b.span, %[1]s, b.offset_ms, b.value from metric_block b
		join metric_series s on s.id = b.series_id
		join metric_type t on t.id = s.metric_type_id
		where %[2]s),
	got as (
		select b.bk, u.ord, u.ts, u.value from b cross join %[3]s u),
	want as (
		select b.bk, m.ts, m.value, row_number() over (partition by b.bk order by m.ts, m.id) as ord
		from b join metric m on m.series_id = b.series_id and m.ts >= b.span_start and m.ts < b.span_start +
			case b.span when 'hour' then interval '1 hour' when 'day' then interval '24 hours' else interval '168 hours' end
			and %[4]s
		where b.span <> 'week' or not exists (select 1 from metric_block o where o.series_id = b.series_id and o.span <> 'week'
			and o.span_start >= date_bin('1 day', m.ts, %[5]s) and o.span_start < date_bin('1 day', m.ts, %[5]s) + interval '24 hours'
			and %[6]s)),
	bad as (
		select distinct coalesce(g.bk, w.bk) as bk
		from got g full join want w on w.bk = g.bk and w.ord = g.ord
		where g.ts is distinct from w.ts or float8send(g.value) is distinct from float8send(w.value))
	select b.series_id, b.span_start, b.span, b.source_rule, b.source_rule_id::text, b.source_rule_version
	from bad join b on b.bk = bad.bk
	order by 1, 2, 3, 6, 4 limit 100`, blockLineage("b"), blockIntact("b.span_start", "$1"), blockSamplesOf("b"),
	sameLineage("m", "b"), trendEpoch, sameLineage("o", "b"))

// VerifyMetricBlocks holds every block inside its window to the raw rows it
// was folded from (verifyBlocksSQL) and returns the blocks that differ, none
// when every block round-trips. laneDays is the metric lane's window.
func (p *PG) VerifyMetricBlocks(ctx context.Context, laneDays int) ([]BlockKey, error) {
	rows, err := p.pool.Query(ctx, verifyBlocksSQL, laneDays)
	if err != nil {
		return nil, fmt.Errorf("storage: verify metric blocks: %w", err)
	}
	keys, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (BlockKey, error) {
		var k BlockKey
		var rule, ruleID *string
		var version *int64
		err := r.Scan(&k.SeriesID, &k.SpanStart, &k.Span, &rule, &ruleID, &version)
		k.Lineage = scanLineage(rule, ruleID, version)
		return k, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: verify metric blocks: %w", err)
	}
	return keys, nil
}

// blockSamplesSQL reads series $1's samples in [$2, $3) back from its
// blocks, each with its block's lineage: by ts, then by lineage (rows of one
// instant under two rule versions are rare, and the older version wrote
// first), then in landing order. A series' blocks of one lineage never
// overlap, so within a lineage the order is the raw rows' ts then id.
var blockSamplesSQL = `select u.ts, u.value, b.source_rule, b.source_rule_id::text, b.source_rule_version
	from metric_block b cross join ` + blockSamplesOf("b") + ` u
	where b.series_id = $1 and b.span_start < $3 and b.span_start + case b.span when 'hour' then interval '1 hour'
		when 'day' then interval '24 hours' else interval '168 hours' end > $2
		and u.ts >= $2 and u.ts < $3
	order by u.ts, b.source_rule_version nulls first, b.source_rule_id, b.source_rule, u.ord`

// MetricBlockSamples reads series seriesID's samples in [from, to) back from
// its blocks (blockSamplesSQL), each with the lineage its raw row was
// written with: the unnest the compacted tier's series reads build on. It
// reads blocks alone, so a span the fold has not reached reads as empty.
func (p *PG) MetricBlockSamples(ctx context.Context, seriesID int64, from, to time.Time) ([]BlockSample, error) {
	rows, err := p.pool.Query(ctx, blockSamplesSQL, seriesID, from, to)
	if err != nil {
		return nil, fmt.Errorf("storage: read series %d's blocks: %w", seriesID, err)
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
		return nil, fmt.Errorf("storage: read series %d's blocks: %w", seriesID, err)
	}
	return out, nil
}

// String is the block as the errors name it.
func (k BlockKey) String() string {
	parts := []string{fmt.Sprint(k.SeriesID), k.Span, k.SpanStart.UTC().Format(time.RFC3339)}
	if k.Lineage != nil {
		parts = append(parts, fmt.Sprintf("%s@%d", k.Lineage.Rule, k.Lineage.Version))
	}
	return strings.Join(parts, " ")
}
