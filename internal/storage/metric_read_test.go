package storage_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The series read (#1572, ADR-1530): every reader of a metric series' samples
// answers the same before its days fold into blocks and after, and the same
// again once the raw rows the blocks cover are gone, which only a read that
// takes a covered day from its blocks can. A late sample into a folded day
// reads back once, and a day past its window's cutoff, which took no late
// sample into its block, reads back from its raw rows.

// seriesReads is what every reader of the rig's series answers: the series
// read over the rig's week, the raw read, and the buckets holding samples at
// a sub-hour, an hourly and a daily resolution (the grid and its covered edge
// move with the clock; the buckets holding rows are what a fold could change).
type seriesReads struct {
	samples []storage.BlockSample
	raw     []storage.MetricSample
	buckets map[time.Duration][]storage.MetricBucket
}

func (r *blockRig) reads(ids ...int64) seriesReads {
	r.t.Helper()
	var out seriesReads
	for _, id := range ids {
		got, err := r.gw.MetricSeriesSamples(r.ctx, id, r.day(-1), r.day(8))
		if err != nil {
			r.t.Fatal(err)
		}
		out.samples = append(out.samples, got...)
	}
	raw, err := r.gw.ListMetricSeries(r.ctx, "component", "prune-c1", "prune-rtt", blockLaneDays*24*time.Hour, 10000)
	if err != nil {
		r.t.Fatal(err)
	}
	out.raw = raw
	out.buckets = map[time.Duration][]storage.MetricBucket{}
	for _, res := range []time.Duration{10 * time.Minute, time.Hour, 24 * time.Hour} {
		grid, err := r.gw.ListMetricBuckets(r.ctx, "component", "prune-c1", "prune-rtt", nil, blockLaneDays*24*time.Hour, res)
		if err != nil {
			r.t.Fatal(err)
		}
		for _, b := range grid.Buckets {
			if b.Count > 0 {
				out.buckets[res] = append(out.buckets[res], b)
			}
		}
	}
	return out
}

// same fails the test where got differs from want, naming the reader.
func (r *blockRig) same(when string, want, got seriesReads) {
	r.t.Helper()
	if err := sameSamples(want.samples, got.samples); err != nil {
		r.t.Errorf("%s, the series read differs: %v", when, err)
	}
	if !reflect.DeepEqual(want.raw, got.raw) {
		r.t.Errorf("%s, the raw read differs: %d samples, want %d", when, len(got.raw), len(want.raw))
	}
	for res, b := range want.buckets {
		if !reflect.DeepEqual(b, got.buckets[res]) {
			r.t.Errorf("%s, the %s buckets differ: %d buckets, want %d", when, res, len(got.buckets[res]), len(b))
		}
	}
}

// dropCovered deletes ids' raw rows of the rig's week, every day of which a
// block covers once the week has folded: what a read of the blocks alone
// would see.
func (r *blockRig) dropCovered(ids ...int64) {
	r.t.Helper()
	mustExec(r.t, r.conn, `delete from metric where series_id = any($1) and ts >= $2 and ts < $3`, ids, r.day(0), r.day(7))
}

// foldAndRead folds the rig's week and holds every reader of ids to what it
// answered before: once the week has folded, its raw rows still in place
// (none of them dropped), and again with them gone.
func (r *blockRig) foldAndRead(ids ...int64) {
	r.t.Helper()
	before := r.reads(ids...)
	if len(before.samples) == 0 || len(before.raw) == 0 {
		r.t.Fatal("the series hold no samples to read")
	}
	rows := r.count(`select count(*) from metric where series_id = any($1)`, ids)
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	if n := r.count(`select count(*) from metric where series_id = any($1)`, ids); n != rows {
		r.t.Fatalf("the fold left %d raw rows of %d", n, rows)
	}
	r.same("once the week folds", before, r.reads(ids...))
	r.dropCovered(ids...)
	r.same("with the folded week's raw rows gone", before, r.reads(ids...))
}

// TestAnObservedSeriesReadsTheSameOnceFolded: an observed series' days cut
// into hour blocks, a day block and a week block (a leftover day holding no
// sample among them) read back sample for sample, through every reader.
func TestAnObservedSeriesReadsTheSameOnceFolded(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	id := r.series("", "observed")
	r.spaced(id, r.day(0), 4300)
	r.spaced(id, r.day(1), 170)
	r.spaced(id, r.day(3), 5)
	r.spaced(id, r.day(5), 20)
	r.plantAt(id, r.day(5).Add(time.Hour), -1)
	r.foldAndRead(id)
	if got := strings.Join(r.blocksOf(id), ","); !strings.Contains(got, "hour 0+00:00") || !strings.Contains(got, "day 1+00:00 170") ||
		!strings.Contains(got, "week 0+00:00 26") {
		t.Errorf("the blocks are %s, want hour blocks, a day block and a week block", got)
	}
}

// TestACalculatedSeriesReadsTheSameOnceFolded: a calculated series whose
// days split by lineage, one lineage of a day in hour blocks or a day block
// while another waits for its week, and two lineages at one instant, reads
// back with every sample's lineage.
func TestACalculatedSeriesReadsTheSameOnceFolded(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	id := r.series("", "calculated")
	r.spacedAs(id, r.day(0), 4400, 0, 4100, avgV1)
	r.spacedAs(id, r.day(0), 4400, 4100, 4400, meanV1)
	r.spacedAs(id, r.day(1), 400, 0, 100, meanV1)
	r.spacedAs(id, r.day(1), 400, 100, 400, meanV2)
	r.spacedAs(id, r.day(2), 50, 0, 50, otherRule)
	r.spacedAs(id, r.day(4), 10, 0, 10, avgV1)
	r.spacedAs(id, r.day(4), 10, 0, 10, meanV2)
	r.foldAndRead(id)
	got := r.reads(id).samples
	if len(got) == 0 || got[0].Lineage == nil || *got[0].Lineage != avgV1 {
		t.Fatalf("the calculated series reads back without its lineage")
	}
}

// TestObservedAndCalculatedSeriesOfOneInstanceReadTheSameOnceFolded: the
// buckets aggregate one instance's measured series, an observed and a
// calculated one, both folded.
func TestObservedAndCalculatedSeriesOfOneInstanceReadTheSameOnceFolded(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	obs, calc := r.series("", "observed"), r.series("", "calculated")
	r.spaced(obs, r.day(1), 300)
	r.spaced(obs, r.day(2), 7)
	r.spacedAs(calc, r.day(1), 300, 0, 300, avgV1)
	r.spacedAs(calc, r.day(2), 7, 0, 7, meanV2)
	r.foldAndRead(obs, calc)
}

// TestLateSamplesIntoFoldedDaysReadBackOnce: late samples of a folded week,
// into an hour block (and an hour the fold found empty), a day block, a week
// block and a leftover day that held none, each land raw and merge into
// their block, and read back once, before the raw rows go and after.
func TestLateSamplesIntoFoldedDaysReadBackOnce(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	id := r.series("", "observed")
	r.spaced(id, r.day(0), 4300)
	mustExec(t, r.conn, `delete from metric where series_id = $1 and ts >= $2`, id, r.day(0).Add(23*time.Hour))
	r.spaced(id, r.day(1), 170)
	r.spaced(id, r.day(3), 5)
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	r.ingest(
		sampleOf("", r.day(0).Add(10*time.Hour+time.Millisecond), -1),
		sampleOf("", spacedAt(r.day(0), 4300, 1000), -2),
		sampleOf("", r.day(0).Add(23*time.Hour+30*time.Minute), -3),
		sampleOf("", r.day(1).Add(5*time.Hour+time.Millisecond), -4),
		sampleOf("", spacedAt(r.day(1), 170, 10), -5),
		sampleOf("", r.day(3).Add(time.Hour), -6),
		sampleOf("", r.day(5).Add(2*time.Hour), -7))
	r.verify()
	want := r.rawOf(id, r.day(-1), r.day(8))
	got := r.reads(id)
	if err := sameSamples(want, got.samples); err != nil {
		t.Errorf("the series read differs from the raw rows, the late samples among them: %v", err)
	}
	r.dropCovered(id)
	r.same("with the folded week's raw rows gone", got, r.reads(id))
}

// TestADayPastTheWindowCutoffReadsItsRawRows: a day folded while intact
// whose start has since slipped past its series' window, the cutoff inside
// it, took no late sample into its block, so its late sample is read from
// its raw row, and none of its samples twice, whatever span it was folded
// at. A day whose start is the cutoff is still intact: its late sample
// merged, and its blocks are read.
func TestADayPastTheWindowCutoffReadsItsRawRows(t *testing.T) {
	t.Parallel()
	for _, c := range foldedDays {
		for _, cut := range []struct {
			name string
			at   time.Duration
		}{{"inside the day", 6*time.Hour + 30*time.Minute}, {"at the day's start", 0}} {
			t.Run(c.name+"/"+cut.name, func(t *testing.T) {
				t.Parallel()
				r := newBlockRig(t)
				day := r.day(c.day)
				id := r.windowRig(c, day.Add(cut.at), 7, true)
				r.ingest(sampleOf("", day.Add(10*time.Hour+30*time.Minute+time.Millisecond), -7))
				want := r.rawOf(id, day, day.Add(24*time.Hour))
				got, err := r.gw.MetricSeriesSamples(r.ctx, id, day, day.Add(24*time.Hour))
				if err != nil {
					t.Fatal(err)
				}
				if err := sameSamples(want, got); err != nil {
					t.Errorf("the day reads back otherwise than its raw rows: %v", err)
				}
			})
		}
	}
}
