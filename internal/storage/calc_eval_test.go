package storage_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// evalRig is a calc rig with a component to derive on.
type evalRig struct {
	*calcRig
	comp string
}

func newEvalRig(t *testing.T) *evalRig {
	r := newCalcRig(t)
	c, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: "calc-target"}, r.all, r.all, r.all, r.all)
	if err != nil {
		t.Fatal(err)
	}
	return &evalRig{calcRig: r, comp: c.ID}
}

func (r *evalRig) metric(key, instance string, v float64) {
	r.t.Helper()
	if err := r.gw.InsertMetricSamples(r.ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: r.comp, Key: key,
		Instance: instance, Value: v, Source: "test", TS: time.Now()}}); err != nil {
		r.t.Fatal(err)
	}
}

func (r *evalRig) property(key, v string) {
	r.t.Helper()
	if err := r.gw.InsertPropertySamples(r.ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: r.comp, Key: key,
		Value: v, Source: "test", TS: time.Now()}}); err != nil {
		r.t.Fatal(err)
	}
}

func (r *evalRig) drain() int {
	r.t.Helper()
	n, err := r.gw.DrainCalcPending(r.ctx, 100)
	if err != nil {
		r.t.Fatal(err)
	}
	return n
}

func (r *evalRig) pending() int {
	r.t.Helper()
	return count(r.t, r.conn, `select count(*) from calc_pending`)
}

// calculated is the calculated rows of a metric output on the component, as
// value, source_rule and source_rule_version, oldest first.
func (r *evalRig) calculated(key string) []string {
	r.t.Helper()
	rows, err := r.conn.Query(r.ctx, `select value::text || ' ' || source_rule || ' ' || source_rule_version::text from metric
		where component_id = $1 and metric_type_id = (select id from metric_type where name = $2) and provenance = 'calculated'
		and instance = '' order by id`, r.comp, key)
	if err != nil {
		r.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			r.t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// TestCalcDerivesComponentSamples proves the evaluation loop (#996): a
// sample on a key a rule reads marks the rule on its owner in the
// sample-write transaction; the tick drains the mark, reduces the inputs'
// current values, and writes the output as a calculated sample naming the
// rule and its version; unchanged inputs write no row; a missing input writes
// nothing and is recorded in the rule's state.
func TestCalcDerivesComponentSamples(t *testing.T) {
	r := newEvalRig(t)
	if _, err := r.create(storage.CalcRuleSpec{Name: "temp-mean", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{As: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Reducer: "average"}, OutputKey: "avg-temp"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.create(storage.CalcRuleSpec{Name: "per-person", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{As: "t", Key: "room-temperature"}, {As: "n", Key: "occupancy-count"}},
		Reduce: rules.CalcReduce{Expr: "t * n"}, OutputKey: "temp-sum"}); err != nil {
		t.Fatal(err)
	}
	if r.pending() != 0 {
		t.Fatal("a mark before any sample")
	}
	// Two instances of the temperature, folded by the reducer; the cross-key
	// rule reads the aggregate instance only and has no occupancy yet.
	r.metric("room-temperature", "north", 20)
	r.metric("room-temperature", "south", 24)
	if n := r.pending(); n != 2 {
		t.Fatalf("%d marks after a sample both rules read, want 2", n)
	}
	if n := r.drain(); n != 2 || r.pending() != 0 {
		t.Fatalf("drained %d, %d left; want 2 drained, none left", n, r.pending())
	}
	if got := r.calculated("avg-temp"); len(got) != 1 || got[0] != "22 temp-mean 1" {
		t.Fatalf("avg-temp rows %v, want one calculated 22 by temp-mean v1", got)
	}
	if got := r.calculated("temp-sum"); len(got) != 0 {
		t.Fatalf("temp-sum rows %v with an input missing, want none", got)
	}
	if got := scalar(t, r.conn, `select s.missing::text || ' ' || (s.value is null)::text from calc_rule_state s join calc_rule c on c.id = s.rule_id
		where c.name = 'per-person'`); got != `["room-temperature", "occupancy-count"] true` {
		t.Fatalf("per-person state %q, want both inputs missing on the aggregate instance and no value", got)
	}

	// The same values again: marked, drained, no new row.
	r.metric("room-temperature", "north", 20)
	r.drain()
	if got := r.calculated("avg-temp"); len(got) != 1 {
		t.Fatalf("avg-temp rows %v after an unchanged input, want still one", got)
	}
	r.metric("room-temperature", "", 21)
	r.metric("occupancy-count", "", 3)
	r.drain()
	if got := r.calculated("temp-sum"); len(got) != 1 || got[0] != "63 per-person 1" {
		t.Fatalf("temp-sum rows %v, want one calculated 63", got)
	}
	if got := r.calculated("avg-temp"); len(got) != 2 || got[1] != "21.666666666666668 temp-mean 1" {
		t.Fatalf("avg-temp rows %v, want a second row over three instances", got)
	}
	if got := scalar(t, r.conn, `select s.missing::text || ' ' || s.value::text from calc_rule_state s join calc_rule c on c.id = s.rule_id
		where c.name = 'per-person'`); got != `[] 63` {
		t.Fatalf("per-person state %q, want nothing missing and 63", got)
	}

	// A rule disabled is not marked, and a mark it already had drains to
	// nothing.
	off := false
	r.metric("occupancy-count", "", 4)
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "per-person", storage.CalcRulePatch{Enabled: &off}, r.all); err != nil {
		t.Fatal(err)
	}
	r.drain()
	if got := r.calculated("temp-sum"); len(got) != 1 {
		t.Fatalf("a disabled rule wrote %v", got)
	}
	r.metric("occupancy-count", "", 5)
	if r.pending() != 0 {
		t.Fatal("a disabled rule was marked")
	}
}

// A property output is transition-only, and an event rule judges a derived
// sample on the output key in the tick's write.
func TestCalcPropertyOutputAndEventRule(t *testing.T) {
	r := newEvalRig(t)
	if _, err := r.create(storage.CalcRuleSpec{Name: "busy", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{As: "call", Key: "in-call"}, {As: "n", Key: "occupancy-count"}},
		Reduce: rules.CalcReduce{Expr: "call || n > 0"}, OutputKey: "busy-flag"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.gw.CreateEventRule(r.ctx, "", storage.EventRuleSpec{Name: "busy-now", SampleKey: "busy-flag",
		Fire: rules.Criteria{Expr: "value"}, Severity: "high", FireEventType: "condition-started"}, r.all); err != nil {
		t.Fatal(err)
	}
	r.property("in-call", "false")
	r.metric("occupancy-count", "", 0)
	r.drain()
	r.metric("occupancy-count", "", 0)
	r.drain()
	if n := count(t, r.conn, `select count(*) from property where component_id = '`+r.comp+`' and provenance = 'calculated'
		and source_rule = 'busy'`); n != 1 {
		t.Fatalf("%d calculated busy-flag rows after an unchanged false, want 1", n)
	}
	if n := count(t, r.conn, `select count(*) from alarm where cleared_at is null and component_id = '`+r.comp+`'`); n != 0 {
		t.Fatalf("%d open alarms while not busy", n)
	}
	r.property("in-call", "true")
	r.drain()
	if got := scalar(t, r.conn, `select value #>> '{}' from property where component_id = '`+r.comp+`' and source_rule = 'busy' order by id desc limit 1`); got != "true" {
		t.Fatalf("busy-flag reads %q, want true", got)
	}
	if n := count(t, r.conn, `select count(*) from alarm a join event_rule e on e.id = a.raised_by_rule_id
		where a.cleared_at is null and a.component_id = '`+r.comp+`' and e.name = 'busy-now'`); n != 1 {
		t.Fatalf("%d open busy-now alarms on the derived sample, want 1", n)
	}
}

// ADR-0069 at runtime: a derived sample marks downstream rules, never its
// own. A chain a -> b recomputes b once a's output changes, and a rule whose
// output is also one of its inputs' keys cannot exist (the cycle check), so
// the only self-mark possible is a rule reading another rule's output that
// it does not write; the test drives a chain and asserts no mark survives.
func TestCalcChainAndNoSelfMark(t *testing.T) {
	r := newEvalRig(t)
	if _, err := r.create(storage.CalcRuleSpec{Name: "first", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{As: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Expr: "t + 1"}, OutputKey: "avg-temp"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.create(storage.CalcRuleSpec{Name: "second", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{As: "a", Key: "avg-temp"}, {As: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Expr: "a * 2 + t"},
		OutputKey: "temp-twice"}); err != nil {
		t.Fatal(err)
	}
	r.metric("room-temperature", "", 10)
	// Both are marked by the observed sample.
	if n := r.pending(); n != 2 {
		t.Fatalf("%d marks, want 2", n)
	}
	// Drain one at a time until the queue is empty: first's write marks
	// second (again), never first.
	for i := 0; i < 5 && r.pending() > 0; i++ {
		if _, err := r.gw.DrainCalcPending(r.ctx, 1); err != nil {
			t.Fatal(err)
		}
		if n := count(t, r.conn, `select count(*) from calc_pending p join calc_rule c on c.id = p.rule_id where c.name = 'first'`); n != 0 {
			t.Fatalf("first is marked after a drain: its own write re-marked it")
		}
	}
	if r.pending() != 0 {
		t.Fatal("the chain did not settle")
	}
	if got := r.calculated("temp-twice"); len(got) == 0 || got[len(got)-1] != "32 second 1" {
		t.Fatalf("temp-twice rows %v, want the chain to end at 32 (a = 11)", got)
	}

	// A rule reading its own output can only exist outside the gateway (the
	// cycle check refuses it); written straight into the table, it still
	// never marks itself: one drain computes it once and leaves nothing.
	if _, err := r.conn.Exec(r.ctx, `insert into calc_rule (name, owner_kind, inputs, reduce, output_lane, output_metric_type_id)
		values ('self', 'component', '[{"as": "s", "key": "temp-sum"}]', '{"expr": "s + 1"}', 'metric',
		(select id from metric_type where name = 'temp-sum'))`); err != nil {
		t.Fatal(err)
	}
	r.metric("temp-sum", "", 1)
	if n := r.pending(); n != 1 {
		t.Fatalf("%d marks after a temp-sum sample, want self's one", n)
	}
	if n := r.drain(); n != 1 || r.pending() != 0 {
		t.Fatalf("drained %d, %d left: self's own write re-marked it", n, r.pending())
	}
	if got := r.calculated("temp-sum"); len(got) != 1 || got[0] != "2 self 1" {
		t.Fatalf("temp-sum rows %v, want self's one write of 2", got)
	}
}

// Two replicas draining one backlog at once compute every mark exactly
// once, and a restart mid-backlog loses nothing: the marks are rows.
func TestCalcDrainReplicasAndRestart(t *testing.T) {
	r := newEvalRig(t)
	if _, err := r.create(storage.CalcRuleSpec{Name: "temp-mean", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{As: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Reducer: "average"}, OutputKey: "avg-temp"}); err != nil {
		t.Fatal(err)
	}
	const owners = 12
	var ids []string
	for i := 0; i < owners; i++ {
		c, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: "rep-" + string(rune('a'+i))}, r.all, r.all, r.all, r.all)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
		if err := r.gw.InsertMetricSamples(r.ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: c.ID,
			Key: "room-temperature", Value: float64(i), TS: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	if n := r.pending(); n != owners {
		t.Fatalf("%d marks, want %d", n, owners)
	}
	// A restart mid-backlog: one gateway drains a few and closes; a fresh
	// one (a new process) finds the rest.
	first, err := storage.NewPG(r.ctx, r.dsn)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := first.DrainCalcPending(r.ctx, 3); err != nil || n != 3 {
		t.Fatalf("the first process drained %d (%v), want 3", n, err)
	}
	first.Close()
	if n := r.pending(); n != owners-3 {
		t.Fatalf("%d marks after the restart, want %d", n, owners-3)
	}
	// Two replicas drain the rest concurrently.
	a, err := storage.NewPG(r.ctx, r.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := storage.NewPG(r.ctx, r.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var wg sync.WaitGroup
	served := make([]int, 2)
	errs := make([]error, 2)
	for i, gw := range []*storage.PG{a, b} {
		wg.Add(1)
		go func(i int, gw *storage.PG) {
			defer wg.Done()
			for {
				n, err := gw.DrainCalcPending(context.Background(), 1)
				if err != nil {
					errs[i] = err
					return
				}
				if n == 0 {
					return
				}
				served[i] += n
			}
		}(i, gw)
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("replica errors %v", errs)
	}
	if served[0]+served[1] != owners-3 || r.pending() != 0 {
		t.Fatalf("replicas served %v, %d left; want %d in all, none left", served, r.pending(), owners-3)
	}
	if n := count(t, r.conn, `select count(*) from metric where provenance = 'calculated' and source_rule = 'temp-mean'`); n != owners {
		t.Fatalf("%d calculated rows, want exactly %d (one per owner)", n, owners)
	}
	if n := count(t, r.conn, `select count(*) from (select component_id from metric where provenance = 'calculated'
		group by component_id having count(*) > 1) d`); n != 0 {
		t.Fatalf("%d owners computed twice", n)
	}
}
