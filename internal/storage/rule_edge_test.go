package storage_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// ruleEvent is one event a rule derived, as the edge tests read it.
type ruleEvent struct {
	key, instance, origin, message, sourceRule string
	alarmID, ruleID                            *string
	attrs                                      map[string]any
}

// edgeRig is the health fixture with a rule-under-test on bar-1 and the
// helpers the edge tests drive and read it with. The official reachability
// rule reads the same key and is disabled, so only the rule under test acts.
type edgeRig struct {
	f    *healthFixture
	ctx  context.Context
	t    *testing.T
	name string
}

func newEdgeRig(t *testing.T) *edgeRig {
	t.Helper()
	f := newHealthFixture(t)
	r := &edgeRig{f: f, ctx: context.Background(), t: t}
	off := false
	if _, err := f.conn.Exec(r.ctx, `update event_rule set enabled = $1 where name = 'reachability-lost'`, off); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *edgeRig) rule(spec storage.EventRuleSpec) {
	r.t.Helper()
	spec.SampleKey = "endpoint-reachable"
	spec.Fire = criteria("eq", "down")
	spec.Clear = ptrCriteria(criteria("eq", "up"))
	spec.Selector = storage.EventRuleSelector{Components: []string{"bar-1"}}
	if spec.FireEventType == "" {
		spec.FireEventType = "condition-started"
	}
	if _, err := r.f.gw.CreateEventRule(r.ctx, "", spec, r.f.all); err != nil {
		r.t.Fatal(err)
	}
	r.name = spec.Name
}

func (r *edgeRig) observe(instance, v string) {
	r.t.Helper()
	if err := r.f.gw.InsertPropertySamples(r.ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
		Key: "endpoint-reachable", Instance: instance, Value: v, TS: time.Now()}}); err != nil {
		r.t.Fatal(err)
	}
}

func (r *edgeRig) patch(p storage.EventRulePatch) {
	r.t.Helper()
	if _, err := r.f.gw.UpdateEventRule(r.ctx, "", r.name, p, r.f.all); err != nil {
		r.t.Fatal(err)
	}
}

// events reads the events the rule derived (by its name, which survives
// the rule's delete), oldest first.
func (r *edgeRig) events() []ruleEvent {
	r.t.Helper()
	rows, err := r.f.conn.Query(r.ctx, `select t.name, e.instance, e.origin, e.message, coalesce(e.source_rule, ''),
		e.alarm_id::text, e.derived_by_rule_id::text, e.attributes
		from event e join event_type t on t.id = e.event_type_id where e.source_rule = $1 order by e.id`, r.name)
	if err != nil {
		r.t.Fatal(err)
	}
	defer rows.Close()
	var out []ruleEvent
	for rows.Next() {
		var e ruleEvent
		var attrs []byte
		if err := rows.Scan(&e.key, &e.instance, &e.origin, &e.message, &e.sourceRule, &e.alarmID, &e.ruleID, &attrs); err != nil {
			r.t.Fatal(err)
		}
		_ = json.Unmarshal(attrs, &e.attrs)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		r.t.Fatal(err)
	}
	return out
}

// openAlarm is the rule's open alarm id on an instance, "" when none.
func (r *edgeRig) openAlarm(instance string) string {
	r.t.Helper()
	var id string
	err := r.f.conn.QueryRow(r.ctx, `select coalesce((select a.id::text from alarm a join event_rule er on er.id = a.raised_by_rule_id
		where er.name = $1 and a.dedup_key = 'rule:' || er.id::text || ':' || $2 and a.cleared_at is null), '')`, r.name, instance).Scan(&id)
	if err != nil {
		r.t.Fatal(err)
	}
	return id
}

func (r *edgeRig) conditions() int {
	r.t.Helper()
	return count(r.t, r.f.conn, `select count(*) from event_rule_condition c join event_rule e on e.id = c.rule_id where e.name = '`+r.name+`'`)
}

// editor bootstraps a named principal (root) to act as a rule's editor.
func (r *edgeRig) editor() string {
	r.t.Helper()
	zeros := make([]byte, 32)
	if _, err := r.f.gw.BootstrapOwner(r.ctx, storage.OwnerSpec{Username: "root", SecretHash: zeros, Prefix: "root0000"}); err != nil {
		r.t.Fatal(err)
	}
	p, err := r.f.gw.AuthenticateBearer(r.ctx, zeros)
	if err != nil {
		r.t.Fatal(err)
	}
	return p.ID
}

func (r *edgeRig) ruleID() string {
	r.t.Helper()
	return scalar(r.t, r.f.conn, `select id::text from event_rule where name = '`+r.name+`'`)
}

func edgeStr(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// An alarm-raising rule writes one fire event when its condition starts,
// carrying the alarm it opened, and one clear event when it ends, carrying
// the alarm it resolved: derived, naming its rule, with the rule's message
// and the sample's key and value. A condition held across many samples, and
// a redelivered sample, write nothing more.
func TestRuleEdgeEventsCarryTheAlarm(t *testing.T) {
	r := newEdgeRig(t)
	r.rule(storage.EventRuleSpec{Name: "edge", Severity: "high", Message: "bar unreachable", ClearEventType: "condition-ended"})
	id := r.ruleID()

	r.observe("tcp", "down")
	evs := r.events()
	alarm := r.openAlarm("tcp")
	if alarm == "" || len(evs) != 1 {
		t.Fatalf("after down: alarm %q, events %+v; want an open alarm and one fire event", alarm, evs)
	}
	e := evs[0]
	if e.key != "condition-started" || e.instance != "tcp" || e.origin != "derived" || edgeStr(e.alarmID) != alarm ||
		edgeStr(e.ruleID) != id || e.message != "bar unreachable" || e.attrs["key"] != "endpoint-reachable" || e.attrs["value"] != "down" {
		t.Fatalf("fire event = %+v (alarm %s, rule %s), want condition-started on tcp, derived, carrying both, with the message, key and value",
			e, edgeStr(e.alarmID), edgeStr(e.ruleID))
	}
	if r.conditions() != 1 {
		t.Fatalf("conditions = %d, want 1 in force", r.conditions())
	}
	r.observe("tcp", "down")
	r.observe("tcp", "down")
	if n := len(r.events()); n != 1 {
		t.Fatalf("a condition held across samples wrote %d events, want still 1", n)
	}

	r.observe("tcp", "up")
	evs = r.events()
	if len(evs) != 2 || evs[1].key != "condition-ended" || edgeStr(evs[1].alarmID) != alarm || evs[1].attrs["value"] != "up" {
		t.Fatalf("after up: events %+v, want a clear event carrying alarm %s", evs, alarm)
	}
	if r.openAlarm("tcp") != "" || r.conditions() != 0 {
		t.Fatalf("after up: alarm %q open, %d conditions; want none", r.openAlarm("tcp"), r.conditions())
	}
	r.observe("tcp", "up")
	if n := len(r.events()); n != 2 {
		t.Fatalf("a repeated up wrote %d events, want still 2", n)
	}
}

// An informational rule writes its fire and clear events and opens no
// alarm; a rule with no clear type resolves its alarm with no clear event,
// and its condition still ends.
func TestRuleEdgeInformationalAndNoClearType(t *testing.T) {
	r := newEdgeRig(t)
	r.rule(storage.EventRuleSpec{Name: "note", ClearEventType: "condition-ended"})
	r.observe("tcp", "down")
	r.observe("tcp", "up")
	evs := r.events()
	if len(evs) != 2 || evs[0].key != "condition-started" || evs[1].key != "condition-ended" || evs[0].alarmID != nil || evs[1].alarmID != nil {
		t.Fatalf("informational events = %+v, want a fire and a clear event, neither carrying an alarm", evs)
	}
	if n := count(t, r.f.conn, `select count(*) from alarm where component_id = (select id from component where name = 'bar-1')`); n != 0 {
		t.Fatalf("an informational rule left %d alarms, want none", n)
	}

	r2 := &edgeRig{f: r.f, ctx: r.ctx, t: t}
	r2.rule(storage.EventRuleSpec{Name: "no-clear", Severity: "high"})
	r2.observe("icmp", "down")
	alarm := r2.openAlarm("icmp")
	r2.observe("icmp", "up")
	evs = r2.events()
	if alarm == "" || len(evs) != 1 || edgeStr(evs[0].alarmID) != alarm {
		t.Fatalf("no-clear-type events = %+v (alarm %q), want the fire event alone, carrying the alarm", evs, alarm)
	}
	if r2.openAlarm("icmp") != "" || r2.conditions() != 0 {
		t.Fatal("a rule with no clear type left its alarm open or its condition in force")
	}
}

// Sustains, the sweep, priming and the reconcile pass write the same
// events as live ingest: a condition sustained past its fire sustain writes
// its fire event once, a later sweep or reconcile pass writes nothing, and
// the reconcile pass sees an informational condition in force as settled,
// leaving no streak behind.
func TestRuleEdgeSweepPrimeReconcile(t *testing.T) {
	r := newEdgeRig(t)
	r.observe("tcp", "down") // before the rule exists: priming finds it
	r.rule(storage.EventRuleSpec{Name: "slow", FireSustain: 10 * time.Second, ClearEventType: "condition-ended"})
	if n := len(r.events()); n != 0 {
		t.Fatalf("inside the sustain: %d events, want 0", n)
	}
	later := time.Now().Add(time.Hour)
	for i := 0; i < 2; i++ {
		if err := r.f.gw.SweepEventRules(r.ctx, later); err != nil {
			t.Fatal(err)
		}
	}
	if evs := r.events(); len(evs) != 1 || evs[0].key != "condition-started" {
		t.Fatalf("after the sweep: %+v, want one fire event", evs)
	}
	st := &storage.RuleReconcileState{}
	for i := 0; i < 2; i++ {
		if err := r.f.gw.ReconcileEventRules(r.ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(r.events()); n != 1 {
		t.Fatalf("the reconcile pass wrote %d events, want still 1", n)
	}
	if n := count(t, r.f.conn, `select count(*) from event_rule_state s join event_rule e on e.id = s.rule_id where e.name = 'slow'`); n != 0 {
		t.Fatalf("a condition in force left %d streaks after reconcile, want 0 (nothing to wait for)", n)
	}

	// Priming on create: a rule with no sustain created while the series reads
	// down fires at once.
	r2 := &edgeRig{f: r.f, ctx: r.ctx, t: t}
	r2.rule(storage.EventRuleSpec{Name: "primed"})
	if evs := r2.events(); len(evs) != 1 || evs[0].instance != "tcp" {
		t.Fatalf("a rule created over a down series wrote %+v, want one fire event on tcp", evs)
	}
}

// Turning a rule's severity on opens alarms for the conditions it holds in
// force, and turning it off resolves them, with no condition event either
// way: the condition did not move, so the episode keeps one started and one
// ended event, and the alarm's open and resolve are recorded in its audit
// rows, naming the editor.
func TestRuleEdgeSeverityToggle(t *testing.T) {
	r := newEdgeRig(t)
	editor := r.editor()
	r.rule(storage.EventRuleSpec{Name: "toggle", ClearEventType: "condition-ended"})
	r.observe("tcp", "down")
	if r.openAlarm("tcp") != "" {
		t.Fatal("an informational rule opened an alarm")
	}
	if _, err := r.f.gw.UpdateEventRule(r.ctx, editor, r.name, storage.EventRulePatch{Severity: ptrString("high")}, r.f.all); err != nil {
		t.Fatal(err)
	}
	alarm := r.openAlarm("tcp")
	if alarm == "" || len(r.events()) != 1 {
		t.Fatalf("severity on: alarm %q, events %+v; want the condition in force alarmed, and no new event", alarm, r.events())
	}
	if got := scalar(t, r.f.conn, `select coalesce(actor_username, '') from audit_log where resource = 'alarm' and verb = 'create' and resource_id = '`+alarm+`'`); got != "root" {
		t.Fatalf("the severity-on raise is audited as %q, want the editor (root)", got)
	}
	r.patch(storage.EventRulePatch{Severity: ptrString("")})
	if r.openAlarm("tcp") != "" || len(r.events()) != 1 {
		t.Fatalf("severity off: alarm %q, events %+v; want the alarm resolved, and no new event", r.openAlarm("tcp"), r.events())
	}
	if r.conditions() != 1 {
		t.Fatalf("severity off left %d conditions, want the condition still in force", r.conditions())
	}
	// Still in force: a further down writes nothing; the recovery ends it.
	r.observe("tcp", "down")
	r.observe("tcp", "up")
	evs := r.events()
	if len(evs) != 2 || evs[0].key != "condition-started" || evs[1].key != "condition-ended" || evs[1].alarmID != nil {
		t.Fatalf("the episode wrote %+v, want one started and one ended event, the ended one carrying no alarm", evs)
	}
}

// A material edit that also turns a severity on primes first: a condition
// the edited rule no longer holds ends (with its clear event) before the
// severity reaches it, so no alarm is raised and resolved in one write.
func TestRuleEdgeMaterialEditPrimesBeforeSeverity(t *testing.T) {
	r := newEdgeRig(t)
	r.rule(storage.EventRuleSpec{Name: "reword", ClearEventType: "condition-ended"})
	r.observe("tcp", "down")
	fire := criteria("eq", "gone")
	r.patch(storage.EventRulePatch{Fire: &fire, ClearNegates: true, Severity: ptrString("high")})
	if n := count(t, r.f.conn, `select count(*) from alarm a join event_rule e on e.id = a.raised_by_rule_id where e.name = 'reword'`); n != 0 {
		t.Fatalf("the edit raised %d alarms for a condition it ended, want 0", n)
	}
	if evs := r.events(); len(evs) != 2 || evs[1].key != "condition-ended" || r.conditions() != 0 {
		t.Fatalf("events %+v, %d conditions; want the condition ended with its clear event", evs, r.conditions())
	}
}

// A hand clear of a rule's alarm holds while the condition does: on a
// polled input that keeps reporting down, the alarm stays cleared and
// nothing is written, the condition row and its events untouched; it
// reopens only on the next new edge, after the condition ends and returns.
func TestRuleEdgeHandClearHolds(t *testing.T) {
	r := newEdgeRig(t)
	r.rule(storage.EventRuleSpec{Name: "polled", Severity: "high", ClearEventType: "condition-ended"})
	r.observe("tcp", "down")
	alarm := r.openAlarm("tcp")
	if err := r.f.gw.ClearAlarm(r.ctx, "", "bar-1", alarm); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		r.observe("tcp", "down")
	}
	if r.openAlarm("tcp") != "" || len(r.events()) != 1 || r.conditions() != 1 {
		t.Fatalf("after a hand clear and more downs: alarm %q, events %+v, %d conditions; want it cleared, one event, the condition in force",
			r.openAlarm("tcp"), r.events(), r.conditions())
	}
	r.observe("tcp", "up")
	r.observe("tcp", "down")
	evs := r.events()
	again := r.openAlarm("tcp")
	if again == "" || again == alarm || len(evs) != 3 || evs[1].key != "condition-ended" || evs[1].alarmID != nil || edgeStr(evs[2].alarmID) != again {
		t.Fatalf("after the condition returned: alarm %q, events %+v; want the end (carrying no alarm), then a new alarm with its fire event", again, evs)
	}
}

// An alarm the rule holds open with no condition row (written by an older
// binary mid-upgrade) counts as in force: a further fire neither adopts it
// nor writes an event, and a recovery resolves it, with no clear event,
// since no condition row ended.
func TestRuleEdgeOrphanAlarmIsInForce(t *testing.T) {
	r := newEdgeRig(t)
	r.rule(storage.EventRuleSpec{Name: "orphan", Severity: "high", ClearEventType: "condition-ended"})
	mustExec(t, r.f.conn, `insert into alarm (component_id, severity, message, dedup_key, raised_by_rule_id)
		values ((select id from component where name = 'bar-1'), 'high', 'old', 'rule:' || (select id::text from event_rule where name = 'orphan') || ':tcp',
			(select id from event_rule where name = 'orphan'))`)
	alarm := r.openAlarm("tcp")
	r.observe("tcp", "down")
	if r.openAlarm("tcp") != alarm || len(r.events()) != 0 || r.conditions() != 0 {
		t.Fatalf("a down over an orphan alarm: alarm %q (was %q), events %+v, %d conditions; want it untouched, nothing written",
			r.openAlarm("tcp"), alarm, r.events(), r.conditions())
	}
	// The live state agrees with the evaluator: the orphan counts as a
	// condition in force, so open alarms never outnumber conditions.
	got, err := r.f.gw.GetEventRule(r.ctx, "orphan", r.f.all)
	if err != nil || got.ConditionsInForce != 1 || got.OpenAlarms != 1 {
		t.Fatalf("an orphan alarm reads %+v (%v), want 1 condition in force and 1 open", got, err)
	}
	for _, x := range mustList(t, r) {
		if x.Name == "orphan" && x.ConditionsInForce != 1 {
			t.Fatalf("the list counts %d conditions for the orphan, want 1", x.ConditionsInForce)
		}
	}
	st, err := r.f.gw.EventRuleState(r.ctx, "orphan", r.f.all)
	if err != nil || len(st.Conditions) != 1 || st.Conditions[0].Instance != "tcp" {
		t.Fatalf("the state lists %+v (%v), want the orphan's condition on tcp", st, err)
	}
	r.observe("tcp", "up")
	if r.openAlarm("tcp") != "" || len(r.events()) != 0 {
		t.Fatalf("a recovery over an orphan alarm: alarm %q, events %+v; want it resolved with no event", r.openAlarm("tcp"), r.events())
	}
}

// Disabling, narrowing or deleting a rule ends its conditions and writes
// their clear events, carrying the alarms they resolve.
func TestRuleEdgeSettlePathsEndConditions(t *testing.T) {
	r := newEdgeRig(t)
	r.rule(storage.EventRuleSpec{Name: "settle", Severity: "high", ClearEventType: "condition-ended"})
	end := func(what string, act func(), wantEvents int) {
		t.Helper()
		r.observe("tcp", "up")
		r.observe("tcp", "down")
		alarm := r.openAlarm("tcp")
		act()
		evs := r.events()
		if len(evs) == 0 {
			t.Fatalf("%s: no events at all", what)
		}
		last := evs[len(evs)-1]
		if alarm == "" || len(evs) != wantEvents || last.key != "condition-ended" || edgeStr(last.alarmID) != alarm || r.conditions() != 0 || r.openAlarm("tcp") != "" {
			t.Fatalf("%s: events %+v, alarm %q; want the condition ended with a clear event carrying it", what, evs, alarm)
		}
	}
	off, on := false, true
	end("disable", func() { r.patch(storage.EventRulePatch{Enabled: &off}) }, 2)
	r.patch(storage.EventRulePatch{Enabled: &on}) // the series reads down: priming fires again
	r.observe("tcp", "up")
	// Narrowing to a component that is not bar-1.
	if _, err := r.f.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: "other-1"}, r.f.all, r.f.all, r.f.all, r.f.all); err != nil {
		t.Fatal(err)
	}
	end("narrow", func() {
		r.patch(storage.EventRulePatch{Selector: &storage.EventRuleSelector{Components: []string{"other-1"}}})
	}, 6)
	r.patch(storage.EventRulePatch{Selector: &storage.EventRuleSelector{Components: []string{"bar-1"}}})
	r.observe("tcp", "up")
	n := len(r.events())
	r.observe("tcp", "down")
	if err := r.f.gw.DeleteEventRule(r.ctx, "", "settle", r.f.all); err != nil {
		t.Fatal(err)
	}
	evs := r.events()
	if len(evs) != n+2 || evs[len(evs)-1].key != "condition-ended" || evs[len(evs)-1].ruleID != nil || evs[len(evs)-1].sourceRule != "settle" {
		t.Fatalf("delete: events %+v, want a final clear event naming the rule by name, its id set null with the rule", evs[n:])
	}
}

// TestRuleConditionBackfill proves an upgrade gives every rule-raised
// alarm open at upgrade its condition row, so the next recovery resolves it
// with a clear event; a cleared or hand-raised alarm gets none.
func TestRuleConditionBackfill(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, "20261020020000"); err != nil {
		t.Fatal(err)
	}
	conn := connectDSN(t, dsn)
	mustExec(t, conn, `insert into severity_level (name, rank) values ('high', 40)`)
	mustExec(t, conn, `insert into event_type (name) values ('cond-x')`)
	mustExec(t, conn, `insert into event_rule (name, sample_key, sample_lane, fire_criteria, severity, fire_event_type_id)
		values ('reach', 'endpoint-reachable', 'property', '{"op":"eq","value":"down"}', 'high', (select id from event_type where name = 'cond-x'))`)
	mustExec(t, conn, `insert into component (name, product_id) values ('bf-c', (select id from product where name = 'generic-device'))`)
	ins := func(dedup string, rule bool, cleared bool) {
		mustExec(t, conn, `insert into alarm (component_id, severity, message, dedup_key, raised_by_rule_id, raised_at, cleared_at)
			values ((select id from component where name = 'bf-c'), 'high', 'm', $1,
				case when $2 then (select id from event_rule where name = 'reach') end, '2026-10-01T00:00:00Z',
				case when $3 then now() end)`, dedup, rule, cleared)
	}
	ins("rule:reach:tcp", true, false)
	ins("rule:reach", true, false)
	ins("rule:reach:icmp", true, true)
	ins("by-hand", false, false)
	if err := migrate.Run(dsn); err != nil {
		t.Fatal(err)
	}
	got := scalar(t, conn, `select string_agg(instance || '@' || to_char(since at time zone 'UTC', 'YYYY-MM-DD'), ',' order by instance)
		from event_rule_condition`)
	if got != "@2026-10-01,tcp@2026-10-01" {
		t.Fatalf("backfilled conditions = %q, want the two open rule alarms (no instance, and tcp), since they opened", got)
	}
	if n := count(t, conn, `select count(*) from pg_constraint where conname = 'event_derived_by_rule_id_fkey' and contype = 'f'`); n != 1 {
		t.Fatal("event.derived_by_rule_id does not reference event_rule")
	}
}

// TestRuleConditionsInForceRead proves a rule's live state counts and lists
// the conditions it holds in force (#951), fenced to the reader's component
// scope like its alarms and streaks: an informational rule holding a
// condition reads one in force and no open alarm.
func TestRuleConditionsInForceRead(t *testing.T) {
	r := newEdgeRig(t)
	r.rule(storage.EventRuleSpec{Name: "watching"})
	r.observe("tcp", "down")
	none := scope.Set{}
	list, err := r.f.gw.ListEventRules(r.ctx, r.f.all)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range list {
		if x.Name == "watching" && (x.ConditionsInForce != 1 || x.OpenAlarms != 0) {
			t.Fatalf("listed %+v, want 1 condition in force and no open alarm", x)
		}
	}
	got, err := r.f.gw.GetEventRule(r.ctx, "watching", r.f.all)
	if err != nil || got.ConditionsInForce != 1 {
		t.Fatalf("got %+v (%v), want 1 condition in force", got, err)
	}
	if got, _ := r.f.gw.GetEventRule(r.ctx, "watching", none); got.ConditionsInForce != 0 {
		t.Fatalf("a reader who sees no component counts %d conditions, want 0", got.ConditionsInForce)
	}
	st, err := r.f.gw.EventRuleState(r.ctx, "watching", r.f.all)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Conditions) != 1 || st.Conditions[0].ComponentName != "bar-1" || st.Conditions[0].Instance != "tcp" || st.Conditions[0].Since.IsZero() {
		t.Fatalf("state conditions = %+v, want bar-1 on tcp with its since", st.Conditions)
	}
	if st, _ := r.f.gw.EventRuleState(r.ctx, "watching", none); len(st.Conditions) != 0 {
		t.Fatalf("a reader who sees no component lists %+v, want none", st.Conditions)
	}
}

func mustList(t *testing.T, r *edgeRig) []storage.EventRule {
	t.Helper()
	list, err := r.f.gw.ListEventRules(r.ctx, r.f.all)
	if err != nil {
		t.Fatal(err)
	}
	return list
}
