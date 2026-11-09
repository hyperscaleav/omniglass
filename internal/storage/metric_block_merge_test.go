package storage_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
)

// A late observed sample merges into the block that covers it (#1529,
// ADR-1529): in its own batch's transaction, once a block however many of
// the batch's samples it takes, so the block unnests to what it would hold
// had the sample arrived on time. A sample older than its series' window is
// dropped and logged, and the rest of its batch lands. The trend's recompute
// of a dirtied hour reads the blocks that cover it.

// sampleOf is one prune-rtt sample on prune-c1 at instance, as the ingest
// writes it.
func sampleOf(instance string, ts time.Time, v float64) storage.MetricSampleWrite {
	return storage.MetricSampleWrite{OwnerKind: "component", OwnerID: "prune-c1", Key: "prune-rtt", Instance: instance, Value: v, TS: ts}
}

// ingest lands evs through the ingest's own write, as one batch.
func (r *blockRig) ingest(evs ...storage.MetricSampleWrite) {
	r.t.Helper()
	if err := r.gw.InsertMetricSamples(r.ctx, storage.PushWriter(), evs); err != nil {
		r.t.Fatal(err)
	}
}

// plantAt lands one sample of series id at ts, after every row it holds.
func (r *blockRig) plantAt(id int64, ts time.Time, v float64) {
	r.t.Helper()
	if err := r.gw.MakeMetricDays(r.ctx, ts); err != nil {
		r.t.Fatal(err)
	}
	mustExec(r.t, r.conn, `insert into metric (series_id, ts, value) values ($1, $2, $3)`, id, ts, v)
}

// spacedAt is the instant of the i-th of n samples spaced lands over day.
func spacedAt(day time.Time, n, i int) time.Time {
	return day.Add(time.Duration(int64(i)*86400000/int64(n)) * time.Millisecond)
}

// TestALateSampleLandsInTheBlockThatCoversIt: for each span, a series whose
// samples arrive after their day folded ends with exactly the blocks of a
// twin whose same samples arrived on time: into an hour block (and a new
// hour block for an hour the fold found empty), a day block, and a week
// block (a leftover day that held no sample included), an instant reported
// again landing after the samples already there.
func TestALateSampleLandsInTheBlockThatCoversIt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		rows func(r *blockRig, id int64)
		late func(r *blockRig) []time.Time
		want string
	}{
		{"hour", func(r *blockRig, id int64) {
			r.spaced(id, r.day(0), 4300)
			mustExec(r.t, r.conn, `delete from metric where series_id = $1 and ts >= $2`, id, r.day(0).Add(23*time.Hour))
		}, func(r *blockRig) []time.Time {
			return []time.Time{r.day(0).Add(10*time.Hour + time.Millisecond), spacedAt(r.day(0), 4300, 1000), r.day(0).Add(23*time.Hour + 30*time.Minute)}
		}, "hour 0+23:00 1"},
		{"day", func(r *blockRig, id int64) { r.spaced(id, r.day(1), 170) }, func(r *blockRig) []time.Time {
			return []time.Time{r.day(1).Add(5*time.Hour + time.Millisecond), spacedAt(r.day(1), 170, 10)}
		}, "day 1+00:00 172"},
		{"week", func(r *blockRig, id int64) { r.spaced(id, r.day(3), 5) }, func(r *blockRig) []time.Time {
			return []time.Time{r.day(3).Add(time.Hour), spacedAt(r.day(3), 5, 2), r.day(5).Add(2 * time.Hour)}
		}, "week 0+00:00 8"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := newBlockRig(t)
			on, late := r.series("on", "observed"), r.series("late", "observed")
			c.rows(r, on)
			c.rows(r, late)
			var evs []storage.MetricSampleWrite
			for i, ts := range c.late(r) {
				r.plantAt(on, ts, float64(-1-i))
				evs = append(evs, sampleOf("late", ts, float64(-1-i)))
			}
			r.fold(storage.BlockFold{MaxDays: blockLaneDays})
			if got := r.blocksOf(late); slices.Contains(got, c.want) {
				t.Fatalf("before the late samples the blocks %v already hold %q", got, c.want)
			}
			r.ingest(evs...)
			if got := r.blocksOf(late); !slices.Contains(got, c.want) {
				t.Errorf("after the late samples the blocks are %v, want %q among them", got, c.want)
			}
			if err := sameBlocks(r.readBlocks([]int64{on}), r.readBlocks([]int64{late})); err != nil {
				t.Errorf("the late series' blocks differ from its on-time twin's: %v", err)
			}
			if n := r.count(`select count(*) from metric where series_id = $1 and value < 0`, late); n != len(evs) {
				t.Errorf("%d of the %d late samples have their raw row, want all (raw stays authoritative until #1530)", n, len(evs))
			}
			r.verify()
		})
	}
}

// TestLateSamplesRewriteTheirBlockOnce: one batch of 120 late samples,
// 40 for each of an hour, a day and a week block, writes each block once,
// and the WAL it costs over the same batch landing raw only is the merge's.
func TestLateSamplesRewriteTheirBlockOnce(t *testing.T) {
	// Serial: the WAL it measures is the whole cluster's, so a parallel test's
	// writes would land in its figure.
	r := newBlockRig(t)
	a := r.series("", "observed")
	r.spaced(a, r.day(0), 4080)
	r.spaced(a, r.day(1), 170)
	r.spaced(a, r.day(3), 5)
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	mustExec(t, r.conn, `create table block_write (series_id bigint, span_start timestamptz, span text)`)
	mustExec(t, r.conn, `create function block_written() returns trigger language plpgsql as $$
		begin insert into block_write values (new.series_id, new.span_start, new.span); return new; end $$`)
	mustExec(t, r.conn, `create trigger block_written after insert or update on metric_block for each row execute function block_written()`)
	wal := func(evs []storage.MetricSampleWrite) int64 {
		t.Helper()
		var from string
		if err := r.conn.QueryRow(r.ctx, `select pg_current_wal_lsn()::text`).Scan(&from); err != nil {
			t.Fatal(err)
		}
		r.ingest(evs...)
		var bytes int64
		if err := r.conn.QueryRow(r.ctx, `select pg_wal_lsn_diff(pg_current_wal_lsn(), $1::pg_lsn)::bigint`, from).Scan(&bytes); err != nil {
			t.Fatal(err)
		}
		return bytes
	}
	// The baseline is the same batch's shape into a day the fold has not
	// reached, after a batch that warms the same pages, so it is the raw rows'
	// and the trend marks' cost alone.
	var late, warm, raw []storage.MetricSampleWrite
	today := time.Now().UTC().Truncate(24 * time.Hour)
	for i := range 40 {
		s := time.Duration(i) * time.Second
		late = append(late, sampleOf("", r.day(0).Add(10*time.Hour+500*time.Millisecond+s), float64(i)),
			sampleOf("", r.day(1).Add(5*time.Hour+s), float64(i)), sampleOf("", r.day(3).Add(2*time.Hour+s), float64(i)))
		for j := range 3 {
			at := today.Add(time.Duration(j)*time.Hour + s)
			warm = append(warm, sampleOf("", at, float64(i)))
			raw = append(raw, sampleOf("", at.Add(time.Millisecond), float64(i)))
		}
	}
	if err := r.gw.MakeMetricDays(r.ctx, today); err != nil {
		t.Fatal(err)
	}
	r.ingest(warm...)
	baseline := wal(raw)
	merged := wal(late)
	rows, err := r.conn.Query(r.ctx, `select span || ' ' || count(*) from block_write group by span order by span`)
	if err != nil {
		t.Fatal(err)
	}
	var writes []string
	for rows.Next() {
		var w string
		if err := rows.Scan(&w); err != nil {
			t.Fatal(err)
		}
		writes = append(writes, w)
	}
	rows.Close()
	if want := []string{"day 1", "hour 1", "week 1"}; !slices.Equal(writes, want) {
		t.Errorf("the batch wrote the blocks %v times, want each once %v", writes, want)
	}
	got := r.blocksOf(a)
	for _, want := range []string{"hour 0+10:00 210", "day 1+00:00 210", "week 0+00:00 45"} {
		if !slices.Contains(got, want) {
			t.Errorf("after the batch the blocks are %v, want %q among them", got, want)
		}
	}
	t.Logf("120 late samples into three blocks: %d WAL bytes, %d a sample; the same batch landing raw only: %d; the merge: %d",
		merged, merged/120, baseline, merged-baseline)
	r.verify()
}

// TestAReplayedSpoolLandsWhole: a node's three-day spool replayed after its
// first two days folded lands every sample: Sunday's into its ISO week's
// block, Monday's into its day block, and Tuesday's, a day the fold has not
// reached, as raw rows the next pass folds. A batch delivered again lands
// nothing twice.
func TestAReplayedSpoolLandsWhole(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	a := r.series("", "observed")
	r.spaced(a, r.day(3), 1)
	r.spaced(a, r.day(6), 2)
	r.spaced(a, r.day(7), 170)
	r.fold(storage.BlockFold{MaxDays: 8})
	if got := r.blocksOf(a); !slices.Equal(got, []string{"week 0+00:00 3", "day 7+00:00 170"}) {
		t.Fatalf("before the replay the blocks are %v", got)
	}
	var spool []storage.MetricSampleWrite
	for i := range 3 * 48 {
		spool = append(spool, sampleOf("", r.day(6).Add(time.Duration(i)*30*time.Minute+7*time.Second), float64(i)))
	}
	batch := func(k int) storage.IngestBatch {
		end := min((k+1)*50, len(spool))
		return storage.IngestBatch{BatchID: fmt.Sprintf("spool-%d", k), Writer: storage.PushWriter(), Metrics: spool[k*50 : end]}
	}
	for k := range 3 {
		if landed, err := landOf(r.gw.LandBatch(r.ctx, batch(k))); err != nil || !landed {
			t.Fatalf("spool batch %d: landed %v, %v", k, landed, err)
		}
	}
	if landed, err := landOf(r.gw.LandBatch(r.ctx, batch(1))); err != nil || landed {
		t.Fatalf("a redelivered spool batch landed %v, %v; want nothing", landed, err)
	}
	if n := r.count(`select count(*) from metric where series_id = $1`, a); n != 3+170+144 {
		t.Errorf("%d raw rows, want all %d", n, 3+170+144)
	}
	if got := r.blocksOf(a); !slices.Equal(got, []string{"week 0+00:00 51", "day 7+00:00 218"}) {
		t.Errorf("after the replay the blocks are %v, want Sunday's 48 in the week block and Monday's in its day block", got)
	}
	r.verify()
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	if got := r.blocksOf(a); !slices.Equal(got, []string{"week 0+00:00 51", "day 7+00:00 218", "week 7+00:00 48"}) {
		t.Errorf("after the next pass the blocks are %v, want Tuesday's 48 in its week's block", got)
	}
}

// TestATooOldSampleIsDroppedAndCounted: a late sample older than its series'
// window is dropped, with one log line naming its series and its age, and
// the rest of its batch lands, the batch acked once. The batch's newest
// sample has no day partition, so its first attempt misses and the batch is
// retried with the day made: the line is still logged once, on the commit,
// at the real clock and either side of a UTC midnight (#1567).
func TestATooOldSampleIsDroppedAndCounted(t *testing.T) {
	// Serial: it swaps the process-wide slog default to read the line.
	storagetest.AtEachMidnight(t, func(t *testing.T, at time.Time) {
		r := newBlockRigAt(t, at)
		a := r.series("", "observed")
		r.spaced(a, r.day(1), 170)
		r.fold(storage.BlockFold{MaxDays: blockLaneDays})
		now := r.now().UTC()
		mustExec(t, r.conn, `drop table if exists `+dayName(now.Add(-time.Minute)))
		var mu sync.Mutex
		var buf bytes.Buffer
		prev := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{mu: &mu, w: &buf}, nil)))
		t.Cleanup(func() { slog.SetDefault(prev) })
		b := storage.IngestBatch{BatchID: "too-old", Writer: storage.PushWriter(), Metrics: []storage.MetricSampleWrite{
			sampleOf("old", now.Add(-(blockLaneDays+10)*24*time.Hour), 1),
			sampleOf("", r.day(1).Add(5*time.Hour+time.Millisecond), 2),
			sampleOf("", now.Add(-time.Minute), 3),
		}}
		if landed, err := landOf(r.gw.LandBatch(r.ctx, b)); err != nil || !landed {
			t.Fatalf("the batch landed %v, %v; want it landed", landed, err)
		}
		if landed, err := landOf(r.gw.LandBatch(r.ctx, b)); err != nil || landed {
			t.Fatalf("the batch again landed %v, %v; want nothing", landed, err)
		}
		if n := r.count(`select count(*) from metric m join metric_series s on s.id = m.series_id where s.instance = 'old'`); n != 0 {
			t.Errorf("%d rows of the too-old sample landed, want none", n)
		}
		if n := r.count(`select count(*) from metric where series_id = $1`, a); n != 172 {
			t.Errorf("series a holds %d rows, want its 170 and the batch's other two", n)
		}
		if got := r.blocksOf(a); !slices.Equal(got, []string{"day 1+00:00 171"}) {
			t.Errorf("the blocks are %v, want the late sample in its day block", got)
		}
		mu.Lock()
		logged := buf.String()
		mu.Unlock()
		var lines []string
		for _, l := range strings.Split(logged, "\n") {
			if strings.Contains(l, "older than their series' window") {
				lines = append(lines, l)
			}
		}
		if len(lines) != 1 || !strings.Contains(lines[0], "instance=old") || !strings.Contains(lines[0], "samples=1") ||
			!strings.Contains(lines[0], "key=prune-rtt") || !strings.Contains(lines[0], "age=") {
			t.Errorf("the drop logged %q, want one line naming the series, its count and its age", lines)
		}
	})
}

// TestADirtiedHourRecomputesFromItsBlocks: a late sample's hour is
// recomputed from the block that covers it, an hour, day or week block, so
// with the folded day's raw rows gone (as #1530 will drop them) the trend
// row is the one its raw rows give.
func TestADirtiedHourRecomputesFromItsBlocks(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	spans := []struct {
		name string
		day  int
		n    int
	}{{"hour", 0, 4080}, {"day", 1, 170}, {"week", 3, 5}}
	var evs []storage.MetricSampleWrite
	type pair struct {
		blocks, raw int64
		hour        time.Time
	}
	pairs := map[string]pair{}
	for _, s := range spans {
		p := pair{blocks: r.series(s.name+"-blocks", "observed"), raw: r.series(s.name+"-raw", "observed"), hour: r.day(s.day).Add(5 * time.Hour)}
		r.spaced(p.blocks, r.day(s.day), s.n)
		r.spaced(p.raw, r.day(s.day), s.n)
		pairs[s.name] = p
		evs = append(evs, sampleOf(s.name+"-blocks", p.hour.Add(30*time.Minute), -5), sampleOf(s.name+"-raw", p.hour.Add(30*time.Minute), -5))
	}
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	for _, s := range spans {
		mustExec(t, r.conn, `delete from metric where series_id = $1`, pairs[s.name].blocks)
	}
	r.ingest(evs...)
	if _, err := r.gw.CompactMetricTrends(r.ctx, 1, blockLaneDays); err != nil {
		t.Fatal(err)
	}
	trend := func(id int64, hour time.Time) string {
		var out string
		if err := r.conn.QueryRow(r.ctx, `select coalesce((select concat_ws(' ', count, min, max, sum, first, last)
			from metric_trend where series_id = $1 and hour = $2), '')`, id, hour).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	for _, s := range spans {
		p := pairs[s.name]
		var want string
		if err := r.conn.QueryRow(r.ctx, `select concat_ws(' ', count(*), min(value), max(value), sum(value),
			(array_agg(value order by ts, id))[1], (array_agg(value order by ts desc, id desc))[1])
			from metric where series_id = $1 and ts >= $2 and ts < $2 + interval '1 hour'`, p.raw, p.hour).Scan(&want); err != nil {
			t.Fatal(err)
		}
		if got := trend(p.blocks, p.hour); got != want {
			t.Errorf("%s: the hour recomputed from its block is %q, want %q, the trend of its raw rows", s.name, got, want)
		}
		if got := trend(p.raw, p.hour); got != want {
			t.Errorf("%s: the twin's hour recomputed is %q, want %q", s.name, got, want)
		}
	}
}

// TestABatchFailingMidMergeLandsNothing: a batch whose merge fails rolls back
// whole, its raw rows, its blocks and its receipt alike, and its redelivery
// lands every sample once.
func TestABatchFailingMidMergeLandsNothing(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	a := r.series("", "observed")
	r.spaced(a, r.day(1), 170)
	r.spaced(a, r.day(2), 170)
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	mustExec(t, r.conn, fmt.Sprintf(`create function block_refused() returns trigger language plpgsql as $$
		begin if new.span_start = '%s' then raise exception 'block refused'; end if; return new; end $$`, r.day(2).Format(time.RFC3339)))
	mustExec(t, r.conn, `create trigger block_refused before update on metric_block for each row execute function block_refused()`)
	b := storage.IngestBatch{BatchID: "mid-merge", Writer: storage.PushWriter(), Metrics: []storage.MetricSampleWrite{
		sampleOf("", r.day(1).Add(time.Hour+time.Millisecond), 1),
		sampleOf("", r.day(2).Add(time.Hour+time.Millisecond), 2),
		sampleOf("", time.Now().Add(-time.Minute), 3),
	}}
	if landed, err := landOf(r.gw.LandBatch(r.ctx, b)); err == nil {
		t.Fatalf("the batch landed %v with its merge refused, want an error", landed)
	}
	if n := r.count(`select count(*) from metric where series_id = $1`, a); n != 340 {
		t.Errorf("after the failed batch series a holds %d rows, want its 340", n)
	}
	if got := r.blocksOf(a); !slices.Equal(got, []string{"day 1+00:00 170", "day 2+00:00 170"}) {
		t.Errorf("after the failed batch the blocks are %v, want them as folded", got)
	}
	if n := r.count(`select count(*) from telemetry_receipt where batch_id = 'mid-merge'`); n != 0 {
		t.Errorf("the failed batch left %d receipts", n)
	}
	mustExec(t, r.conn, `drop trigger block_refused on metric_block`)
	for i, want := range []bool{true, false} {
		if landed, err := landOf(r.gw.LandBatch(r.ctx, b)); err != nil || landed != want {
			t.Fatalf("delivery %d landed %v, %v; want %v", i+2, landed, err, want)
		}
	}
	if n := r.count(`select count(*) from metric where series_id = $1`, a); n != 343 {
		t.Errorf("after the redelivery series a holds %d rows, want 343", n)
	}
	if got := r.blocksOf(a); !slices.Equal(got, []string{"day 1+00:00 171", "day 2+00:00 171"}) {
		t.Errorf("after the redelivery the blocks are %v, want each late sample in its day block once", got)
	}
	r.verify()
}

// freezeClock stands the rig's database clock at now for every connection
// opened after it: a schema ahead of pg_catalog on the search path holds a
// now() that reads the database setting clock.now, so the window's cutoff
// (now minus the series' days) sits exactly where a test puts it, and the
// rig's gateway is replaced by one opened afterward. The merge, the ingest's
// window check and the trend recompute all read it, as they read now().
func (r *blockRig) freezeClock(now time.Time) {
	r.t.Helper()
	var db string
	if err := r.conn.QueryRow(r.ctx, `select current_database()`).Scan(&db); err != nil {
		r.t.Fatal(err)
	}
	mustExec(r.t, r.conn, `create schema clock`)
	mustExec(r.t, r.conn, `create function clock.now() returns timestamptz language sql stable
		as $$ select current_setting('clock.now')::timestamptz $$`)
	id := pgx.Identifier{db}.Sanitize()
	mustExec(r.t, r.conn, `alter database `+id+` set clock.now = '`+now.UTC().Format(time.RFC3339Nano)+`'`)
	mustExec(r.t, r.conn, `alter database `+id+` set search_path = clock, pg_catalog, public`)
	gw, err := storage.NewPG(r.ctx, r.dsn)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(gw.Close)
	r.gw = gw
}

// trendRow is the trend row of series id at hour as "count min max sum first
// last", empty when it has none.
func (r *blockRig) trendRow(id int64, hour time.Time) string {
	r.t.Helper()
	var out string
	if err := r.conn.QueryRow(r.ctx, `select coalesce((select concat_ws(' ', count, min, max, sum, first, last)
		from metric_trend where series_id = $1 and hour = $2), '')`, id, hour).Scan(&out); err != nil {
		r.t.Fatal(err)
	}
	return out
}

// rawTrendRow is the row a trend of series id's hour would hold were it folded
// from the raw rows, in trendRow's form, empty when the hour has none.
func (r *blockRig) rawTrendRow(id int64, hour time.Time) string {
	r.t.Helper()
	var out string
	if err := r.conn.QueryRow(r.ctx, `select coalesce(concat_ws(' ', count(*), min(value), max(value), sum(value),
		(array_agg(value order by ts, id))[1], (array_agg(value order by ts desc, id desc))[1]), '')
		from metric where series_id = $1 and ts >= $2 and ts < $2 + interval '1 hour'`, id, hour).Scan(&out); err != nil {
		r.t.Fatal(err)
	}
	return out
}

// foldedDay is how a test's folded day is cut: the span its block takes, the
// day of the rig's week it sits on and the samples it holds.
type foldedDay struct {
	name string
	day  int
	n    int
	want string
}

var foldedDays = []foldedDay{
	{"hour", 0, 4080, "hour 0+00:00 170"},
	{"day", 1, 170, "day 1+00:00 170"},
	{"week", 3, 5, "week 0+00:00 5"},
}

// windowRig folds c's day under the rig's window, then moves the window to
// days (a type's own retention_days when override, else the metric lane's, by
// the platform setting) and freezes the clock so the window's cutoff is at.
// It returns the series.
func (r *blockRig) windowRig(c foldedDay, cutoff time.Time, days int, override bool) (series int64) {
	r.t.Helper()
	series = r.series("", "observed")
	r.spaced(series, r.day(c.day), c.n)
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	if got := r.blocksOf(series); !slices.Contains(got, c.want) {
		r.t.Fatalf("before the window moves the blocks are %v, want %q among them", got, c.want)
	}
	if override {
		mustExec(r.t, r.conn, `update metric_type set retention_days = $1 where name = 'prune-rtt'`, days)
	} else {
		mustExec(r.t, r.conn, `update metric_type set retention_days = null where name = 'prune-rtt'`)
		if _, err := r.gw.UpsertSettingOverride(r.ctx, "", "platform", "retention", map[string]any{"metric_days": days}, nil); err != nil {
			r.t.Fatal(err)
		}
	}
	r.freezeClock(cutoff.Add(time.Duration(days) * 24 * time.Hour))
	return series
}

// TestAnIntactHourOfADayPastTheWindowCutoffTrendsFromItsRawRows: a day folded
// while it was intact whose start has since slipped past its series' window,
// the cutoff inside it, takes no late sample into its block (the merge needs
// the whole day), so a late sample of one of its hours still inside the
// window lands raw only; the recompute of that hour reads the same
// predicate, so it reads the raw rows, and the hour's trend row is the one
// they give, whatever span the day was folded at (#1546, ADR-1529).
func TestAnIntactHourOfADayPastTheWindowCutoffTrendsFromItsRawRows(t *testing.T) {
	t.Parallel()
	for _, c := range foldedDays {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := newBlockRig(t)
			day := r.day(c.day)
			a := r.windowRig(c, day.Add(6*time.Hour+30*time.Minute), 7, true)
			hour := day.Add(10 * time.Hour)
			r.ingest(sampleOf("", hour.Add(30*time.Minute+time.Millisecond), -7))
			if got := r.blocksOf(a); !slices.Contains(got, c.want) {
				t.Fatalf("the late sample of a day past the cutoff reached its block: the blocks are %v", got)
			}
			if _, err := r.gw.CompactMetricTrends(r.ctx, 1, blockLaneDays); err != nil {
				t.Fatal(err)
			}
			want := r.rawTrendRow(a, hour)
			if !strings.Contains(want, "-7") {
				t.Fatalf("the raw rows' fold %q lacks the late sample", want)
			}
			if got := r.trendRow(a, hour); got != want {
				t.Errorf("the trend of the intact hour is %q, want %q, the fold of its raw rows", got, want)
			}
		})
	}
}

// TestEachHourAtTheWindowCutoffTrendsFromOneSource: at the window's boundary
// each dirtied hour reads exactly one source, raw rows or its covering block,
// by the merge's own test of its day, and its trend row matches the raw rows'
// (a late sample is in it, whichever source held it). The cutoff sits on a
// day's start (the day is intact: the sample merged into the block and the
// block is read), on an hour's start (the day is not: raw), inside an hour (that
// hour is out of the window, so not recomputed, and the next is read from
// raw), and a series of a type with its own retention_days is held to that
// window rather than the lane's.
func TestEachHourAtTheWindowCutoffTrendsFromOneSource(t *testing.T) {
	t.Parallel()
	cuts := []struct {
		name   string
		at     time.Duration // the cutoff, from the folded day's start
		intact bool          // the folded day's start is inside the window
		hours  []int         // dirtied hours, in the window or not
		inside func(h int) bool
	}{
		{"the day's start is the cutoff", 0, true, []int{0, 1, 23}, func(h int) bool { return true }},
		{"an hour's start is the cutoff", 6 * time.Hour, false, []int{5, 6, 7, 23}, func(h int) bool { return h >= 6 }},
		{"the cutoff is inside an hour", 6*time.Hour + 30*time.Minute, false, []int{5, 6, 7, 23}, func(h int) bool { return h >= 7 }},
		{"a second past the day's start", time.Second, false, []int{0, 1, 23}, func(h int) bool { return h >= 1 }},
	}
	for _, c := range foldedDays {
		for _, cut := range cuts {
			for _, override := range []bool{true, false} {
				name := fmt.Sprintf("%s/%s/override=%v", c.name, cut.name, override)
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					r := newBlockRig(t)
					day := r.day(c.day)
					a := r.windowRig(c, day.Add(cut.at), 7, override)
					lane := blockLaneDays
					if !override {
						lane = 7
					}
					// A late sample of an hour inside the window comes through the
					// ingest, which merges it where its day is intact. The ingest
					// drops one older than the cutoff, so an hour outside it is
					// planted and marked dirty directly: the recompute's own filter
					// is what holds that hour to the window.
					for _, h := range cut.hours {
						hour := day.Add(time.Duration(h) * time.Hour)
						ts := hour.Add(20*time.Minute + time.Millisecond)
						if cut.inside(h) {
							r.ingest(sampleOf("", ts, -float64(h+1)))
							continue
						}
						r.plantAt(a, ts, -float64(h+1))
						mustExec(t, r.conn, `insert into metric_trend_dirty (series_id, hour) values ($1, $2) on conflict do nothing`, a, hour)
					}
					if merged := !slices.Contains(r.blocksOf(a), c.want); merged != cut.intact {
						t.Errorf("the late samples merged into the folded day's blocks: %v, want %v (the day is intact: %v); the blocks are %v",
							merged, cut.intact, cut.intact, r.blocksOf(a))
					}
					if _, err := r.gw.CompactMetricTrends(r.ctx, 1, lane); err != nil {
						t.Fatal(err)
					}
					for _, h := range cut.hours {
						hour := day.Add(time.Duration(h) * time.Hour)
						got, want := r.trendRow(a, hour), r.rawTrendRow(a, hour)
						if !cut.inside(h) {
							want = ""
						}
						if got != want {
							t.Errorf("hour %02d: trend row %q, want %q", h, got, want)
						}
					}
				})
			}
		}
	}
}

// TestALateBatchWaitsOutAFoldWithoutBeingGivenUp: a fold holding the block
// lock exclusive for longer than the consumer's five deliveries last (about
// 15 s, then the batch is Term'd and lost) leaves the late batch waiting, not
// failing: the merge sets no lock_timeout, and when the fold commits the
// batch lands every sample once, on its first and only delivery (#1546,
// ADR-1529).
func TestALateBatchWaitsOutAFoldWithoutBeingGivenUp(t *testing.T) {
	t.Parallel()
	const hold = 21 * time.Second
	r := newBlockRig(t)
	a := r.series("", "observed")
	r.spaced(a, r.day(1), 170)
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	fold, err := r.conn.Begin(r.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fold.Rollback(context.WithoutCancel(r.ctx)) }()
	if _, err := fold.Exec(r.ctx, `select pg_advisory_xact_lock(hashtextextended('metric_block:fold', 0))`); err != nil {
		t.Fatal(err)
	}
	b := storage.IngestBatch{BatchID: "waits-on-fold", Writer: storage.PushWriter(), Metrics: []storage.MetricSampleWrite{
		sampleOf("", r.day(1).Add(time.Hour+time.Millisecond), 1),
		sampleOf("", r.day(1).Add(2*time.Hour+time.Millisecond), 2),
	}}
	type result struct {
		landed bool
		err    error
		after  time.Duration
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		landed, err := landOf(r.gw.LandBatch(r.ctx, b))
		done <- result{landed, err, time.Since(start)}
	}()
	select {
	case got := <-done:
		t.Fatalf("the batch returned %v, %v after %s behind a fold holding the block lock, want it waiting", got.landed, got.err, got.after)
	case <-time.After(hold):
	}
	if err := fold.Commit(r.ctx); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || !got.landed {
		t.Fatalf("after the fold committed the batch landed %v, %v, want it landed without an error (an error is a Nak, and the fifth is a Term)", got.landed, got.err)
	}
	if got.after < hold {
		t.Errorf("the batch landed after %s, before the fold committed at %s", got.after, hold)
	}
	if n := r.count(`select count(*) from metric where series_id = $1`, a); n != 172 {
		t.Errorf("series a holds %d rows, want 172, each sample once", n)
	}
	if blocks := r.blocksOf(a); !slices.Equal(blocks, []string{"day 1+00:00 172"}) {
		t.Errorf("the blocks are %v, want both samples in the day block once", blocks)
	}
	if n := r.count(`select count(*) from telemetry_receipt where batch_id = 'waits-on-fold'`); n != 1 {
		t.Errorf("the batch left %d receipts, want 1", n)
	}
	r.verify()
}

// TestMergeLateBlocksSetsNoLockTimeout: the late merge takes the block lock
// with no lock_timeout of its own, so a batch is never failed for waiting on
// a fold (the wait is bounded by trendDrainTimeout failing the compaction, not
// the batch, ADR-1529).
func TestMergeLateBlocksSetsNoLockTimeout(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("metric_block.go")
	if err != nil {
		t.Fatal(err)
	}
	if set := regexp.MustCompile(`(?i)set\s+(local\s+)?lock_timeout`); set.Match(src) {
		t.Error("metric_block.go sets a lock_timeout: a late batch must wait for the fold, a timeout would Term its batch after the consumer's redeliveries")
	}
}

// TestALateBatchAddsThreeStatementsAndAnOldOneAddsTheWindowCheck: against a
// batch landing on time, a batch with a late sample adds the settings read,
// the block lock and the merge (3), a batch carrying a sample a day old or
// more adds the settings read and the window check (2), and a batch that is
// both adds the settings read once (4); the on-time batch's own count, 18,
// is held by the ingest footprint test (ADR-1529 decision 8).
func TestALateBatchAddsThreeStatementsAndAnOldOneAddsTheWindowCheck(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	counter := querycount.New()
	gw, err := storage.NewPG(r.ctx, r.dsn, storage.WithQueryTracer(counter))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	a := r.series("", "observed")
	r.spaced(a, r.day(1), 170)
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	now := time.Now().UTC()
	statements := func(name string, announced time.Time, ts time.Time) int {
		t.Helper()
		if err := r.gw.SetMetricTrendMark(r.ctx, announced); err != nil {
			t.Fatal(err)
		}
		b := storage.IngestBatch{BatchID: name, Writer: storage.PushWriter(), Metrics: []storage.MetricSampleWrite{sampleOf("", ts, 1)}}
		if _, err := gw.LandBatch(r.ctx, b); err != nil { // warms the gateway's caches
			t.Fatal(err)
		}
		counter.Reset()
		b.BatchID += "-measured"
		if _, err := gw.LandBatch(r.ctx, b); err != nil {
			t.Fatal(err)
		}
		return counter.N()
	}
	onTime := statements("on-time", now.Add(-72*time.Hour), now.Add(-time.Minute))
	for _, c := range []struct {
		name      string
		announced time.Time
		ts        time.Time
		add       int
	}{
		{"late", now.Add(time.Hour), now.Add(-time.Minute), 3},
		{"old", now.Add(-72 * time.Hour), now.Add(-30 * time.Hour), 2},
		{"late and old", now.Add(time.Hour), r.day(1).Add(2*time.Hour + time.Millisecond), 4},
	} {
		if got := statements(c.name, c.announced, c.ts); got != onTime+c.add {
			t.Errorf("a %s batch cost %d statements, want the on-time batch's %d and %d more", c.name, got, onTime, c.add)
		}
	}
}
