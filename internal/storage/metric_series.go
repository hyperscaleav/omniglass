package storage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The narrow metric lane (#1275, ADR-1275). A metric sample is a row of
// (id, series_id, ts, value) in metric, range partitioned by UTC day on ts (#1527); what
// it is a sample of (type, owner arc, instance, provenance, source, and the
// node that collected it) is one row of metric_series, shared by every sample
// of the series. A command's intended rows live in metric_declared, a plain
// table retention never touches. Every reader joins the series; nothing reads
// a sample without it, so a sample whose series is gone (its owner deleted)
// is invisible until retention removes it.

// metricSeriesWhere is the predicate that a metric_series row (alias s) is
// one of an owner's series of a type: ownerKind, owner and typeID are SQL
// expressions. It reads through metric_series_key's prefix.
func metricSeriesWhere(s, ownerKind, owner, typeID string) string {
	return fmt.Sprintf(`%[1]s.owner_id = %[3]s and %[1]s.owner_kind = %[2]s and %[1]s.metric_type_id = %[4]s`, s, ownerKind, owner, typeID)
}

// metricHeldSQL is the predicate that an owner holds a stored row of a metric
// type, in either tier: owner is an SQL expression, and typeMatch the
// comparison's right side (a type id expression, or any(...)).
func metricHeldSQL(owner, typeMatch string) string {
	return `exists (select 1 from metric_series hs where hs.owner_id = ` + owner + ` and hs.metric_type_id = ` + typeMatch + `
		and (exists (select 1 from metric hm where hm.series_id = hs.id)
			or exists (select 1 from metric_declared hd where hd.series_id = hs.id)))`
}

// Daily partitions (#1527, ADR-1527, amending ADR-1275's months). A day is a
// UTC calendar day, and its partition is named metric_pYYYYMMDD. A day, not a
// month, so retention drops raw rows whole a day after they pass the window,
// rather than deleting them chunk by chunk for up to a month first.

// metricPartitionsAhead is how many days past the current one the server
// keeps a partition for, so a sample stamped a little ahead, or a day that
// starts while the roll is late, always has somewhere to land.
const metricPartitionsAhead = 3

// partitionLockTimeout bounds how long a partition's attach or detach waits
// for its lock on metric, so it never queues long in front of the readers
// and writers behind it; a roll that times out is tried again by the next.
const partitionLockTimeout = 5 * time.Second

// metricDay is the UTC day t falls in.
func metricDay(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// metricPartitionName is a day's partition's table name.
func metricPartitionName(day time.Time) string {
	return "metric_p" + day.UTC().Format("20060102")
}

// MetricPartition is one day's partition of the metric table.
type MetricPartition struct {
	Name     string
	From, To time.Time
}

// metricPartitions lists metric's partitions oldest first, with their bounds,
// leaving out one still detaching (DropMetricPartitions finishes it).
func metricPartitions(ctx context.Context, q querier) ([]MetricPartition, error) {
	rows, err := q.Query(ctx, `select c.relname::text,
			(regexp_match(pg_get_expr(c.relpartbound, c.oid), 'FROM \(''([^'']+)''\)'))[1]::timestamptz,
			(regexp_match(pg_get_expr(c.relpartbound, c.oid), 'TO \(''([^'']+)''\)'))[1]::timestamptz
		from pg_inherits i join pg_class c on c.oid = i.inhrelid
		where i.inhparent = 'public.metric'::regclass and not i.inhdetachpending
		order by 2`)
	if err != nil {
		return nil, fmt.Errorf("storage: list metric partitions: %w", err)
	}
	parts, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (MetricPartition, error) {
		var p MetricPartition
		return p, r.Scan(&p.Name, &p.From, &p.To)
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list metric partitions: %w", err)
	}
	return parts, nil
}

// MetricPartitions lists the metric table's daily partitions, oldest first.
func (p *PG) MetricPartitions(ctx context.Context) ([]MetricPartition, error) {
	return metricPartitions(ctx, p.pool)
}

// metricCoverageGap is the first instant in [from, to) that no partition in
// parts (oldest first, as metricPartitions lists them) covers, ok false when
// they cover all of it.
func metricCoverageGap(parts []MetricPartition, from, to time.Time) (time.Time, bool) {
	at := from
	for _, part := range parts {
		if !at.Before(to) {
			break
		}
		if part.From.After(at) {
			return at, true
		}
		if part.To.After(at) {
			at = part.To
		}
	}
	if at.Before(to) {
		return at, true
	}
	return time.Time{}, false
}

// EnsureMetricPartitions makes sure the metric table has a partition for the
// current day and the metricPartitionsAhead after it, creating any that is
// missing, and returns the names it created. It then reads the partitions
// back and refuses, naming the first instant uncovered, unless they cover
// every instant from the start of today through the end of the last day
// ahead: the server calls it at boot and starts no ingest over a refusal,
// and on every retention pass after (#420's roll).
func (p *PG) EnsureMetricPartitions(ctx context.Context) ([]string, error) {
	now, err := dbNow(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	days := make([]time.Time, 0, metricPartitionsAhead+1)
	for i := 0; i <= metricPartitionsAhead; i++ {
		days = append(days, metricDay(now).AddDate(0, 0, i))
	}
	created, err := p.ensureMetricDays(ctx, days)
	if err != nil {
		return created, err
	}
	parts, err := metricPartitions(ctx, p.pool)
	if err != nil {
		return created, err
	}
	through := metricDay(now).AddDate(0, 0, metricPartitionsAhead+1)
	if gap, ok := metricCoverageGap(parts, metricDay(now), through); ok {
		return created, fmt.Errorf("storage: metric partitions do not cover %s (want every instant from %s to %s)",
			gap.Format(time.RFC3339), metricDay(now).Format(time.RFC3339), through.Format(time.RFC3339))
	}
	return created, nil
}

// ensureMetricDays creates the partition of each day that has none and
// returns the names it created. Each is created detached and then attached,
// so metric is held only at SHARE UPDATE EXCLUSIVE (readers and writers go
// on), under partitionLockTimeout; two servers ensuring at once serialize on
// an advisory lock.
func (p *PG) ensureMetricDays(ctx context.Context, days []time.Time) ([]string, error) {
	have, err := metricPartitions(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	covered := func(d time.Time) bool {
		return slices.ContainsFunc(have, func(x MetricPartition) bool { return !d.Before(x.From) && d.Before(x.To) })
	}
	var created []string
	for _, d := range days {
		d = metricDay(d)
		if covered(d) || slices.Contains(created, metricPartitionName(d)) {
			continue
		}
		made, err := p.createMetricPartition(ctx, d)
		if err != nil {
			return created, err
		}
		if made {
			created = append(created, metricPartitionName(d))
		}
	}
	return created, nil
}

// createMetricPartition creates and attaches one day's partition, reporting
// false when another server's attach got there first.
func (p *PG) createMetricPartition(ctx context.Context, day time.Time) (bool, error) {
	name := pgx.Identifier{"public", metricPartitionName(day)}.Sanitize()
	from, to := day.Format(time.RFC3339), day.AddDate(0, 0, 1).Format(time.RFC3339)
	made := false
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`set local lock_timeout = '%dms'`, partitionLockTimeout.Milliseconds())); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('metric partition roll'))`); err != nil {
			return err
		}
		var attached, pending bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from pg_inherits where inhparent = 'public.metric'::regclass
			and inhrelid = to_regclass($1)), exists (select 1 from pg_inherits where inhparent = 'public.metric'::regclass
			and inhrelid = to_regclass($1) and inhdetachpending)`, name).Scan(&attached, &pending); err != nil {
			return err
		}
		if pending {
			return errDetachPending
		}
		if attached {
			return nil
		}
		if _, err := tx.Exec(ctx, `create table if not exists `+name+` (like public.metric including defaults)`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`alter table public.metric attach partition %s for values from ('%s') to ('%s')`, name, from, to)); err != nil {
			return err
		}
		made = true
		return nil
	})
	if errors.Is(err, errDetachPending) {
		// A retention pass's detach of this day stopped halfway (a reader
		// held the parent past its lock timeout): the day routes nothing
		// until the detach is finished. Finish it and drop the day, then
		// make it afresh, so a sample dated in it lands rather than missing
		// again on every delivery.
		if ferr := p.finishMetricDetach(ctx, metricPartitionName(day)); ferr != nil {
			return false, fmt.Errorf("storage: create metric partition %s: %w", metricPartitionName(day), ferr)
		}
		return p.createMetricPartition(ctx, day)
	}
	if err != nil {
		return false, fmt.Errorf("storage: create metric partition %s: %w", metricPartitionName(day), err)
	}
	return made, nil
}

// errDetachPending is a day whose partition is still detaching.
var errDetachPending = errors.New("storage: metric partition still detaching")

// finishMetricDetach finishes a pending detach of one day and drops it,
// outside a transaction block (FINALIZE cannot run in one), under
// partitionLockTimeout.
func (p *PG) finishMetricDetach(ctx context.Context, name string) error {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, fmt.Sprintf(`set lock_timeout = '%dms'`, partitionLockTimeout.Milliseconds())); err != nil {
		return err
	}
	defer func() { _, _ = conn.Exec(context.Background(), `reset lock_timeout`) }()
	ident := pgx.Identifier{"public", name}.Sanitize()
	if _, err := conn.Exec(ctx, `alter table public.metric detach partition `+ident+` finalize`); err != nil {
		return fmt.Errorf("finish detaching %s: %w", name, err)
	}
	if _, err := conn.Exec(ctx, `drop table if exists `+ident); err != nil {
		return fmt.Errorf("drop %s: %w", name, err)
	}
	return nil
}

// metricPartitionHeldSQL is whether one partition (part, a sanitized name)
// holds a sample a type's own window still reaches: a series of a type with
// retention_days whose cutoff (now less its days) falls before the
// partition's end ($1), with a sample in the partition, probed series by
// series through the partition's series index. Only a type kept longer than
// the lane can hold a day the lane's window has passed.
func metricPartitionHeldSQL(part string) string {
	return `select exists (select 1 from metric_type t join metric_series s on s.metric_type_id = t.id
		where t.retention_days is not null and now() - make_interval(days => t.retention_days) < $1
			and exists (select 1 from ` + part + ` m where m.series_id = s.id))`
}

// DropMetricPartitions drops every metric partition whose whole day lies
// before before (the lane's window) and before the trend compaction's
// compacted mark (#1276), and that holds no sample of a type whose own
// window (retention_days) still reaches into the day: a day holding a sample
// of a type kept longer than the lane stays until that window passes too,
// and only the chunked prune runs inside it until then. It returns the names
// it dropped. Each is detached CONCURRENTLY first, which waits for the
// readers already in it rather than queueing every reader behind an
// exclusive lock, and then dropped; a detach an earlier pass left unfinished
// is finished first.
func (p *PG) DropMetricPartitions(ctx context.Context, before time.Time) ([]string, error) {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: drop metric partitions: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, fmt.Sprintf(`set lock_timeout = '%dms'`, partitionLockTimeout.Milliseconds())); err != nil {
		return nil, fmt.Errorf("storage: drop metric partitions: %w", err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), `reset lock_timeout`) }()

	pending, err := conn.Query(ctx, `select c.relname::text from pg_inherits i join pg_class c on c.oid = i.inhrelid
		where i.inhparent = 'public.metric'::regclass and i.inhdetachpending`)
	if err != nil {
		return nil, fmt.Errorf("storage: drop metric partitions: %w", err)
	}
	unfinished, err := pgx.CollectRows(pending, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("storage: drop metric partitions: %w", err)
	}
	for _, name := range unfinished {
		if err := p.finishMetricDetach(ctx, name); err != nil {
			return nil, fmt.Errorf("storage: %w", err)
		}
	}

	// A day holding an hour the trend compaction has not closed stays
	// (#1276).
	horizon, err := metricPruneHorizon(ctx, conn, before)
	if err != nil {
		return nil, err
	}
	if horizon.IsZero() {
		return nil, nil
	}
	parts, err := metricPartitions(ctx, conn)
	if err != nil {
		return nil, err
	}
	var dropped []string
	for _, part := range parts {
		if part.To.After(horizon) {
			break
		}
		name := pgx.Identifier{"public", part.Name}.Sanitize()
		var held bool
		if err := conn.QueryRow(ctx, metricPartitionHeldSQL(name), part.To).Scan(&held); err != nil {
			return dropped, fmt.Errorf("storage: drop metric partitions: %s: %w", part.Name, err)
		}
		if held {
			continue
		}
		if _, err := conn.Exec(ctx, `alter table public.metric detach partition `+name+` concurrently`); err != nil {
			return dropped, fmt.Errorf("storage: detach %s: %w", part.Name, err)
		}
		if _, err := conn.Exec(ctx, `drop table if exists `+name); err != nil {
			return dropped, fmt.Errorf("storage: drop %s: %w", part.Name, err)
		}
		dropped = append(dropped, part.Name)
	}
	return dropped, nil
}

// isPartitionRoutingMiss reports a write Postgres refused because no
// partition of metric covers a row's ts: SQLSTATE 23514 on the table metric
// with no constraint named (a CHECK violation names its constraint). It reads
// the error's fields, never its message, which a server translates under its
// own lc_messages. It is not a refusal of the data: the partition is made and
// the write retried.
func isPartitionRoutingMiss(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514" && pgErr.ConstraintName == "" && pgErr.TableName == "metric"
}

// landMetricDays makes the partitions a write's samples need after it
// missed one: the day of every sample, as the write will stamp it (a zero
// ts lands now). A collected key keeps its producer's time however far it
// is (TestNodeProbeSampleCannotOutliveItsTime), so its day is made
// whatever it is; a day in the past is dropped again by the next
// retention pass once it is past every window.
func (p *PG) landMetricDays(ctx context.Context, samples []MetricSampleWrite) error {
	now, err := dbNow(ctx, p.pool)
	if err != nil {
		return err
	}
	days := make([]time.Time, 0, len(samples)+1)
	days = append(days, metricDay(now))
	for _, s := range samples {
		if !s.TS.IsZero() {
			days = append(days, metricDay(s.TS))
		}
	}
	_, err = p.ensureMetricDays(ctx, days)
	return err
}
