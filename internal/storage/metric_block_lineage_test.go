package storage_test

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A calculated series compacts into blocks as an observed one does (#1534,
// ADR-1534), each block carrying the one rule lineage its samples were
// written under: a span splits at every lineage change, the count rule holds
// for each lineage alone, a block unnests to its raw rows lineage and all,
// and a late calculated sample merges into the block of its own lineage or
// starts one.

// The lineages the planted calculated rows carry: a rule, the same rule
// renamed (a rename keeps the version), and the rule edited.
var (
	avgV1     = storage.BlockLineage{Rule: "blk-avg", RuleID: "0b1c0000-0000-4000-8000-000000001534", Version: 1}
	meanV1    = storage.BlockLineage{Rule: "blk-mean", RuleID: avgV1.RuleID, Version: 1}
	meanV2    = storage.BlockLineage{Rule: "blk-mean", RuleID: avgV1.RuleID, Version: 2}
	otherRule = storage.BlockLineage{Rule: "blk-avg", RuleID: "0b1c0000-0000-4000-8000-000000001535", Version: 1}
)

// spacedAs lands samples lo through hi-1 of the n spaced lays over day on
// series id, each with the value of its place, as calc rule l's output.
func (r *blockRig) spacedAs(id int64, day time.Time, n, lo, hi int, l storage.BlockLineage) {
	r.t.Helper()
	if err := r.gw.MakeMetricDays(r.ctx, day); err != nil {
		r.t.Fatal(err)
	}
	mustExec(r.t, r.conn, `insert into metric (series_id, ts, value, source_rule, source_rule_version, source_rule_id)
		select $1, $2::timestamptz + (i * 86400000::bigint / $3) * interval '1 millisecond', i, $6, $7, $8::uuid
		from generate_series($4::int, $5::int - 1) i order by i`, id, day, n, lo, hi, l.Rule, l.Version, l.RuleID)
}

// rawOf is series id's raw rows in [from, to), by ts then id, as the block
// read returns them.
func (r *blockRig) rawOf(id int64, from, to time.Time) []storage.BlockSample {
	r.t.Helper()
	rows, err := r.conn.Query(r.ctx, `select ts, value, source_rule, source_rule_id::text, source_rule_version from metric
		where series_id = $1 and ts >= $2 and ts < $3 order by ts, id`, id, from, to)
	if err != nil {
		r.t.Fatal(err)
	}
	defer rows.Close()
	var out []storage.BlockSample
	for rows.Next() {
		var s storage.BlockSample
		var rule, ruleID *string
		var version *int64
		if err := rows.Scan(&s.TS, &s.Value, &rule, &ruleID, &version); err != nil {
			r.t.Fatal(err)
		}
		if rule != nil || ruleID != nil || version != nil {
			s.Lineage = &storage.BlockLineage{}
			if rule != nil {
				s.Lineage.Rule = *rule
			}
			if ruleID != nil {
				s.Lineage.RuleID = *ruleID
			}
			if version != nil {
				s.Lineage.Version = *version
			}
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		r.t.Fatal(err)
	}
	return out
}

// blockSamples is series id's samples in [from, to) as its blocks hold them.
func (r *blockRig) blockSamples(id int64, from, to time.Time) []storage.BlockSample {
	r.t.Helper()
	got, err := r.gw.MetricBlockSamples(r.ctx, id, from, to)
	if err != nil {
		r.t.Fatal(err)
	}
	return got
}

// sameSamples is nil when got is want exactly: every sample in order, its
// timestamp, value bits and lineage alike.
func sameSamples(want, got []storage.BlockSample) error {
	if len(got) != len(want) {
		return fmt.Errorf("got %d samples, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if !g.TS.Equal(w.TS) || math.Float64bits(g.Value) != math.Float64bits(w.Value) || lineageName(g.Lineage) != lineageName(w.Lineage) {
			return fmt.Errorf("sample %d: got %s %v %s, want %s %v %s", i, g.TS.UTC().Format(time.RFC3339Nano), g.Value, lineageName(g.Lineage),
				w.TS.UTC().Format(time.RFC3339Nano), w.Value, lineageName(w.Lineage))
		}
	}
	return nil
}

// lineageName is a lineage as the tests name it, "observed" for none.
func lineageName(l *storage.BlockLineage) string {
	if l == nil {
		return "observed"
	}
	return fmt.Sprintf("%s/%s@%d", l.Rule, l.RuleID, l.Version)
}

// lineageBlocksOf lists series id's blocks by start, as blocksOf does, each
// with its lineage's rule and version.
func (r *blockRig) lineageBlocksOf(id int64) []string {
	r.t.Helper()
	rows, err := r.conn.Query(r.ctx, `select span, span_start, n, coalesce(source_rule || '@' || source_rule_version, 'observed')
		from metric_block where series_id = $1 order by span_start, span, source_rule_version, source_rule`, id)
	if err != nil {
		r.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var span, lineage string
		var start time.Time
		var n int
		if err := rows.Scan(&span, &start, &n, &lineage); err != nil {
			r.t.Fatal(err)
		}
		d := start.Sub(r.week)
		out = append(out, fmt.Sprintf("%s %d+%02d:00 %d %s", span, int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour), n, lineage))
	}
	if err := rows.Err(); err != nil {
		r.t.Fatal(err)
	}
	return out
}

// TestACalculatedDayFoldsByTheCountRuleWithItsLineage: a calculated series'
// days fold as an observed series' do, a dense day into hour blocks, a
// moderate one into a day block and a sparse one into its week's block, and
// every block carries the lineage of the rows it was folded from and unnests
// to exactly them.
func TestACalculatedDayFoldsByTheCountRuleWithItsLineage(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	calc := r.series("calc", "calculated")
	r.spacedAs(calc, r.day(0), 4080, 0, 4080, avgV1)
	r.spacedAs(calc, r.day(1), 170, 0, 170, avgV1)
	r.spacedAs(calc, r.day(3), 5, 0, 5, avgV1)
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	got := r.lineageBlocksOf(calc)
	var want []string
	for h := range 24 {
		want = append(want, fmt.Sprintf("hour 0+%02d:00 170 blk-avg@1", h))
	}
	want = slices.Insert(want, 1, "week 0+00:00 5 blk-avg@1")
	want = append(want, "day 1+00:00 170 blk-avg@1")
	if !slices.Equal(got, want) {
		t.Errorf("the calculated series' blocks are\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	end := r.week.AddDate(0, 0, 7)
	if err := sameSamples(r.rawOf(calc, r.week, end), r.blockSamples(calc, r.week, end)); err != nil {
		t.Errorf("the calculated blocks do not unnest to their raw rows: %v", err)
	}
	r.verify()
}

// TestARuleEditedMidDaySplitsItsBlocksAtTheLineage: a day whose samples
// change lineage partway (the rule renamed, which keeps its version; the rule
// edited, which bumps it; another rule of the same name) folds into a block
// for each lineage, each lineage counted alone: one with enough for a day
// block gets one, and one with too few waits for its week block, beside a
// day block of another lineage in the same day. No sample is read back under
// a lineage it was not written with.
func TestARuleEditedMidDaySplitsItsBlocksAtTheLineage(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	calc := r.series("calc", "calculated")
	r.spacedAs(calc, r.day(0), 400, 0, 200, avgV1)
	r.spacedAs(calc, r.day(0), 400, 200, 400, meanV1)
	r.spacedAs(calc, r.day(1), 400, 0, 250, meanV1)
	r.spacedAs(calc, r.day(1), 400, 250, 400, meanV2)
	r.spacedAs(calc, r.day(2), 300, 0, 300, meanV2)
	r.spacedAs(calc, r.day(3), 400, 0, 5, meanV2)
	r.spacedAs(calc, r.day(3), 400, 5, 180, otherRule)
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	got := r.lineageBlocksOf(calc)
	want := []string{
		"day 0+00:00 200 blk-avg@1",
		"day 0+00:00 200 blk-mean@1",
		// meanV2's 150 of day 1 and 5 of day 3; day 2's 300 are its day block.
		"week 0+00:00 155 blk-mean@2",
		"day 1+00:00 250 blk-mean@1",
		"day 2+00:00 300 blk-mean@2",
		"day 3+00:00 175 blk-avg@1",
	}
	if !slices.Equal(got, want) {
		t.Errorf("the calculated series' blocks are\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if n := r.count(`select count(*) from metric_block where series_id = $1 and source_rule_id = $2::uuid`, calc, otherRule.RuleID); n != 1 {
		t.Errorf("the other rule of the same name has %d blocks, want its own one", n)
	}
	end := r.week.AddDate(0, 0, 7)
	if err := sameSamples(r.rawOf(calc, r.week, end), r.blockSamples(calc, r.week, end)); err != nil {
		t.Errorf("a sample comes back under another lineage than its raw row's: %v", err)
	}
	r.verify()
}

// TestBlockSamplesReadTheRawRowsBack: the block read returns a series'
// samples in a range exactly as its raw rows hold them, lineage included: an
// observed series' with none, an instant reported twice twice and in landing
// order, a calculated series' with its rule's, and only the range's own.
// Observed blocks carry no lineage.
func TestBlockSamplesReadTheRawRowsBack(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	obs, calc := r.series("obs", "observed"), r.series("calc", "calculated")
	r.spaced(obs, r.day(1), 200)
	r.plantAt(obs, spacedAt(r.day(1), 200, 100), -1)
	r.spacedAs(calc, r.day(1), 400, 0, 180, avgV1)
	r.spacedAs(calc, r.day(1), 400, 180, 400, meanV2)
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	from, to := r.day(1).Add(6*time.Hour), r.day(1).Add(18*time.Hour)
	for _, id := range []int64{obs, calc} {
		want := r.rawOf(id, from, to)
		if len(want) == 0 {
			t.Fatalf("series %d has no raw rows in the range", id)
		}
		if err := sameSamples(want, r.blockSamples(id, from, to)); err != nil {
			t.Errorf("series %d's block read differs from its raw rows: %v", id, err)
		}
	}
	if n := r.count(`select count(*) from metric_block where series_id = $1`, obs); n != 1 {
		t.Fatalf("the observed series has %d blocks, want its day block", n)
	}
	if n := r.count(`select count(*) from metric_block where series_id = $1
		and (source_rule is not null or source_rule_id is not null or source_rule_version is not null)`, obs); n != 0 {
		t.Errorf("%d observed blocks carry a lineage, want none", n)
	}
}

// calcRig creates calc rule blk-rule over prune-rtt, its output blk-avg on
// components inside the rig's window, and returns it.
func (r *blockRig) calcRig() *storage.CalcRule {
	r.t.Helper()
	all := scope.Set{All: true}
	if _, err := r.gw.CreateMetricType(r.ctx, "", storage.MetricTypeSpec{Name: "blk-avg", DataType: "float"}); err != nil {
		r.t.Fatal(err)
	}
	mustExec(r.t, r.conn, `update metric_type set retention_days = $1 where name = 'blk-avg'`, blockLaneDays)
	rule, err := r.gw.CreateCalcRule(r.ctx, "", storage.CalcRuleSpec{Name: "blk-rule", OwnerKind: "component",
		Inputs: []rules.CalcInput{{Alias: "x", Key: "prune-rtt"}}, Reduce: rules.CalcReduce{Expr: "avg(x)"}, OutputKey: "blk-avg"}, all)
	if err != nil {
		r.t.Fatal(err)
	}
	return rule
}

// calcAt is blk-avg on prune-c1 at ts, as rule's output.
func calcAt(ts time.Time, v float64) storage.MetricSampleWrite {
	return storage.MetricSampleWrite{OwnerKind: "component", OwnerID: "prune-c1", Key: "blk-avg", Value: v, TS: ts}
}

// writeCalc lands evs as rule's output, one batch, in their days' partitions.
func (r *blockRig) writeCalc(rule *storage.CalcRule, evs ...storage.MetricSampleWrite) {
	r.t.Helper()
	for _, ev := range evs {
		if err := r.gw.MakeMetricDays(r.ctx, ev.TS); err != nil {
			r.t.Fatal(err)
		}
	}
	if err := r.gw.WriteCalcSamples(r.ctx, rule, evs); err != nil {
		r.t.Fatal(err)
	}
}

// TestALateCalculatedSampleMergesIntoTheBlockOfItsLineage: a calculated
// sample landing late, through a calc rule's own write, merges into the
// block of its own lineage that covers it; one written after the rule was
// edited finds no block of its new version in its day and starts its week's
// block of that version, which a second such sample in another day joins.
// The calculated series marks no trend hour: trends stay observed.
func TestALateCalculatedSampleMergesIntoTheBlockOfItsLineage(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	rule := r.calcRig()
	var evs []storage.MetricSampleWrite
	for i := range 200 {
		evs = append(evs, calcAt(spacedAt(r.day(1), 200, i), float64(i)))
	}
	for i := range 5 {
		evs = append(evs, calcAt(spacedAt(r.day(3), 5, i), float64(i)))
	}
	r.writeCalc(rule, evs...)
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	var calc int64
	if err := r.conn.QueryRow(r.ctx, `select id from metric_series where provenance = 'calculated'`).Scan(&calc); err != nil {
		t.Fatal(err)
	}
	want := []string{"week 0+00:00 5 blk-rule@1", "day 1+00:00 200 blk-rule@1"}
	if got := r.lineageBlocksOf(calc); !slices.Equal(got, want) {
		t.Fatalf("before the late samples the blocks are %v, want %v", got, want)
	}
	r.writeCalc(rule, calcAt(spacedAt(r.day(1), 200, 10).Add(time.Millisecond), -1))
	edited, err := r.gw.UpdateCalcRule(r.ctx, "", rule.ID, storage.CalcRulePatch{Reduce: &rules.CalcReduce{Expr: "max(x)"}}, scope.Set{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Version == rule.Version {
		t.Fatalf("the edit left the rule at version %d", edited.Version)
	}
	r.writeCalc(edited, calcAt(r.day(1).Add(7*time.Hour), -2))
	r.writeCalc(edited, calcAt(r.day(4).Add(time.Hour), -3))
	want = []string{"week 0+00:00 5 blk-rule@1", fmt.Sprintf("week 0+00:00 2 blk-rule@%d", edited.Version), "day 1+00:00 201 blk-rule@1"}
	if got := r.lineageBlocksOf(calc); !slices.Equal(got, want) {
		t.Errorf("after the late samples the blocks are %v, want %v", got, want)
	}
	end := r.week.AddDate(0, 0, 7)
	if err := sameSamples(r.rawOf(calc, r.week, end), r.blockSamples(calc, r.week, end)); err != nil {
		t.Errorf("the merged blocks do not unnest to their raw rows: %v", err)
	}
	if n := r.count(`select count(*) from metric_trend_dirty where series_id = $1`, calc); n != 0 {
		t.Errorf("the calculated series marked %d trend hours, want none: trends stay observed", n)
	}
	r.verify()
}

// calcFootprint is the bytes a sample measured for a dense calculated series
// (#1534): #1525's 30 s cadence, its 30 series, over one ISO week, the rule
// edited midweek. Its lineage (the rule's name, version and id, in the row
// and its key) costs a block about 70 bytes, a few hundredths of a byte a
// sample over an observed series at the same cadence.
const calcFootprint = 7.25

// TestCalculatedBlockFootprint reports what a dense calculated series'
// blocks cost a sample, its rule edited midweek so each series splits a day
// at the version change, beside what its raw rows cost, and holds the
// blocks within 10% of calcFootprint.
func TestCalculatedBlockFootprint(t *testing.T) {
	t.Parallel()
	const series = 30
	r := newBlockRig(t)
	ids := make([]int64, series)
	for i := range ids {
		ids[i] = r.series(fmt.Sprint("c", i+1), "calculated")
	}
	samples := storagetest.Generate(storagetest.GenSpec{Cadence: 30 * time.Second, Series: series, Start: r.week,
		Span: 7 * 24 * time.Hour, Seed: storagetest.GenSeed})
	var days []time.Time
	sid, ts, values := make([]int64, len(samples)), make([]time.Time, len(samples)), make([]float64, len(samples))
	for i, s := range samples {
		sid[i], ts[i], values[i] = ids[s.Series-1], s.TS, s.Value
		if d := s.TS.UTC().Truncate(24 * time.Hour); !slices.ContainsFunc(days, d.Equal) {
			days = append(days, d)
		}
	}
	if err := r.gw.MakeMetricDays(r.ctx, days...); err != nil {
		t.Fatal(err)
	}
	edit := r.day(3).Add(12 * time.Hour)
	mustExec(t, r.conn, `insert into metric (series_id, ts, value, source_rule, source_rule_version, source_rule_id)
		select s, t, v, $5, case when t < $4 then 1 else 2 end, $6::uuid
		from unnest($1::bigint[], $2::timestamptz[], $3::float8[]) with ordinality as u(s, t, v, o) order by o`,
		sid, ts, values, edit, avgV1.Rule, avgV1.RuleID)
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	mustExec(t, r.conn, `vacuum analyze metric_block`)
	mustExec(t, r.conn, `vacuum analyze metric`)
	var bytes, raw int64
	var n, blocks int
	if err := r.conn.QueryRow(r.ctx, `select pg_total_relation_size('metric_block'), (select coalesce(sum(n), 0) from metric_block),
		(select count(*) from metric_block),
		(select coalesce(sum(pg_total_relation_size(inhrelid)), 0) from pg_inherits where inhparent = 'metric'::regclass)`).Scan(&bytes, &n, &blocks, &raw); err != nil {
		t.Fatal(err)
	}
	if n != len(samples) {
		t.Fatalf("the blocks hold %d samples, want all %d", n, len(samples))
	}
	if split := r.count(`select count(*) from metric_block where span_start = $1 and span = 'day'`, r.day(3)); split != 2*series {
		t.Errorf("the edit's day has %d day blocks, want %d, one a version for each series", split, 2*series)
	}
	got := float64(bytes) / float64(n)
	t.Logf("calculated block footprint at 30s: %.2f bytes a sample (%d samples, %d blocks, %d bytes); its raw rows %.2f bytes a sample (%d bytes)",
		got, n, blocks, bytes, float64(raw)/float64(n), raw)
	if math.Abs(got-calcFootprint) > 0.10*calcFootprint {
		t.Errorf("calculated blocks cost %.2f bytes a sample, more than 10%% from %.2f", got, calcFootprint)
	}
}
