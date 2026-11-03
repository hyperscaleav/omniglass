package storage

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The bucketed series reads (#1269): one series over a range at a resolution,
// as regular date_bin buckets rather than the newest N rows. A read that
// truncates to a tail answers a 30-day request with the last 16 hours; a bucket
// grid answers it with 720 hourly buckets whatever the cadence, and a one-sample
// spike survives as its bucket's max. Nothing is stored for it: the buckets are
// computed from the raw rows at read time, inside the hot window. Beyond the
// window the hourly trends will answer (#1276); until they exist nothing is read
// past it, and CoveredFrom says where that edge is.
//
// A bucket aggregates one logical series (#1485): one instance of the type on
// the owner, measured rows only (observed and calculated). Two instances are
// two series, and a declared or intended row is a want, not a reading, so
// neither may move a bucket's min, max, avg or last. Rows of one instance from
// different sources or collector nodes stay one series: a node move or a task
// reassignment does not split what was measured.

// MetricBucket is one regular bucket of a metric series. An empty bucket has
// Count 0 and every aggregate nil.
type MetricBucket struct {
	TS                  time.Time
	Count               int64
	Min, Max, Avg, Last *float64
}

// MetricBuckets is a metric series' bucket grid, oldest first, and the
// earliest instant the read could have data for: a bucket before it is empty
// because retention removed its rows, not because collection stopped.
type MetricBuckets struct {
	// Instance is the one instance the buckets aggregate (#1485).
	Instance    string
	CoveredFrom time.Time
	Buckets     []MetricBucket
}

// PropertyBucket is one regular bucket of a property series: how many changes
// it holds and the value in force at its end. Min, max and avg mean nothing to a
// string; a step chart holds Last across the empty buckets after it.
type PropertyBucket struct {
	TS    time.Time
	Count int64
	Last  *string
}

// PropertyBuckets mirrors MetricBuckets over the property lane.
type PropertyBuckets struct {
	Instance    string
	CoveredFrom time.Time
	Buckets     []PropertyBucket
}

// bucketCount is how many buckets of resolution cover window: the window
// rounded up to a whole bucket, so the grid always reaches the range's start.
func bucketCount(window, resolution time.Duration) int {
	return int(math.Ceil(float64(window) / float64(resolution)))
}

func checkBucketing(window, resolution time.Duration) error {
	if window <= 0 || resolution <= 0 {
		return fmt.Errorf("storage: bucketed series read: window %s and resolution %s must be positive", window, resolution)
	}
	return nil
}

// AmbiguousInstanceError answers a bucketed read that named no instance when
// more than one instance of the series has measured rows in the window: the
// buckets would blend them, so the caller picks one of Instances.
type AmbiguousInstanceError struct {
	Key       string
	Instances []string
}

func (e *AmbiguousInstanceError) Error() string {
	quoted := make([]string, len(e.Instances))
	for i, in := range e.Instances {
		quoted[i] = fmt.Sprintf("%q", in)
	}
	return fmt.Sprintf("storage: %s has %d instances with measured samples in the window (%s); name one", e.Key, len(e.Instances), strings.Join(quoted, ", "))
}

// pickInstance decides which instance a bucketed read aggregates. A named
// instance is used as asked. Unnamed, the one instance with measured rows in
// the window is used; with none the grid is empty whichever is read; with more
// than one the read is refused rather than blended.
func pickInstance(key string, asked *string, measured []string) (string, error) {
	if asked != nil {
		return *asked, nil
	}
	switch len(measured) {
	case 0:
		return "", nil
	case 1:
		return measured[0], nil
	}
	return "", &AmbiguousInstanceError{Key: key, Instances: measured}
}

// The grid's bounds as SQL over bind parameters and now(), never a CTE's
// column, so the same expressions reach the sample table's WHERE: the planner
// prunes the metric partitions and bounds the (series_id, ts) index scan from
// them, as tsSince does for the raw read. $3 is the bucket width in seconds,
// $4 the grid's span in seconds and $5 the effective retention in days.
// bucketHiSQL is the end of the bucket after now's, so the newest bucket is
// the one still filling, and the grid is aligned to the epoch, so a bucket's
// boundaries do not move between two refreshes. bucketFloorSQL is the later of
// the grid's start and the retention horizon: covered_from. One statement, so
// now() is one instant across the grid, the horizon and the rows.
const (
	bucketHiSQL    = `(date_bin(make_interval(secs => $3), now(), timestamptz 'epoch') + make_interval(secs => $3))`
	bucketLoSQL    = `(` + bucketHiSQL + ` - make_interval(secs => $4))`
	bucketFloorSQL = `greatest(` + bucketLoSQL + `, now() - make_interval(days => $5::int))`
)

// bucketJoinSQL lays the aggregates (the agg CTE, keyed by bucket start b) over
// every bucket of the grid, an empty bucket left null.
const bucketJoinSQL = `
		select g.b, ` + bucketFloorSQL + `, coalesce(agg.n, 0), %s
		from generate_series(` + bucketLoSQL + `, ` + bucketHiSQL + ` - make_interval(secs => $3), make_interval(secs => $3)) g(b)
		left join agg on agg.b = g.b
		order by g.b`

// measuredProvenance is the provenance a bucket aggregates: what was read off
// the device or derived from such readings, never a declared or intended want.
const measuredProvenance = `('observed', 'calculated')`

// bucketType resolves a series' type for a bucketed read: its id (nil for a
// name no type in effect carries, so the read matches nothing) and its
// effective retention, its own retention_days else the lane's.
func (p *PG) bucketType(ctx context.Context, typeTable, key string, laneDays int) (*string, int, error) {
	var id string
	var days *int
	err := p.pool.QueryRow(ctx, fmt.Sprintf(`select t.id::text, t.retention_days from %s_in_effect() t where t.name = $1`, typeTable), key).Scan(&id, &days)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, laneDays, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("storage: resolve %s %q: %w", typeTable, key, err)
	}
	if days != nil {
		return &id, *days, nil
	}
	return &id, laneDays, nil
}

// scanInstances reads a single text column of instance names.
func scanInstances(rows pgx.Rows) ([]string, error) {
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("storage: scan instance: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListMetricBuckets reads one metric series as regular buckets of resolution
// over the last window, oldest first: one instance's measured rows, bounded to
// the hot window the series' type retains for. instance nil asks the read to
// pick the one instance with measured rows in the window, and an
// *AmbiguousInstanceError lists them when there is more than one. The caller
// resolves the owner in scope first, as for ListMetricSeries.
func (p *PG) ListMetricBuckets(ctx context.Context, ownerKind, ownerRef, key string, instance *string, window, resolution time.Duration) (MetricBuckets, error) {
	var out MetricBuckets
	if err := checkBucketing(window, resolution); err != nil {
		return out, err
	}
	if _, err := ownerColumn(ownerKind); err != nil {
		return out, err
	}
	arc, err := p.ownerArcValue(ctx, p.pool, ownerKind, ownerRef)
	if err != nil {
		return out, err
	}
	s, err := p.platformSettings(ctx, p.pool)
	if err != nil {
		return out, err
	}
	typeID, days, err := p.bucketType(ctx, "metric_type", key, s.Retention.MetricDays)
	if err != nil {
		return out, err
	}
	n := bucketCount(window, resolution)
	args := []any{arc, typeID, resolution.Seconds(), (time.Duration(n) * resolution).Seconds(), days, ownerKind}
	var measured []string
	if instance == nil {
		rows, err := p.pool.Query(ctx, `
			select s.instance from metric_series s
			where s.owner_id = $1::uuid and s.owner_kind = $6 and s.metric_type_id = $2::uuid
			  and s.provenance in `+measuredProvenance+`
			  and exists (select 1 from metric m where m.series_id = s.id
			    and m.ts >= `+bucketFloorSQL+` and m.ts < `+bucketHiSQL+`)
			group by s.instance
			order by s.instance`, args...)
		if err != nil {
			return out, fmt.Errorf("storage: list metric bucket instances %s/%s: %w", ownerRef, key, err)
		}
		if measured, err = scanInstances(rows); err != nil {
			return out, err
		}
	}
	inst, err := pickInstance(key, instance, measured)
	if err != nil {
		return out, err
	}
	rows, err := p.pool.Query(ctx, `with
		agg as (
			select date_bin(make_interval(secs => $3), m.ts, timestamptz 'epoch') as b, count(*) as n,
				min(m.value) as mn, max(m.value) as mx, avg(m.value) as av,
				(array_agg(m.value order by m.ts desc, m.id desc))[1] as lst
			from metric_series s
			join metric m on m.series_id = s.id
			where s.owner_id = $1::uuid and s.owner_kind = $6 and s.metric_type_id = $2::uuid
			  and s.instance = $7 and s.provenance in `+measuredProvenance+`
			  and m.ts >= `+bucketFloorSQL+` and m.ts < `+bucketHiSQL+`
			group by 1)`+fmt.Sprintf(bucketJoinSQL, "agg.mn, agg.mx, agg.av, agg.lst"),
		append(args, inst)...)
	if err != nil {
		return out, fmt.Errorf("storage: list metric buckets %s/%s: %w", ownerRef, key, err)
	}
	defer rows.Close()
	out.Instance = inst
	out.Buckets = make([]MetricBucket, 0, n)
	for rows.Next() {
		var b MetricBucket
		if err := rows.Scan(&b.TS, &out.CoveredFrom, &b.Count, &b.Min, &b.Max, &b.Avg, &b.Last); err != nil {
			return out, fmt.Errorf("storage: scan metric bucket: %w", err)
		}
		out.Buckets = append(out.Buckets, b)
	}
	return out, rows.Err()
}

// ListPropertyBuckets mirrors ListMetricBuckets over the property lane: a
// bucket's change count and the value held at its end, over one instance's
// measured changes.
func (p *PG) ListPropertyBuckets(ctx context.Context, ownerKind, ownerRef, key string, instance *string, window, resolution time.Duration) (PropertyBuckets, error) {
	var out PropertyBuckets
	if err := checkBucketing(window, resolution); err != nil {
		return out, err
	}
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return out, err
	}
	arc, err := p.ownerArcValue(ctx, p.pool, ownerKind, ownerRef)
	if err != nil {
		return out, err
	}
	s, err := p.platformSettings(ctx, p.pool)
	if err != nil {
		return out, err
	}
	typeID, days, err := p.bucketType(ctx, "property_type", key, s.Retention.PropertyDays)
	if err != nil {
		return out, err
	}
	n := bucketCount(window, resolution)
	args := []any{arc, typeID, resolution.Seconds(), (time.Duration(n) * resolution).Seconds(), days}
	var measured []string
	if instance == nil {
		rows, err := p.pool.Query(ctx, fmt.Sprintf(`
			select pr.instance from property pr
			where pr.%s = $1::uuid and pr.property_type_id = $2::uuid
			  and pr.provenance in `+measuredProvenance+`
			  and pr.ts >= `+bucketFloorSQL+` and pr.ts < `+bucketHiSQL+`
			group by pr.instance
			order by pr.instance`, col), args...)
		if err != nil {
			return out, fmt.Errorf("storage: list property bucket instances %s/%s: %w", ownerRef, key, err)
		}
		if measured, err = scanInstances(rows); err != nil {
			return out, err
		}
	}
	inst, err := pickInstance(key, instance, measured)
	if err != nil {
		return out, err
	}
	rows, err := p.pool.Query(ctx, fmt.Sprintf(`with
		agg as (
			select date_bin(make_interval(secs => $3), pr.ts, timestamptz 'epoch') as b, count(*) as n,
				(array_agg(pr.value #>> '{}' order by pr.ts desc, pr.id desc))[1] as lst
			from property pr
			where pr.%s = $1::uuid and pr.property_type_id = $2::uuid
			  and pr.instance = $6 and pr.provenance in `+measuredProvenance+`
			  and pr.ts >= `+bucketFloorSQL+` and pr.ts < `+bucketHiSQL+`
			group by 1)`, col)+fmt.Sprintf(bucketJoinSQL, "agg.lst"),
		append(args, inst)...)
	if err != nil {
		return out, fmt.Errorf("storage: list property buckets %s/%s: %w", ownerRef, key, err)
	}
	defer rows.Close()
	out.Instance = inst
	out.Buckets = make([]PropertyBucket, 0, n)
	for rows.Next() {
		var b PropertyBucket
		if err := rows.Scan(&b.TS, &out.CoveredFrom, &b.Count, &b.Last); err != nil {
			return out, fmt.Errorf("storage: scan property bucket: %w", err)
		}
		out.Buckets = append(out.Buckets, b)
	}
	return out, rows.Err()
}
