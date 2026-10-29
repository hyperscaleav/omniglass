package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// An alarm's audit rows outlive the alarm (#1339, ruled 2026-10-29): retention
// may remove a cleared alarm, never its audit rows, so each row must say on
// its own what was done, what it changed and what it became. Every alarm
// audit row but the raise (a create, whose new image is the whole alarm)
// carries a before and an after image of the field it moved, and names the
// condition (component and dedup key) it was about.
func TestEveryAlarmAuditRowCarriesItsBeforeAndAfter(t *testing.T) {
	gw, dsn, comp, raised := ackFixture(t)
	ctx := context.Background()
	actor := ackActor(t, dsn, "jordan-ops")
	// The API addresses the component by its uuid (requireComponentInScope),
	// and the row must still name it: a uuid outlives nothing once the
	// component is deleted.
	byID := raised.ComponentID

	if _, err := gw.SnoozeAlarm(ctx, actor, byID, raised.ID, time.Hour); err != nil {
		t.Fatalf("snooze: %v", err)
	}
	if _, err := gw.UnsnoozeAlarm(ctx, actor, byID, raised.ID); err != nil {
		t.Fatalf("unsnooze: %v", err)
	}
	if _, err := gw.AcknowledgeAlarm(ctx, actor, byID, raised.ID); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if err := gw.ClearAlarm(ctx, actor, byID, raised.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	var raiseNew map[string]any
	if err := conn.QueryRow(ctx, `select new from audit_log where resource = 'alarm' and resource_id = $1 and verb = 'create'`,
		raised.ID).Scan(&raiseNew); err != nil {
		t.Fatalf("read the raise row: %v", err)
	}
	for _, k := range []string{"Severity", "Message", "DedupKey"} {
		if raiseNew[k] == nil || raiseNew[k] == "" {
			t.Errorf("the raise row's new image lacks %s: %v", k, raiseNew)
		}
	}

	// Each verb, the field it moves.
	moved := map[string]string{
		"snooze":      "snoozed_until",
		"unsnooze":    "snoozed_until",
		"acknowledge": "acknowledged_at",
		"update":      "cleared_at",
	}
	for verb, field := range moved {
		var old, new map[string]any
		if err := conn.QueryRow(ctx, `select old, new from audit_log
			where resource = 'alarm' and resource_id = $1 and verb = $2 order by seq desc limit 1`,
			raised.ID, verb).Scan(&old, &new); err != nil {
			t.Fatalf("read the %s row: %v", verb, err)
		}
		if old == nil || new == nil {
			t.Errorf("%s row: old %v, new %v; want both images", verb, old, new)
			continue
		}
		before, hasBefore := old[field]
		after, hasAfter := new[field]
		if !hasBefore || !hasAfter {
			t.Errorf("%s row: %s before %v (present %v), after %v (present %v); want it in both images",
				verb, field, before, hasBefore, after, hasAfter)
			continue
		}
		if before == after {
			t.Errorf("%s row: %s is %v before and after; want the change", verb, field, before)
		}
		if new["component"] != comp || new["dedup_key"] != raised.DedupKey {
			t.Errorf("%s row: new image names %v / %v, want the condition %s / %s",
				verb, new["component"], new["dedup_key"], comp, raised.DedupKey)
		}
	}
}

// A rule's clear (clearAlarmByDedupTx) carries the same before and after as a
// hand clear, and names its condition.
func TestARulesClearAuditsItsBeforeAndAfter(t *testing.T) {
	r := newEdgeRig(t)
	r.rule(storage.EventRuleSpec{Name: "audited", Severity: "high", ClearEventType: "condition-ended"})
	r.observe("tcp", "down")
	alarm := r.openAlarm("tcp")
	if alarm == "" {
		t.Fatal("the rule opened no alarm")
	}
	r.observe("tcp", "up")
	var old, new map[string]any
	if err := r.f.conn.QueryRow(r.ctx, `select old, new from audit_log
		where resource = 'alarm' and resource_id = $1 and verb = 'update'`, alarm).Scan(&old, &new); err != nil {
		t.Fatalf("read the rule's clear row: %v", err)
	}
	if v, ok := old["cleared_at"]; !ok || v != nil || new["cleared_at"] == nil {
		t.Errorf("rule clear: old %v, new %v; want cleared_at null before and set after", old, new)
	}
	if new["component"] != "bar-1" || new["dedup_key"] == nil || new["dedup_key"] == "" {
		t.Errorf("rule clear: new %v, want the condition named (bar-1 and its dedup key)", new)
	}
}
