package storage_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

func (r *evalRig) avgRule(name, input, output string) {
	r.t.Helper()
	if _, err := r.create(storage.CalcRuleSpec{Name: name, OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "t", Key: input}}, Reduce: rules.CalcReduce{Expr: "max(t)"}, OutputKey: output}); err != nil {
		r.t.Fatal(err)
	}
}

func (r *evalRig) latest(key string) string {
	r.t.Helper()
	return scalar(r.t, r.conn, `select coalesce((select value::text from metric where component_id = '`+r.comp+`'
		and metric_type_id = (select id from metric_type where name = '`+key+`') and provenance = 'calculated'
		order by id desc limit 1), '')`)
}

// A mark a batch re-marks while its sample is uncommitted is held by the
// batch until it commits, so a drain in between cannot serve it from the
// old value and drop the change (review finding 1: input 99, output 30,
// nothing pending).
func TestCalcMarkHeldByUncommittedBatch(t *testing.T) {
	r := newEvalRig(t)
	r.avgRule("echo", "room-temperature", "avg-temp")
	r.metric("room-temperature", "", 30)
	r.drain()
	if got := r.latest("avg-temp"); got != "30" {
		t.Fatalf("avg-temp %q, want 30", got)
	}
	r.metric("room-temperature", "", 31) // a mark waiting to be served
	held, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- r.gw.WriteMetricHeld(r.ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: r.comp,
			Key: "room-temperature", Value: 99, TS: time.Now()}}, func() { close(held); <-release })
	}()
	<-held
	if _, err := r.gw.DrainCalcPending(r.ctx, 100); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	r.drain()
	if got := r.latest("avg-temp"); got != "99" {
		t.Fatalf("avg-temp %q after the batch committed, %d pending; want 99", got, r.pending())
	}
}

// The ingest and the tick lock two marks in opposite orders (the tick has
// deleted (first, c) and its write marks (second, c); a batch marks
// (second, c) then (first, c)): Postgres aborts one with a deadlock, and the
// ingest retries its whole transaction, so the batch lands and both rules
// compute (review finding 2).
func TestCalcIngestRetriesADeadlock(t *testing.T) {
	r := newEvalRig(t)
	// Two rules reading room-temperature, the later one also reading the
	// earlier one's output, so the tick's write of the first marks the
	// second. Mark order is by rule id, so the second rule is created first
	// (uuidv7 ids sort by creation).
	if _, err := r.create(storage.CalcRuleSpec{Name: "second", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "a", Key: "avg-temp"}, {Alias: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Expr: "max(a) + max(t)"},
		OutputKey: "temp-twice", Enabled: ptrBool(false)}); err != nil {
		t.Fatal(err)
	}
	r.avgRule("first", "room-temperature", "avg-temp")
	on := true
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "second", storage.CalcRulePatch{Enabled: &on}, r.all); err != nil {
		t.Fatal(err)
	}
	r.metric("room-temperature", "", 10)
	r.drain()
	if got := r.latest("temp-twice"); got != "20" {
		t.Fatalf("temp-twice %q, want 20", got)
	}
	// Leave only first's mark, then hold the tick mid-serve of it.
	r.metric("room-temperature", "", 11)
	if _, err := r.conn.Exec(r.ctx, `delete from calc_pending where rule_id = (select id from calc_rule where name = 'second')`); err != nil {
		t.Fatal(err)
	}
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
	ingested := make(chan error, 1)
	go func() {
		ingested <- r.gw.InsertMetricSamples(r.ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: r.comp,
			Key: "room-temperature", Value: 12, TS: time.Now()}})
	}()
	// Wait until the batch is blocked on the tick's deleted mark, then let
	// the tick write (and mark second).
	deadline := time.Now().Add(10 * time.Second)
	for count(t, r.conn, `select count(*) from pg_stat_activity where wait_event_type = 'Lock'`) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the batch never waited on the tick")
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(resume)
	if err := <-ingested; err != nil {
		t.Fatalf("the ingest failed: %v (it retries a deadlock)", err)
	}
	if err := <-drained; err != nil && !strings.Contains(err.Error(), "deadlock") {
		t.Fatalf("the drain: %v", err)
	}
	for i := 0; i < 5 && r.pending() > 0; i++ {
		r.drain()
	}
	if a, b := r.latest("avg-temp"), r.latest("temp-twice"); a != "12" || b != "24" {
		t.Fatalf("avg-temp %q temp-twice %q, want 12 and 24", a, b)
	}
}

// A rule created, enabled, or edited computes over values already stored
// (review finding 3): the write marks every selected component holding one
// of its inputs, so a rule over a stable property computes without a new
// sample, and an edited reduce replaces the old output.
func TestCalcRuleWriteMarks(t *testing.T) {
	r := newEvalRig(t)
	r.metric("room-temperature", "", 20)
	r.avgRule("echo", "room-temperature", "avg-temp")
	if n := r.pending(); n != 1 {
		t.Fatalf("%d marks after create over a stored value, want 1", n)
	}
	r.drain()
	if got := r.latest("avg-temp"); got != "20" {
		t.Fatalf("avg-temp %q, want 20", got)
	}
	twice := rules.CalcReduce{Expr: "max(t) * 2"}
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "echo", storage.CalcRulePatch{Reduce: &twice}, r.all); err != nil {
		t.Fatal(err)
	}
	r.drain()
	if got := r.latest("avg-temp"); got != "40" {
		t.Fatalf("avg-temp %q after the reduce edit, want 40", got)
	}
	if got := scalar(t, r.conn, `select source_rule_version::text from metric where provenance = 'calculated' order by id desc limit 1`); got != "2" {
		t.Fatalf("the new output names version %s, want 2", got)
	}
	// A component without the input is not marked.
	if _, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: "empty-one"}, r.all, r.all, r.all, r.all); err != nil {
		t.Fatal(err)
	}
	off, on := false, true
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "echo", storage.CalcRulePatch{Enabled: &off}, r.all); err != nil {
		t.Fatal(err)
	}
	if r.pending() != 0 || count(t, r.conn, `select count(*) from calc_rule_state`) != 0 {
		t.Fatal("a disabled rule kept marks or state")
	}
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "echo", storage.CalcRulePatch{Enabled: &on}, r.all); err != nil {
		t.Fatal(err)
	}
	if n := r.pending(); n != 1 {
		t.Fatalf("%d marks after enable, want 1 (only the component holding the input)", n)
	}
}

// A narrowed selector drops the state of owners it no longer selects
// (review finding 8).
func TestCalcStateFollowsTheSelector(t *testing.T) {
	r := newEvalRig(t)
	other, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: "other-one"}, r.all, r.all, r.all, r.all)
	if err != nil {
		t.Fatal(err)
	}
	r.avgRule("echo", "room-temperature", "avg-temp")
	r.metric("room-temperature", "", 20)
	if err := r.gw.InsertMetricSamples(r.ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: other.ID,
		Key: "room-temperature", Value: 5, TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	r.drain()
	if n := count(t, r.conn, `select count(*) from calc_rule_state`); n != 2 {
		t.Fatalf("%d state rows, want 2", n)
	}
	sel := storage.CalcRuleSelector{Components: []string{"calc-target"}}
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "echo", storage.CalcRulePatch{Selector: &sel}, r.all); err != nil {
		t.Fatal(err)
	}
	r.drain()
	if n := count(t, r.conn, `select count(*) from calc_rule_state where component_id = '`+other.ID+`'`); n != 0 {
		t.Fatalf("the dropped owner kept %d state rows", n)
	}
	if n := count(t, r.conn, `select count(*) from calc_rule_state where component_id = '`+r.comp+`' and rule_version = 2`); n != 1 {
		t.Fatal("the kept owner's state was not rewritten at the new version")
	}
}

// One failing mark does not stall the queue (review finding 4): its error is
// recorded, its mark pushed back by the backoff, and the pass goes on to the
// next mark.
func TestCalcFailingMarkDefers(t *testing.T) {
	r := newEvalRig(t)
	r.avgRule("echo", "room-temperature", "avg-temp")
	r.avgRule("people", "occupancy-count", "temp-sum")
	r.metric("room-temperature", "", 20)
	r.metric("occupancy-count", "", 3)
	failed := false
	r.gw.SetCalcServeHook(func(stage string) error {
		if stage == "taken" && !failed {
			failed = true
			return errors.New("injected fault")
		}
		return nil
	})
	d, err := r.gw.DrainCalcPending(r.ctx, 100)
	r.gw.SetCalcServeHook(nil)
	if err != nil || d.Served != 1 || d.Deferred != 1 || d.Depth != 1 {
		t.Fatalf("drain %+v (%v), want one served, one deferred, one still pending", d, err)
	}
	if n := count(t, r.conn, `select count(*) from calc_pending where marked_at > now()`); n != 1 {
		t.Fatal("the failed mark was not pushed back")
	}
	if n := count(t, r.conn, `select count(*) from calc_rule_state where error like '%injected fault%'`); n != 1 {
		t.Fatal("the failure is not in the rule's state")
	}
	// Not due yet: a drain leaves it.
	if d, _ := r.gw.DrainCalcPending(r.ctx, 100); d.Served != 0 || d.Depth != 1 {
		t.Fatalf("a drain before the backoff: %+v", d)
	}
}

// An output whose name is registered on both lanes is written on the lane
// the rule recorded, never re-resolved by name (review finding 4: the
// float64 assertion panicked).
func TestCalcOutputByRecordedLane(t *testing.T) {
	r := newEvalRig(t)
	r.avgRule("echo", "room-temperature", "avg-temp")
	if _, err := r.conn.Exec(r.ctx, `insert into property_type (name, data_type) values ('avg-temp', 'string')`); err != nil {
		t.Fatal(err)
	}
	r.metric("room-temperature", "", 20)
	r.drain()
	if got := r.latest("avg-temp"); got != "20" {
		t.Fatalf("avg-temp %q, want the metric written", got)
	}
}

// A reducer folds each instance's newest value, the aggregate instance only
// when there is no other, and skips instances not heard from within the
// staleness bound, naming them in the state (review finding 6).
func TestCalcReducerInstances(t *testing.T) {
	r := newEvalRig(t)
	if _, err := r.create(storage.CalcRuleSpec{Name: "mean", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Expr: "avg(t)"}, OutputKey: "avg-temp"}); err != nil {
		t.Fatal(err)
	}
	r.metric("room-temperature", "", 100) // the aggregate, ignored beside instances
	r.metric("room-temperature", "a", 10)
	if err := r.gw.InsertMetricSamples(r.ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: r.comp,
		Key: "room-temperature", Instance: "old", Value: 1000, TS: time.Now().Add(-storage.CalcStaleAfter - time.Minute)}}); err != nil {
		t.Fatal(err)
	}
	r.metric("room-temperature", "b", 20)
	r.drain()
	if got := r.latest("avg-temp"); got != "15" {
		t.Fatalf("avg-temp %q, want 15 (a and b; not the aggregate, not the stale one)", got)
	}
	if got := scalar(t, r.conn, `select stale::text from calc_rule_state`); got != `[{"key": "room-temperature", "instance": "old", "component": "`+r.comp+`"}]` {
		t.Fatalf("state stale %s, want the old instance named", got)
	}
}
