package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The trend tier (#1276, ADR-1276): one metric_trend row per observed metric
// series per hour (count, min, max, sum, first, last), written by compaction
// once the hour is past the late-data horizon, and kept for a window of its
// own (retention.trend_days), which nothing forces to exceed the samples'.
// Calculated series are sparse
// already and are not compacted; properties have no trends (time in state is
// derived on read).

// MetricTrendHorizon is how long after an hour ends the compaction waits
// before closing it: the late-data horizon (ADR-0311, the node spool replays
// up to 12 hours), so an on-time spool replay lands before its hour closes.
const MetricTrendHorizon = 24 * time.Hour

// trendEpoch anchors the hour buckets on UTC hours.
const trendEpoch = `timestamptz '2000-01-01 00:00:00+00'`

// trendLockKey names the advisory lock between the metric insert and the
// compaction's drain. metric_trend_announced() (the trend migration) takes it
// shared, by the same name, before it reads the announced mark.
const trendLockKey = "metric_trend:announce"

// trendDrainTimeout bounds how long a compaction waits, after announcing the
// hours it is about to close, for the metric inserts already holding the
// trend lock to commit. While it waits, a new insert queues behind it, so the
// bound is also the longest the ingest can stall on a compaction.
const trendDrainTimeout = 5 * time.Second

// trendDirtyChunk is how many dirty (series, hour) marks one statement
// recomputes.
const trendDirtyChunk = 500

// trendAggregates is the select list that folds one series' samples of one
// hour (alias m) into a trend row; hour is the bucket expression.
func trendAggregates(hour string) string {
	return `m.series_id, ` + hour + `, count(*), min(m.value), max(m.value), sum(m.value),
		(array_agg(m.value order by m.ts, m.id))[1], (array_agg(m.value order by m.ts desc, m.id desc))[1]`
}

// trendUpsert replaces a recomputed hour whole: a trend row is always the
// fold of every sample its hour holds, never an increment.
const trendUpsert = `on conflict (series_id, hour) do update set count = excluded.count, min = excluded.min,
	max = excluded.max, sum = excluded.sum, first = excluded.first, last = excluded.last`

// TrendCompaction is what one compaction did.
type TrendCompaction struct {
	// Hours is how many hours it closed, and Through the mark it left:
	// every hour before Through is closed.
	Hours   int
	Through time.Time
	// Recomputed is how many (series, hour) a late sample had dirtied and the
	// compaction folded again.
	Recomputed int64
	// Behind is true when hours past the horizon remain unclosed (the pass
	// reached maxHours), so the next pass should come soon.
	Behind bool
}

// trendMarks is the compaction's two marks: how far it has announced it will
// close hours (a sample dated before announced dirties its hour), and how far
// it has folded them (every hour before compacted has its trend).
type trendMarks struct {
	announced, compacted time.Time
}

// metricTrendMarks reads the compaction's marks, ok false before its first
// pass.
func metricTrendMarks(ctx context.Context, q querier) (trendMarks, bool, error) {
	var m trendMarks
	err := q.QueryRow(ctx, `select announced_through, compacted_through from metric_trend_mark`).Scan(&m.announced, &m.compacted)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, false, nil
	}
	if err != nil {
		return m, false, fmt.Errorf("storage: read the trend marks: %w", err)
	}
	return m, true, nil
}

// setMetricTrendMarks stands both marks at through.
func setMetricTrendMarks(ctx context.Context, q txQuerier, through time.Time) error {
	if _, err := q.Exec(ctx, `insert into metric_trend_mark (id, announced_through, compacted_through) values (true, $1, $1)
		on conflict (id) do update set announced_through = excluded.announced_through,
			compacted_through = excluded.compacted_through`, through); err != nil {
		return fmt.Errorf("storage: set the trend marks: %w", err)
	}
	return nil
}

// CompactMetricTrends closes up to maxHours hours of observed metric samples
// into hourly trends, oldest first, up to the hour that ended a horizon ago,
// then recomputes every closed hour a late sample has dirtied since. laneDays
// is the metric lane's window: a dirtied hour whose samples are already past
// its type's window is not recomputed (what remains would be only part of
// the hour), and the late sample it held goes with the next prune.
//
// Closing is announced first: the announced mark moves past the hours about
// to close and commits, so every metric insert that reads it after dirties the
// hour it lands in. The drain then waits for the inserts that may have read
// the old mark (each holds the trend lock) to commit, so the fold sees them.
// Each hour then folds in a transaction of its own that moves the compacted
// mark past it, so a pass that stops anywhere (a cancel, a failed fold, a
// crash) leaves every hour before the compacted mark folded and every hour
// after it open, for the next pass to fold; the prunes stop at the compacted
// mark.
func (p *PG) CompactMetricTrends(ctx context.Context, maxHours, laneDays int) (TrendCompaction, error) {
	var out TrendCompaction
	if maxHours <= 0 {
		return out, fmt.Errorf("storage: compact metric trends: maxHours %d, want at least 1", maxHours)
	}
	now, err := dbNow(ctx, p.pool)
	if err != nil {
		return out, err
	}
	target := now.Add(-MetricTrendHorizon).UTC().Truncate(time.Hour)
	marks, ok, err := metricTrendMarks(ctx, p.pool)
	if err != nil {
		return out, err
	}
	if !ok {
		if marks, err = p.seedTrendMarks(ctx, now, target, laneDays); err != nil {
			return out, err
		}
	}
	from := marks.compacted
	out.Through = from
	if from.Before(target) {
		through := from.Add(time.Duration(maxHours) * time.Hour)
		if through.After(target) {
			through = target
		}
		if _, err := p.pool.Exec(ctx, `update metric_trend_mark set announced_through = greatest(announced_through, $1)`, through); err != nil {
			return out, fmt.Errorf("storage: announce the trend hours to %s: %w", through.Format(time.RFC3339), err)
		}
		if err := p.drainTrendWrites(ctx); err != nil {
			return out, err
		}
		for h := from; h.Before(through); h = h.Add(time.Hour) {
			if err := p.foldTrendHour(ctx, h); err != nil {
				return out, err
			}
			out.Hours++
			out.Through = h.Add(time.Hour)
		}
		out.Behind = through.Before(target)
	}
	n, err := p.recomputeDirtyTrends(ctx, laneDays)
	out.Recomputed = n
	return out, err
}

// seedTrendMarks stands both marks where the first pass starts: the oldest
// month the hot tier holds, or the longest window (the metric lane's, or a
// type's own) back from now when that is later, so an install upgraded with
// years of samples kept does not fold hours every prune is about to remove.
// Never past target. A second server seeding at once keeps the first's.
func (p *PG) seedTrendMarks(ctx context.Context, now, target time.Time, laneDays int) (trendMarks, error) {
	var days int
	if err := p.pool.QueryRow(ctx, `select greatest($1::int, coalesce(max(retention_days), 0)) from metric_type`, laneDays).Scan(&days); err != nil {
		return trendMarks{}, fmt.Errorf("storage: seed the trend marks: %w", err)
	}
	parts, err := metricPartitions(ctx, p.pool)
	if err != nil {
		return trendMarks{}, err
	}
	from := now.Add(-time.Duration(days) * 24 * time.Hour).UTC().Truncate(time.Hour)
	if len(parts) > 0 && parts[0].From.After(from) {
		from = parts[0].From
	}
	if from.After(target) {
		from = target
	}
	if _, err := p.pool.Exec(ctx, `insert into metric_trend_mark (id, announced_through, compacted_through)
		values (true, $1, $1) on conflict (id) do nothing`, from); err != nil {
		return trendMarks{}, fmt.Errorf("storage: seed the trend marks: %w", err)
	}
	marks, _, err := metricTrendMarks(ctx, p.pool)
	return marks, err
}

// drainTrendWrites waits until no metric insert that read the announced mark
// before it last moved is still open: it takes the trend lock exclusive, which
// waits for every insert holding it shared, and lets it go at once. An insert
// that takes the lock after reads the moved mark. Only the inserts wait: a
// transaction of any other kind, or in another database, holds no trend lock.
// An insert that outlasts trendDrainTimeout fails the pass with the hours
// still open, for the next pass to retry; the announced mark stays where it
// moved, which only dirties a few hours the fold would have seen.
func (p *PG) drainTrendWrites(ctx context.Context) error {
	tx, err := p.pool.BeginTx(ctx, trendTxOptions)
	if err != nil {
		return fmt.Errorf("storage: drain the metric inserts: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := lockTrendWrites(ctx, tx, "the hours stay open"); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: drain the metric inserts: %w", err)
	}
	return nil
}

// trendTxOptions pins the compaction's and the ingest's transactions to READ
// COMMITTED, whatever default_transaction_isolation the operator's Postgres
// sets: each relies on a statement that follows the trend lock seeing what
// committed before the lock was granted, which a REPEATABLE READ snapshot,
// taken at the transaction's first statement, would not.
var trendTxOptions = pgx.TxOptions{IsoLevel: pgx.ReadCommitted}

// lockTrendWrites takes the trend lock exclusive in tx, waiting for every
// metric insert holding it shared to end, and holds it until tx ends; a new
// insert waits behind it. An insert that outlasts trendDrainTimeout fails it,
// the error saying what stays undone.
func lockTrendWrites(ctx context.Context, tx pgx.Tx, undone string) error {
	if _, err := tx.Exec(ctx, fmt.Sprintf(`set local lock_timeout = %d`, trendDrainTimeout.Milliseconds())); err != nil {
		return fmt.Errorf("storage: drain the metric inserts: %w", err)
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, trendLockKey); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
			return fmt.Errorf("storage: compact metric trends: a metric insert in flight outlasted %s; %s", trendDrainTimeout, undone)
		}
		return fmt.Errorf("storage: drain the metric inserts: %w", err)
	}
	return nil
}

// foldTrendHour folds the hour from h into one trend row per observed series
// and moves the compacted mark past it, in one transaction: the mark never
// passes an hour whose trend did not commit.
func (p *PG) foldTrendHour(ctx context.Context, h time.Time) error {
	tx, err := p.pool.BeginTx(ctx, trendTxOptions)
	if err != nil {
		return fmt.Errorf("storage: compact the hour from %s: %w", h.Format(time.RFC3339), err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `insert into metric_trend (series_id, hour, count, min, max, sum, first, last)
		select `+trendAggregates("$1::timestamptz")+`
		from metric m join metric_series s on s.id = m.series_id and s.provenance = 'observed'
		where m.ts >= $1 and m.ts < $2
		group by m.series_id `+trendUpsert, h, h.Add(time.Hour)); err != nil {
		return fmt.Errorf("storage: compact the hour from %s: %w", h.Format(time.RFC3339), err)
	}
	if _, err := tx.Exec(ctx, `update metric_trend_mark set compacted_through = greatest(compacted_through, $1)`, h.Add(time.Hour)); err != nil {
		return fmt.Errorf("storage: compact the hour from %s: %w", h.Format(time.RFC3339), err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: compact the hour from %s: %w", h.Format(time.RFC3339), err)
	}
	return nil
}

// recomputeDirtyTrends folds again every closed (series, hour) a late sample
// dirtied, a chunk a transaction, clearing each mark it takes, and returns how
// many it recomputed.
func (p *PG) recomputeDirtyTrends(ctx context.Context, laneDays int) (int64, error) {
	var total int64
	for {
		var left bool
		if err := p.pool.QueryRow(ctx, `select exists (select 1 from metric_trend_dirty
			where hour < (select compacted_through from metric_trend_mark))`).Scan(&left); err != nil {
			return total, fmt.Errorf("storage: recompute dirtied trend hours: %w", err)
		}
		if !left {
			return total, nil
		}
		n, err := p.recomputeDirtyChunk(ctx, laneDays)
		total += n
		if err != nil {
			return total, err
		}
	}
}

// recomputeDirtyChunk recomputes up to trendDirtyChunk dirtied hours in one
// transaction that holds the trend lock exclusive from before its statement:
// a late insert into a dirtied hour finds the mark already there and takes no
// row lock on it, so only the trend lock orders the two. An insert that holds
// it commits before the statement's snapshot, which folds its samples; an
// insert that waits for it lands after the mark is cleared, and marks the
// hour again.
func (p *PG) recomputeDirtyChunk(ctx context.Context, laneDays int) (int64, error) {
	tx, err := p.pool.BeginTx(ctx, trendTxOptions)
	if err != nil {
		return 0, fmt.Errorf("storage: recompute dirtied trend hours: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := lockTrendWrites(ctx, tx, "the dirtied hours stay marked"); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `with d as (
			delete from metric_trend_dirty x using (
				select series_id, hour from metric_trend_dirty
				where hour < (select compacted_through from metric_trend_mark)
				order by hour limit $1 for update skip locked) y
			where x.series_id = y.series_id and x.hour = y.hour
			returning x.series_id, x.hour),
		intact as (
			select d.series_id, d.hour from d
			join metric_series s on s.id = d.series_id
			join metric_type t on t.id = s.metric_type_id
			where d.hour >= now() - make_interval(days => coalesce(t.retention_days, $2::int)))
		insert into metric_trend (series_id, hour, count, min, max, sum, first, last)
		select `+trendAggregates("f.hour")+`
		from intact f join metric m on m.series_id = f.series_id and m.ts >= f.hour and m.ts < f.hour + interval '1 hour'
		group by m.series_id, f.hour `+trendUpsert, trendDirtyChunk, laneDays)
	if err != nil {
		return 0, fmt.Errorf("storage: recompute dirtied trend hours: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("storage: recompute dirtied trend hours: %w", err)
	}
	return tag.RowsAffected(), nil
}

// PruneMetricTrends deletes up to limit trend rows of hours before before (the
// trend window), and returns how many.
func (p *PG) PruneMetricTrends(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("storage: prune metric trends: limit %d, want at least 1", limit)
	}
	tag, err := p.pool.Exec(ctx, `delete from metric_trend where ctid = any(array(
		select ctid from metric_trend where hour < $1 limit $2))`, before, limit)
	if err != nil {
		return 0, fmt.Errorf("storage: prune metric trends: %w", err)
	}
	return tag.RowsAffected(), nil
}

// metricPruneHorizon caps a metric prune's cutoff at the compacted mark, so
// no sample of an hour the compaction has not folded is ever removed (an
// announced hour is not yet folded): a pass that compacted nothing yet prunes
// nothing.
func metricPruneHorizon(ctx context.Context, q querier, before time.Time) (time.Time, error) {
	marks, ok, err := metricTrendMarks(ctx, q)
	if err != nil {
		return time.Time{}, err
	}
	if !ok {
		return time.Time{}, nil
	}
	if marks.compacted.Before(before) {
		return marks.compacted, nil
	}
	return before, nil
}
