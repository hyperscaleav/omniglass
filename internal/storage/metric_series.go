package storage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The narrow metric lane (#1275, ADR-1275). A metric sample is a row of
// (id, series_id, ts, value) in metric, range partitioned by month on ts; what
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

// Monthly partitions. A month is a UTC calendar month, and its partition is
// named metric_pYYYYMM.

// metricPartitionsAhead is how many months past the current one the server
// keeps a partition for, so a sample stamped a little ahead, or a month
// that starts while the roll is late, always has somewhere to land.
const metricPartitionsAhead = 2

// partitionLockTimeout bounds how long a partition's attach or detach waits
// for its lock on metric, so it never queues long in front of the readers
// and writers behind it; a roll that times out is tried again by the next.
const partitionLockTimeout = 5 * time.Second

// metricMonth is the UTC month t falls in.
func metricMonth(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// metricPartitionName is a month's partition's table name.
func metricPartitionName(month time.Time) string {
	return "metric_p" + month.UTC().Format("200601")
}

// MetricPartition is one month's partition of the metric table.
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

// MetricPartitions lists the metric table's monthly partitions, oldest first.
func (p *PG) MetricPartitions(ctx context.Context) ([]MetricPartition, error) {
	return metricPartitions(ctx, p.pool)
}

// EnsureMetricPartitions makes sure the metric table has a partition for the
// current month and the metricPartitionsAhead after it, creating any that is
// missing, and returns the names it created. The server calls it at boot,
// before its ingest starts, and on every retention pass after (#420's roll).
func (p *PG) EnsureMetricPartitions(ctx context.Context) ([]string, error) {
	now, err := dbNow(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	months := make([]time.Time, 0, metricPartitionsAhead+1)
	for i := 0; i <= metricPartitionsAhead; i++ {
		months = append(months, metricMonth(now).AddDate(0, i, 0))
	}
	return p.ensureMetricMonths(ctx, months)
}

// ensureMetricMonths creates the partition of each month that has none and
// returns the names it created. Each is created detached and then attached,
// so metric is held only at SHARE UPDATE EXCLUSIVE (readers and writers go
// on), under partitionLockTimeout; two servers ensuring at once serialize on
// an advisory lock.
func (p *PG) ensureMetricMonths(ctx context.Context, months []time.Time) ([]string, error) {
	have, err := metricPartitions(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	covered := func(m time.Time) bool {
		return slices.ContainsFunc(have, func(x MetricPartition) bool { return !m.Before(x.From) && m.Before(x.To) })
	}
	var created []string
	for _, m := range months {
		m = metricMonth(m)
		if covered(m) || slices.Contains(created, metricPartitionName(m)) {
			continue
		}
		made, err := p.createMetricPartition(ctx, m)
		if err != nil {
			return created, err
		}
		if made {
			created = append(created, metricPartitionName(m))
		}
	}
	return created, nil
}

// createMetricPartition creates and attaches one month's partition, reporting
// false when another server's attach got there first.
func (p *PG) createMetricPartition(ctx context.Context, month time.Time) (bool, error) {
	name := pgx.Identifier{"public", metricPartitionName(month)}.Sanitize()
	from, to := month.Format(time.RFC3339), month.AddDate(0, 1, 0).Format(time.RFC3339)
	made := false
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`set local lock_timeout = '%dms'`, partitionLockTimeout.Milliseconds())); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('metric partition roll'))`); err != nil {
			return err
		}
		var attached bool
		if err := tx.QueryRow(ctx, `select exists (select 1 from pg_inherits where inhparent = 'public.metric'::regclass
			and inhrelid = to_regclass($1))`, name).Scan(&attached); err != nil {
			return err
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
	if err != nil {
		return false, fmt.Errorf("storage: create metric partition %s: %w", metricPartitionName(month), err)
	}
	return made, nil
}

// DropMetricPartitions drops every metric partition whose whole month lies
// before both before (the lane's window) and the longest window any metric
// type keeps for itself (retention_days), so a type kept longer than the
// lane keeps its months; it returns the names it dropped. Each is detached
// CONCURRENTLY first, which waits for the readers already in it rather than
// queueing every reader behind an exclusive lock, and then dropped; a detach
// an earlier pass left unfinished is finished first.
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
		if _, err := conn.Exec(ctx, `alter table public.metric detach partition `+pgx.Identifier{"public", name}.Sanitize()+` finalize`); err != nil {
			return nil, fmt.Errorf("storage: finish detaching %s: %w", name, err)
		}
		if _, err := conn.Exec(ctx, `drop table if exists `+pgx.Identifier{"public", name}.Sanitize()); err != nil {
			return nil, fmt.Errorf("storage: drop %s: %w", name, err)
		}
	}

	var horizon time.Time
	if err := conn.QueryRow(ctx, `select least($1::timestamptz,
			now() - make_interval(days => coalesce((select max(retention_days) from metric_type), 0)))`, before).Scan(&horizon); err != nil {
		return nil, fmt.Errorf("storage: drop metric partitions: %w", err)
	}
	parts, err := metricPartitions(ctx, conn)
	if err != nil {
		return nil, err
	}
	var dropped []string
	for _, part := range parts {
		if part.To.After(horizon) {
			continue
		}
		name := pgx.Identifier{"public", part.Name}.Sanitize()
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
// partition of metric covers a row's ts (SQLSTATE 23514 with no constraint
// named; a CHECK violation names its constraint). It is not a refusal of the
// data: the partition is made and the write retried.
func isPartitionRoutingMiss(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514" && pgErr.ConstraintName == "" &&
		strings.HasPrefix(pgErr.Message, "no partition of relation")
}

// landMetricMonths makes the partitions a write's samples need after it
// missed one: the month of every sample, as the write will stamp it (a zero
// ts lands now). A collected key keeps its producer's time however far it
// is (TestNodeProbeSampleCannotOutliveItsTime), so its month is made
// whatever it is; a month in the past is dropped again by the next
// retention pass once it is past every window.
func (p *PG) landMetricMonths(ctx context.Context, samples []MetricSampleWrite) error {
	now, err := dbNow(ctx, p.pool)
	if err != nil {
		return err
	}
	months := make([]time.Time, 0, len(samples)+1)
	months = append(months, metricMonth(now))
	for _, s := range samples {
		if !s.TS.IsZero() {
			months = append(months, metricMonth(s.TS))
		}
	}
	_, err = p.ensureMetricMonths(ctx, months)
	return err
}
