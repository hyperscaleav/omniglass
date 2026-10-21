package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

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
	r := &calcRig{t: t, ctx: ctx, gw: gw, conn: conn, all: scope.Set{All: true}}
	// Keys no collection writes, for the rules to derive.
	for _, m := range []string{"avg-temp", "temp-sum", "temp-twice"} {
		if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: m, DataType: "float"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"room-in-use", "in-call"} {
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
	got, err := r.create(avgSpec("room-avg"))
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
	for _, ref := range []string{"room-avg", got.ID} {
		if g, err := r.gw.GetCalcRule(r.ctx, ref); err != nil || g.ID != got.ID {
			t.Errorf("get %q: %+v (%v)", ref, g, err)
		}
	}
	if _, err := r.gw.GetCalcRule(r.ctx, "nobody"); !errors.Is(err, storage.ErrCalcRuleNotFound) {
		t.Errorf("an unknown rule: %v", err)
	}

	// A cross-key component expression writing a bool property.
	inUse, err := r.create(storage.CalcRuleSpec{Name: "in-use", OwnerKind: rules.OwnerComponent,
		Selector: storage.EventRuleSelector{ComponentTypes: []string{"display"}},
		Inputs:   []rules.CalcInput{{As: "input", Key: "video-input"}, {As: "call", Key: "in-call"}},
		Reduce:   rules.CalcReduce{Expr: `call || input != "hdmi1"`}, OutputKey: "room-in-use"})
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
	if err != nil || len(list) != 3 || list[0].Name != "in-use" || list[1].Name != "room-avg" || list[2].Name != "sys-avg" {
		t.Fatalf("list %+v (%v), want three rules by name", list, err)
	}

	for name, tc := range map[string]struct {
		spec  storage.CalcRuleSpec
		field string
	}{
		"unknown input key": {storage.CalcRuleSpec{Name: "x1", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "no-such-key"}},
			Reduce: rules.CalcReduce{Expr: "1.0"}, OutputKey: "temp-sum"}, "inputs[0].key"},
		"bad as": {storage.CalcRuleSpec{Name: "x2", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "in-call", Key: "in-call"}},
			Reduce: rules.CalcReduce{Expr: "true"}, OutputKey: "room-in-use"}, "inputs[0].as"},
		"reduce does not compile": {storage.CalcRuleSpec{Name: "x3", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}},
			Reduce: rules.CalcReduce{Expr: "a + nope"}, OutputKey: "temp-sum"}, "reduce.expr"},
		"result is not the output's type": {storage.CalcRuleSpec{Name: "x4", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}},
			Reduce: rules.CalcReduce{Expr: "a > 1"}, OutputKey: "temp-sum"}, "reduce.expr"},
		"reducer of the wrong type": {storage.CalcRuleSpec{Name: "x5", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}},
			Reduce: rules.CalcReduce{Reducer: "average"}, OutputKey: "room-in-use"}, "output_key"},
		"unknown output": {storage.CalcRuleSpec{Name: "x6", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}},
			Reduce: rules.CalcReduce{Reducer: "average"}, OutputKey: "no-such-key"}, "output_key"},
		"health as input": {storage.CalcRuleSpec{Name: "x7", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "h", Key: "health"}},
			Reduce: rules.CalcReduce{Expr: `h == "outage"`}, OutputKey: "room-in-use"}, "inputs[0].key"},
		"health as output": {storage.CalcRuleSpec{Name: "x8", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "video-input"}},
			Reduce: rules.CalcReduce{Reducer: "majority"}, OutputKey: "health"}, "output_key"},
		"output a driver emits": {storage.CalcRuleSpec{Name: "x9", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}},
			Reduce: rules.CalcReduce{Reducer: "max"}, OutputKey: "uptime"}, "output_key"},
		"system selector": {storage.CalcRuleSpec{Name: "x10", OwnerKind: "system", Selector: storage.EventRuleSelector{ComponentTypes: []string{"display"}},
			Inputs: []rules.CalcInput{{As: "a", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Reducer: "max"}, OutputKey: "temp-sum"}, "selector"},
		"unknown component type": {storage.CalcRuleSpec{Name: "x11", OwnerKind: "component", Selector: storage.EventRuleSelector{ComponentTypes: []string{"no-such-type"}},
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
	if _, err := r.create(avgSpec("room-avg")); !errors.Is(err, storage.ErrCalcRuleExists) {
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
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "room-avg", storage.CalcRulePatch{}, bounded); !errors.Is(err, storage.ErrCalcRuleForbidden) {
		t.Errorf("a bounded update: %v, want forbidden", err)
	}
	if _, err := r.gw.RenameCalcRule(r.ctx, "", "room-avg", "x", bounded); !errors.Is(err, storage.ErrCalcRuleForbidden) {
		t.Errorf("a bounded rename: %v, want forbidden", err)
	}
	if err := r.gw.DeleteCalcRule(r.ctx, "", "room-avg", bounded); !errors.Is(err, storage.ErrCalcRuleForbidden) {
		t.Errorf("a bounded delete: %v, want forbidden", err)
	}

	// An update bumps the version and is audited; one that changes nothing
	// writes nothing.
	label := "Room average"
	up, err := r.gw.UpdateCalcRule(r.ctx, "", "room-avg", storage.CalcRulePatch{Label: &label}, r.all)
	if err != nil || up.Version != 2 || up.Label != label {
		t.Fatalf("update %+v (%v), want version 2", up, err)
	}
	same, err := r.gw.UpdateCalcRule(r.ctx, "", "room-avg", storage.CalcRulePatch{Label: &label}, r.all)
	if err != nil || same.Version != 2 {
		t.Fatalf("a no-op update %+v (%v), want version 2 kept", same, err)
	}
	reducer := rules.CalcReduce{Reducer: "max"}
	if up, err = r.gw.UpdateCalcRule(r.ctx, "", "room-avg", storage.CalcRulePatch{Reduce: &reducer}, r.all); err != nil || up.Version != 3 || up.Reduce.Reducer != "max" {
		t.Fatalf("a reduce update %+v (%v)", up, err)
	}
	bad := rules.CalcReduce{Expr: "t + nope"}
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "room-avg", storage.CalcRulePatch{Reduce: &bad}, r.all); !errors.Is(err, storage.ErrCalcRuleInvalid) {
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

// An output key a collection has observed is refused: a current value would
// mix an observed producer and a calculated one by newest ts (#525 decision
// 6). The signal is a stored sample with provenance observed.
func TestCalcRuleOutputObserved(t *testing.T) {
	r := newCalcRig(t)
	c, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: "probe-target"}, r.all, r.all, r.all, r.all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.create(avgSpec("before")); err != nil {
		t.Fatalf("an output nothing observed yet: %v", err)
	}
	if err := r.gw.DeleteCalcRule(r.ctx, "", "before", r.all); err != nil {
		t.Fatal(err)
	}
	if err := r.gw.InsertMetricSamples(r.ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: c.ID, Key: "avg-temp",
		Value: 21, Source: "sensor", TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	_, err = r.create(avgSpec("after"))
	var ce *rules.CalcError
	if !errors.Is(err, storage.ErrCalcRuleInvalid) || !errors.As(err, &ce) || ce.Field != "output_key" || !strings.Contains(err.Error(), "observed") {
		t.Fatalf("an observed output: %v, want refused on output_key, saying it is observed", err)
	}
	// The same holds on the property lane.
	if err := r.gw.InsertPropertySamples(r.ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: c.ID, Key: "room-in-use",
		Value: "true", Source: "sensor", TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	_, err = r.create(storage.CalcRuleSpec{Name: "prop", OwnerKind: "component", Inputs: []rules.CalcInput{{As: "c", Key: "in-call"}},
		Reduce: rules.CalcReduce{Reducer: "any"}, OutputKey: "room-in-use"})
	if !errors.Is(err, storage.ErrCalcRuleInvalid) || !strings.Contains(err.Error(), "observed") {
		t.Fatalf("an observed property output: %v", err)
	}
}

// The graph from input keys to output keys across every enabled rule stays
// acyclic: a rule closing a loop is refused naming the chain, a disabled rule
// is outside the graph until it is enabled, and an update is judged against
// the graph without its own stored version.
func TestCalcRuleCycle(t *testing.T) {
	r := newCalcRig(t)
	if _, err := r.create(avgSpec("room-avg")); err != nil { // room-temperature -> avg-temp
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
	if _, err := r.gw.UpdateCalcRule(r.ctx, "", "closing", storage.CalcRulePatch{Enabled: &on}, r.all); err != nil {
		t.Fatalf("enabling once the loop is open: %v", err)
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
	if _, err := r.create(avgSpec("other")); err != nil {
		t.Fatal(err)
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
