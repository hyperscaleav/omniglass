package storage_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
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

// beginTrendWriter opens a transaction that has done what a metric insert
// does first (#1276): taken the trend lock shared and read the announced
// mark, through metric_trend_announced(). It has written nothing, so it holds
// no transaction id yet.
func (f *pruneFixture) beginTrendWriter() pgx.Tx {
	f.t.Helper()
	writer := connectDSN(f.t, f.dsn)
	tx, err := writer.Begin(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(f.ctx, `select metric_trend_announced()`); err != nil {
		f.t.Fatal(err)
	}
	return tx
}

// writeSample lands one observed sample of prune-rtt on prune-c1 at ts in tx.
func (f *pruneFixture) writeSample(tx pgx.Tx, ts time.Time, v float64) {
	f.t.Helper()
	if _, err := tx.Exec(f.ctx, storagetest.WideMetricInsert(`ts, owner_kind, component_id, metric_type_id, value`,
		`values ($1, 'component', (select id from component where name = 'prune-c1'), (select id from metric_type where name = 'prune-rtt'), $2)`),
		ts, v); err != nil {
		f.t.Fatal(err)
	}
}

// marks reads the compaction's announced and compacted marks.
func (f *pruneFixture) marks() (announced, compacted time.Time) {
	f.t.Helper()
	announced, compacted, ok, err := f.gw.MetricTrendMarks(f.ctx)
	if err != nil || !ok {
		f.t.Fatalf("read the trend marks: ok %v, %v", ok, err)
	}
	return announced, compacted
}

// TestCompactionWaitsForTheWritesInFlight: a metric insert that read the
// announced mark before the compaction moved it, and so dirtied nothing, is
// waited for and folded into the hour it lands in, even while it has written
// nothing yet (no transaction id to wait on).
func TestCompactionWaitsForTheWritesInFlight(t *testing.T) {
	f := newTrendRig(t)
	now := time.Now().UTC()
	hour := now.Add(-3 * 24 * time.Hour).Truncate(time.Hour)
	f.plant(hour, "observed", 1)
	if err := f.gw.SetMetricTrendMark(f.ctx, hour); err != nil {
		t.Fatal(err)
	}
	tx := f.beginTrendWriter()
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
		t.Fatalf("the compaction did not wait for the insert in flight: %+v", c)
	case <-time.After(500 * time.Millisecond):
	}
	f.writeSample(tx, hour.Add(time.Minute), 2)
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	<-done
	if got := f.trendOf(hour); got != "2 1 2 3 1 2" {
		t.Errorf("the hour's trend %q, want both samples", got)
	}
}

// TestACompactionCancelledMidDrainSkipsNoHour: a pass cancelled while it waits
// for an insert in flight has announced its hours but folded none, so the
// compacted mark stays, no sample of the hours is pruned, and the next pass
// folds them, the insert's sample included.
func TestACompactionCancelledMidDrainSkipsNoHour(t *testing.T) {
	f := newTrendRig(t)
	now := time.Now().UTC()
	hour := now.Add(-3 * 24 * time.Hour).Truncate(time.Hour)
	f.plant(hour, "observed", 1)
	if err := f.gw.SetMetricTrendMark(f.ctx, hour); err != nil {
		t.Fatal(err)
	}
	tx := f.beginTrendWriter()
	f.writeSample(tx, hour.Add(time.Minute), 2)
	ctx, cancel := context.WithTimeout(f.ctx, 500*time.Millisecond)
	defer cancel()
	if c, err := f.gw.CompactMetricTrends(ctx, 1, 14); err == nil {
		t.Fatalf("a pass cancelled mid-drain returned %+v and no error", c)
	}
	announced, compacted := f.marks()
	if !announced.Equal(hour.Add(time.Hour)) || !compacted.Equal(hour) {
		t.Errorf("after the cancel the marks are announced %v and compacted %v, want %v and %v (announced, not folded)",
			announced, compacted, hour.Add(time.Hour), hour)
	}
	if n := f.prune("metric", now, 100); n != 0 {
		t.Errorf("the prune deleted %d samples of the unfolded hour, want 0", n)
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if c := f.compact(1); c.Hours != 1 {
		t.Fatalf("the next pass closed %d hours, want the one left open", c.Hours)
	}
	if got := f.trendOf(hour); got != "2 1 2 3 1 2" {
		t.Errorf("the hour's trend %q, want both samples", got)
	}
}

// TestACompactionCancelledMidFoldSkipsNoHour: a pass cancelled while it folds
// keeps every hour it folded, and the compacted mark stops at the first hour
// it did not, so the prune removes only the folded hours' samples and the
// next pass folds the rest.
func TestACompactionCancelledMidFoldSkipsNoHour(t *testing.T) {
	f := newTrendRig(t)
	now := time.Now().UTC()
	start := now.Add(-4 * 24 * time.Hour).Truncate(time.Hour)
	for i := range 5 {
		f.plant(start.Add(time.Duration(i)*time.Hour+time.Minute), "observed", float64(i+1))
	}
	if err := f.gw.SetMetricTrendMark(f.ctx, start); err != nil {
		t.Fatal(err)
	}
	// The third hour's fold waits on another transaction's uncommitted trend
	// row for the same (series, hour), which is where the pass is cancelled.
	blocker, err := connectDSN(t, f.dsn).Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	stuck := start.Add(2 * time.Hour)
	if _, err := blocker.Exec(f.ctx, `insert into metric_trend (series_id, hour, count, min, max, sum, first, last)
		select id, $1, 1, 0, 0, 0, 0, 0 from metric_series where provenance = 'observed'`, stuck); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	type result struct {
		c   storage.TrendCompaction
		err error
	}
	done := make(chan result, 1)
	go func() {
		c, err := f.gw.CompactMetricTrends(ctx, 5, 14)
		done <- result{c, err}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, compacted := f.marks(); compacted.Equal(stuck) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the pass never folded the two hours before the stuck one")
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	cancel()
	r := <-done
	if r.err == nil || r.c.Hours != 2 {
		t.Fatalf("a pass cancelled mid-fold returned %+v, %v; want 2 hours closed and an error", r.c, r.err)
	}
	announced, compacted := f.marks()
	if !compacted.Equal(stuck) || !announced.Equal(start.Add(5*time.Hour)) {
		t.Errorf("after the cancel the marks are announced %v and compacted %v, want %v and %v", announced, compacted, start.Add(5*time.Hour), stuck)
	}
	if got := f.trendOf(start.Add(time.Hour)); got != "1 2 2 2 2 2" {
		t.Errorf("a folded hour's trend %q, want it kept", got)
	}
	if n := f.prune("metric", now, 100); n != 2 {
		t.Errorf("the prune deleted %d samples, want the 2 of the folded hours", n)
	}
	if err := blocker.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	if c := f.compact(3); c.Hours != 3 || !c.Through.Equal(start.Add(5*time.Hour)) {
		t.Errorf("the next pass %+v, want the 3 hours left open closed", c)
	}
	for i := 2; i < 5; i++ {
		v := strconv.Itoa(i + 1)
		if got, want := f.trendOf(start.Add(time.Duration(i)*time.Hour)), "1 "+strings.Repeat(v+" ", 4)+v; got != want {
			t.Errorf("hour %d's trend %q, want %q", i, got, want)
		}
	}
}

// TestAnIdleTransactionElsewhereDoesNotStallCompaction: a transaction left
// idle with a transaction id, in another database or in this one on another
// table, holds no trend lock, so the compaction and the metric prune it
// unlocks go on without it.
func TestAnIdleTransactionElsewhereDoesNotStallCompaction(t *testing.T) {
	f := newTrendRig(t)
	now := time.Now().UTC()
	hour := now.Add(-3 * 24 * time.Hour).Truncate(time.Hour)
	f.plant(hour, "observed", 1)
	if err := f.gw.SetMetricTrendMark(f.ctx, hour); err != nil {
		t.Fatal(err)
	}
	other := "og_trend_elsewhere_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	mustExec(t, f.conn, `create database `+other)
	t.Cleanup(func() { _, _ = f.conn.Exec(context.Background(), `drop database if exists `+other+` with (force)`) })
	cfg, err := pgx.ParseConfig(f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database = other
	elsewhere, err := pgx.ConnectConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = elsewhere.Close(context.Background()) })
	here := connectDSN(t, f.dsn)
	for _, conn := range []*pgx.Conn{elsewhere, here} {
		tx, err := conn.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
		if _, err := tx.Exec(f.ctx, `select pg_current_xact_id()`); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	c, err := f.gw.CompactMetricTrends(ctx, 1, 14)
	if err != nil || c.Hours != 1 {
		t.Fatalf("with idle transactions elsewhere the compaction returned %+v, %v; want the hour closed", c, err)
	}
	if n, err := f.gw.PruneSamples(ctx, "metric", now, 100); err != nil || n != 1 {
		t.Errorf("the prune deleted %d (%v), want the folded hour's sample", n, err)
	}
}

// TestTheFirstMarkIsSeededAtTheLongestWindow: the first pass on an install
// holding samples years old starts at the longest window back from now (a
// type's own, when it is longer than the lane's), not at the oldest month, so
// an upgrade does not fold years of hours the prune is about to remove; on a
// hot tier younger than the window it starts at the oldest month.
func TestTheFirstMarkIsSeededAtTheLongestWindow(t *testing.T) {
	t.Run("old samples", func(t *testing.T) {
		f := newTrendRig(t)
		f.plant(time.Now().UTC().Add(-2*365*24*time.Hour), "observed", 1)
		mustExec(t, f.conn, `update metric_type set retention_days = 30 where name = 'prune-rtt'`)
		want := time.Now().UTC().Add(-30 * 24 * time.Hour).Truncate(time.Hour)
		f.compact(1)
		announced, compacted := f.marks()
		seed := compacted.Add(-time.Hour)
		if !seed.Equal(want) && !seed.Equal(want.Add(time.Hour)) {
			t.Errorf("the first pass started at %v, want %v (30 days back, the type's window)", seed, want)
		}
		if !announced.Equal(compacted) {
			t.Errorf("announced %v, compacted %v; want the one hour announced and folded", announced, compacted)
		}
	})
	t.Run("a young hot tier", func(t *testing.T) {
		f := newTrendRig(t)
		parts, err := f.gw.MetricPartitions(f.ctx)
		if err != nil || len(parts) == 0 {
			t.Fatalf("partitions %v (%v)", parts, err)
		}
		oldest := parts[0].From
		f.compact(1)
		_, compacted := f.marks()
		seed := compacted.Add(-time.Hour)
		window := time.Now().UTC().Add(-14 * 24 * time.Hour).Truncate(time.Hour)
		if oldest.After(window) {
			if !seed.Equal(oldest) {
				t.Errorf("the first pass started at %v, want the oldest month %v", seed, oldest)
			}
		} else if !seed.Equal(window) && !seed.Equal(window.Add(time.Hour)) {
			t.Errorf("the first pass started at %v, want %v (the lane's 14 days)", seed, window)
		}
	})
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
