package storage_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// systemRig is an eval rig with systems: room-1 (a boardroom, under the
// room type), room-2 (a classroom), and lab (an av system, outside room),
// each with roles, and components to staff them.
type systemRig struct {
	*evalRig
	ids map[string]string // system and component name -> id
}

func newSystemRig(t *testing.T) *systemRig {
	r := &systemRig{evalRig: newEvalRig(t), ids: map[string]string{}}
	for name, typ := range map[string]string{"room-1": "board", "room-2": "class", "lab": "av"} {
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
	if err := r.gw.AssignRole(r.ctx, "", system, role, comp, r.all, r.all); err != nil {
		r.t.Fatal(err)
	}
}

func (r *systemRig) temp(comp, instance string, v float64) {
	r.t.Helper()
	if err := r.gw.InsertMetricSamples(r.ctx, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: r.ids[comp],
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
		Inputs: []rules.CalcInput{{As: "t", Key: "room-temperature"}}, Reduce: reduce, OutputKey: "avg-temp"}); err != nil {
		r.t.Fatal(err)
	}
}

// TestCalcSystemRule proves system rules (#997): a rule over the room type
// (subtypes included) averages each selected system's role-bound members'
// values into a system-owned calculated sample carrying the rule's lineage;
// a member's sample marks the systems it staffs; a member leaving its role
// drops out on the next tick; a system outside the selector gets nothing.
func TestCalcSystemRule(t *testing.T) {
	r := newSystemRig(t)
	r.assign("room-1", "left", "m1")
	r.assign("room-1", "right", "m2")
	r.assign("lab", "left", "m3")
	r.systemRule("room-mean", storage.CalcRuleSelector{SystemTypes: []string{"room"}}, rules.CalcReduce{Reducer: "average"})
	r.temp("m1", "", 20)
	r.temp("m2", "", 24)
	r.temp("m3", "", 99)
	r.drain()
	if got := r.systemOutput("room-1"); got != "22 room-mean 1 system" {
		t.Fatalf("room-1 output %q, want 22 from room-mean v1, system-owned", got)
	}
	if got := r.systemOutput("lab"); got != "" {
		t.Fatalf("lab (not a room) got %q", got)
	}
	if n := count(t, r.conn, `select count(*) from metric where provenance = 'calculated' and component_id is not null`); n != 0 {
		t.Fatalf("%d calculated rows on components, want none", n)
	}
	// A member leaving the role drops out on the next tick.
	if err := r.gw.UnassignRole(r.ctx, "", "room-1", "right", "m2", r.all, r.all); err != nil {
		t.Fatal(err)
	}
	if n := count(t, r.conn, `select count(*) from calc_pending where system_id = '`+r.ids["room-1"]+`'`); n != 1 {
		t.Fatal("the unassignment did not mark room-1's rule")
	}
	r.drain()
	if got := r.systemOutput("room-1"); got != "20 room-mean 1 system" {
		t.Fatalf("room-1 output %q after m2 left, want 20", got)
	}
	// A member's later sample marks the system; a sample of a component in
	// no selected system marks nothing.
	r.temp("m1", "", 30)
	r.temp("m3", "", 1)
	if n := r.pending(); n != 1 {
		t.Fatalf("%d marks, want room-1's one", n)
	}
	r.drain()
	if got := r.systemOutput("room-1"); got != "30 room-mean 1 system" {
		t.Fatalf("room-1 output %q, want 30", got)
	}
	// Retyping lab into a room marks it, and it computes.
	board := "board"
	if _, err := r.gw.UpdateSystem(r.ctx, "", "lab", storage.SystemPatch{SystemTypeID: &board}, r.all, r.all); err != nil {
		t.Fatal(err)
	}
	r.drain()
	if got := r.systemOutput("lab"); got != "1 room-mean 1 system" {
		t.Fatalf("lab output %q after its retype into a room, want 1", got)
	}
	// Event rules do not judge system-owned samples yet (#1002): an event
	// rule on the output raises nothing for a system.
	if _, err := r.gw.CreateEventRule(r.ctx, "", storage.EventRuleSpec{Name: "hot-room", SampleKey: "avg-temp",
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
// reads each input as the list of its members' aggregate values.
func TestCalcSystemMembersMissing(t *testing.T) {
	r := newSystemRig(t)
	r.assign("room-2", "left", "m1")
	r.assign("room-2", "right", "m2")
	r.systemRule("first", storage.CalcRuleSelector{Systems: []string{"room-2"}}, rules.CalcReduce{Expr: "len(t) * 1.0"})
	r.drain() // the create's marks: no member holds a value yet
	if got := scalar(t, r.conn, `select missing::text from calc_rule_state where system_id = '`+r.ids["room-2"]+`'`); got != `["room-temperature"]` {
		t.Fatalf("state missing %s, want room-temperature", got)
	}
	r.temp("m1", "", 20)
	r.drain()
	if got := r.systemOutput("room-2"); got != "1 first 1 system" {
		t.Fatalf("room-2 output %q, want 1 (one member with a value)", got)
	}
	if got := scalar(t, r.conn, `select skipped::text from calc_rule_state where system_id = '`+r.ids["room-2"]+`'`); got != `[{"key": "room-temperature", "component": "m2"}]` {
		t.Fatalf("state skipped %s, want m2 named", got)
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
			Inputs: []rules.CalcInput{{As: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Reducer: "max"}, OutputKey: "avg-temp"})
		var ce *rules.CalcError
		if !errors.Is(err, storage.ErrCalcRuleInvalid) || !errors.As(err, &ce) || ce.Field != "selector" {
			t.Errorf("%s: %v, want refused on selector", name, err)
		}
	}
	if _, err := r.create(storage.CalcRuleSpec{Name: "bad", OwnerKind: rules.OwnerComponent, Selector: storage.CalcRuleSelector{Systems: []string{"lab"}},
		Inputs: []rules.CalcInput{{As: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Reducer: "max"}, OutputKey: "avg-temp"}); !errors.Is(err, storage.ErrCalcRuleInvalid) {
		t.Errorf("a component rule with a system selector: %v", err)
	}
	r.systemRule("rooms", storage.CalcRuleSelector{SystemTypes: []string{"room"}}, rules.CalcReduce{Reducer: "max"})
	got, err := r.gw.GetCalcRule(r.ctx, "rooms")
	if err != nil || len(got.Selector.SystemTypes) != 1 {
		t.Fatalf("read back %+v (%v)", got, err)
	}
	if n := count(t, r.conn, `select count(*) from calc_pending where owner_kind = 'system'`); n != 2 {
		t.Fatalf("%d system marks after create, want 2 (room-1 and room-2)", n)
	}
	ids, err := r.create(storage.CalcRuleSpec{Name: "lab-only", OwnerKind: rules.OwnerSystem, Selector: storage.CalcRuleSelector{Systems: []string{"lab"}},
		Inputs: []rules.CalcInput{{As: "t", Key: "occupancy-count"}}, Reduce: rules.CalcReduce{Reducer: "max"}, OutputKey: "temp-sum"})
	if err != nil || len(ids.Selector.Systems) != 1 || ids.Selector.Systems[0] != r.ids["lab"] {
		t.Fatalf("a selector by name stores the id: %+v (%v)", ids, err)
	}
}

// The sample write's probe for the calc rules reading its keys is one probe
// of the inputs index, for both owner kinds (#997): a batch no rule reads
// pays for nothing more, and a system rule adds no scan to it.
func TestCalcProbeUsesTheInputsIndex(t *testing.T) {
	r := newEvalRig(t)
	if _, err := r.conn.Exec(r.ctx, "set enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	sql, path := storage.ExportCalcProbeSQL([]string{"room-temperature", "occupancy-count"})
	rows, err := r.conn.Query(r.ctx, "explain "+sql, path, "")
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
	if !strings.Contains(plan, "calc_rule_inputs_idx") {
		t.Fatalf("the calc probe does not reach for calc_rule_inputs_idx:\n%s", plan)
	}
}
