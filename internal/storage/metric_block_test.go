package storage_test

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The compacted tier's blocks (#1528, ADR-1528): each closed UTC day of an
// observed series folds into hour, day or week blocks by its own sample count,
// every block unnests to exactly the raw rows it was folded from, a cancelled
// pass resumes where it stopped, and a late sample merges into the block
// that covers it (metric_block_merge_test.go).

// blockLaneDays is the metric lane's window the block tests fold under: long
// enough that a closed ISO week of samples sits wholly inside it.
const blockLaneDays = 30

// blockRig is a prune fixture whose block marks stand at week, a Monday
// whose ISO week closed more than a week ago and is still inside the window,
// prune-rtt's own window being blockLaneDays.
type blockRig struct {
	*pruneFixture
	week time.Time
}

func newBlockRig(t *testing.T) *blockRig {
	t.Helper()
	f := newPruneFixture(t)
	mustExec(t, f.conn, `update metric_type set retention_days = $1 where name = 'prune-rtt'`, blockLaneDays)
	from := time.Now().UTC().Add(-(blockLaneDays - 3) * 24 * time.Hour).Truncate(24 * time.Hour)
	week := from.AddDate(0, 0, (8-int(from.Weekday()))%7)
	if err := f.gw.SetMetricBlockMarks(f.ctx, week); err != nil {
		t.Fatal(err)
	}
	return &blockRig{pruneFixture: f, week: week}
}

// day is the start of the rig's week's i-th day, Monday the 0th.
func (r *blockRig) day(i int) time.Time { return r.week.AddDate(0, 0, i) }

// series starts a series of prune-rtt on prune-c1 at instance, as provenance,
// and returns its id.
func (r *blockRig) series(instance, provenance string) int64 {
	r.t.Helper()
	var id int64
	if err := r.conn.QueryRow(r.ctx, `insert into metric_series (metric_type_id, owner_kind, component_id, instance, provenance)
		values ((select id from metric_type where name = 'prune-rtt'), 'component', (select id from component where name = 'prune-c1'), $1, $2)
		returning id`, instance, provenance).Scan(&id); err != nil {
		r.t.Fatal(err)
	}
	return id
}

// spaced lands n samples of series id evenly over day, the i-th at the whole
// millisecond i/n of the way through it with the value i.
func (r *blockRig) spaced(id int64, day time.Time, n int) {
	r.t.Helper()
	if err := r.gw.MakeMetricDays(r.ctx, day); err != nil {
		r.t.Fatal(err)
	}
	mustExec(r.t, r.conn, `insert into metric (series_id, ts, value)
		select $1, $2::timestamptz + (i * 86400000::bigint / $3) * interval '1 millisecond', i
		from generate_series(0, $3 - 1) i order by i`, id, day, n)
}

// land lands generated samples in their landing order, series i+1 on ids[i].
func (r *blockRig) land(ids []int64, samples []storagetest.GenSample) {
	r.t.Helper()
	var days []time.Time
	series, ts, values := make([]int64, len(samples)), make([]time.Time, len(samples)), make([]float64, len(samples))
	for i, s := range samples {
		series[i], ts[i], values[i] = ids[s.Series-1], s.TS, s.Value
		if d := s.TS.UTC().Truncate(24 * time.Hour); !slices.ContainsFunc(days, d.Equal) {
			days = append(days, d)
		}
	}
	if err := r.gw.MakeMetricDays(r.ctx, days...); err != nil {
		r.t.Fatal(err)
	}
	mustExec(r.t, r.conn, `insert into metric (series_id, ts, value)
		select s, t, v from unnest($1::bigint[], $2::timestamptz[], $3::float8[]) with ordinality as u(s, t, v, o) order by o`,
		series, ts, values)
}

func (r *blockRig) fold(f storage.BlockFold) storage.BlockCompaction {
	r.t.Helper()
	f.LaneDays, f.Verify = blockLaneDays, true
	c, err := r.gw.CompactMetricBlocks(r.ctx, f)
	if err != nil {
		r.t.Fatalf("fold: %v", err)
	}
	return c
}

// blocksOf lists series id's blocks by start, as "span day+hh:mm n" with day
// counted from the rig's Monday.
func (r *blockRig) blocksOf(id int64) []string {
	r.t.Helper()
	rows, err := r.conn.Query(r.ctx, `select span, span_start, n from metric_block where series_id = $1 order by span_start, span`, id)
	if err != nil {
		r.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var span string
		var start time.Time
		var n int
		if err := rows.Scan(&span, &start, &n); err != nil {
			r.t.Fatal(err)
		}
		d := start.Sub(r.week)
		out = append(out, fmt.Sprintf("%s %d+%02d:00 %d", span, int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour), n))
	}
	return out
}

// readBlocks reads every block of ids' series back as the generator's
// blocks, series i+1 being ids[i], by series, start and span.
func (r *blockRig) readBlocks(ids []int64) []storagetest.GenBlock {
	r.t.Helper()
	rows, err := r.conn.Query(r.ctx, `select series_id, span, span_start, offset_ms, value from metric_block
		where series_id = any($1) order by series_id, span_start, span`, ids)
	if err != nil {
		r.t.Fatal(err)
	}
	defer rows.Close()
	var out []storagetest.GenBlock
	for rows.Next() {
		var id int64
		var b storagetest.GenBlock
		var offsets []int32
		var values []float64
		if err := rows.Scan(&id, &b.Span, &b.SpanStart, &offsets, &values); err != nil {
			r.t.Fatal(err)
		}
		b.Series = slices.Index(ids, id) + 1
		b.SpanStart = b.SpanStart.UTC()
		for i, o := range offsets {
			b.Samples = append(b.Samples, storagetest.GenSample{Series: b.Series, TS: b.SpanStart.Add(time.Duration(o) * time.Millisecond), Value: values[i]})
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		r.t.Fatal(err)
	}
	return out
}

// sameBlocks is nil when got is want exactly: every block at its span and
// start, and every sample in order, timestamp and value bits alike.
func sameBlocks(want, got []storagetest.GenBlock) error {
	if len(got) != len(want) {
		return fmt.Errorf("got %d blocks, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Series != w.Series || g.Span != w.Span || !g.SpanStart.Equal(w.SpanStart) || len(g.Samples) != len(w.Samples) {
			return fmt.Errorf("block %d: got series %d's %s from %v of %d samples, want series %d's %s from %v of %d",
				i, g.Series, g.Span, g.SpanStart, len(g.Samples), w.Series, w.Span, w.SpanStart, len(w.Samples))
		}
		for j, ws := range w.Samples {
			gs := g.Samples[j]
			if !gs.TS.Equal(ws.TS) || math.Float64bits(gs.Value) != math.Float64bits(ws.Value) {
				return fmt.Errorf("block %d (%s from %v) sample %d: got %v %v, want %v %v", i, w.Span, w.SpanStart, j,
					gs.TS.Format(time.RFC3339Nano), gs.Value, ws.TS.Format(time.RFC3339Nano), ws.Value)
			}
		}
	}
	return nil
}

func (r *blockRig) verify() {
	r.t.Helper()
	bad, err := r.gw.VerifyMetricBlocks(r.ctx, blockLaneDays)
	if err != nil {
		r.t.Fatal(err)
	}
	if len(bad) > 0 {
		r.t.Errorf("%d blocks differ from their raw rows, the first %v", len(bad), bad[0])
	}
}

func (r *blockRig) count(sql string, args ...any) int {
	r.t.Helper()
	var n int
	if err := r.conn.QueryRow(r.ctx, sql, args...).Scan(&n); err != nil {
		r.t.Fatal(err)
	}
	return n
}

// TestBlockFoldHoldsTheSpanRule: a series-day of 4,080 samples is cut into
// hour blocks of 170, one of 170 to 4,079 is a day block, and the sparser
// days wait until their ISO week closes, then fold together into one week
// block from its Monday with no minimum. A pass folds no more days than it
// is given and says it is behind.
func TestBlockFoldHoldsTheSpanRule(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	a := r.series("", "observed")
	for d, n := range map[int]int{0: 4080, 1: 4079, 2: 170, 3: 169, 6: 1} {
		r.spaced(a, r.day(d), n)
	}
	c := r.fold(storage.BlockFold{MaxDays: 3})
	if c.Days != 3 || !c.Through.Equal(r.day(3)) || c.Weeks != 0 || !c.Behind {
		t.Fatalf("the first pass %+v, want 3 days folded through Thursday, no week, and behind", c)
	}
	var want []string
	for h := range 24 {
		want = append(want, fmt.Sprintf("hour 0+%02d:00 170", h))
	}
	want = append(want, "day 1+00:00 4079", "day 2+00:00 170")
	if got := r.blocksOf(a); !slices.Equal(got, want) {
		t.Fatalf("after three days the blocks are %v, want %v", got, want)
	}
	c = r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	if c.Behind || c.Weeks == 0 || !c.WeekThrough.After(r.day(7)) {
		t.Fatalf("the second pass %+v, want every closed day and week folded", c)
	}
	want = slices.Insert(want, 1, "week 0+00:00 170")
	if got := r.blocksOf(a); !slices.Equal(got, want) {
		t.Errorf("after the week closed the blocks are %v, want %v", got, want)
	}
	if again := r.fold(storage.BlockFold{MaxDays: blockLaneDays}); again.Blocks != 0 || again.Days > 1 {
		t.Errorf("a third pass %+v, want nothing folded twice", again)
	}
}

// TestTheFirstBlockMarksAreSeededAtAMonday: before the first pass the marks
// are seeded at the Monday that starts the week of the oldest metric day, so
// the week fold starts on a week's first day.
func TestTheFirstBlockMarksAreSeededAtAMonday(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	mustExec(t, r.conn, `delete from metric_block_mark`)
	a := r.series("", "observed")
	r.spaced(a, r.day(2), 170)
	c := r.fold(storage.BlockFold{MaxDays: 1})
	if c.Days != 1 || !c.Through.Equal(r.day(1)) || !c.WeekThrough.Equal(r.week) {
		t.Fatalf("the first pass %+v, want Monday %v folded and the week mark there", c, r.week)
	}
	if c = r.fold(storage.BlockFold{MaxDays: 2}); len(r.blocksOf(a)) != 1 {
		t.Errorf("after Wednesday %+v the blocks are %v, want its day block", c, r.blocksOf(a))
	}
}

// recordedSample is one sample of the recorded scenario, in the order it
// arrived.
type recordedSample struct {
	at time.Duration
	v  float64
}

// recordedScenario is a week's samples of one series as they arrived, out of
// timestamp order and with instants reported twice: Monday a day block's
// worth around its edges and its middle, and Wednesday and Sunday a few
// leftover samples for the week block, values at the extremes a float8 holds.
func recordedScenario() []recordedSample {
	h, ms := time.Hour, time.Millisecond
	var out []recordedSample
	for i := range 180 {
		out = append(out, recordedSample{time.Duration(i) * 7 * time.Minute, float64(i) / 10})
	}
	return append(out,
		recordedSample{0, -0.0},                // the day's first instant, again
		recordedSample{24*h - ms, 1e308},       // the day's last millisecond
		recordedSample{12*h + 1*ms, 5e-324},    // a denormal, a millisecond past a poll
		recordedSample{7 * time.Minute, 41.5},  // a second report of an instant
		recordedSample{7 * time.Minute, -41.5}, // and a third
		recordedSample{2*24*h + 9*h + 250*ms, 3},
		recordedSample{2*24*h + 9*h + 250*ms, 4},
		recordedSample{2*24*h + 8*h, 2}, // arrives after a later sample
		recordedSample{6*24*h + 23*h + 59*time.Minute + 59*time.Second + 999*ms, math.MaxFloat64},
	)
}

// TestBlocksRoundTripExactly: a recorded scenario landed through the ingest's
// own insert, and #1525's week of mixed cadences, fold into blocks that unnest
// to exactly their raw rows (timestamps, values and order, an instant reported
// twice included), and the readers over the raw rows answer as they did.
func TestBlocksRoundTripExactly(t *testing.T) {
	t.Parallel()
	t.Run("recorded", func(t *testing.T) {
		t.Parallel()
		r := newBlockRig(t)
		var samples []storagetest.GenSample
		for _, s := range recordedScenario() {
			ts := r.week.Add(s.at)
			if err := r.gw.MakeMetricDays(r.ctx, ts); err != nil {
				t.Fatal(err)
			}
			if err := r.gw.InsertMetricSamples(r.ctx, storage.PushWriter(), []storage.MetricSampleWrite{
				{OwnerKind: "component", OwnerID: "prune-c1", Key: "prune-rtt", Value: s.v, TS: ts}}); err != nil {
				t.Fatal(err)
			}
			samples = append(samples, storagetest.GenSample{Series: 1, TS: ts, Value: s.v})
		}
		slices.SortStableFunc(samples, func(a, b storagetest.GenSample) int { return a.TS.Compare(b.TS) })
		var id int64
		if err := r.conn.QueryRow(r.ctx, `select id from metric_series where provenance = 'observed'`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		readers := func() (any, any) {
			series, err := r.gw.ListMetricSeries(r.ctx, "component", "prune-c1", "prune-rtt", blockLaneDays*24*time.Hour, 10000)
			if err != nil {
				t.Fatal(err)
			}
			grid, err := r.gw.ListMetricBuckets(r.ctx, "component", "prune-c1", "prune-rtt", nil, blockLaneDays*24*time.Hour, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			// The grid and its covered edge move with the clock; the buckets
			// holding rows are what the fold could change.
			var buckets []storage.MetricBucket
			for _, b := range grid.Buckets {
				if b.Count > 0 {
					buckets = append(buckets, b)
				}
			}
			return series, buckets
		}
		series0, buckets0 := readers()
		r.fold(storage.BlockFold{MaxDays: blockLaneDays})
		if err := sameBlocks(storagetest.Blocks(samples), r.readBlocks([]int64{id})); err != nil {
			t.Error(err)
		}
		r.verify()
		if series1, buckets1 := readers(); !reflect.DeepEqual(series0, series1) || !reflect.DeepEqual(buckets0, buckets1) {
			t.Error("a reader over the raw rows answers differently once its days are folded")
		}
	})
	t.Run("mixed", func(t *testing.T) {
		t.Parallel()
		r := newBlockRig(t)
		spec := storagetest.MixedSpec(r.week, storagetest.GenSeed)
		ids := []int64{r.series("m1", "observed"), r.series("m2", "observed"), r.series("m3", "observed")}
		samples := storagetest.Generate(spec)
		r.land(ids, samples)
		r.fold(storage.BlockFold{MaxDays: blockLaneDays})
		want := storagetest.Blocks(samples)
		if err := sameBlocks(want, r.readBlocks(ids)); err != nil {
			t.Error(err)
		}
		spans := map[string]int{}
		for _, b := range want {
			spans[b.Span]++
		}
		if spans["day"] == 0 || spans["week"] == 0 {
			t.Errorf("the mixed week gave blocks %v, want day and week blocks", spans)
		}
		r.verify()
	})
}

// TestABlockFoldCancelledMidDayFoldsEachSeriesDayWholeOrNot: a pass that
// stops while it waits on a series' day leaves that series-day unfolded and
// every one before it folded, its mark between them, and the next pass folds
// from the mark.
func TestABlockFoldCancelledMidDayFoldsEachSeriesDayWholeOrNot(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	a, b := r.series("a", "observed"), r.series("b", "observed")
	for d := range 3 {
		r.spaced(a, r.day(d), 200)
		r.spaced(b, r.day(d), 200)
	}
	// Tuesday's fold of series b waits on another transaction's uncommitted
	// block for the same key, which is where the pass is cancelled.
	blocker, err := connectDSN(t, r.dsn).Begin(r.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(r.ctx, `insert into metric_block (series_id, span_start, span, n, offset_ms, value)
		values ($1, $2, 'day', 1, '{0}', '{0}')`, b, r.day(1)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	type result struct {
		c   storage.BlockCompaction
		err error
	}
	done := make(chan result, 1)
	go func() {
		c, err := r.gw.CompactMetricBlocks(ctx, storage.BlockFold{MaxDays: blockLaneDays, LaneDays: blockLaneDays, SeriesChunk: 1})
		done <- result{c, err}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if blocked, series, _, _, _, err := r.gw.MetricBlockMarks(r.ctx); err != nil {
			t.Fatal(err)
		} else if blocked.Equal(r.day(1)) && series == a {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the pass never folded series a's Tuesday")
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	cancel()
	res := <-done
	if res.err == nil || res.c.Days != 1 {
		t.Fatalf("a pass cancelled mid-day returned %+v, %v; want Monday folded and an error", res.c, res.err)
	}
	if got := r.blocksOf(a); !slices.Equal(got, []string{"day 0+00:00 200", "day 1+00:00 200"}) {
		t.Errorf("series a's blocks %v, want Monday and Tuesday", got)
	}
	if got := r.blocksOf(b); !slices.Equal(got, []string{"day 0+00:00 200"}) {
		t.Errorf("series b's blocks %v, want Monday only", got)
	}
	if err := blocker.Rollback(r.ctx); err != nil {
		t.Fatal(err)
	}
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	for _, id := range []int64{a, b} {
		if got := r.blocksOf(id); !slices.Equal(got, []string{"day 0+00:00 200", "day 1+00:00 200", "day 2+00:00 200"}) {
			t.Errorf("after the next pass series %d's blocks are %v, want its three days", id, got)
		}
	}
}

// TestIntendedSeriesAndPropertiesNeverBlock: an intended series and the
// property lane fold into no block. A calculated series blocks with its
// lineage (metric_block_lineage_test.go).
func TestIntendedSeriesAndPropertiesNeverBlock(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	// An intended series' rows land in metric_declared, never in metric; one
	// planted in metric anyway is still no observation to fold.
	intended := r.series("want", "intended")
	r.spaced(intended, r.day(0), 200)
	for i := range 200 {
		r.row("property", "component", r.day(1).Add(time.Duration(i)*time.Minute), "observed", float64(i))
	}
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	if n := r.count(`select count(*) from metric_block`); n != 0 {
		t.Errorf("%d blocks, want none", n)
	}
}

// blockFootprints is the bytes a sample for the block at each cadence
// (pg_total_relation_size after VACUUM, Postgres 18 defaults), with the
// series this test lands at it: #1525's counts, over one ISO week rather
// than its 30 days, about 600,000 samples a cadence. Fewer and the table's
// fixed pages (its free space and visibility maps, the index root) show in
// the figure. want is the figure since the block gained its lineage (#1534,
// ADR-1534): an observed block's three null lineage columns give its row a
// null bitmap, 8 bytes a row, which at 5 m, whose day blocks sit at the size
// four rows fill a page by, costs a page for every few blocks. measured is
// what #1525 measured before it.
var blockFootprints = []struct {
	cadence  time.Duration
	series   int
	want     float64
	measured float64
}{
	{10 * time.Second, 10, 7.84, 7.71},
	{30 * time.Second, 30, 7.22, 7.18},
	{time.Minute, 60, 7.38, 7.25},
	{5 * time.Minute, 300, 8.70, 7.84},
	{time.Hour, 3000, 8.36, 8.58},
}

// TestBlockFootprint reports what the blocks cost a sample at each product
// cadence over one ISO week of #1525's generated series, folded by the pass,
// and holds each within 10% of its want. The bytes are
// deterministic in the data and the Postgres version, so a figure that moves
// is the block's layout changing.
func TestBlockFootprint(t *testing.T) {
	t.Parallel()
	for _, fp := range blockFootprints {
		t.Run(fp.cadence.String(), func(t *testing.T) {
			t.Parallel()
			r := newBlockRig(t)
			ids := make([]int64, fp.series)
			for i := range ids {
				ids[i] = r.series(fmt.Sprint("g", i+1), "observed")
			}
			samples := storagetest.Generate(storagetest.GenSpec{Cadence: fp.cadence, Series: fp.series, Start: r.week,
				Span: 7 * 24 * time.Hour, Seed: storagetest.GenSeed})
			r.land(ids, samples)
			r.fold(storage.BlockFold{MaxDays: blockLaneDays})
			mustExec(t, r.conn, `vacuum analyze metric_block`)
			var bytes int64
			var n int
			if err := r.conn.QueryRow(r.ctx, `select pg_total_relation_size('metric_block'), (select coalesce(sum(n), 0) from metric_block)`).Scan(&bytes, &n); err != nil {
				t.Fatal(err)
			}
			if n != len(samples) {
				t.Fatalf("the blocks hold %d samples, want all %d", n, len(samples))
			}
			got := float64(bytes) / float64(n)
			spans := map[string]bool{}
			for _, d := range storagetest.DaySpans(samples) {
				spans[d.Span] = true
			}
			t.Logf("block footprint at %s: %.2f bytes a sample (%d samples, %d bytes; %s blocks; want %.2f, #1525 measured %.2f)",
				fp.cadence, got, n, bytes, strings.Join(slices.Sorted(mapKeys(spans)), ", "), fp.want, fp.measured)
			if math.Abs(got-fp.want) > 0.10*fp.want {
				t.Errorf("blocks cost %.2f bytes a sample at %s, more than 10%% from %.2f", got, fp.cadence, fp.want)
			}
		})
	}
}

func mapKeys(m map[string]bool) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}
