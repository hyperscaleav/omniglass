package storage_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The readers the series read carries (#1573, ADR-1530): the current value,
// settlement's latest observation, an event rule's priming and its stale
// check, the backtest replay, a calc rule's windowed and current inputs and
// the trend recompute each answer the same on a recorded scenario before its
// week folds into blocks, once it has, and again once the raw rows the blocks
// cover are gone, which only a reader that takes a folded day from its blocks
// can. The fold moves no raw row.

// recordReaderScenario lands the recorded scenario on series a and b of
// prune-rtt: on a, day 1 cut into hour blocks, day 3 into a day block and
// day 5 into the week block, each day's values climbing from 0 (spaced); on
// b, day 3 alone. Nothing lands after the week, so every series' current
// value is a sample the fold takes.
func (r *blockRig) recordReaderScenario() (a, b int64) {
	r.t.Helper()
	a, b = r.series("", "observed"), r.series("b", "observed")
	r.spaced(a, r.day(1), 4300)
	r.spaced(a, r.day(3), 170)
	r.spaced(a, r.day(5), 5)
	r.spaced(b, r.day(3), 40)
	return a, b
}

// foldKeepingRaw folds the rig's week and fails unless every raw row of ids
// is still in place.
func (r *blockRig) foldKeepingRaw(ids ...int64) {
	r.t.Helper()
	rows := r.count(`select count(*) from metric where series_id = any($1)`, ids)
	r.fold(storage.BlockFold{MaxDays: blockLaneDays})
	if n := r.count(`select count(*) from metric where series_id = any($1)`, ids); n != rows {
		r.t.Fatalf("the fold left %d raw rows of %d", n, rows)
	}
	if n := r.count(`select count(*) from metric_block where series_id = any($1)`, ids); n == 0 {
		r.t.Fatal("the fold wrote no block")
	}
}

// render is v as JSON, for a comparison that names what differs.
func render(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// sameRead fails the test where got differs from want, naming the reader.
func sameRead(t *testing.T, when, reader, want, got string) {
	t.Helper()
	if got != want {
		t.Errorf("%s, %s differs:\n got  %s\n want %s", when, reader, got, want)
	}
}

// currentValues is every current-value read of the scenario's series, by
// reader: the newest observed sample of the key and of instance b, the
// owner's effective metrics and settlement's latest observation of each
// instance.
func (r *blockRig) currentValues() map[string]string {
	r.t.Helper()
	out := map[string]string{}
	latest, err := r.gw.LatestMetric(r.ctx, "prune-c1", "prune-rtt")
	if err != nil {
		r.t.Fatal(err)
	}
	out["the newest sample"] = render(r.t, latest)
	inst, err := r.gw.LatestMetricInstance(r.ctx, "prune-c1", "prune-rtt", "b")
	if err != nil {
		r.t.Fatal(err)
	}
	out["instance b's newest sample"] = render(r.t, inst)
	eff, err := r.gw.EffectiveMetrics(r.ctx, "component", "prune-c1", scope.Set{All: true})
	if err != nil {
		r.t.Fatal(err)
	}
	out["the effective metrics"] = render(r.t, eff)
	for _, instance := range []string{"", "b"} {
		cv, err := r.gw.ExportLatestMetricValue(r.ctx, "component", "prune-c1", "prune-rtt", instance, "observed")
		if err != nil {
			r.t.Fatal(err)
		}
		out[fmt.Sprintf("settlement's observation of instance %q", instance)] = render(r.t, cv)
	}
	return out
}

// TestTheCurrentValueReadsTheSameOnceFolded: a series with no raw row newer
// than its folded week still has the current value it had (ADR-0079), in
// every read of it, once the week's raw rows are gone.
func TestTheCurrentValueReadsTheSameOnceFolded(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	a, b := r.recordReaderScenario()
	before := r.currentValues()
	if before["the newest sample"] == "null" || before["instance b's newest sample"] == "null" {
		t.Fatalf("the scenario has no current value: %v", before)
	}
	r.foldKeepingRaw(a, b)
	after := r.currentValues()
	for reader, want := range before {
		sameRead(t, "once the week folds", reader, want, after[reader])
	}
	r.dropCovered(a, b)
	dropped := r.currentValues()
	for reader, want := range before {
		sameRead(t, "with the folded week's raw rows gone", reader, want, dropped[reader])
	}
}

// stopAt stops the database's clock at at (a zero at restarts it at the real
// time) and opens the gateway afresh, so no pooled connection keeps a plan of
// the old clock. The rig must have been pinned at its start (newBlockRigAt).
func (r *blockRig) stopAt(at time.Time) {
	r.t.Helper()
	if at.IsZero() {
		r.now = storagetest.PinClock(r.t, r.dsn, time.Now())
	} else {
		r.now = storagetest.StopClock(r.t, r.dsn, at)
	}
	gw, err := storage.NewPG(r.ctx, r.dsn)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(gw.Close)
	r.gw = gw
}

// readerInstants are the instants the rule reads run at, the clock stopped
// at each: inside the day the hour blocks cut, the day the day block holds
// and the day the week block holds, each after an hour holding samples.
func (r *blockRig) readerInstants() []time.Time {
	return []time.Time{r.day(1).Add(12 * time.Hour), r.day(3).Add(10 * time.Hour), r.day(5).Add(10 * time.Hour)}
}

// ruleReads is what the backtest replay and a calc rule's preview, over a
// windowed input and over the current value, answer at each of the reader
// instants, by reader; the clock is left running at the real time.
func (r *blockRig) ruleReads() map[string]string {
	r.t.Helper()
	all := scope.Set{All: true}
	out := map[string]string{}
	replay := storage.RulePreviewSpec{SampleKey: "prune-rtt", Fire: criteria("gt", 3),
		Selector: storage.EventRuleSelector{Components: []string{"prune-c1"}}}
	calcs := map[string]storage.CalcRuleSpec{}
	for name, in := range map[string]rules.CalcInput{
		"the windowed calc": {Alias: "x", Key: "prune-rtt", Window: int(rules.MaxCalcWindow / time.Second)},
		"the current calc":  {Alias: "x", Key: "prune-rtt"},
	} {
		calcs[name] = storage.CalcRuleSpec{Name: "readers-avg", OwnerKind: rules.OwnerComponent,
			Inputs: []rules.CalcInput{in}, Reduce: rules.CalcReduce{Expr: "avg(x)"}, OutputKey: "blk-avg"}
	}
	for _, at := range r.readerInstants() {
		r.stopAt(at)
		p, err := r.gw.PreviewEventRule(r.ctx, replay, all)
		if err != nil {
			r.t.Fatal(err)
		}
		out["the backtest replay at "+at.Format(time.RFC3339)] = render(r.t, p)
		for name, spec := range calcs {
			c, err := r.gw.PreviewCalcRule(r.ctx, spec, all, all)
			if err != nil {
				r.t.Fatal(err)
			}
			out[name+" at "+at.Format(time.RFC3339)] = render(r.t, c)
		}
	}
	r.stopAt(time.Time{})
	return out
}

// TestTheRuleReadsReadTheSameOnceFolded: the backtest replay of an event
// rule and a calc rule's windowed and current inputs reach the same edges,
// series in force and values before the fold, once the week has folded and
// once its raw rows are gone.
func TestTheRuleReadsReadTheSameOnceFolded(t *testing.T) {
	t.Parallel()
	r := newBlockRigAt(t, time.Now())
	if _, err := r.gw.CreateMetricType(r.ctx, "", storage.MetricTypeSpec{Name: "blk-avg", DataType: "float"}); err != nil {
		t.Fatal(err)
	}
	a, b := r.recordReaderScenario()
	before := r.ruleReads()
	for reader, got := range before {
		var p struct {
			Edges  []storage.RulePreviewEdge
			Owners []storage.CalcPreviewOwner
		}
		if err := json.Unmarshal([]byte(got), &p); err != nil {
			t.Fatal(err)
		}
		if len(p.Edges) == 0 && (len(p.Owners) == 0 || p.Owners[0].Value == nil) {
			t.Errorf("%s judged nothing: %s", reader, got)
		}
	}
	r.foldKeepingRaw(a, b)
	folded := r.ruleReads()
	for reader, want := range before {
		sameRead(t, "once the week folds", reader, want, folded[reader])
	}
	r.dropCovered(a, b)
	dropped := r.ruleReads()
	for reader, want := range before {
		sameRead(t, "with the folded week's raw rows gone", reader, want, dropped[reader])
	}
}

// ruleInForce is how many series event rule name holds in force.
func (r *blockRig) ruleInForce(name string) int {
	r.t.Helper()
	return r.count(`select count(*) from event_rule_condition c join event_rule e on e.id = c.rule_id where e.name = $1`, name)
}

// lowRule creates an event rule that fires on a prune-rtt below 10 (and the
// event type it fires, the first time), primed from each series' current
// value as a save primes it.
func (r *blockRig) lowRule(name string) {
	r.t.Helper()
	if r.count(`select count(*) from event_type where name = 'readers-low'`) == 0 {
		if _, err := r.gw.CreateEventType(r.ctx, "", storage.EventTypeSpec{Name: "readers-low"}); err != nil {
			r.t.Fatal(err)
		}
	}
	if _, err := r.gw.CreateEventRule(r.ctx, "", storage.EventRuleSpec{Name: name, SampleKey: "prune-rtt", Fire: criteria("lt", 10),
		Selector: storage.EventRuleSelector{Components: []string{"prune-c1"}}, FireEventType: "readers-low"}, scope.Set{All: true}); err != nil {
		r.t.Fatal(err)
	}
}

// TestAnEventRuleJudgesTheSameOnceFolded: an event rule primes from a
// series' current value, and a sample older than it is history that decides
// nothing, the same with the series' newest day folded and its raw rows gone
// as before the fold: series a's newest sample (4, day 5's last) holds the
// rule in force and a late 50 earlier that day clears nothing; instance b's
// newest (39) holds nothing.
func TestAnEventRuleJudgesTheSameOnceFolded(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	a, b := r.recordReaderScenario()
	r.lowRule("low-before")
	if n := r.ruleInForce("low-before"); n != 1 {
		t.Fatalf("before the fold the rule primes %d series in force, want 1", n)
	}
	r.ingest(sampleOf("", r.day(5).Add(time.Hour), 50))
	if n := r.ruleInForce("low-before"); n != 1 {
		t.Fatalf("before the fold a late sample leaves %d series in force, want 1: it is history", n)
	}
	r.foldKeepingRaw(a, b)
	r.dropCovered(a, b)
	r.lowRule("low-after")
	if n := r.ruleInForce("low-after"); n != 1 {
		t.Errorf("with the folded week's raw rows gone the rule primes %d series in force, want 1", n)
	}
	r.ingest(sampleOf("", r.day(5).Add(2*time.Hour), 60))
	for _, name := range []string{"low-before", "low-after"} {
		if n := r.ruleInForce(name); n != 1 {
			t.Errorf("with the folded week's raw rows gone a late sample leaves %s %d series in force, want 1: it is history", name, n)
		}
	}
}

// dirtyHours are the scenario's hours the trend recompute is held to: in
// series a's hour-blocked day, its day-blocked day and its week-blocked day
// (one holding a sample and one holding none), and in instance b's day.
func (r *blockRig) dirtyHours() []time.Time {
	return []time.Time{r.day(1).Add(5 * time.Hour), r.day(1).Add(23 * time.Hour), r.day(3).Add(5 * time.Hour),
		r.day(5).Add(9 * time.Hour), r.day(5).Add(3 * time.Hour)}
}

// recomputeTrends marks every dirty hour of ids dirty, as a late sample
// would, recomputes them and returns their trend rows.
func (r *blockRig) recomputeTrends(ids ...int64) []string {
	r.t.Helper()
	mustExec(r.t, r.conn, `insert into metric_trend_dirty (series_id, hour)
		select s, h from unnest($1::bigint[]) s cross join unnest($2::timestamptz[]) h on conflict do nothing`, ids, r.dirtyHours())
	if _, err := r.gw.CompactMetricTrends(r.ctx, 1, blockLaneDays); err != nil {
		r.t.Fatal(err)
	}
	if n := r.count(`select count(*) from metric_trend_dirty`); n != 0 {
		r.t.Fatalf("%d dirty hours left after the recompute", n)
	}
	var out []string
	for _, id := range ids {
		for _, h := range r.dirtyHours() {
			out = append(out, fmt.Sprintf("%d %s: %s", id, h.Format(time.RFC3339), r.trendRow(id, h)))
		}
	}
	return out
}

// TestTheTrendRecomputeReadsTheSameOnceFolded: a dirtied hour recomputes
// from the series read to the row it held recomputed from raw rows, whatever
// block covers it, once its week has folded and once its raw rows are gone.
func TestTheTrendRecomputeReadsTheSameOnceFolded(t *testing.T) {
	t.Parallel()
	r := newBlockRig(t)
	a, b := r.recordReaderScenario()
	before := r.recomputeTrends(a, b)
	for i, row := range before {
		want := r.rawTrendRow([]int64{a, b}[i/len(r.dirtyHours())], r.dirtyHours()[i%len(r.dirtyHours())])
		if want == "0" {
			want = "" // an hour holding no sample holds no trend row
		}
		if !strings.HasSuffix(row, ": "+want) {
			t.Fatalf("before the fold the recompute gives %q, want the raw rows' %q", row, want)
		}
	}
	r.foldKeepingRaw(a, b)
	if got := r.recomputeTrends(a, b); !slices.Equal(got, before) {
		t.Errorf("once the week folds the recompute gives\n%v\nwant\n%v", got, before)
	}
	r.dropCovered(a, b)
	if got := r.recomputeTrends(a, b); !slices.Equal(got, before) {
		t.Errorf("with the folded week's raw rows gone the recompute gives\n%v\nwant\n%v", got, before)
	}
}
