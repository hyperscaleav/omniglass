package storage_test

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Trend-served buckets (#1566, ADR-1566): a whole-hour bucket read of a series
// with hourly trends takes each hour the trend fold has closed from its trend
// row, and every other hour from the series read, and answers what the series
// read alone would. A read over a wide window is clamped to whole hours.

// sameBuckets is nil when got is want bucket for bucket: the same instants,
// counts, minima, maxima and lasts, and averages equal up to the order their
// sums were added in.
func sameBuckets(want, got []storage.MetricBucket) error {
	if len(got) != len(want) {
		return fmt.Errorf("got %d buckets, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if !g.TS.Equal(w.TS) || g.Count != w.Count || !sameValue(g.Min, w.Min, 0) || !sameValue(g.Max, w.Max, 0) ||
			!sameValue(g.Last, w.Last, 0) || !sameValue(g.Avg, w.Avg, 1e-9) {
			return fmt.Errorf("bucket %d is %s, want %s", i, bucketString(g), bucketString(w))
		}
	}
	return nil
}

// sameValue is whether two nullable aggregates agree, within tol relative.
func sameValue(a, b *float64, tol float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b || math.Abs(*a-*b) <= tol*math.Max(math.Abs(*a), math.Abs(*b))
}

func bucketString(b storage.MetricBucket) string {
	f := func(v *float64) string {
		if v == nil {
			return "null"
		}
		return fmt.Sprint(*v)
	}
	return fmt.Sprintf("%s n=%d min=%s max=%s avg=%s last=%s", b.TS.Format(time.RFC3339), b.Count, f(b.Min), f(b.Max), f(b.Avg), f(b.Last))
}

func TestClampBucketResolution(t *testing.T) {
	t.Parallel()
	day := 24 * time.Hour
	for _, tc := range []struct {
		name                         string
		window, resolution, retained time.Duration
		want                         time.Duration
	}{
		{"a day at five minutes is kept", day, 5 * time.Minute, 14 * day, 5 * time.Minute},
		{"the clamp span itself is kept", storage.BucketClampSpan, time.Minute, 14 * day, time.Minute},
		{"past the span a minute is an hour", storage.BucketClampSpan + time.Hour, time.Minute, 14 * day, time.Hour},
		{"30 days at five minutes is an hour", 30 * day, 5 * time.Minute, 14 * day, time.Hour},
		{"30 days at an hour is kept", 30 * day, time.Hour, 14 * day, time.Hour},
		{"30 days at 90 minutes rounds up to two hours", 30 * day, 90 * time.Minute, 14 * day, 2 * time.Hour},
		{"30 days at a day is kept", 30 * day, day, 14 * day, day},
		{"inside the span but past the retention is an hour", 36 * time.Hour, time.Minute, day, time.Hour},
		{"inside the span and the retention is kept", 36 * time.Hour, time.Minute, 2 * day, time.Minute},
		{"no retention known clamps on the span alone", 36 * time.Hour, time.Minute, 0, time.Minute},
	} {
		if got := storage.ClampBucketResolution(tc.window, tc.resolution, tc.retained); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

// grid is the rig's prune-rtt buckets of instance over its 30-day window at
// res, from the trends where they serve, or from the samples alone.
func (r *blockRig) grid(instance string, window, res time.Duration, fromSamples bool) storage.MetricBuckets {
	r.t.Helper()
	read := r.gw.ListMetricBuckets
	if fromSamples {
		read = r.gw.ListMetricBucketsFromSamples
	}
	got, err := read(r.ctx, "component", "prune-c1", "prune-rtt", &instance, window, res)
	if err != nil {
		r.t.Fatal(err)
	}
	return got
}

// sameGrids fails the test where the trend-served grid at res differs from
// the samples' own, and returns the trend-served one.
func (r *blockRig) sameGrids(when, instance string, res time.Duration) storage.MetricBuckets {
	r.t.Helper()
	window := blockLaneDays * 24 * time.Hour
	got := r.grid(instance, window, res, false)
	if err := sameBuckets(r.grid(instance, window, res, true).Buckets, got.Buckets); err != nil {
		r.t.Errorf("%s, the %s buckets from the trends differ from the series read's: %v", when, res, err)
	}
	return got
}

// bucketAt is the bucket of g starting at ts.
func bucketAt(t *testing.T, g storage.MetricBuckets, ts time.Time) storage.MetricBucket {
	t.Helper()
	for _, b := range g.Buckets {
		if b.TS.Equal(ts) {
			return b
		}
	}
	t.Fatalf("no bucket starts at %s", ts)
	return storage.MetricBucket{}
}

// TestTrendServedBucketsEqualTheSeriesRead: an observed series' closed hours
// read from their trends, its open hours (past the compacted mark) from its
// samples, a bucket straddling the mark from both, each hour once. A spike
// survives as its bucket's max, and of two samples at an hour's last instant
// the later landed is its last. Proven served from the trends: the closed
// hours read the same with their raw rows gone, and the open ones ignore a
// trend row that disagrees with their samples.
func TestTrendServedBucketsEqualTheSeriesRead(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	id := r.series("", "observed")
	r.spaced(id, r.day(0), 4300)
	r.spaced(id, r.day(1), 170)
	r.spaced(id, r.day(3), 50)
	r.spaced(id, r.day(4), 400)
	r.plantAt(id, r.day(1).Add(5*time.Hour+30*time.Minute), 1e6)
	edge := r.day(1).Add(7*time.Hour - time.Millisecond)
	r.plantAt(id, edge, 7)
	r.plantAt(id, edge, -7)
	r.foldTrends()
	mark := r.day(4).Add(3 * time.Hour)
	if err := r.gw.SetMetricTrendMark(r.ctx, mark); err != nil {
		t.Fatal(err)
	}
	res := []time.Duration{time.Hour, 6 * time.Hour, 24 * time.Hour}
	before := map[time.Duration]storage.MetricBuckets{}
	for _, d := range res {
		before[d] = r.sameGrids("with the trends folded", "", d)
	}
	hourly := before[time.Hour]
	if b := bucketAt(t, hourly, r.day(1).Add(5*time.Hour)); b.Max == nil || *b.Max != 1e6 {
		t.Errorf("the spike's hour is %s, want max 1e6", bucketString(b))
	}
	if b := bucketAt(t, hourly, r.day(1).Add(6*time.Hour)); b.Last == nil || *b.Last != -7 {
		t.Errorf("the hour ending on two samples at one instant is %s, want last -7", bucketString(b))
	}
	if !hourly.CoveredFrom.Equal(hourly.Buckets[0].TS) || hourly.Resolution != time.Hour {
		t.Errorf("covered from %s at %s, want the first bucket %s at 1h", hourly.CoveredFrom, hourly.Resolution, hourly.Buckets[0].TS)
	}

	// An open hour's trend row (a fold the mark has not passed) is never read.
	mustExec(t, r.conn, `update metric_trend set count = count + 1000, min = min - 1000, max = max + 1000, sum = sum + 1000, last = last + 1000
		where series_id = $1 and hour >= $2`, id, mark)
	for _, d := range res {
		got := r.sameGrids("with the open hours' trend rows wrong", "", d)
		if err := sameBuckets(before[d].Buckets, got.Buckets); err != nil {
			t.Errorf("an open hour read its trend row at %s: %v", d, err)
		}
	}

	// A closed hour is read from its trend: its raw rows gone, it reads the same.
	mustExec(t, r.conn, `delete from metric where series_id = $1 and ts >= $2 and ts < $3`, id, r.day(0), r.day(2))
	for _, d := range res {
		if err := sameBuckets(before[d].Buckets, r.grid("", blockLaneDays*24*time.Hour, d, false).Buckets); err != nil {
			t.Errorf("a closed hour read otherwise without its raw rows at %s: %v", d, err)
		}
	}
}

// TestALateSampleReadsBackBeforeAndAfterItsHourIsRecomputed: a late sample
// into a closed hour dirties it, and the hour reads from the samples, the late
// one among them, until the recompute folds it into the hour's trend; then
// the hour reads from the trend, the late sample still in it.
func TestALateSampleReadsBackBeforeAndAfterItsHourIsRecomputed(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	id := r.series("", "observed")
	r.spaced(id, r.day(0), 4300)
	r.spaced(id, r.day(1), 170)
	r.spaced(id, r.day(2), 170)
	r.foldTrends()
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	hour := r.day(1).Add(5 * time.Hour)
	r.ingest(sampleOf("", hour.Add(time.Minute+time.Millisecond), 1e6))
	if n := r.count(`select count(*) from metric_trend_dirty where series_id = $1 and hour = $2`, id, hour); n != 1 {
		t.Fatalf("the late sample dirtied %d hours, want its own", n)
	}
	for _, when := range []string{"before the recompute", "after the recompute"} {
		if when == "after the recompute" {
			if _, err := r.gw.CompactMetricTrends(r.ctx, 1, blockLaneDays); err != nil {
				t.Fatal(err)
			}
			if n := r.count(`select count(*) from metric_trend_dirty`); n != 0 {
				t.Fatalf("the recompute left %d dirty hours", n)
			}
			if got, want := r.trendRow(id, hour), r.rawTrendRow(id, hour); got != want {
				t.Fatalf("the recomputed trend is %q, want %q", got, want)
			}
		}
		for _, d := range []time.Duration{time.Hour, 24 * time.Hour} {
			g := r.sameGrids(when, "", d)
			if b := bucketAt(t, g, hour.Truncate(d)); b.Max == nil || *b.Max != 1e6 {
				t.Errorf("%s, the late sample's %s bucket is %s, want max 1e6", when, d, bucketString(b))
			}
		}
	}
	// After the recompute the hour is served from its trend: the late
	// sample's raw row gone, it still reads.
	mustExec(t, r.conn, `delete from metric where series_id = $1 and ts >= $2 and ts < $3`, id, hour, hour.Add(time.Hour))
	mustExec(t, r.conn, `delete from metric_block where series_id = $1`, id)
	if b := bucketAt(t, r.grid("", blockLaneDays*24*time.Hour, time.Hour, false), hour); b.Max == nil || *b.Max != 1e6 {
		t.Errorf("the recomputed hour is %s without its samples, want its trend's max 1e6", bucketString(b))
	}
}

// TestBucketsWithoutTrendsReadTheSamples: a calculated series keeps no
// trends, so a trend row under it (none is ever folded) is never read; nor is
// an observed series' trend at a resolution finer than an hour, nor one not
// a whole number of hours.
func TestBucketsWithoutTrendsReadTheSamples(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	calc := r.series("calc", "calculated")
	r.spacedAs(calc, r.day(1), 300, 0, 300, avgV1)
	r.foldTrends()
	if n := r.count(`select count(*) from metric_trend where series_id = $1`, calc); n != 0 {
		t.Fatalf("the trend fold folded %d hours of a calculated series", n)
	}
	mustExec(t, r.conn, `insert into metric_trend (series_id, hour, count, min, max, sum, first, last)
		select $1, h, 1000, -1000, 1000, 0, 0, 0 from generate_series($2::timestamptz, $3::timestamptz, interval '1 hour') h`,
		calc, r.day(0), r.day(3))
	for _, d := range []time.Duration{time.Hour, 24 * time.Hour} {
		g := r.sameGrids("a calculated series", "calc", d)
		if b := bucketAt(t, g, r.day(1)); b.Count == 0 || b.Count >= 1000 {
			t.Errorf("the calculated series' %s bucket is %s, want its samples", d, bucketString(b))
		}
		if want := r.grid("calc", blockLaneDays*24*time.Hour, d, true).CoveredFrom; !g.CoveredFrom.Equal(want) {
			t.Errorf("a calculated series covers from %s, want its samples' %s", g.CoveredFrom, want)
		}
	}

	// Inside the clamp span an observed series reads at five minutes, and at
	// 90 minutes, both from its samples, its trends wrong and ignored; at two
	// hours the same trends are read.
	obs := r.series("obs", "observed")
	yesterday := r.now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	r.spaced(obs, yesterday, 2880)
	r.foldTrends()
	if err := r.gw.FoldMetricTrendHours(r.ctx, yesterday, yesterday.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	mustExec(t, r.conn, `update metric_trend set count = count + 1000 where series_id = $1`, obs)
	for _, d := range []time.Duration{5 * time.Minute, 90 * time.Minute} {
		got, want := r.grid("obs", 48*time.Hour, d, false), r.grid("obs", 48*time.Hour, d, true)
		if err := sameBuckets(want.Buckets, got.Buckets); err != nil {
			t.Errorf("at %s the buckets read the trends: %v", d, err)
		}
		if got.Resolution != d {
			t.Errorf("inside the clamp span %s was applied as %s", d, got.Resolution)
		}
	}
	two := r.grid("obs", 48*time.Hour, 2*time.Hour, false)
	if b := bucketAt(t, two, yesterday); b.Count < 1000 {
		t.Errorf("at two hours the bucket is %s, want its (wrong) trends read", bucketString(b))
	}
}

// TestAWideBucketReadIsClampedToWholeHours: 30 days at five minutes, or a
// window past the series' retention, is read at an hour, and says so.
func TestAWideBucketReadIsClampedToWholeHours(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	id := r.series("", "observed")
	r.spaced(id, r.day(1), 170)
	if g := r.grid("", 30*24*time.Hour, 5*time.Minute, false); g.Resolution != time.Hour || len(g.Buckets) != 720 {
		t.Errorf("30 days at five minutes read %d buckets at %s, want 720 at 1h", len(g.Buckets), g.Resolution)
	}
	if g := r.grid("", 24*time.Hour, 5*time.Minute, false); g.Resolution != 5*time.Minute || len(g.Buckets) != 288 {
		t.Errorf("a day at five minutes read %d buckets at %s, want 288 at 5m", len(g.Buckets), g.Resolution)
	}
	mustExec(t, r.conn, `update metric_type set retention_days = 1 where name = 'prune-rtt'`)
	if g := r.grid("", 36*time.Hour, 5*time.Minute, false); g.Resolution != time.Hour || len(g.Buckets) != 36 {
		t.Errorf("36 hours of a series kept a day read %d buckets at %s, want 36 at 1h", len(g.Buckets), g.Resolution)
	}
}

// TestAnInstanceWithACalculatedSeriesBesideReadsTheSamples: an observed
// series whose instance also has a calculated one takes no hour from its
// trends, so a bucket both share picks its last by when each sample fell, as
// the series read does: here the calculated series' last, after the observed
// one's but inside the same hour.
func TestAnInstanceWithACalculatedSeriesBesideReadsTheSamples(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	obs, calc := r.series("mix", "observed"), r.series("mix", "calculated")
	hour := r.day(1).Add(5 * time.Hour)
	r.plantAt(obs, hour.Add(10*time.Minute), 1)
	r.plantAt(obs, hour.Add(20*time.Minute), 2)
	mustExec(t, r.conn, `insert into metric (series_id, ts, value, source_rule, source_rule_version, source_rule_id)
		values ($1, $2, 3, $3, $4, $5::uuid)`, calc, hour.Add(30*time.Minute), avgV1.Rule, avgV1.Version, avgV1.RuleID)
	r.foldTrends()
	if n := r.count(`select count(*) from metric_trend where series_id = $1`, obs); n != 1 {
		t.Fatalf("the observed series folded %d trend hours, want 1", n)
	}
	mustExec(t, r.conn, `update metric_trend set count = count + 1000 where series_id = $1`, obs)
	g := r.sameGrids("an instance with a calculated series beside", "mix", time.Hour)
	if b := bucketAt(t, g, hour); b.Count != 3 || b.Last == nil || *b.Last != 3 {
		t.Errorf("the shared hour is %s, want its 3 samples and the calculated series' last, 3", bucketString(b))
	}
}
