package storage_test

import (
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// ruleDedupKeyVersion is the one-time data migration that rewrote rule-raised
// alarms' dedup keys from the rule's name to its uuid (#977).
const ruleDedupKeyVersion = "20261021050000"

// TestRuleRename proves an event rule's name is renameable (#977, ADR-0062,
// ADR-0076) while everything inside the platform holds its uuid: a rule with
// an open alarm and a condition in force is renamed, the alarm keeps its
// raised_by_rule_id and its dedup key (the uuid), the condition stays in
// force, and the next recovery clears it with a clear event naming the rule
// by its uuid and its new name, while the fire event keeps the name it was
// written under. The old name stops resolving and the new one resolves; the
// rename is audited, keys on the uuid, and bumps no version. An official rule
// refuses, a taken name is a conflict, an illegal name is invalid, and a
// bounded grant is forbidden.
func TestRuleRename(t *testing.T) {
	r := newEdgeRig(t)
	r.rule(storage.EventRuleSpec{Name: "before", Severity: "high", ClearEventType: "condition-ended"})
	id := r.ruleID()
	r.observe("tcp", "down")
	alarm := r.openAlarm("tcp")
	if alarm == "" || r.conditions() != 1 {
		t.Fatalf("before the rename: alarm %q, %d conditions; want one of each", alarm, r.conditions())
	}
	wantKey := "rule:" + id + ":tcp"
	if got := scalar(t, r.f.conn, `select dedup_key from alarm where id = '`+alarm+`'`); got != wantKey {
		t.Fatalf("the rule's alarm is keyed %q, want %q (by the rule's uuid)", got, wantKey)
	}
	before, err := r.f.gw.GetEventRule(r.ctx, "before", r.f.all)
	if err != nil {
		t.Fatal(err)
	}

	editor := r.editor()
	got, err := r.f.gw.RenameEventRule(r.ctx, editor, "before", "after", r.f.all)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id || got.Name != "after" || got.Version != before.Version {
		t.Fatalf("renamed: id %q name %q version %d; want id %q, name after, version %d unchanged", got.ID, got.Name, got.Version, id, before.Version)
	}
	r.name = "after"
	if _, err := r.f.gw.GetEventRule(r.ctx, "before", r.f.all); !errors.Is(err, storage.ErrEventRuleNotFound) {
		t.Fatalf("the old name: %v, want not found", err)
	}
	after, err := r.f.gw.GetEventRule(r.ctx, "after", r.f.all)
	if err != nil || after.ID != id || after.OpenAlarms != 1 || after.ConditionsInForce != 1 {
		t.Fatalf("the new name reads %+v (%v); want the same rule holding its alarm and condition", after, err)
	}
	if got := scalar(t, r.f.conn, `select dedup_key || ' ' || raised_by_rule_id::text || ' ' || (cleared_at is null)::text
		from alarm where id = '`+alarm+`'`); got != wantKey+" "+id+" true" {
		t.Fatalf("after the rename the alarm reads %q, want it open, keyed and raised by the uuid", got)
	}
	if n := count(t, r.f.conn, `select count(*) from audit_log where resource = 'event_rule' and verb = 'rename' and resource_id = '`+id+`'
		and old->>'Name' = 'before' and new->>'Name' = 'after'`); n != 1 {
		t.Fatalf("%d rename audit rows keyed on the uuid, want 1", n)
	}

	// A further down changes nothing: the condition is still in force.
	r.observe("tcp", "down")
	if r.openAlarm("tcp") != alarm || r.conditions() != 1 {
		t.Fatalf("a down after the rename: alarm %q (was %q), %d conditions; want both kept", r.openAlarm("tcp"), alarm, r.conditions())
	}
	// The recovery clears it, with the clear event under the new name.
	r.observe("tcp", "up")
	if r.openAlarm("tcp") != "" || r.conditions() != 0 {
		t.Fatalf("the recovery left alarm %q, %d conditions; want it cleared", r.openAlarm("tcp"), r.conditions())
	}
	if got := scalar(t, r.f.conn, `select (cleared_at is not null)::text from alarm where id = '`+alarm+`'`); got != "true" {
		t.Fatalf("the original alarm cleared = %s, want true", got)
	}
	evs := r.events()
	if len(evs) != 1 || evs[0].key != "condition-ended" || edgeStr(evs[0].ruleID) != id || edgeStr(evs[0].alarmID) != alarm {
		t.Fatalf("events under the new name: %+v; want the clear event, by the rule's uuid, carrying the alarm", evs)
	}
	r.name = "before"
	if evs := r.events(); len(evs) != 1 || evs[0].key != "condition-started" || edgeStr(evs[0].ruleID) != id {
		t.Fatalf("events under the old name: %+v; want the fire event, its historical name kept", evs)
	}
	r.name = "after"

	// Official rules stay fixed: the seed matches them by name.
	if _, err := r.f.gw.RenameEventRule(r.ctx, "", "reachability-lost", "reach-renamed", r.f.all); !errors.Is(err, storage.ErrEventRuleOfficial) {
		t.Fatalf("renaming an official rule: %v, want ErrEventRuleOfficial", err)
	}
	r.rule(storage.EventRuleSpec{Name: "taken"})
	if _, err := r.f.gw.RenameEventRule(r.ctx, "", "after", "taken", r.f.all); !errors.Is(err, storage.ErrEventRuleExists) {
		t.Fatalf("renaming onto a taken name: %v, want ErrEventRuleExists", err)
	}
	if _, err := r.f.gw.RenameEventRule(r.ctx, "", "after", "reachability-lost", r.f.all); !errors.Is(err, storage.ErrEventRuleExists) {
		t.Fatalf("renaming onto an official rule's name: %v, want ErrEventRuleExists", err)
	}
	for _, bad := range []string{"Bad Name", "x:y", id} {
		if _, err := r.f.gw.RenameEventRule(r.ctx, "", "after", bad, r.f.all); !errors.Is(err, storage.ErrEventRuleInvalid) {
			t.Fatalf("renaming to %q: %v, want ErrEventRuleInvalid", bad, err)
		}
	}
	if _, err := r.f.gw.RenameEventRule(r.ctx, "", "after", "elsewhere", scope.Set{}); !errors.Is(err, storage.ErrEventRuleForbidden) {
		t.Fatalf("renaming on a bounded grant: %v, want ErrEventRuleForbidden", err)
	}
	if _, err := r.f.gw.RenameEventRule(r.ctx, "", "nope", "elsewhere", r.f.all); !errors.Is(err, storage.ErrEventRuleNotFound) {
		t.Fatalf("renaming a missing rule: %v, want not found", err)
	}
}

// TestRuleRenameKeepsStreaks proves a rename is not a material change: a
// streak waiting out its sustain keeps running.
func TestRuleRenameKeepsStreaks(t *testing.T) {
	r := newEdgeRig(t)
	r.rule(storage.EventRuleSpec{Name: "slow", Severity: "high", FireSustain: time.Hour})
	r.observe("tcp", "down")
	streak := func() string {
		return scalar(t, r.f.conn, `select coalesce((select pending || '@' || since::text from event_rule_state s
			join event_rule e on e.id = s.rule_id where e.name = '`+r.name+`'), '')`)
	}
	held := streak()
	if held == "" {
		t.Fatal("a down under an hour's sustain left no pending streak")
	}
	if _, err := r.f.gw.RenameEventRule(r.ctx, "", "slow", "slower", r.f.all); err != nil {
		t.Fatal(err)
	}
	r.name = "slower"
	if got := streak(); got != held {
		t.Fatalf("after the rename the streak is %q, want %q kept", got, held)
	}
}

// TestRuleDedupKeyMigration proves the one-time data migration rewrites the
// dedup key of every rule-raised alarm, open or cleared, from the rule's name
// to its uuid, leaves a hand-raised alarm and one whose rule was deleted as
// they are, restores the names on the way down, and changes nothing on a
// second run.
func TestRuleDedupKeyMigration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, ruleDedupKeyVersion); err != nil {
		t.Fatal(err)
	}
	conn := connectDSN(t, dsn)
	mustExec(t, conn, `insert into severity_level (name, rank) values ('high', 40) on conflict do nothing`)
	mustExec(t, conn, `insert into event_type (name) values ('cond-x')`)
	mustExec(t, conn, `insert into event_rule (name, sample_key, sample_lane, fire_criteria, severity, fire_event_type_id)
		values ('reach', 'endpoint-reachable', 'property', '{"expr":"value == \"down\""}', 'high', (select id from event_type where name = 'cond-x'))`)
	mustExec(t, conn, `insert into component (name, product_id) values ('mk-c', (select id from product where name = 'generic-device'))`)
	id := scalar(t, conn, `select id::text from event_rule where name = 'reach'`)
	ins := func(message, dedup string, rule, cleared bool) {
		mustExec(t, conn, `insert into alarm (component_id, severity, message, dedup_key, raised_by_rule_id, raised_at, cleared_at)
			values ((select id from component where name = 'mk-c'), 'high', $1, $2,
				case when $3 then (select id from event_rule where name = 'reach') end, '2026-10-01T00:00:00Z',
				case when $4 then now() end)`, message, dedup, rule, cleared)
	}
	ins("open-tcp", "rule:reach:tcp", true, false)
	ins("open-bare", "rule:reach", true, false)
	ins("cleared-icmp", "rule:reach:icmp", true, true)
	ins("gone", "rule:gone:tcp", false, false)
	ins("by-hand", "rule:reach-by-hand", false, false)
	keys := func() string {
		return scalar(t, conn, `select string_agg(message || '=' || dedup_key, ',' order by message) from alarm`)
	}
	byUUID := "by-hand=rule:reach-by-hand,cleared-icmp=rule:" + id + ":icmp,gone=rule:gone:tcp,open-bare=rule:" + id + ",open-tcp=rule:" + id + ":tcp"
	byName := "by-hand=rule:reach-by-hand,cleared-icmp=rule:reach:icmp,gone=rule:gone:tcp,open-bare=rule:reach,open-tcp=rule:reach:tcp"
	if err := migrate.Run(dsn); err != nil {
		t.Fatal(err)
	}
	if got := keys(); got != byUUID {
		t.Fatalf("after the migration: %s\nwant %s", got, byUUID)
	}
	if err := migrate.RollbackBelow(dsn, ruleDedupKeyVersion); err != nil {
		t.Fatal(err)
	}
	if got := keys(); got != byName {
		t.Fatalf("rolled back: %s\nwant %s", got, byName)
	}
	if err := migrate.Run(dsn); err != nil {
		t.Fatal(err)
	}
	if got := keys(); got != byUUID {
		t.Fatalf("after a second run: %s\nwant %s", got, byUUID)
	}
}
