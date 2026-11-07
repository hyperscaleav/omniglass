package storage_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
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
		if landed, err := r.gw.LandBatch(r.ctx, batch(k)); err != nil || !landed {
			t.Fatalf("spool batch %d: landed %v, %v", k, landed, err)
		}
	}
	if landed, err := r.gw.LandBatch(r.ctx, batch(1)); err != nil || landed {
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
// the rest of its batch lands, the batch acked once.
func TestATooOldSampleIsDroppedAndCounted(t *testing.T) {
	// Serial: it swaps the process-wide slog default to read the line.
	r := newBlockRig(t)
	a := r.series("", "observed")
	r.spaced(a, r.day(1), 170)
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	var mu sync.Mutex
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{mu: &mu, w: &buf}, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	now := time.Now().UTC()
	b := storage.IngestBatch{BatchID: "too-old", Writer: storage.PushWriter(), Metrics: []storage.MetricSampleWrite{
		sampleOf("old", now.Add(-(blockLaneDays+10)*24*time.Hour), 1),
		sampleOf("", r.day(1).Add(5*time.Hour+time.Millisecond), 2),
		sampleOf("", now.Add(-time.Minute), 3),
	}}
	if landed, err := r.gw.LandBatch(r.ctx, b); err != nil || !landed {
		t.Fatalf("the batch landed %v, %v; want it landed", landed, err)
	}
	if landed, err := r.gw.LandBatch(r.ctx, b); err != nil || landed {
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
	if landed, err := r.gw.LandBatch(r.ctx, b); err == nil {
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
		if landed, err := r.gw.LandBatch(r.ctx, b); err != nil || landed != want {
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
