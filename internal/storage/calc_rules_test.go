package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

type calcRig struct {
	t    *testing.T
	ctx  context.Context
	gw   *storage.PG
	conn *pgx.Conn
	all  scope.Set
	dsn  string
}

func newCalcRig(t *testing.T) *calcRig {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	r := &calcRig{t: t, ctx: ctx, gw: gw, conn: conn, all: scope.Set{All: true}, dsn: dsn}
	// Keys no collection writes, for the rules to derive.
	for _, m := range []string{"avg-temp", "temp-sum", "temp-twice"} {
		if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: m, DataType: "float"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"busy-flag", "in-call"} {
		if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: p, DataType: "bool"}); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (r *calcRig) create(spec storage.CalcRuleSpec) (*storage.CalcRule, error) {
	return r.gw.CreateCalcRule(r.ctx, "", spec, r.all)
}

func avgSpec(name string) storage.CalcRuleSpec {
	return storage.CalcRuleSpec{Name: name, OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{As: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Reducer: "average"}, OutputKey: "avg-temp"}
}

// TestCalcRules proves a calc rule is authored config the gateway keeps
// honest (#995): its inputs read registered keys under bindable names, its
// reduce compiles against them and yields the output key's type, its output
// key is referenced by id on its lane, a write bumps its version and is
// audited, and rules read like a registry, written only on an all-scope grant.
func TestCalcRules(t *testing.T) {
	r := newCalcRig(t)
	got, err := r.create(avgSpec("temp-mean"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == "" || got.OutputLane != rules.LaneMetric || got.OutputKey != "avg-temp" || got.Version != 1 || !got.Enabled ||
		got.Official || got.OwnerKind != "component" || got.Reduce.Reducer != "average" || len(got.Inputs) != 1 {
		t.Fatalf("created %+v, want an enabled component rule writing avg-temp on the metric lane", got)
	}
	if n := count(t, r.conn, `select count(*) from calc_rule c join metric_type m on m.id = c.output_metric_type_id
		where c.id = '`+got.ID+`' and m.name = 'avg-temp' and c.output_property_type_id is null`); n != 1 {
		t.Fatal("the output key is not held by the metric type's id")
	}
	if n := count(t, r.conn, `select count(*) from audit_log where resource = 'calc_rule' and verb = 'create' and resource_id = '`+got.ID+`'`); n != 1 {
		t.Fatalf("%d create audit rows, want 1", n)
	}
	for _, ref := range []string{"temp-mean", got.ID} {
		if g, err := r.gw.GetCalcRule(r.ctx, ref); err != nil || g.ID != got.ID {
			t.Errorf("get %q: %+v (%v)", ref, g, err)
		}
	}
	if _, err := r.gw.GetCalcRule(r.ctx, "nobody"); !errors.Is(err, storage.ErrCalcRuleNotFound) {
		t.Errorf("an unknown rule: %v", err)
	}

	// A cross-key component expression writing a bool property.
	inUse, err := r.create(storage.CalcRuleSpec{Name: "in-use", OwnerKind: rules.OwnerComponent,
		Selector: storage.CalcRuleSelector{ComponentTypes: []string{"display"}},
		Inputs:   []rules.CalcInput{{As: "input", Key: "video-input"}, {As: "call", Key: "in-call"}},
		Reduce:   rules.CalcReduce{Expr: `call || input != "hdmi1"`}, OutputKey: "busy-flag"})
	if err != nil {
		t.Fatal(err)
	}
	if inUse.OutputLane != rules.LaneProperty || inUse.Reduce.Expr == "" {
		t.Fatalf("created %+v, want a property-lane expression rule", inUse)
	}

	// A system rule averages its members' values.
	if _, err := r.create(storage.CalcRuleSpec{Name: "sys-avg", OwnerKind: rules.OwnerSystem,
		Inputs: []rules.CalcInput{{As: "temps", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Expr: "len(temps) > 0 ? temps[0] : 0.0"},
		OutputKey: "temp-twice"}); err != nil {
		t.Fatalf("a system rule: %v", err)
	}

	list, err := r.gw.ListCalcRules(r.ctx)
	if err != nil || len(list) != 3 || list[0].Name != "in-use" || list[1].Name != "sys-avg" || list[2].Name != "temp-mean" {
		t.Fatalf("list %+v (%v), want three rules by name", list, err)
	}

	for name, tc := range map[string]struct {
		spec  storage.CalcRuleSpec
		field string
	}{
		"unknown input key": {storage.CalcRuleSpec{Name: "x1", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "no-such-key"}},
			Reduce: rules.CalcReduce{Expr: "1.0"}, OutputKey: "temp-sum"}, "inputs[0].key"},
		"bad as": {storage.CalcRuleSpec{Name: "x2", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "in-call", Key: "in-call"}},
			Reduce: rules.CalcReduce{Expr: "true"}, OutputKey: "busy-flag"}, "inputs[0].as"},
		"reduce does not compile": {storage.CalcRuleSpec{Name: "x3", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}},
			Reduce: rules.CalcReduce{Expr: "a + nope"}, OutputKey: "temp-sum"}, "reduce.expr"},
		"result is not the output's type": {storage.CalcRuleSpec{Name: "x4", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}},
			Reduce: rules.CalcReduce{Expr: "a > 1"}, OutputKey: "temp-sum"}, "reduce.expr"},
		"reducer of the wrong type": {storage.CalcRuleSpec{Name: "x5", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}},
			Reduce: rules.CalcReduce{Reducer: "average"}, OutputKey: "busy-flag"}, "output_key"},
		"unknown output": {storage.CalcRuleSpec{Name: "x6", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}},
			Reduce: rules.CalcReduce{Reducer: "average"}, OutputKey: "no-such-key"}, "output_key"},
		"health as input": {storage.CalcRuleSpec{Name: "x7", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "h", Key: "health"}},
			Reduce: rules.CalcReduce{Expr: `h == "outage"`}, OutputKey: "busy-flag"}, "inputs[0].key"},
		"health as output": {storage.CalcRuleSpec{Name: "x8", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "video-input"}},
			Reduce: rules.CalcReduce{Reducer: "majority"}, OutputKey: "health"}, "output_key"},
		"output a driver emits": {storage.CalcRuleSpec{Name: "x9", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}},
			Reduce: rules.CalcReduce{Reducer: "max"}, OutputKey: "uptime"}, "output_key"},
		"system selector": {storage.CalcRuleSpec{Name: "x10", OwnerKind: "system", Selector: storage.CalcRuleSelector{ComponentTypes: []string{"display"}},
			Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Reducer: "max"}, OutputKey: "temp-sum"}, "selector"},
		"unknown component type": {storage.CalcRuleSpec{Name: "x11", OwnerKind: "component", Selector: storage.CalcRuleSelector{ComponentTypes: []string{"no-such-type"}},
			Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Reducer: "max"}, OutputKey: "temp-sum"}, "selector"},
		"bad owner": {storage.CalcRuleSpec{Name: "x12", OwnerKind: "location", Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}},
			Reduce: rules.CalcReduce{Reducer: "max"}, OutputKey: "temp-sum"}, "owner_kind"},
		"bad name": {storage.CalcRuleSpec{Name: "Bad Name", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}},
			Reduce: rules.CalcReduce{Reducer: "max"}, OutputKey: "temp-sum"}, ""},
	} {
		_, err := r.create(tc.spec)
		if !errors.Is(err, storage.ErrCalcRuleInvalid) {
			t.Errorf("%s: err %v, want ErrCalcRuleInvalid", name, err)
			continue
		}
		var ce *rules.CalcError
		if tc.field != "" && (!errors.As(err, &ce) || ce.Field != tc.field) {
			t.Errorf("%s: %v, want a fault on %s", name, err, tc.field)
		}
	}
	taken, disabled := avgSpec("temp-mean"), false
	taken.Enabled = &disabled
	if _, err := r.create(taken); !errors.Is(err, storage.ErrCalcRuleExists) {
		t.Errorf("a taken name: %v, want ErrCalcRuleExists", err)
	}
	// The driver refusal names the driver that declares the key.
	_, err = r.create(storage.CalcRuleSpec{Name: "x13", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}},
		Reduce: rules.CalcReduce{Reducer: "max"}, OutputKey: "uptime"})
	if err == nil || !strings.Contains(err.Error(), "driver") {
		t.Errorf("an output a driver emits: %v, want a refusal naming the driver", err)
	}

	// A bounded grant writes nothing.
	bounded := scope.Set{}
	if _, err := r.gw.CreateCalcRule(r.ctx, "", avgSpec("other"), bounded); !errors.Is(err, storage.ErrCalcRuleForbidden) {
		t.Errorf("a bounded create: %v, want forbidden", err)
	}
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "temp-mean", storage.CalcRulePatch{}, bounded); !errors.Is(err, storage.ErrCalcRuleForbidden) {
		t.Errorf("a bounded update: %v, want forbidden", err)
	}
	if _, err := r.gw.RenameCalcRule(r.ctx, "", "temp-mean", "x", bounded); !errors.Is(err, storage.ErrCalcRuleForbidden) {
		t.Errorf("a bounded rename: %v, want forbidden", err)
	}
	if err := r.gw.DeleteCalcRule(r.ctx, "", "temp-mean", bounded); !errors.Is(err, storage.ErrCalcRuleForbidden) {
		t.Errorf("a bounded delete: %v, want forbidden", err)
	}

	// An update bumps the version and is audited; one that changes nothing
	// writes nothing.
	label := "Room average"
	up, err := r.gw.UpdateCalcRule(r.ctx, "", "temp-mean", storage.CalcRulePatch{Label: &label}, r.all)
	if err != nil || up.Version != 2 || up.Label != label {
		t.Fatalf("update %+v (%v), want version 2", up, err)
	}
	same, err := r.gw.UpdateCalcRule(r.ctx, "", "temp-mean", storage.CalcRulePatch{Label: &label}, r.all)
	if err != nil || same.Version != 2 {
		t.Fatalf("a no-op update %+v (%v), want version 2 kept", same, err)
	}
	reducer := rules.CalcReduce{Reducer: "max"}
	if up, err = r.gw.UpdateCalcRule(r.ctx, "", "temp-mean", storage.CalcRulePatch{Reduce: &reducer}, r.all); err != nil || up.Version != 3 || up.Reduce.Reducer != "max" {
		t.Fatalf("a reduce update %+v (%v)", up, err)
	}
	bad := rules.CalcReduce{Expr: "t + nope"}
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "temp-mean", storage.CalcRulePatch{Reduce: &bad}, r.all); !errors.Is(err, storage.ErrCalcRuleInvalid) {
		t.Errorf("a bad reduce update: %v", err)
	}
	if n := count(t, r.conn, `select count(*) from audit_log where resource = 'calc_rule' and verb = 'update' and resource_id = '`+got.ID+`'`); n != 2 {
		t.Errorf("%d update audit rows, want 2", n)
	}

	// An official rule is read-only.
	if _, err := r.conn.Exec(r.ctx, `update calc_rule set official = true where name = 'sys-avg'`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "sys-avg", storage.CalcRulePatch{Label: &label}, r.all); !errors.Is(err, storage.ErrCalcRuleOfficial) {
		t.Errorf("an official update: %v", err)
	}
	if err := r.gw.DeleteCalcRule(r.ctx, "", "sys-avg", r.all); !errors.Is(err, storage.ErrCalcRuleOfficial) {
		t.Errorf("an official delete: %v", err)
	}

	// The output key a rule writes cannot be deleted from its registry.
	if _, err := r.conn.Exec(r.ctx, `delete from metric_type where name = 'avg-temp'`); err == nil {
		t.Error("the output key a rule writes was deleted")
	}

	if err := r.gw.DeleteCalcRule(r.ctx, "", "in-use", r.all); err != nil {
		t.Fatal(err)
	}
	if _, err := r.gw.GetCalcRule(r.ctx, "in-use"); !errors.Is(err, storage.ErrCalcRuleNotFound) {
		t.Errorf("a deleted rule: %v", err)
	}
	if n := count(t, r.conn, `select count(*) from audit_log where resource = 'calc_rule' and verb = 'delete' and resource_id = '`+inUse.ID+`'`); n != 1 {
		t.Errorf("%d delete audit rows, want 1", n)
	}
}

// An output key a collection writes is refused, judged from declarations: a
// driver's declared emits (TestCalcRules: uptime) and the native probes'
// keys (probekeys). An observed sample is no declaration: a hand-posted or
// simulated one leaves the key free.
func TestCalcRuleOutputCollected(t *testing.T) {
	r := newCalcRig(t)
	for _, key := range probekeys.Native() {
		lane, _, err := r.gw.SampleKeyType(r.ctx, key)
		if err != nil || lane == "" {
			t.Fatalf("native probe key %s is not a registered property or metric (%v)", key, err)
		}
	}
	_, err := r.create(storage.CalcRuleSpec{Name: "probe-out", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}},
		Reduce: rules.CalcReduce{Reducer: "max"}, OutputKey: probekeys.ICMPRTTAvg})
	var ce *rules.CalcError
	if !errors.Is(err, storage.ErrCalcRuleInvalid) || !errors.As(err, &ce) || ce.Field != "output_key" || !strings.Contains(err.Error(), "probe") {
		t.Fatalf("a native probe's key as output: %v, want refused on output_key naming the probe", err)
	}
	c, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: "probe-target"}, r.all, r.all, r.all, r.all)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.gw.InsertMetricSamples(r.ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: c.ID, Key: "avg-temp",
		Value: 21, Source: "hand", TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.create(avgSpec("after-sample")); err != nil {
		t.Fatalf("an output with a hand-posted sample: %v, want it accepted", err)
	}
}

// One enabled rule writes an output key: a second enabled writer is refused
// naming the first, a disabled one is accepted, and enabling it is checked.
func TestCalcRuleOneWriter(t *testing.T) {
	r := newCalcRig(t)
	if _, err := r.create(avgSpec("temp-mean")); err != nil {
		t.Fatal(err)
	}
	second := storage.CalcRuleSpec{Name: "temp-high", OwnerKind: rules.OwnerSystem,
		Inputs: []rules.CalcInput{{As: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Reducer: "max"}, OutputKey: "avg-temp"}
	_, err := r.create(second)
	var ce *rules.CalcError
	if !errors.Is(err, storage.ErrCalcRuleInvalid) || !errors.As(err, &ce) || ce.Field != "output_key" || !strings.Contains(err.Error(), "temp-mean") {
		t.Fatalf("a second writer: %v, want refused on output_key naming temp-mean", err)
	}
	off, on := false, true
	second.Enabled = &off
	if _, err := r.create(second); err != nil {
		t.Fatalf("a disabled second writer: %v", err)
	}
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "temp-high", storage.CalcRulePatch{Enabled: &on}, r.all); !errors.Is(err, storage.ErrCalcRuleInvalid) ||
		!strings.Contains(err.Error(), "temp-mean") {
		t.Fatalf("enabling the second writer: %v", err)
	}
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "temp-mean", storage.CalcRulePatch{Enabled: &off}, r.all); err != nil {
		t.Fatal(err)
	}
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "temp-high", storage.CalcRulePatch{Enabled: &on}, r.all); err != nil {
		t.Fatalf("enabling once the first is disabled: %v", err)
	}
	// Moving a rule onto a written key is the same refusal.
	other := "temp-twice"
	if _, err := r.create(storage.CalcRuleSpec{Name: "twice", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "t", Key: "room-temperature"}},
		Reduce: rules.CalcReduce{Expr: "t * 2"}, OutputKey: other}); err != nil {
		t.Fatal(err)
	}
	taken := "avg-temp"
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "twice", storage.CalcRulePatch{OutputKey: &taken}, r.all); !errors.Is(err, storage.ErrCalcRuleInvalid) {
		t.Fatalf("moving onto a written key: %v", err)
	}
}

// Deleting a property or metric type an enabled or disabled calc rule writes
// is refused naming the rule, on both lanes.
func TestCalcRuleOutputTypeDelete(t *testing.T) {
	r := newCalcRig(t)
	if _, err := r.create(avgSpec("temp-mean")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.create(storage.CalcRuleSpec{Name: "busy", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "c", Key: "in-call"}},
		Reduce: rules.CalcReduce{Reducer: "any"}, OutputKey: "busy-flag"}); err != nil {
		t.Fatal(err)
	}
	var inUse *storage.CalcOutputInUseError
	err := r.gw.DeleteMetricType(r.ctx, "", "avg-temp")
	if !errors.As(err, &inUse) || !errors.Is(err, storage.ErrKeyWrittenByCalcRule) || len(inUse.Rules) != 1 || inUse.Rules[0] != "temp-mean" {
		t.Fatalf("deleting a written metric type: %v, want refused naming temp-mean", err)
	}
	err = r.gw.DeletePropertyType(r.ctx, "", "busy-flag")
	if !errors.As(err, &inUse) || inUse.Rules[0] != "busy" {
		t.Fatalf("deleting a written property type: %v, want refused naming busy", err)
	}
	if err := r.gw.DeleteMetricType(r.ctx, "", "temp-sum"); err != nil {
		t.Fatalf("deleting an unwritten type: %v", err)
	}
}

// A stored rule whose reduce no longer compiles (an input key deleted, a
// row written outside the gateway) carries calc_error on every read, with
// its line and column when the expression is what failed; it can still be
// disabled, but enabling it again is refused until it compiles.
func TestCalcRuleCalcError(t *testing.T) {
	r := newCalcRig(t)
	got, err := r.create(storage.CalcRuleSpec{Name: "busy", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "c", Key: "in-call"}},
		Reduce: rules.CalcReduce{Expr: "c"}, OutputKey: "busy-flag"})
	if err != nil {
		t.Fatal(err)
	}
	if got.CalcError != nil {
		t.Fatalf("a compiling rule carries %+v", got.CalcError)
	}
	if _, err := r.conn.Exec(r.ctx, `update calc_rule set reduce = '{"expr": "c &&\n nope"}', version = version + 1 where id = $1`, got.ID); err != nil {
		t.Fatal(err)
	}
	g, err := r.gw.GetCalcRule(r.ctx, "busy")
	if err != nil || g.CalcError == nil || g.CalcError.Field != "reduce.expr" || g.CalcError.Line != 2 || g.CalcError.Message == "" {
		t.Fatalf("a broken reduce reads %+v (%v), want calc_error at reduce.expr line 2", g, err)
	}
	list, err := r.gw.ListCalcRules(r.ctx)
	if err != nil || len(list) != 1 || list[0].CalcError == nil {
		t.Fatalf("the list %+v (%v), want the rule's calc_error", list, err)
	}
	off, on := false, true
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "busy", storage.CalcRulePatch{Enabled: &off}, r.all); err != nil {
		t.Fatalf("disabling a broken rule: %v", err)
	}
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "busy", storage.CalcRulePatch{Enabled: &on}, r.all); !errors.Is(err, storage.ErrCalcRuleInvalid) {
		t.Fatalf("enabling a broken rule: %v, want refused", err)
	}
	fixed := rules.CalcReduce{Expr: "!c"}
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "busy", storage.CalcRulePatch{Reduce: &fixed, Enabled: &on}, r.all); err != nil {
		t.Fatalf("enabling with a fixed reduce: %v", err)
	}
	// An input key deleted under the rule is a fault on that input.
	if _, err := r.conn.Exec(r.ctx, `delete from property_type where name = 'in-call'`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.conn.Exec(r.ctx, `update calc_rule set version = version + 1 where id = $1`, got.ID); err != nil {
		t.Fatal(err)
	}
	g, err = r.gw.GetCalcRule(r.ctx, "busy")
	if err != nil || g.CalcError == nil || g.CalcError.Field != "inputs[0].key" {
		t.Fatalf("a deleted input key reads %+v (%v), want calc_error on inputs[0].key", g, err)
	}
}

// The graph from input keys to output keys across every enabled rule stays
// acyclic: a rule closing a loop is refused naming the chain, a disabled rule
// is outside the graph until it is enabled, and an update is judged against
// the graph without its own stored version.
func TestCalcRuleCycle(t *testing.T) {
	r := newCalcRig(t)
	if _, err := r.create(avgSpec("temp-mean")); err != nil { // room-temperature -> avg-temp
		t.Fatal(err)
	}
	if _, err := r.create(storage.CalcRuleSpec{Name: "doubled", OwnerKind: "component", // avg-temp -> temp-twice
		Inputs: []rules.CalcInput{{As: "a", Key: "avg-temp"}}, Reduce: rules.CalcReduce{Expr: "a * 2"}, OutputKey: "temp-twice"}); err != nil {
		t.Fatal(err)
	}
	closing := storage.CalcRuleSpec{Name: "closing", OwnerKind: "component", // temp-twice -> avg-temp: a loop
		Inputs: []rules.CalcInput{{As: "a", Key: "temp-twice"}}, Reduce: rules.CalcReduce{Expr: "a / 2"}, OutputKey: "avg-temp"}
	_, err := r.create(closing)
	if !errors.Is(err, storage.ErrCalcRuleInvalid) || !errors.Is(err, rules.ErrCalcCycle) {
		t.Fatalf("a closing rule: %v, want a cycle refusal", err)
	}
	var cyc *storage.CalcCycleError
	if !errors.As(err, &cyc) || cyc.Cycle.String() != "temp-twice -> avg-temp -> temp-twice" ||
		!strings.Contains(err.Error(), "temp-twice -> avg-temp -> temp-twice") {
		t.Fatalf("the refusal %v, want the chain temp-twice -> avg-temp -> temp-twice", err)
	}
	off := false
	closing.Enabled = &off
	if _, err := r.create(closing); err != nil {
		t.Fatalf("a disabled closing rule is outside the graph: %v", err)
	}
	on := true
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "closing", storage.CalcRulePatch{Enabled: &on}, r.all); !errors.Is(err, rules.ErrCalcCycle) {
		t.Fatalf("enabling the closing rule: %v, want a cycle refusal", err)
	}
	// Repointing doubled away from avg-temp opens the loop, and closing may
	// then be enabled.
	inputs := []rules.CalcInput{{As: "a", Key: "room-temperature"}}
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "doubled", storage.CalcRulePatch{Inputs: &inputs}, r.all); err != nil {
		t.Fatal(err)
	}
	// With the loop open, closing is refused only as avg-temp's second writer.
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "closing", storage.CalcRulePatch{Enabled: &on}, r.all); errors.Is(err, rules.ErrCalcCycle) ||
		!strings.Contains(err.Error(), "temp-mean") {
		t.Fatalf("enabling once the loop is open: %v, want only the second-writer refusal", err)
	}
	// A rule reading its own output is the shortest loop.
	_, err = r.create(storage.CalcRuleSpec{Name: "self", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "s", Key: "temp-sum"}},
		Reduce: rules.CalcReduce{Expr: "s + 1"}, OutputKey: "temp-sum"})
	if !errors.As(err, &cyc) || cyc.Cycle.String() != "temp-sum -> temp-sum" {
		t.Fatalf("a self loop: %v", err)
	}
}

// A calc rule's name is renameable (ADR-0062, ADR-0076): the rule keeps its
// uuid and version, the old name stops resolving, the rename is audited on
// the uuid, and the cycle graph (keyed by rule) still knows it.
func TestCalcRuleRename(t *testing.T) {
	r := newCalcRig(t)
	got, err := r.create(avgSpec("before"))
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := r.gw.RenameCalcRule(r.ctx, "", "before", "after", r.all)
	if err != nil || renamed.ID != got.ID || renamed.Name != "after" || renamed.Version != got.Version {
		t.Fatalf("renamed %+v (%v), want the same id and version under the new name", renamed, err)
	}
	if _, err := r.gw.GetCalcRule(r.ctx, "before"); !errors.Is(err, storage.ErrCalcRuleNotFound) {
		t.Errorf("the old name: %v", err)
	}
	if n := count(t, r.conn, `select count(*) from audit_log where resource = 'calc_rule' and verb = 'rename' and resource_id = '`+got.ID+`'
		and old->>'Name' = 'before' and new->>'Name' = 'after'`); n != 1 {
		t.Errorf("%d rename audit rows, want 1", n)
	}
	off := false
	other := avgSpec("other")
	other.Enabled = &off
	if _, err := r.create(other); err != nil {
		t.Fatal(err)
	}
	// A rename to the rule's own name changes nothing and writes no audit row.
	same, err := r.gw.RenameCalcRule(r.ctx, "", "after", "after", r.all)
	if err != nil || same.ID != got.ID {
		t.Fatalf("a rename to the same name: %+v (%v)", same, err)
	}
	if n := count(t, r.conn, `select count(*) from audit_log where resource = 'calc_rule' and verb = 'rename' and resource_id = '`+got.ID+`'`); n != 1 {
		t.Errorf("%d rename audit rows after a same-name rename, want 1", n)
	}
	if _, err := r.gw.RenameCalcRule(r.ctx, "", "after", "other", r.all); !errors.Is(err, storage.ErrCalcRuleExists) {
		t.Errorf("a taken name: %v", err)
	}
	if _, err := r.gw.RenameCalcRule(r.ctx, "", "after", "Bad Name", r.all); !errors.Is(err, storage.ErrCalcRuleInvalid) {
		t.Errorf("an illegal name: %v", err)
	}
	// The renamed rule still closes a loop under its new name.
	_, err = r.create(storage.CalcRuleSpec{Name: "loop", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "avg-temp"}},
		Reduce: rules.CalcReduce{Expr: "a"}, OutputKey: "room-temperature"})
	var cyc *storage.CalcCycleError
	if !errors.As(err, &cyc) {
		t.Fatalf("a loop through the renamed rule: %v", err)
	}
}
