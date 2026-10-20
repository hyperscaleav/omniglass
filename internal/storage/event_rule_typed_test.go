package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestEventRuleTypedAndOptionalAlarm proves a rule's alarm is optional and
// its events are typed (#948): a rule saves with no severity (informational),
// a rule names a fire event type from the registry (required) and may name a
// clear one, each refused when it does not exist, and an update can set,
// change or drop them.
func TestEventRuleTypedAndOptionalAlarm(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	gw, all := f.gw, scope.Set{All: true}

	info, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "hot-note", SampleKey: "room-temperature",
		Fire: criteria("gt", 30), FireEventType: "condition-started"}, all)
	if err != nil {
		t.Fatalf("an informational rule: %v", err)
	}
	if info.Severity != "" || info.FireEventType != "condition-started" || info.ClearEventType != "" {
		t.Fatalf("created %+v, want no severity, fire condition-started, no clear type", info)
	}
	paged, err := gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "hot-page", SampleKey: "room-temperature",
		Fire: criteria("gt", 40), Severity: "high", FireEventType: "condition-started", ClearEventType: "condition-ended"}, all)
	if err != nil || paged.Severity != "high" || paged.ClearEventType != "condition-ended" {
		t.Fatalf("an alarm-raising rule %+v (%v), want high with the generic pair", paged, err)
	}

	for name, bad := range map[string]storage.EventRuleSpec{
		"no fire type":      {Name: "t1", SampleKey: "room-temperature", Fire: criteria("gt", 1)},
		"unknown fire type": {Name: "t2", SampleKey: "room-temperature", Fire: criteria("gt", 1), FireEventType: "no-such-type"},
		"unknown clear":     {Name: "t3", SampleKey: "room-temperature", Fire: criteria("gt", 1), FireEventType: "condition-started", ClearEventType: "no-such-type"},
		"unknown level":     {Name: "t4", SampleKey: "room-temperature", Fire: criteria("gt", 1), FireEventType: "condition-started", Severity: "critical"},
	} {
		if _, err := gw.CreateEventRule(ctx, "", bad, all); !errors.Is(err, storage.ErrEventRuleInvalid) {
			t.Errorf("%s: err %v, want ErrEventRuleInvalid", name, err)
		}
	}

	// An update sets a clear type, makes an alarm-raising rule informational
	// (an empty severity), and refuses dropping the fire type or naming a type
	// that does not exist.
	upd, err := gw.UpdateEventRule(ctx, "", "hot-page", storage.EventRulePatch{Severity: ptrString(""),
		FireEventType: ptrString("call-started"), ClearEventType: ptrString("call-ended")}, all)
	if err != nil || upd.Severity != "" || upd.FireEventType != "call-started" || upd.ClearEventType != "call-ended" || upd.Version != 2 {
		t.Fatalf("updated %+v (%v), want informational, the call pair, version 2", upd, err)
	}
	if upd, err = gw.UpdateEventRule(ctx, "", "hot-page", storage.EventRulePatch{ClearEventType: ptrString("")}, all); err != nil || upd.ClearEventType != "" {
		t.Fatalf("dropping the clear type: %+v (%v), want none", upd, err)
	}
	for name, p := range map[string]storage.EventRulePatch{
		"empty fire type":   {FireEventType: ptrString("")},
		"unknown fire type": {FireEventType: ptrString("no-such-type")},
		"unknown clear":     {ClearEventType: ptrString("no-such-type")},
	} {
		if _, err := gw.UpdateEventRule(ctx, "", "hot-page", p, all); !errors.Is(err, storage.ErrEventRuleInvalid) {
			t.Errorf("%s: err %v, want ErrEventRuleInvalid", name, err)
		}
	}
	if got, err := gw.GetEventRule(ctx, "hot-note", all); err != nil || got.Severity != "" || got.FireEventType != "condition-started" {
		t.Fatalf("read back %+v (%v)", got, err)
	}

	// An event type a rule names cannot be deleted out from under it.
	if err := gw.UpsertEventType(ctx, storage.EventType{Name: "kept-type"}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.UpdateEventRule(ctx, "", "hot-note", storage.EventRulePatch{ClearEventType: ptrString("kept-type")}, all); err != nil {
		t.Fatal(err)
	}
	if err := gw.DeleteEventType(ctx, "", "kept-type"); !errors.Is(err, storage.ErrEventTypeInUse) {
		t.Fatalf("deleting a type a rule names: err %v, want ErrEventTypeInUse", err)
	}
}

// TestOfficialRuleCarriesTheGenericPair proves the boot seed lands the
// official condition-started and condition-ended event types and gives the
// official reachability rule that pair.
func TestOfficialRuleCarriesTheGenericPair(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	for _, name := range []string{"condition-started", "condition-ended"} {
		et, err := f.gw.GetEventType(ctx, name)
		if err != nil || !et.Official {
			t.Fatalf("event type %s = %+v (%v), want an official type", name, et, err)
		}
	}
	r, err := f.gw.GetEventRule(ctx, "reachability-lost", f.all)
	if err != nil {
		t.Fatal(err)
	}
	if r.FireEventType != "condition-started" || r.ClearEventType != "condition-ended" || r.Severity != "high" {
		t.Fatalf("the official rule = %+v, want high with condition-started and condition-ended", r)
	}
}

// TestInformationalRuleRaisesNoAlarm proves a rule with no severity opens no
// alarm when its condition holds, on any path that acts on a rule (the
// ingest, the sweep that ends a sustain, the reconcile pass), and moves no
// verdict.
func TestInformationalRuleRaisesNoAlarm(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{Name: "note-down", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), FireSustain: 10 * time.Second, FireEventType: "condition-started",
		Selector: storage.EventRuleSelector{Components: []string{"bar-1"}}},
		f.all); err != nil {
		t.Fatal(err)
	}
	// The official reachability rule reads the same key; disable it here so
	// the only rule judging is the informational one.
	off := false
	if _, err := f.conn.Exec(ctx, `update event_rule set enabled = $1 where name = 'reachability-lost'`, off); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
		Key: "endpoint-reachable", Instance: "tcp", Value: "down", TS: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.SweepEventRules(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := f.gw.ReconcileEventRules(ctx, &storage.RuleReconcileState{}); err != nil {
		t.Fatal(err)
	}
	alarms, err := f.gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(alarms) != 0 {
		t.Fatalf("an informational rule opened %+v, want no alarm", alarms)
	}
	if _, v := f.recorded(t, ctx, "component", "bar-1"); v == "outage" || v == "degraded" {
		t.Fatalf("an informational rule moved bar-1 to %q", v)
	}
}

// TestEventTypeEditKeepsStreaks proves changing a rule's event types is not
// a material change: it changes what the rule writes, not when, so a pending
// streak keeps running.
func TestEventTypeEditKeepsStreaks(t *testing.T) {
	r := newStreakRig(t, "typed-streak", time.Minute)
	ctx := context.Background()
	r.observe(t, "tcp", "down", time.Now().UTC())
	streaks := func() int {
		t.Helper()
		return count(t, r.f.conn, `select count(*) from event_rule_state s join event_rule e on e.id = s.rule_id where e.name = 'typed-streak'`)
	}
	if n := streaks(); n != 1 {
		t.Fatalf("after a down: %d streaks, want 1 pending", n)
	}
	if _, err := r.f.gw.UpdateEventRule(ctx, "", "typed-streak", storage.EventRulePatch{FireEventType: ptrString("call-started"),
		ClearEventType: ptrString("")}, r.f.all); err != nil {
		t.Fatal(err)
	}
	if n := streaks(); n != 1 {
		t.Fatalf("an event type edit left %d streaks, want the pending one kept", n)
	}
}

// TestEventRuleTypeBackfill proves an upgrade gives every existing rule the
// generic pair (condition-started fires, condition-ended clears), creating
// the two types when the boot seed has not yet run, and that a fresh database
// gets neither type from a migration (the boot seed owns them).
func TestEventRuleTypeBackfill(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	fresh := connectDSN(t, storagetest.NewDSN(t))
	if n := count(t, fresh, `select count(*) from event_type where name in ('condition-started', 'condition-ended')`); n != 0 {
		t.Fatalf("a fresh database holds %d generic types from a migration, want 0 (the boot seed lands them)", n)
	}

	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, "20261020010000"); err != nil {
		t.Fatal(err)
	}
	conn := connectDSN(t, dsn)
	mustExec(t, conn, `insert into severity_level (name, rank) values ('high', 40)`)
	mustExec(t, conn, `insert into event_rule (name, sample_key, sample_lane, fire_criteria, severity)
		values ('old-rule', 'endpoint-reachable', 'property', '{"op":"eq","value":"down"}', 'high')`)
	if err := migrate.Run(dsn); err != nil {
		t.Fatal(err)
	}
	fire := scalar(t, conn, `select t.name from event_rule r join event_type t on t.id = r.fire_event_type_id where r.name = 'old-rule'`)
	clear := scalar(t, conn, `select t.name from event_rule r join event_type t on t.id = r.clear_event_type_id where r.name = 'old-rule'`)
	if fire != "condition-started" || clear != "condition-ended" {
		t.Fatalf("the upgraded rule fires %q and clears %q, want condition-started and condition-ended", fire, clear)
	}
	if got := scalar(t, conn, `select is_nullable from information_schema.columns where table_name = 'event_rule' and column_name = 'fire_event_type_id'`); got != "NO" {
		t.Fatalf("fire_event_type_id nullable = %s, want NO after the backfill", got)
	}
	if got := scalar(t, conn, `select is_nullable from information_schema.columns where table_name = 'event_rule' and column_name = 'severity'`); got != "YES" {
		t.Fatalf("severity nullable = %s, want YES (null raises no alarm)", got)
	}
	if got := scalar(t, conn, `select is_nullable from information_schema.columns where table_name = 'event' and column_name = 'alarm_id'`); got != "YES" {
		t.Fatalf("event.alarm_id nullable = %s, want YES", got)
	}

	// The condition table cascades like the streak table: a row goes with its
	// component.
	mustExec(t, conn, `insert into component (name, product_id) values ('cond-c', (select id from product where name = 'generic-device'))`)
	mustExec(t, conn, `insert into event_rule_condition (rule_id, component_id, instance, since)
		values ((select id from event_rule where name = 'old-rule'), (select id from component where name = 'cond-c'), 'tcp', now())`)
	if _, err := conn.Exec(context.Background(), `insert into event_rule_condition (rule_id, component_id, instance, since)
		values ((select id from event_rule where name = 'old-rule'), (select id from component where name = 'cond-c'), 'tcp', now())`); err == nil {
		t.Fatal("a second condition row for the same rule, component and instance was accepted")
	}
	mustExec(t, conn, `delete from component where name = 'cond-c'`)
	if n := count(t, conn, `select count(*) from event_rule_condition`); n != 0 {
		t.Fatalf("%d condition rows outlived their component, want 0", n)
	}
}

// TestEventRuleTypeBackfillDown proves rolling the slice back never turns an
// informational rule into one that pages: the restored NOT NULL needs a
// severity, so the rule takes the least severe level, and is disabled with it.
func TestEventRuleTypeBackfillDown(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	dsn := storagetest.NewDSN(t)
	conn := connectDSN(t, dsn)
	mustExec(t, conn, `insert into severity_level (name, rank) values ('info', 10), ('high', 40)`)
	mustExec(t, conn, `insert into event_type (name) values ('cond-x')`)
	for _, r := range []struct{ name, severity string }{{"quiet", ""}, {"loud", "high"}} {
		mustExec(t, conn, `insert into event_rule (name, sample_key, sample_lane, fire_criteria, severity, fire_event_type_id)
			values ($1, 'endpoint-reachable', 'property', '{"op":"eq","value":"down"}', nullif($2, ''), (select id from event_type where name = 'cond-x'))`,
			r.name, r.severity)
	}
	if err := migrate.RollbackBelow(dsn, "20261020010100"); err != nil {
		t.Fatal(err)
	}
	if got := scalar(t, conn, `select severity || ' ' || enabled from event_rule where name = 'quiet'`); got != "info false" {
		t.Fatalf("the informational rule rolled back to %q, want the least severe level, disabled (info false)", got)
	}
	if got := scalar(t, conn, `select severity || ' ' || enabled from event_rule where name = 'loud'`); got != "high true" {
		t.Fatalf("an alarm-raising rule rolled back to %q, want it untouched (high true)", got)
	}
}
