package storage_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
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
	if err := r.gw.InsertMetricSamples(r.ctx, storage.PushWriter(), []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: r.comp, Key: key,
		Instance: instance, Value: v, Source: "test", TS: time.Now()}}); err != nil {
		r.t.Fatal(err)
	}
}

func (r *evalRig) property(key, v string) {
	r.t.Helper()
	if err := r.gw.InsertPropertySamples(r.ctx, storage.PushWriter(), []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: r.comp, Key: key,
		Value: v, Source: "test", TS: time.Now()}}); err != nil {
		r.t.Fatal(err)
	}
}

func (r *evalRig) drain() int {
	r.t.Helper()
	d, err := r.gw.DrainCalcPending(r.ctx, 100)
	if err != nil {
		r.t.Fatal(err)
	}
	return d.Served
}

func (r *evalRig) pending() int {
	r.t.Helper()
	return count(r.t, r.conn, `select count(*) from calc_pending`)
}

// calculated is the calculated rows of a metric output on the component, as
// value, source_rule and source_rule_version, oldest first.
func (r *evalRig) calculated(key string) []string {
	r.t.Helper()
	rows, err := r.conn.Query(r.ctx, `select value::text || ' ' || source_rule || ' ' || source_rule_version::text from `+storagetest.MetricRows+` metric
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
		Inputs: []rules.CalcInput{{Alias: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Expr: "avg(t)"}, OutputKey: "avg-temp"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.create(storage.CalcRuleSpec{Name: "per-person", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "t", Key: "room-temperature"}, {Alias: "n", Key: "occupancy-count"}},
		Reduce: rules.CalcReduce{Expr: "avg(t) * max(n)"}, OutputKey: "temp-sum"}); err != nil {
		t.Fatal(err)
	}
	if r.pending() != 0 {
		t.Fatal("a mark before any sample")
	}
	// Two instances of the temperature, averaged over the input's list; the
	// cross-key rule reads both too, and has no occupancy yet.
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
		where c.name = 'per-person'`); got != `["occupancy-count"] true` {
		t.Fatalf("per-person state %q, want occupancy-count missing and no value", got)
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
	if got := r.calculated("temp-sum"); len(got) != 1 || got[0] != "66 per-person 1" {
		t.Fatalf("temp-sum rows %v, want one calculated 66 (the named instances averaged, times 3)", got)
	}
	if got := r.calculated("avg-temp"); len(got) != 1 {
		t.Fatalf("avg-temp rows %v, want still one: the reducer folds the named instances, not the aggregate beside them", got)
	}
	if got := scalar(t, r.conn, `select s.missing::text || ' ' || s.value::text from calc_rule_state s join calc_rule c on c.id = s.rule_id
		where c.name = 'per-person'`); got != `[] 66` {
		t.Fatalf("per-person state %q, want nothing missing and 66", got)
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

	// Deleting a rule keeps its history: the samples it wrote stay, with
	// the name, version and id of the rule that wrote them.
	if err := r.gw.DeleteCalcRule(r.ctx, "", "per-person", r.all); err != nil {
		t.Fatal(err)
	}
	if got := r.calculated("temp-sum"); len(got) != 1 || got[0] != "66 per-person 1" {
		t.Fatalf("temp-sum rows %v after the rule's delete, want its one calculated 66 kept", got)
	}
	if n := count(t, r.conn, `select count(*) from `+storagetest.MetricRows+` metric where component_id = '`+r.comp+`' and source_rule = 'per-person'
		and source_rule_id is not null`); n != 1 {
		t.Fatalf("%d temp-sum rows keep the deleted rule's id, want 1", n)
	}
}

// A property output is transition-only, and an event rule judges a derived
// sample on the output key in the tick's write.
func TestCalcPropertyOutputAndEventRule(t *testing.T) {
	r := newEvalRig(t)
	if _, err := r.create(storage.CalcRuleSpec{Name: "busy", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "call", Key: "in-call"}, {Alias: "n", Key: "occupancy-count"}},
		Reduce: rules.CalcReduce{Expr: "any(call) || max(n) > 0"}, OutputKey: "busy-flag"}); err != nil {
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
		Inputs: []rules.CalcInput{{Alias: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Expr: "max(t) + 1"}, OutputKey: "avg-temp"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.create(storage.CalcRuleSpec{Name: "second", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "a", Key: "avg-temp"}, {Alias: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Expr: "max(a) * 2 + max(t)"},
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
	// never marks itself: one drain computes it once and leaves nothing. Its
	// output is its own (the calc writer class, ADR-0161), so no other writer
	// lands a temp-sum sample to mark it: the sample and the mark it would
	// leave are planted the same way.
	if _, err := r.conn.Exec(r.ctx, `with c as (insert into calc_rule (name, owner_kind, reduce, output_lane, output_metric_type_id)
			values ('self', 'component', '{"expr": "max(s) + 1"}', 'metric', (select id from metric_type where name = 'temp-sum')) returning id)
		insert into calc_rule_input (rule_id, position, alias, lane, metric_type_id)
		select c.id, 0, 's', 'metric', (select id from metric_type where name = 'temp-sum') from c`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.conn.Exec(r.ctx, storagetest.WideMetricInsert(`ts, owner_kind, component_id, metric_type_id, instance, value, provenance, source`,
		`values (now(), 'component', $1::uuid, (select id from metric_type where name = 'temp-sum'), '', 1, 'observed', 'test')`), r.comp); err != nil {
		t.Fatal(err)
	}
	if _, err := r.conn.Exec(r.ctx, `insert into calc_pending (rule_id, owner_kind, component_id)
		values ((select id from calc_rule where name = 'self'), 'component', $1::uuid)`, r.comp); err != nil {
		t.Fatal(err)
	}
	if n := r.pending(); n != 1 {
		t.Fatalf("%d marks after self's planted mark, want its one", n)
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
		Inputs: []rules.CalcInput{{Alias: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Expr: "avg(t)"}, OutputKey: "avg-temp"}); err != nil {
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
		if err := r.gw.InsertMetricSamples(r.ctx, storage.PushWriter(), []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: c.ID,
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
	if d, err := first.DrainCalcPending(r.ctx, 3); err != nil || d.Served != 3 {
		t.Fatalf("the first process drained %+v (%v), want 3", d, err)
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
				d, err := gw.DrainCalcPending(context.Background(), 1)
				if err != nil {
					errs[i] = err
					return
				}
				if d.Served == 0 {
					return
				}
				served[i] += d.Served
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
	if n := count(t, r.conn, `select count(*) from `+storagetest.MetricRows+` metric where provenance = 'calculated' and source_rule = 'temp-mean'`); n != owners {
		t.Fatalf("%d calculated rows, want exactly %d (one per owner)", n, owners)
	}
	if n := count(t, r.conn, `select count(*) from (select component_id from `+storagetest.MetricRows+` metric where provenance = 'calculated'
		group by component_id having count(*) > 1) d`); n != 0 {
		t.Fatalf("%d owners computed twice", n)
	}
}

// A rule reads observed and calculated values only (calculations.md): an
// operator's declared value, which marks no rule when it changes, is never
// an input, and a cleared declaration (a declared 'null' tombstone) neither
// fails the rule nor defers its mark.
func TestCalcIgnoresDeclaredInputs(t *testing.T) {
	r := newEvalRig(t)
	if _, err := r.create(storage.CalcRuleSpec{Name: "call-copy", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "call", Key: "in-call"}}, Reduce: rules.CalcReduce{Expr: "any(call)"}, OutputKey: "busy-flag"}); err != nil {
		t.Fatal(err)
	}
	// A sample on another instance marks the rule; the expression reads the
	// aggregate instance, whose newest row is then the declaration.
	mark := func() {
		t.Helper()
		if err := r.gw.InsertPropertySamples(r.ctx, storage.PushWriter(), []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: r.comp, Key: "in-call",
			Instance: "aux", Value: "false", Source: "test", TS: time.Now()}}); err != nil {
			t.Fatal(err)
		}
		r.drain()
	}
	output := func() string {
		t.Helper()
		return scalar(t, r.conn, `select value #>> '{}' from property where component_id = '`+r.comp+`' and source_rule = 'call-copy' order by id desc limit 1`)
	}
	r.property("in-call", "false")
	r.drain()
	if _, err := r.gw.SetProperty(r.ctx, "", "component", r.comp, "in-call", "", []byte(`true`), r.all, r.all); err != nil {
		t.Fatal(err)
	}
	mark()
	if got := output(); got != "false" {
		t.Fatalf("busy-flag reads %q over a declared true, want false (the observed value)", got)
	}
	if err := r.gw.ClearProperty(r.ctx, "", "component", r.comp, "in-call", "", r.all, r.all); err != nil {
		t.Fatal(err)
	}
	mark()
	if got := scalar(t, r.conn, `select error from calc_rule_state where component_id = '`+r.comp+`'`); got != "" {
		t.Fatalf("the rule's state records %q over a cleared declaration, want no error", got)
	}
	if n := r.pending(); n != 0 {
		t.Fatalf("%d marks left after the drain, want none deferred", n)
	}
}

// TestCalcComponentRetype proves a component's product change brings the
// component rules in line (#1015), as markSystemCalcRules does for a system:
// retyped out of a rule's selector, its state and any mark there go; retyped
// back in, the rule is marked over the values it already holds and computes
// without waiting for a new sample.
func TestCalcComponentRetype(t *testing.T) {
	r := newEvalRig(t)
	display, generic := storagetest.MintProduct(t, r.ctx, r.gw, "display").Name, "generic-device"
	if _, err := r.gw.UpdateComponent(r.ctx, "", r.comp, storage.ComponentPatch{ProductName: &display}, r.all, r.all); err != nil {
		t.Fatal(err)
	}
	spec := avgSpec("display-mean")
	spec.Selector = storage.CalcRuleSelector{ComponentTypes: []string{"display"}}
	if _, err := r.create(spec); err != nil {
		t.Fatal(err)
	}
	r.metric("room-temperature", "", 20)
	r.drain()
	state := `select count(*) from calc_rule_state where component_id = '` + r.comp + `'`
	if n := count(t, r.conn, state); n != 1 {
		t.Fatalf("%d state rows on the display, want 1", n)
	}

	// Out of the selector, with a mark pending: the state and the mark go.
	r.metric("room-temperature", "", 21)
	if r.pending() != 1 {
		t.Fatal("the sample did not mark the rule")
	}
	if _, err := r.gw.UpdateComponent(r.ctx, "", r.comp, storage.ComponentPatch{ProductName: &generic}, r.all, r.all); err != nil {
		t.Fatal(err)
	}
	if n := count(t, r.conn, state); n != 0 {
		t.Fatalf("%d state rows after the retype out of the selector, want none", n)
	}
	if n := r.pending(); n != 0 {
		t.Fatalf("%d marks after the retype out of the selector, want none", n)
	}

	// Back in: marked on the value it holds, and it computes.
	if _, err := r.gw.UpdateComponent(r.ctx, "", r.comp, storage.ComponentPatch{ProductName: &display}, r.all, r.all); err != nil {
		t.Fatal(err)
	}
	if n := r.pending(); n != 1 {
		t.Fatalf("%d marks after the retype into the selector, want 1", n)
	}
	r.drain()
	if got := r.calculated("avg-temp"); len(got) != 2 || got[1] != "21 display-mean 1" {
		t.Fatalf("avg-temp rows %v, want 20 then 21 by display-mean v1", got)
	}

	// A product change within the selector, or a save that re-states the
	// product, marks nothing it would not compute.
	if _, err := r.gw.UpdateComponent(r.ctx, "", r.comp, storage.ComponentPatch{ProductName: &display}, r.all, r.all); err != nil {
		t.Fatal(err)
	}
	if n := r.pending(); n != 0 {
		t.Fatalf("%d marks after re-stating the product, want none", n)
	}
}

// A component retyped out of a rule's selector while a serve holds the
// rule's mark there does not wait on the serve, and the serve, rechecking
// selection after the retype commits, computes nothing and leaves no state.
func TestCalcComponentRetypeWhileServing(t *testing.T) {
	r := newEvalRig(t)
	display, generic := storagetest.MintProduct(t, r.ctx, r.gw, "display").Name, "generic-device"
	if _, err := r.gw.UpdateComponent(r.ctx, "", r.comp, storage.ComponentPatch{ProductName: &display}, r.all, r.all); err != nil {
		t.Fatal(err)
	}
	spec := avgSpec("display-mean")
	spec.Selector = storage.CalcRuleSelector{ComponentTypes: []string{"display"}}
	if _, err := r.create(spec); err != nil {
		t.Fatal(err)
	}
	r.metric("room-temperature", "", 20)
	taken, resume := make(chan struct{}), make(chan struct{})
	r.gw.SetCalcServeHook(func(stage string) error {
		if stage == "taken" {
			close(taken)
			<-resume
		}
		return nil
	})
	drained := make(chan error, 1)
	go func() { _, err := r.gw.DrainCalcPending(r.ctx, 1); drained <- err }()
	<-taken
	r.gw.SetCalcServeHook(nil)
	retyped := make(chan error, 1)
	go func() {
		_, err := r.gw.UpdateComponent(r.ctx, "", r.comp, storage.ComponentPatch{ProductName: &generic}, r.all, r.all)
		retyped <- err
	}()
	select {
	case err := <-retyped:
		if err != nil {
			t.Fatalf("the retype: %v", err)
		}
	case <-time.After(10 * time.Second):
		close(resume)
		t.Fatal("the retype waited on the held serve")
	}
	close(resume)
	if err := <-drained; err != nil {
		t.Fatal(err)
	}
	r.drain()
	if got := r.calculated("avg-temp"); len(got) != 0 {
		t.Fatalf("avg-temp rows %v after leaving the selector, want none", got)
	}
	if n := count(t, r.conn, `select count(*) from calc_rule_state where component_id = '`+r.comp+`'`); n != 0 {
		t.Fatalf("%d state rows after leaving the selector, want none", n)
	}
}
