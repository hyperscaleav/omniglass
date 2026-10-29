package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The trend tier (#1276, ADR-1276): one metric_trend row per observed metric
// series per hour (count, min, max, sum, first, last), written by compaction
// once the hour is past the late-data horizon, and kept for the trend window
// after the hour's samples leave the hot tier. Calculated series are sparse
// already and are not compacted; properties have no trends (time in state is
// derived on read).

// MetricTrendHorizon is how long after an hour ends the compaction waits
// before closing it: the late-data horizon (ADR-0311, the node spool replays
// up to 12 hours), so an on-time spool replay lands before its hour closes.
const MetricTrendHorizon = 24 * time.Hour

// trendEpoch anchors the hour buckets on UTC hours.
const trendEpoch = `timestamptz '2000-01-01 00:00:00+00'`

// trendWriteDrainTimeout bounds how long a compaction waits, after announcing
// the hours it is about to close, for the sample writes already in flight to
// finish.
const trendWriteDrainTimeout = 30 * time.Second

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

// metricTrendMark reads the compaction's mark, ok false before its first pass.
func metricTrendMark(ctx context.Context, q querier) (time.Time, bool, error) {
	var through time.Time
	err := q.QueryRow(ctx, `select compacted_through from metric_trend_mark`).Scan(&through)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("storage: read the trend mark: %w", err)
	}
	return through, true, nil
}

func setMetricTrendMark(ctx context.Context, q txQuerier, through time.Time) error {
	if _, err := q.Exec(ctx, `insert into metric_trend_mark (id, compacted_through) values (true, $1)
		on conflict (id) do update set compacted_through = excluded.compacted_through`, through); err != nil {
		return fmt.Errorf("storage: set the trend mark: %w", err)
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
// Closing is announced first: the mark moves past the hours about to close and
// commits, so every sample write that starts after it dirties the hour it
// lands in; then the compaction waits for the writes already in flight (each
// read the old mark and dirtied nothing) to finish, so its fold sees them. A
// write that outlasts trendWriteDrainTimeout puts the mark back and fails the
// pass, which the next pass retries.
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
	from, ok, err := metricTrendMark(ctx, p.pool)
	if err != nil {
		return out, err
	}
	if !ok {
		// The first pass starts at the oldest month the hot tier holds.
		parts, err := metricPartitions(ctx, p.pool)
		if err != nil {
			return out, err
		}
		from = target
		if len(parts) > 0 && parts[0].From.Before(target) {
			from = parts[0].From
		}
	}
	out.Through = from
	if from.Before(target) {
		through := from.Add(time.Duration(maxHours) * time.Hour)
		if through.After(target) {
			through = target
		}
		if err := p.announceTrendHours(ctx, through, from, ok); err != nil {
			return out, err
		}
		for h := from; h.Before(through); h = h.Add(time.Hour) {
			if _, err := p.pool.Exec(ctx, `insert into metric_trend (series_id, hour, count, min, max, sum, first, last)
				select `+trendAggregates("$1::timestamptz")+`
				from metric m join metric_series s on s.id = m.series_id and s.provenance = 'observed'
				where m.ts >= $1 and m.ts < $2
				group by m.series_id `+trendUpsert, h, h.Add(time.Hour)); err != nil {
				return out, fmt.Errorf("storage: compact the hour from %s: %w", h.Format(time.RFC3339), err)
			}
			out.Hours++
		}
		out.Through = through
		out.Behind = through.Before(target)
	} else if !ok {
		if err := setMetricTrendMark(ctx, p.pool, from); err != nil {
			return out, err
		}
	}
	n, err := p.recomputeDirtyTrends(ctx, laneDays)
	out.Recomputed = n
	return out, err
}

// announceTrendHours moves the mark to through and waits out the sample
// writes in flight when it committed; on a timeout it puts the mark back to
// from (or removes it, when there was none).
func (p *PG) announceTrendHours(ctx context.Context, through, from time.Time, hadMark bool) error {
	if err := setMetricTrendMark(ctx, p.pool, through); err != nil {
		return err
	}
	var snapshot string
	if err := p.pool.QueryRow(ctx, `select pg_current_snapshot()::text`).Scan(&snapshot); err != nil {
		return fmt.Errorf("storage: compact metric trends: %w", err)
	}
	deadline := time.Now().Add(trendWriteDrainTimeout)
	for {
		var drained bool
		if err := p.pool.QueryRow(ctx, `select not exists (select 1 from pg_snapshot_xip($1::pg_snapshot) x
			where pg_xact_status(x) = 'in progress')`, snapshot).Scan(&drained); err != nil {
			return fmt.Errorf("storage: compact metric trends: %w", err)
		}
		if drained {
			return nil
		}
		if time.Now().After(deadline) {
			restore := context.Background()
			if hadMark {
				_ = setMetricTrendMark(restore, p.pool, from)
			} else {
				_, _ = p.pool.Exec(restore, `delete from metric_trend_mark`)
			}
			return fmt.Errorf("storage: compact metric trends: a sample write in flight outlasted %s; the hours stay open", trendWriteDrainTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// recomputeDirtyTrends folds again every closed (series, hour) a late sample
// dirtied, a chunk a statement, clearing each mark it takes, and returns how
// many it recomputed.
func (p *PG) recomputeDirtyTrends(ctx context.Context, laneDays int) (int64, error) {
	var total int64
	for {
		tag, err := p.pool.Exec(ctx, `with d as (
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
			return total, fmt.Errorf("storage: recompute dirtied trend hours: %w", err)
		}
		total += tag.RowsAffected()
		var left bool
		if err := p.pool.QueryRow(ctx, `select exists (select 1 from metric_trend_dirty
			where hour < (select compacted_through from metric_trend_mark))`).Scan(&left); err != nil {
			return total, fmt.Errorf("storage: recompute dirtied trend hours: %w", err)
		}
		if !left {
			return total, nil
		}
	}
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

// metricPruneHorizon caps a metric prune's cutoff at the trend mark, so no
// sample of an hour the compaction has not closed is ever removed: a pass
// that compacted nothing yet prunes nothing.
func metricPruneHorizon(ctx context.Context, q querier, before time.Time) (time.Time, error) {
	through, ok, err := metricTrendMark(ctx, q)
	if err != nil {
		return time.Time{}, err
	}
	if !ok {
		return time.Time{}, nil
	}
	if through.Before(before) {
		return through, nil
	}
	return before, nil
}
