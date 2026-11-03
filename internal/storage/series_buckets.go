package storage

import (
	"context"
	"fmt"
	"math"
	"time"
)

// The bucketed series reads (#1269): one series over a range at a resolution,
// as regular date_bin buckets rather than the newest N rows. A read that
// truncates to a tail answers a 30-day request with the last 16 hours; a bucket
// grid answers it with 720 hourly buckets whatever the cadence, and a one-sample
// spike survives as its bucket's max. Nothing is stored for it: the buckets are
// computed from the raw rows at read time, inside the hot window. Beyond the
// window the hourly trends will answer (#1276); until they exist nothing is read
// past it, and CoveredFrom says where that edge is.

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

// bucketGridSQL is the grid every bucketed read starts from, as two CTEs. The
// grid is aligned to the epoch, so a bucket's boundaries do not move between
// two refreshes, and ends at the bucket after now's, so the newest bucket is the
// one still filling. It spans $4 seconds of $3-second buckets. covered is the
// later of the grid's start and the retention horizon: the type's own window
// ($2 the type's name, typeTable its catalog), else the lane's ($5 days). One
// statement, so now() is one instant across the grid, the horizon and the rows.
func bucketGridSQL(typeTable string) string {
	return fmt.Sprintf(`
		span as (
			select h.hi, h.hi - make_interval(secs => $4) as lo
			from (select date_bin(make_interval(secs => $3), now(), timestamptz 'epoch') + make_interval(secs => $3) as hi) h),
		cov as (
			select lo, hi, greatest(lo, now() - make_interval(days => coalesce(
				(select t.retention_days from %s_in_effect() t where t.name = $2), $5::int))) as covered
			from span)`, typeTable)
}

const bucketJoinSQL = `
		select g.b, cov.covered, coalesce(agg.n, 0), %s
		from cov
		cross join lateral generate_series(cov.lo, cov.hi - make_interval(secs => $3), make_interval(secs => $3)) g(b)
		left join agg on agg.b = g.b
		order by g.b`

// ListMetricBuckets reads one metric series as regular buckets of resolution
// over the last window, oldest first. The rows are the raw read's (every
// provenance), bounded to the hot window the series' type retains for. The
// caller resolves the owner in scope first, as for ListMetricSeries.
func (p *PG) ListMetricBuckets(ctx context.Context, ownerKind, ownerRef, key string, window, resolution time.Duration) (MetricBuckets, error) {
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
	n := bucketCount(window, resolution)
	rows, err := p.pool.Query(ctx, `with`+bucketGridSQL("metric_type")+`,
		agg as (
			select date_bin(make_interval(secs => $3), x.ts, timestamptz 'epoch') as b, count(*) as n,
				min(x.value) as mn, max(x.value) as mx, avg(x.value) as av,
				(array_agg(x.value order by x.ts desc, x.id desc))[1] as lst
			from metric_series s
			join metric_type_in_effect() mt on mt.id = s.metric_type_id
			cross join lateral (
				select m.ts, m.id, m.value from metric m where m.series_id = s.id
				union all
				select d.ts, d.id, d.value from metric_declared d where d.series_id = s.id) x
			cross join cov
			where s.owner_id = $1::uuid and s.owner_kind = $6 and mt.name = $2
			  and x.ts >= cov.covered and x.ts < cov.hi
			group by 1)`+fmt.Sprintf(bucketJoinSQL, "agg.mn, agg.mx, agg.av, agg.lst"),
		arc, key, resolution.Seconds(), (time.Duration(n) * resolution).Seconds(), s.Retention.MetricDays, ownerKind)
	if err != nil {
		return out, fmt.Errorf("storage: list metric buckets %s/%s: %w", ownerRef, key, err)
	}
	defer rows.Close()
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
// bucket's change count and the value held at its end.
func (p *PG) ListPropertyBuckets(ctx context.Context, ownerKind, ownerRef, key string, window, resolution time.Duration) (PropertyBuckets, error) {
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
	n := bucketCount(window, resolution)
	rows, err := p.pool.Query(ctx, `with`+bucketGridSQL("property_type")+fmt.Sprintf(`,
		agg as (
			select date_bin(make_interval(secs => $3), pr.ts, timestamptz 'epoch') as b, count(*) as n,
				(array_agg(pr.value #>> '{}' order by pr.ts desc, pr.id desc))[1] as lst
			from property pr
			cross join cov
			where pr.%s = $1::uuid
			  and pr.property_type_id = (select id from property_type_in_effect() property_type where name = $2)
			  and pr.ts >= cov.covered and pr.ts < cov.hi
			group by 1)`, col)+fmt.Sprintf(bucketJoinSQL, "agg.lst"),
		arc, key, resolution.Seconds(), (time.Duration(n) * resolution).Seconds(), s.Retention.PropertyDays)
	if err != nil {
		return out, fmt.Errorf("storage: list property buckets %s/%s: %w", ownerRef, key, err)
	}
	defer rows.Close()
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
