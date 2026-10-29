package storage_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The trend tier (#1276, ADR-1276): hourly trends written by compaction once an
// hour is past the late-data horizon, a late sample recomputing its hour, and
// the hot tier never pruned past what the compaction has closed.

// trendRig is a prune fixture with no trend mark yet: a fresh install before
// its first compaction.
func newTrendRig(t *testing.T) *pruneFixture {
	f := newPruneFixture(t)
	mustExec(t, f.conn, `delete from metric_trend_mark`)
	return f
}

// plant lands one sample of prune-rtt on prune-c1 at ts, as provenance.
func (f *pruneFixture) plant(ts time.Time, provenance string, v float64) {
	f.t.Helper()
	f.row("metric", "component", ts, provenance, v)
}

// trendOf reads the trend row of prune-rtt's observed series on prune-c1 for
// the hour starting at hour, as "count min max sum first last", or "" for none.
func (f *pruneFixture) trendOf(hour time.Time) string {
	f.t.Helper()
	var out string
	err := f.conn.QueryRow(f.ctx, `select concat_ws(' ', t.count, t.min, t.max, t.sum, t.first, t.last)
		from metric_trend t join metric_series s on s.id = t.series_id
		where s.provenance = 'observed' and t.hour = $1`, hour).Scan(&out)
	if err != nil {
		return ""
	}
	return out
}

func (f *pruneFixture) compact(maxHours int) storage.TrendCompaction {
	f.t.Helper()
	c, err := f.gw.CompactMetricTrends(f.ctx, maxHours, 14)
	if err != nil {
		f.t.Fatalf("compact: %v", err)
	}
	return c
}

// TestCompactionClosesTheHoursPastTheHorizon: each hour that ended a horizon
// ago folds into one trend row per observed series (count, min, max, sum,
// first, last), a one-sample spike surviving as the hour's max; an hour still
// inside the horizon stays open, a calculated series has no trends, and a
// second pass closes nothing twice.
func TestCompactionClosesTheHoursPastTheHorizon(t *testing.T) {
	f := newTrendRig(t)
	now := time.Now().UTC()
	old := now.Add(-3 * 24 * time.Hour).Truncate(time.Hour)
	for i, v := range []float64{10, 12, 95, 11} { // 95 the spike
		f.plant(old.Add(time.Duration(i+1)*10*time.Minute), "observed", v)
	}
	f.plant(old.Add(5*time.Minute), "calculated", 7)
	recent := now.Add(-time.Hour).Truncate(time.Hour)
	f.plant(recent.Add(time.Minute), "observed", 3)

	c := f.compact(10000)
	target := now.Add(-storage.MetricTrendHorizon).Truncate(time.Hour)
	if c.Hours == 0 || c.Behind || !c.Through.Equal(target) {
		t.Fatalf("compaction %+v, want it through %v and not behind", c, target)
	}
	if got := f.trendOf(old); got != "4 10 95 128 10 11" {
		t.Errorf("the old hour's trend %q, want 4 samples, min 10, max 95 (the spike), sum 128, first 10, last 11", got)
	}
	if got := f.trendOf(recent); got != "" {
		t.Errorf("an hour inside the horizon has a trend %q, want none yet", got)
	}
	var calc int
	if err := f.conn.QueryRow(f.ctx, `select count(*) from metric_trend t join metric_series s on s.id = t.series_id
		where s.provenance <> 'observed'`).Scan(&calc); err != nil || calc != 0 {
		t.Errorf("%d trend rows of calculated series (%v), want none", calc, err)
	}
	if again := f.compact(10000); again.Hours != 0 {
		t.Errorf("a second pass closed %d hours, want none", again.Hours)
	}
}

// TestCompactionCatchesUpInBoundedPasses: a backlog closes maxHours a pass,
// oldest first, saying it is behind until it reaches the horizon.
func TestCompactionCatchesUpInBoundedPasses(t *testing.T) {
	f := newTrendRig(t)
	now := time.Now().UTC()
	start := now.Add(-4 * 24 * time.Hour).Truncate(time.Hour)
	f.plant(start, "observed", 1)
	if err := f.gw.SetMetricTrendMark(f.ctx, start); err != nil {
		t.Fatal(err)
	}
	c := f.compact(24)
	if c.Hours != 24 || !c.Behind || !c.Through.Equal(start.Add(24*time.Hour)) {
		t.Fatalf("first pass %+v, want 24 hours closed and behind", c)
	}
	for c.Behind {
		c = f.compact(24)
	}
	if got := f.trendOf(start); got != "1 1 1 1 1 1" {
		t.Errorf("the first hour's trend %q", got)
	}
}

// TestALateSampleRecomputesItsHour: a sample landing in an hour already closed
// (a spool replayed past the horizon) dirties the hour in its own insert, and
// the next compaction folds the hour again, the late sample included, and
// clears the mark. An on-time sample dirties nothing.
func TestALateSampleRecomputesItsHour(t *testing.T) {
	f := newTrendRig(t)
	now := time.Now().UTC()
	hour := now.Add(-2 * 24 * time.Hour).Truncate(time.Hour)
	f.plant(hour.Add(time.Minute), "observed", 5)
	f.compact(10000)
	if got := f.trendOf(hour); got != "1 5 5 5 5 5" {
		t.Fatalf("closed hour %q", got)
	}
	land := func(ts time.Time, v float64) {
		t.Helper()
		if err := f.gw.InsertMetricSamples(f.ctx, storage.PushWriter(), []storage.MetricSampleWrite{
			{OwnerKind: "component", OwnerID: "prune-c1", Key: "prune-rtt", Value: v, TS: ts}}); err != nil {
			t.Fatal(err)
		}
	}
	land(now, 1) // on time: no mark
	land(hour.Add(30*time.Minute), 50)
	var dirty int
	if err := f.conn.QueryRow(f.ctx, `select count(*) from metric_trend_dirty`).Scan(&dirty); err != nil || dirty != 1 {
		t.Fatalf("%d dirty marks (%v), want the late sample's hour alone", dirty, err)
	}
	// The pushed sample is its own series (source ''), so the hour has two:
	// the planted one's trend is unchanged and the pushed one has its own.
	c := f.compact(10000)
	if c.Recomputed != 1 {
		t.Errorf("recomputed %d, want 1", c.Recomputed)
	}
	var count, max float64
	if err := f.conn.QueryRow(f.ctx, `select sum(count), max(max) from metric_trend where hour = $1`, hour).Scan(&count, &max); err != nil ||
		count != 2 || max != 50 {
		t.Errorf("the hour's trends sum %v samples, max %v (%v); want 2 and the late 50", count, max, err)
	}
	if err := f.conn.QueryRow(f.ctx, `select count(*) from metric_trend_dirty`).Scan(&dirty); err != nil || dirty != 0 {
		t.Errorf("%d dirty marks left (%v), want none", dirty, err)
	}
}

// TestTheHotTierIsNeverPrunedPastTheMark: the metric prune and the month drop
// stop at the hours the compaction has closed, so no hour reaches retention
// before its trend exists; before any compaction they remove nothing. The
// trends outlive the month their samples were dropped with.
func TestTheHotTierIsNeverPrunedPastTheMark(t *testing.T) {
	f := newTrendRig(t)
	now := time.Now().UTC()
	old := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -3, 0).Add(2 * time.Hour)
	f.plant(old, "observed", 9)
	if n := f.prune("metric", now, 100); n != 0 {
		t.Fatalf("before any compaction the prune deleted %d, want 0", n)
	}
	if dropped, err := f.gw.DropMetricPartitions(f.ctx, now); err != nil || len(dropped) != 0 {
		t.Fatalf("before any compaction %v were dropped (%v), want none", dropped, err)
	}
	mark := old.Truncate(time.Hour) // the sample's hour still open
	if err := f.gw.SetMetricTrendMark(f.ctx, mark); err != nil {
		t.Fatal(err)
	}
	if n := f.prune("metric", now, 100); n != 0 {
		t.Fatalf("with its hour open the prune deleted %d, want 0", n)
	}
	f.compact(10000)
	if got := f.trendOf(mark); got != "1 9 9 9 9 9" {
		t.Fatalf("the hour's trend %q", got)
	}
	dropped, err := f.gw.DropMetricPartitions(f.ctx, now.AddDate(0, 0, -14))
	if err != nil || len(dropped) == 0 {
		t.Fatalf("dropped %v (%v), want the old month", dropped, err)
	}
	if got := f.trendOf(mark); got != "1 9 9 9 9 9" {
		t.Errorf("after the month's drop the trend reads %q, want it kept", got)
	}
}

// TestCompactionWaitsForTheWritesInFlight: a sample write that began before
// the compaction announced its hours, and so dirtied nothing, is waited for
// and folded into the hour it lands in.
func TestCompactionWaitsForTheWritesInFlight(t *testing.T) {
	f := newTrendRig(t)
	now := time.Now().UTC()
	hour := now.Add(-3 * 24 * time.Hour).Truncate(time.Hour)
	f.plant(hour, "observed", 1)
	if err := f.gw.SetMetricTrendMark(f.ctx, hour); err != nil {
		t.Fatal(err)
	}
	writer := connectDSN(t, f.dsn)
	tx, err := writer.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(f.ctx, storagetest.WideMetricInsert(`ts, owner_kind, component_id, metric_type_id, value`,
		`values ($1, 'component', (select id from component where name = 'prune-c1'), (select id from metric_type where name = 'prune-rtt'), 2)`),
		hour.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	done := make(chan storage.TrendCompaction, 1)
	go func() {
		c, err := f.gw.CompactMetricTrends(context.Background(), 1, 14)
		if err != nil {
			t.Error(err)
		}
		done <- c
	}()
	select {
	case c := <-done:
		t.Fatalf("the compaction did not wait for the write in flight: %+v", c)
	case <-time.After(500 * time.Millisecond):
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	<-done
	if got := f.trendOf(hour); got != "2 1 2 3 1 2" {
		t.Errorf("the hour's trend %q, want both samples", got)
	}
}

// TestTrendFootprint reports what a series' hourly trends cost a day, after
// VACUUM, and holds it under a bound: a day is 24 rows of eight values beside
// their key.
func TestTrendFootprint(t *testing.T) {
	f := newTrendRig(t)
	const series = 200
	day := time.Now().UTC().Add(-3 * 24 * time.Hour).Truncate(24 * time.Hour)
	if err := f.gw.MakeMetricMonths(f.ctx, day); err != nil {
		t.Fatal(err)
	}
	mustExec(t, f.conn, storagetest.WideMetricInsert(`ts, owner_kind, component_id, metric_type_id, instance, value`,
		`select $1::timestamptz + make_interval(mins => m), 'component', (select id from component where name = 'prune-c1'),
			(select id from metric_type where name = 'prune-rtt'), 'i' || i, m
		from generate_series(1, `+fmt.Sprint(series)+`) i, generate_series(0, 24*60 - 1, 30) m`), day)
	if err := f.gw.SetMetricTrendMark(f.ctx, day); err != nil {
		t.Fatal(err)
	}
	f.compact(24)
	mustExec(t, f.conn, `vacuum metric_trend`)
	var bytes int64
	var rows int
	if err := f.conn.QueryRow(f.ctx, `select pg_relation_size('metric_trend') + pg_indexes_size('metric_trend'), (select count(*) from metric_trend)`).Scan(&bytes, &rows); err != nil {
		t.Fatal(err)
	}
	if rows != series*24 {
		t.Fatalf("%d trend rows, want %d", rows, series*24)
	}
	perSeriesDay := float64(bytes) / series
	t.Logf("trend footprint: %.0f bytes per series per day (%d rows, %d bytes, heap and indexes)", perSeriesDay, rows, bytes)
	if perSeriesDay > 3600 {
		t.Errorf("trends cost %.0f bytes per series per day, over the 3,600-byte bound (measured 3,482 at PG18)", perSeriesDay)
	}
}
