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
// computed at read time. A metric read chooses its tier (#1566, ADR-1566): a
// series with trends, read at whole hours, takes each hour the trend fold has
// closed from its metric_trend row, so a 30-day read sums 720 rows rather than
// unnesting a month of blocks, and reads back as far as the trends are kept;
// every other hour, and every bucket of a series without trends, comes from
// the series read (its raw rows or the blocks a folded day is read from),
// inside the hot window. CoveredFrom says where the read's edge is.
//
// A bucket aggregates one logical series (#1485): one instance of the type on
// the owner, measured rows only (observed and calculated). Two instances are
// two series, and a declared or intended row is a want, not a reading, so
// neither may move a bucket's min, max, avg or last. Rows of one instance from
// different sources or collector nodes stay one series: a node move or a task
// reassignment does not split what was measured.

// BucketClampSpan is the longest window a metric read may bucket finer than an
// hour (ADR-1566): past it the resolution is raised to whole hours, so a wide
// read is served from the hourly trends.
const BucketClampSpan = 48 * time.Hour

// ClampBucketResolution is the resolution a metric read of window applies to
// a requested one (ADR-1566): unchanged for a window of at most BucketClampSpan
// that stays inside retained (the series' hot window; zero when not yet
// known), else rounded up to a whole number of hours, at least one. Pure, so
// the rule tests without a database.
func ClampBucketResolution(window, resolution, retained time.Duration) time.Duration {
	if window <= BucketClampSpan && (retained <= 0 || window <= retained) {
		return resolution
	}
	if resolution <= time.Hour {
		return time.Hour
	}
	return (resolution + time.Hour - 1) / time.Hour * time.Hour
}

// MetricBucket is one regular bucket of a metric series. An empty bucket has
// Count 0 and every aggregate nil.
type MetricBucket struct {
	TS                  time.Time
	Count               int64
	Min, Max, Avg, Last *float64
}

// MetricBuckets is a metric series' bucket grid, oldest first, the bucket
// width it applied (ClampBucketResolution), and the earliest instant the read
// could have data for: a bucket before it is empty because retention removed
// its rows, not because collection stopped.
type MetricBuckets struct {
	// Instance is the one instance the buckets aggregate (#1485).
	Instance    string
	Resolution  time.Duration
	CoveredFrom time.Time
	Buckets     []MetricBucket
}

// PropertyBucket is one regular bucket of a property series: how many changes
// it holds and the value its own changes end on. Min, max and avg mean nothing
// to a string. Last is only the bucket's own rows (nil when it has none); Held
// is the value in force at the bucket's end, Last else what was held before it
// (#1488), so a step chart reads a flat stretch off Held.
type PropertyBucket struct {
	TS    time.Time
	Count int64
	Last  *string
	Held  *string
}

// PropertyBuckets mirrors MetricBuckets over the property lane. CarriedIn is
// the value in force when the grid opens (#1488): the newest measured row of
// the instance before the first bucket, no older than the coverage floor, nil
// when none. It is what the leading empty buckets hold.
type PropertyBuckets struct {
	Instance    string
	CoveredFrom time.Time
	CarriedIn   *string
	Buckets     []PropertyBucket
}

// holdAcross fills each bucket's Held: its own Last when it has changes, else
// the value held at the end of the bucket before it, the first falling back to
// carriedIn. Pure, so the carry rule tests without a database.
func holdAcross(buckets []PropertyBucket, carriedIn *string) {
	held := carriedIn
	for i := range buckets {
		if buckets[i].Last != nil {
			held = buckets[i].Last
		}
		buckets[i].Held = held
	}
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
// every bucket of the grid, an empty bucket left null, each row carrying the
// read's coverage floor (the first verb, bucketFloorSQL or a wider one).
const bucketJoinSQL = `
		select g.b, %s, coalesce(agg.n, 0), %s
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

// trendedSeries is the condition that series a has hourly trends: the trend
// fold closes its hours (foldTrendHour) and a bucketed read may take them from
// metric_trend. Observed series only; a calculated series is sparse already
// (ADR-1534). The per-type trend setting (#1531) narrows it here.
func trendedSeries(a string) string {
	return a + `.provenance = 'observed'`
}

// The trend-served stretch of a bucketed metric read (#1566), as SQL over the
// grid's bind parameters: $8 is the trend window in days and $9 whether the
// read may use trends at all (a whole-hour resolution). trendLoSQL is the
// first whole hour the trends still hold inside the grid, or the grid's end
// when the read uses none; pure, so it bounds the series read below it as the
// grid's own bounds do. trendHiSQL is where a series' (alias s) trend-served
// hours end: at the compacted mark, the grid's end, or the first hour inside
// the hot window a late sample has dirtied and the recompute has not yet
// folded again, whichever is first, never before trendLoSQL. From there the
// series read answers, so a late sample is counted whether or not its hour has
// been recomputed: the dirty mark is written in the sample's own transaction
// and cleared in the recompute's, and the read is one statement. A series
// whose instance also has a series without trends (a calculated one beside
// the observed) takes no hour from its trends: a trend row does not say when
// in its hour its last sample fell, so a bucket both share could not pick its
// last as the series read does.
const trendLoSQL = `(case when $9 then least(` + bucketHiSQL + `, date_bin('1 hour', greatest(` + bucketLoSQL + `,
		now() - make_interval(days => $8::int)) - interval '1 microsecond', ` + trendEpoch + `) + interval '1 hour')
		else ` + bucketHiSQL + ` end)`

// untrendedSibling is the condition that series s's instance also has a
// measured series without trends.
var untrendedSibling = `exists (select 1 from metric_series u where u.owner_id = s.owner_id and u.owner_kind = s.owner_kind
	and u.metric_type_id = s.metric_type_id and u.instance = s.instance
	and u.provenance in ` + measuredProvenance + ` and not ` + trendedSeries("u") + `)`

var trendHiSQL = `greatest(` + trendLoSQL + `, least(` + bucketHiSQL + `,
		case when ` + untrendedSibling + ` then '-infinity'::timestamptz end,
		coalesce((select tm.compacted_through from metric_trend_mark tm), '-infinity'),
		coalesce((select min(td.hour) from metric_trend_dirty td
			where td.series_id = s.id and td.hour >= greatest(` + trendLoSQL + `, ` + bucketFloorSQL + `)), 'infinity')))`

// metricBucketSeries is the series a bucketed metric read aggregates (alias
// s): the type on the owner, measured rows only.
const metricBucketSeries = `s.owner_id = $1::uuid and s.owner_kind = $6 and s.metric_type_id = $2::uuid
	and s.provenance in ` + measuredProvenance

// ListMetricBuckets reads one metric series as regular buckets over the last
// window, oldest first: one instance's measured rows, at the resolution
// ClampBucketResolution applies, which it returns. At whole hours a series
// with trends takes every hour the trend fold has closed from metric_trend,
// back to the trend window's edge; every other hour, and a series without
// trends, is read through the series read (metricReadSQL) inside the hot
// window the series' type retains for, so a folded day buckets as its raw rows
// did. Each hour is counted once, from one of them. A bucket's last is its
// newest sample, of the newest series of the instance when two share its
// instant, then of the newest lineage and the last landed; an instance with a
// series without trends beside one with them reads every hour through the
// series read (trendHiSQL), so a trend row's last never has to be placed
// against another series' samples. instance nil asks the read
// to pick the one instance with measured rows in the window, and an
// *AmbiguousInstanceError lists them when there is more than one. The caller
// resolves the owner in scope first, as for ListMetricSeries.
func (p *PG) ListMetricBuckets(ctx context.Context, ownerKind, ownerRef, key string, instance *string, window, resolution time.Duration) (MetricBuckets, error) {
	return p.listMetricBuckets(ctx, ownerKind, ownerRef, key, instance, window, resolution, true)
}

// listMetricBuckets is ListMetricBuckets, with trends only when trends is
// set: unset, every bucket comes from the series read, the answer the trends
// must equal.
func (p *PG) listMetricBuckets(ctx context.Context, ownerKind, ownerRef, key string, instance *string, window, resolution time.Duration, trends bool) (MetricBuckets, error) {
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
	resolution = ClampBucketResolution(window, resolution, time.Duration(days)*24*time.Hour)
	useTrends := trends && resolution%time.Hour == 0
	n := bucketCount(window, resolution)
	args := []any{arc, typeID, resolution.Seconds(), (time.Duration(n) * resolution).Seconds(), days, ownerKind,
		s.Retention.MetricDays, s.Retention.TrendDays, useTrends}
	read := func(from, to string) string { return metricReadSQL("s.id", from, to, "$7") }
	var measured []string
	if instance == nil {
		rows, err := p.pool.Query(ctx, `
			select s.instance from metric_series s
			where `+metricBucketSeries+`
			  and (exists (`+read(bucketFloorSQL, bucketHiSQL)+`)
				or `+trendedSeries("s")+` and not `+untrendedSibling+` and exists (select 1 from metric_trend t
					where t.series_id = s.id and t.hour >= `+trendLoSQL+` and t.hour < `+bucketHiSQL+`))
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
	// pieces is every sample, or trend hour, the buckets aggregate, each with
	// its bucket b, the instant it orders by for last (k: a sample's ts, a trend
	// hour's last instant), and its part of count, min, max, sum and last. A
	// series without trends is read whole; one with trends is read below the
	// trends and after them, and takes its trend rows between.
	const sample = `date_bin(make_interval(secs => $3), m.ts, timestamptz 'epoch') as b, m.ts as k, s.id as sid,
				m.source_rule, m.source_rule_version, m.source_rule_id, m.ord,
				1::bigint as n, m.value as mn, m.value as mx, m.value as sm, m.value as lst`
	of := metricBucketSeries + ` and s.instance = $10`
	covered := `(case when $9 and exists (select 1 from metric_series s where ` + of + ` and ` + trendedSeries("s") + `
			and not ` + untrendedSibling + `)
		then least(` + bucketFloorSQL + `, ` + trendLoSQL + `) else ` + bucketFloorSQL + ` end)`
	rows, err := p.pool.Query(ctx, `with
		pieces as (
			select `+sample+`
			from metric_series s cross join lateral (`+read(bucketFloorSQL, bucketHiSQL)+`) m
			where `+of+` and not `+trendedSeries("s")+`
			union all
			select `+sample+`
			from metric_series s cross join lateral (`+read(bucketFloorSQL, trendLoSQL)+`) m
			where `+of+` and `+trendedSeries("s")+`
			union all
			select date_bin(make_interval(secs => $3), t.hour, timestamptz 'epoch'), t.hour + interval '1 hour' - interval '1 microsecond', s.id,
				null::text, null::bigint, null::uuid, null::bigint,
				t.count::bigint, t.min, t.max, t.sum, t.last
			from metric_series s cross join lateral (select `+trendHiSQL+` as t_hi) th
			join metric_trend t on t.series_id = s.id and t.hour >= `+trendLoSQL+` and t.hour < th.t_hi
			where `+of+` and `+trendedSeries("s")+`
			union all
			select `+sample+`
			from metric_series s cross join lateral (select greatest(`+trendHiSQL+`, `+bucketFloorSQL+`) as r_from) th
			cross join lateral (`+read("th.r_from", bucketHiSQL)+`) m
			where `+of+` and `+trendedSeries("s")+`),
		agg as (
			select p.b, sum(p.n)::bigint as n, min(p.mn) as mn, max(p.mx) as mx, sum(p.sm) / sum(p.n)::float8 as av,
				(array_agg(p.lst order by p.k desc, p.sid desc, `+metricLineageOrder("p", true)+`))[1] as lst
			from pieces p
			group by p.b)`+fmt.Sprintf(bucketJoinSQL, covered, "agg.mn, agg.mx, agg.av, agg.lst"),
		append(args, inst)...)
	if err != nil {
		return out, fmt.Errorf("storage: list metric buckets %s/%s: %w", ownerRef, key, err)
	}
	defer rows.Close()
	out.Instance = inst
	out.Resolution = resolution
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
// bucket's change count, the value its own changes end on and the value held at
// its end, over one instance's measured changes. It also returns the value in
// force when the grid opens (CarriedIn, #1488), which the leading empty buckets
// hold. With no instance named and none measured in the window, the one
// instance that holds a value from before it is the series.
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
	var carriedIn *string
	carry := true
	if instance == nil && len(measured) == 0 {
		held, err := p.measuredPropertyInstances(ctx, col, bucketRetentionSQL, bucketLoSQL, args)
		if err != nil {
			return out, err
		}
		inst, carry = oneInstance(held)
	}
	if carry {
		if carriedIn, err = p.propertyCarriedIn(ctx, col, bucketLoSQL, bucketRetentionSQL, "$6", append(args, inst)); err != nil {
			return out, err
		}
	}
	rows, err := p.pool.Query(ctx, fmt.Sprintf(`with
		agg as (
			select date_bin(make_interval(secs => $3), pr.ts, timestamptz 'epoch') as b, count(*) as n,
				(array_agg(pr.value #>> '{}' order by pr.ts desc, pr.id desc))[1] as lst
			from property pr
			where pr.%s = $1::uuid and pr.property_type_id = $2::uuid
			  and pr.instance = $6 and pr.provenance in `+measuredProvenance+`
			  and pr.ts >= `+bucketFloorSQL+` and pr.ts < `+bucketHiSQL+`
			group by 1)`, col)+fmt.Sprintf(bucketJoinSQL, bucketFloorSQL, "agg.lst"),
		append(args, inst)...)
	if err != nil {
		return out, fmt.Errorf("storage: list property buckets %s/%s: %w", ownerRef, key, err)
	}
	defer rows.Close()
	out.Instance = inst
	out.CarriedIn = carriedIn
	out.Buckets = make([]PropertyBucket, 0, n)
	for rows.Next() {
		var b PropertyBucket
		if err := rows.Scan(&b.TS, &out.CoveredFrom, &b.Count, &b.Last); err != nil {
			return out, fmt.Errorf("storage: scan property bucket: %w", err)
		}
		out.Buckets = append(out.Buckets, b)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	holdAcross(out.Buckets, out.CarriedIn)
	return out, nil
}

// bucketRetentionSQL is the retention horizon of a bucketed property read ($5 is
// the effective retention in days): the oldest instant a carried-in value may
// come from, the coverage floor below the grid.
const bucketRetentionSQL = `(now() - make_interval(days => $5::int))`

// measuredPropertyInstances lists the instances with a measured row in
// [fromSQL, toSQL), by name. The bounds are SQL over the caller's bind
// parameters, as the bucket grid's are.
func (p *PG) measuredPropertyInstances(ctx context.Context, col, fromSQL, toSQL string, args []any) ([]string, error) {
	rows, err := p.pool.Query(ctx, fmt.Sprintf(`
		select pr.instance from property pr
		where pr.%s = $1::uuid and pr.property_type_id = $2::uuid
		  and pr.provenance in `+measuredProvenance+`
		  and pr.ts >= `+fromSQL+` and pr.ts < `+toSQL+`
		group by pr.instance
		order by pr.instance`, col), args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list held property instances: %w", err)
	}
	return scanInstances(rows)
}

// oneInstance is the instance when exactly one holds the series, and false
// otherwise: a carried-in value is never guessed between two series.
func oneInstance(instances []string) (string, bool) {
	if len(instances) == 1 {
		return instances[0], true
	}
	return "", false
}

// propertyCarriedIn is the value in force when a window opens at loSQL (#1488):
// the newest measured row of one instance before it, no older than horizonSQL
// (the retention floor), nil when there is none. instParam names the bind
// parameter holding the instance; args carries $1 (the owner arc), $2 (the type)
// and whatever the bounds read.
func (p *PG) propertyCarriedIn(ctx context.Context, col, loSQL, horizonSQL, instParam string, args []any) (*string, error) {
	var v *string
	err := p.pool.QueryRow(ctx, fmt.Sprintf(`
		select pr.value #>> '{}' from property pr
		where pr.%s = $1::uuid and pr.property_type_id = $2::uuid
		  and pr.instance = `+instParam+` and pr.provenance in `+measuredProvenance+`
		  and pr.ts >= `+horizonSQL+` and pr.ts < `+loSQL+`
		order by pr.ts desc, pr.id desc
		limit 1`, col), args...).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: read property carried-in: %w", err)
	}
	return v, nil
}

// PropertyCarriedIn is the raw property read's carried-in value (#1488): the
// value in force when the window opens, the newest measured row of the instance
// before now minus window, no older than the type's retention. instance nil
// asks for the one instance with a measured row in the window, else the one
// that holds a value from before it; with more than one the answer is nil, never
// a pick between two series. A window of zero or less is unbounded and has no
// opening.
func (p *PG) PropertyCarriedIn(ctx context.Context, ownerKind, ownerRef, key string, instance *string, window time.Duration) (*string, error) {
	if window <= 0 {
		return nil, nil
	}
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	arc, err := p.ownerArcValue(ctx, p.pool, ownerKind, ownerRef)
	if err != nil {
		return nil, err
	}
	s, err := p.platformSettings(ctx, p.pool)
	if err != nil {
		return nil, err
	}
	typeID, days, err := p.bucketType(ctx, "property_type", key, s.Retention.PropertyDays)
	if err != nil {
		return nil, err
	}
	const (
		lo      = `(now() - make_interval(secs => $3))`
		horizon = `(now() - make_interval(days => $4::int))`
	)
	args := []any{arc, typeID, window.Seconds(), days}
	var inst string
	if instance != nil {
		inst = *instance
	} else {
		var ok bool
		inWindow, err := p.measuredPropertyInstances(ctx, col, lo, `timestamptz 'infinity'`, args[:3])
		if err != nil {
			return nil, err
		}
		if len(inWindow) == 0 {
			if inWindow, err = p.measuredPropertyInstances(ctx, col, horizon, lo, args); err != nil {
				return nil, err
			}
		}
		if inst, ok = oneInstance(inWindow); !ok {
			return nil, nil
		}
	}
	return p.propertyCarriedIn(ctx, col, lo, horizon, "$5", append(args, inst))
}
