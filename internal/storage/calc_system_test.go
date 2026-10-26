package storage_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// systemRig is an eval rig with systems: sys-1 (type kind-b) and sys-2
// (kind-c), both subtypes of kind-a, and sys-3 (kind-z, outside kind-a),
// each with roles, and components to staff them.
type systemRig struct {
	*evalRig
	ids map[string]string // system and component name -> id
}

func newSystemRig(t *testing.T) *systemRig {
	r := &systemRig{evalRig: newEvalRig(t), ids: map[string]string{}}
	stem := "sys"
	root, err := r.gw.CreateSystemType(r.ctx, "", storage.SystemType{Name: "kind-a", Label: "Kind A", Stem: &stem})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"kind-b", "kind-c"} {
		if _, err := r.gw.CreateSystemType(r.ctx, "", storage.SystemType{Name: name, Label: name, ParentID: &root.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.gw.CreateSystemType(r.ctx, "", storage.SystemType{Name: "kind-z", Label: "Kind Z", Stem: &stem}); err != nil {
		t.Fatal(err)
	}
	for name, typ := range map[string]string{"sys-1": "kind-b", "sys-2": "kind-c", "sys-3": "kind-z"} {
		ty := typ
		s, err := r.gw.CreateSystem(r.ctx, "", storage.SystemSpec{Name: name, SystemTypeID: &ty}, r.all, r.all)
		if err != nil {
			t.Fatal(err)
		}
		r.ids[name] = s.ID
		for _, role := range []string{"left", "right", "spare"} {
			if _, err := r.gw.SetSystemRole(r.ctx, "", "system", name, storage.SystemRoleSpec{Name: role, Label: role, Quorum: 1, Impact: "none"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, c := range []string{"m1", "m2", "m3", "m4"} {
		comp, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: c}, r.all, r.all, r.all, r.all)
		if err != nil {
			t.Fatal(err)
		}
		r.ids[c] = comp.ID
	}
	return r
}

func (r *systemRig) assign(system, role, comp string) {
	r.t.Helper()
	if err := r.gw.AssignRole(r.ctx, "", system, role, comp, r.all, r.all, r.all, r.all); err != nil {
		r.t.Fatal(err)
	}
}

func (r *systemRig) temp(comp, instance string, v float64) {
	r.t.Helper()
	if err := r.gw.InsertMetricSamples(r.ctx, storage.PushWriter(), []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: r.ids[comp],
		Key: "room-temperature", Instance: instance, Value: v, TS: time.Now()}}); err != nil {
		r.t.Fatal(err)
	}
}

// systemOutput is the newest calculated avg-temp on a system, with its
// lineage, "" when none.
func (r *systemRig) systemOutput(system string) string {
	r.t.Helper()
	return scalar(r.t, r.conn, `select coalesce((select value::text || ' ' || source_rule || ' ' || source_rule_version::text || ' ' || owner_kind
		from metric where system_id = '`+r.ids[system]+`' and provenance = 'calculated' and instance = ''
		and metric_type_id = (select id from metric_type where name = 'avg-temp') order by id desc limit 1), '')`)
}

func (r *systemRig) systemRule(name string, sel storage.CalcRuleSelector, reduce rules.CalcReduce) {
	r.t.Helper()
	if _, err := r.create(storage.CalcRuleSpec{Name: name, OwnerKind: rules.OwnerSystem, Selector: sel,
		Inputs: []rules.CalcInput{{Alias: "t", Key: "room-temperature"}}, Reduce: reduce, OutputKey: "avg-temp"}); err != nil {
		r.t.Fatal(err)
	}
}

// TestCalcSystemRule proves system rules (#997): a rule over the kind-a type
// (subtypes included) averages each selected system's role-bound members'
// values into a system-owned calculated sample carrying the rule's lineage;
// a member's sample marks the systems it staffs; a member leaving its role
// drops out on the next tick; a system outside the selector gets nothing.
func TestCalcSystemRule(t *testing.T) {
	r := newSystemRig(t)
	r.assign("sys-1", "left", "m1")
	r.assign("sys-1", "right", "m2")
	r.assign("sys-3", "left", "m3")
	r.systemRule("kind-mean", storage.CalcRuleSelector{SystemTypes: []string{"kind-a"}}, rules.CalcReduce{Expr: "avg(t)"})
	r.temp("m1", "", 20)
	r.temp("m2", "", 24)
	r.temp("m3", "", 99)
	r.drain()
	if got := r.systemOutput("sys-1"); got != "22 kind-mean 1 system" {
		t.Fatalf("sys-1 output %q, want 22 from kind-mean v1, system-owned", got)
	}
	if got := r.systemOutput("sys-3"); got != "" {
		t.Fatalf("sys-3 (not a kind-a) got %q", got)
	}
	if n := count(t, r.conn, `select count(*) from metric where provenance = 'calculated' and component_id is not null`); n != 0 {
		t.Fatalf("%d calculated rows on components, want none", n)
	}
	// A member leaving the role drops out on the next tick.
	if err := r.gw.UnassignRole(r.ctx, "", "sys-1", "right", "m2", r.all, r.all, r.all); err != nil {
		t.Fatal(err)
	}
	if n := count(t, r.conn, `select count(*) from calc_pending where system_id = '`+r.ids["sys-1"]+`'`); n != 1 {
		t.Fatal("the unassignment did not mark sys-1's rule")
	}
	r.drain()
	if got := r.systemOutput("sys-1"); got != "20 kind-mean 1 system" {
		t.Fatalf("sys-1 output %q after m2 left, want 20", got)
	}
	// A member's later sample marks the system; a sample of a component in
	// no selected system marks nothing.
	r.temp("m1", "", 30)
	r.temp("m3", "", 1)
	if n := r.pending(); n != 1 {
		t.Fatalf("%d marks, want sys-1's one", n)
	}
	r.drain()
	if got := r.systemOutput("sys-1"); got != "30 kind-mean 1 system" {
		t.Fatalf("sys-1 output %q, want 30", got)
	}
	// Retyping sys-3 into kind-a marks it, and it computes.
	retype := "kind-b"
	if _, err := r.gw.UpdateSystem(r.ctx, "", "sys-3", storage.SystemPatch{SystemTypeID: &retype}, r.all, r.all); err != nil {
		t.Fatal(err)
	}
	r.drain()
	if got := r.systemOutput("sys-3"); got != "1 kind-mean 1 system" {
		t.Fatalf("sys-3 output %q after its retype into kind-a, want 1", got)
	}
	// Event rules do not judge system-owned samples yet (#1002): an event
	// rule on the output raises nothing for a system.
	if _, err := r.gw.CreateEventRule(r.ctx, "", storage.EventRuleSpec{Name: "hot-output", SampleKey: "avg-temp",
		Fire: rules.Criteria{Expr: "value > 0.0"}, Severity: "high", FireEventType: "condition-started"}, r.all); err != nil {
		t.Fatal(err)
	}
	r.temp("m1", "", 31)
	r.drain()
	if n := count(t, r.conn, `select count(*) from alarm`); n != 0 {
		t.Fatalf("%d alarms on a system-owned output, want none until #1002", n)
	}
}

// A member with no value is skipped and named in the state; with no member
// holding a value the input is missing and nothing is written. An expression
// reads its members as records, one per member.
func TestCalcSystemMembersMissing(t *testing.T) {
	r := newSystemRig(t)
	r.assign("sys-2", "left", "m1")
	r.assign("sys-2", "right", "m2")
	r.systemRule("first", storage.CalcRuleSelector{Systems: []string{"sys-2"}}, rules.CalcReduce{Expr: "count(members, .has.t) * 1.0"})
	r.drain() // the create's marks: no member holds a value yet
	if got := scalar(t, r.conn, `select missing::text from calc_rule_state where system_id = '`+r.ids["sys-2"]+`'`); got != `["room-temperature"]` {
		t.Fatalf("state missing %s, want room-temperature", got)
	}
	r.temp("m1", "", 20)
	r.drain()
	if got := r.systemOutput("sys-2"); got != "1 first 1 system" {
		t.Fatalf("sys-2 output %q, want 1 (one member with a value)", got)
	}
	if got := scalar(t, r.conn, `select skipped::text from calc_rule_state where system_id = '`+r.ids["sys-2"]+`'`); got != `[{"key": "room-temperature", "component": "`+r.ids["m2"]+`"}]` {
		t.Fatalf("state skipped %s, want m2 by id", got)
	}
}

// A system rule's selector names systems or system types that exist; a
// component rule takes no system selector and a system rule no component
// one. Creating a rule marks every system it selects.
func TestCalcSystemSelector(t *testing.T) {
	r := newSystemRig(t)
	for name, sel := range map[string]storage.CalcRuleSelector{
		"unknown system":   {Systems: []string{"nowhere"}},
		"unknown type":     {SystemTypes: []string{"no-such-type"}},
		"component fields": {ComponentTypes: []string{"display"}},
	} {
		_, err := r.create(storage.CalcRuleSpec{Name: "bad", OwnerKind: rules.OwnerSystem, Selector: sel,
			Inputs: []rules.CalcInput{{Alias: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Expr: "max(t)"}, OutputKey: "avg-temp"})
		var ce *rules.CalcError
		if !errors.Is(err, storage.ErrCalcRuleInvalid) || !errors.As(err, &ce) || ce.Field != "selector" {
			t.Errorf("%s: %v, want refused on selector", name, err)
		}
	}
	if _, err := r.create(storage.CalcRuleSpec{Name: "bad", OwnerKind: rules.OwnerComponent, Selector: storage.CalcRuleSelector{Systems: []string{"sys-3"}},
		Inputs: []rules.CalcInput{{Alias: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Expr: "max(t)"}, OutputKey: "avg-temp"}); !errors.Is(err, storage.ErrCalcRuleInvalid) {
		t.Errorf("a component rule with a system selector: %v", err)
	}
	r.systemRule("kinds", storage.CalcRuleSelector{SystemTypes: []string{"kind-a"}}, rules.CalcReduce{Expr: "max(t)"})
	got, err := r.gw.GetCalcRule(r.ctx, "kinds")
	if err != nil || len(got.Selector.SystemTypes) != 1 {
		t.Fatalf("read back %+v (%v)", got, err)
	}
	if n := count(t, r.conn, `select count(*) from calc_pending where owner_kind = 'system'`); n != 2 {
		t.Fatalf("%d system marks after create, want 2 (sys-1 and sys-2)", n)
	}
	ids, err := r.create(storage.CalcRuleSpec{Name: "one-only", OwnerKind: rules.OwnerSystem, Selector: storage.CalcRuleSelector{Systems: []string{"sys-3"}},
		Inputs: []rules.CalcInput{{Alias: "t", Key: "occupancy-count"}}, Reduce: rules.CalcReduce{Expr: "max(t)"}, OutputKey: "temp-sum"})
	if err != nil || len(ids.Selector.Systems) != 1 || ids.Selector.Systems[0] != r.ids["sys-3"] {
		t.Fatalf("a selector by name stores the id: %+v (%v)", ids, err)
	}
}

// The sample write's probe for the calc rules reading its keys is an index
// probe of calc_rule_input by key id, on both lanes and for both owner kinds
// (#997, #1018): a batch no rule reads pays for nothing more, and a system
// rule adds no scan to it.
func TestCalcProbeUsesTheInputIndexes(t *testing.T) {
	r := newEvalRig(t)
	if _, err := r.conn.Exec(r.ctx, "set enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	sql, lanes, names := storage.ExportCalcProbeSQL([]string{"metric:room-temperature", "property:in-call"})
	rows, err := r.conn.Query(r.ctx, "explain "+sql, lanes, names, "")
	if err != nil {
		t.Fatal(err)
	}
	var plan string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan += line + "\n"
	}
	rows.Close()
	for _, idx := range []string{"calc_rule_input_metric_key", "calc_rule_input_property_key"} {
		if !strings.Contains(plan, idx) {
			t.Fatalf("the calc probe does not reach for %s:\n%s", idx, plan)
		}
	}
}

// A serve held mid-transaction does not hold up a retype that moves its
// system out of the rule's selector, and the serve, rechecking selection
// once it resumes, computes nothing there: no state and no output remain
// (review finding 1: the retype deadlocked against the held serve).
func TestCalcRetypeWhileServing(t *testing.T) {
	r := newSystemRig(t)
	r.assign("sys-1", "left", "m1")
	r.temp("m1", "", 20)
	r.systemRule("kind-mean", storage.CalcRuleSelector{SystemTypes: []string{"kind-a"}}, rules.CalcReduce{Expr: "avg(t)"})
	if _, err := r.conn.Exec(r.ctx, `delete from calc_pending where system_id <> $1`, r.ids["sys-1"]); err != nil {
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
	retyped := make(chan error, 1)
	go func() {
		z := "kind-z"
		_, err := r.gw.UpdateSystem(r.ctx, "", "sys-1", storage.SystemPatch{SystemTypeID: &z}, r.all, r.all)
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
	if got := r.systemOutput("sys-1"); got != "" {
		t.Fatalf("sys-1 got output %q after leaving the selector", got)
	}
	if n := count(t, r.conn, `select count(*) from calc_rule_state where system_id = '`+r.ids["sys-1"]+`'`); n != 0 {
		t.Fatalf("sys-1 kept %d state rows after leaving the selector", n)
	}
}

// A system whose last member leaves keeps its last calculated value (no
// tombstone sample), and its state says since when it has had no members.
func TestCalcSystemNoMembers(t *testing.T) {
	r := newSystemRig(t)
	r.assign("sys-1", "left", "m1")
	r.temp("m1", "", 20)
	r.systemRule("kind-mean", storage.CalcRuleSelector{Systems: []string{"sys-1"}}, rules.CalcReduce{Expr: "avg(t)"})
	r.drain()
	if got := r.systemOutput("sys-1"); got != "20 kind-mean 1 system" {
		t.Fatalf("sys-1 output %q, want 20", got)
	}
	if err := r.gw.UnassignRole(r.ctx, "", "sys-1", "left", "m1", r.all, r.all, r.all); err != nil {
		t.Fatal(err)
	}
	r.drain()
	if got := r.systemOutput("sys-1"); got != "20 kind-mean 1 system" {
		t.Fatalf("sys-1 output %q with no members, want the last value kept", got)
	}
	since := scalar(t, r.conn, `select coalesce(no_members_since::text, '') from calc_rule_state where system_id = '`+r.ids["sys-1"]+`'`)
	if since == "" {
		t.Fatal("the state does not say the system has no members")
	}
	// Still none on the next computation: the since stays.
	if _, err := r.conn.Exec(r.ctx, `insert into calc_pending (rule_id, owner_kind, system_id) select id, 'system', $1::uuid from calc_rule`, r.ids["sys-1"]); err != nil {
		t.Fatal(err)
	}
	r.drain()
	if again := scalar(t, r.conn, `select no_members_since::text from calc_rule_state where system_id = '`+r.ids["sys-1"]+`'`); again != since {
		t.Fatalf("no_members_since moved from %s to %s", since, again)
	}
	r.assign("sys-1", "left", "m2")
	r.temp("m2", "", 30)
	r.drain()
	if got := scalar(t, r.conn, `select coalesce(no_members_since::text, 'null') from calc_rule_state where system_id = '`+r.ids["sys-1"]+`'`); got != "null" {
		t.Fatalf("no_members_since %s with a member back, want null", got)
	}
}
