package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A rule's live state says when it last judged a sample (#929): never, until
// a batch judges one by it; then the batch's time, carried on the get, the
// list and the state alike. The stamp is coalesced: a batch inside the bound
// of the last stamp leaves it, one past the bound moves it. A sample the
// rule does not judge (its rule disabled, its component not selected, older
// than its series' newest) stamps nothing.
func TestRuleLastJudged(t *testing.T) {
	f := newHealthFixture(t)
	ctx, all := context.Background(), scope.Set{All: true}
	if _, err := f.gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "other-c"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := f.gw.CreateEventRule(ctx, "", storage.EventRuleSpec{FireEventType: "condition-started", Name: "judge", SampleKey: "endpoint-reachable",
		Fire: criteria("eq", "down"), Severity: "high", Selector: storage.EventRuleSelector{Components: []string{"bar-1"}}}, all); err != nil {
		t.Fatal(err)
	}
	judged := func() *time.Time {
		t.Helper()
		r, err := f.gw.GetEventRule(ctx, "judge", all)
		if err != nil {
			t.Fatal(err)
		}
		st, err := f.gw.EventRuleState(ctx, "judge", all)
		if err != nil {
			t.Fatal(err)
		}
		list, err := f.gw.ListEventRules(ctx, all)
		if err != nil {
			t.Fatal(err)
		}
		var listed *time.Time
		for _, l := range list {
			if l.Name == "judge" {
				listed = l.LastJudgedAt
			}
		}
		if !sameTime(r.LastJudgedAt, st.LastJudgedAt) || !sameTime(r.LastJudgedAt, listed) {
			t.Fatalf("the get (%v), the state (%v) and the list (%v) disagree on when the rule last judged", r.LastJudgedAt, st.LastJudgedAt, listed)
		}
		return r.LastJudgedAt
	}
	observe := func(component, v string, at time.Time) {
		t.Helper()
		if err := f.gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: component,
			Key: "endpoint-reachable", Instance: "tcp", Value: v, TS: at}}); err != nil {
			t.Fatal(err)
		}
	}

	if got := judged(); got != nil {
		t.Fatalf("a rule that has judged nothing reads last judged %v", got)
	}
	now := time.Now().UTC()
	observe("other-c", "down", now)
	if got := judged(); got != nil {
		t.Fatalf("a sample on a component the rule does not watch stamped it %v", got)
	}
	before := time.Now()
	observe("bar-1", "down", now)
	first := judged()
	if first == nil || first.Before(before.Add(-time.Second)) || first.After(time.Now().Add(time.Second)) {
		t.Fatalf("after judging a sample the rule reads last judged %v, want about now", first)
	}
	observe("bar-1", "up", now.Add(time.Second))
	if got := judged(); !sameTime(got, first) {
		t.Fatalf("a batch inside the bound restamped the rule from %v to %v", first, got)
	}
	if _, err := f.conn.Exec(ctx, `update event_rule_live l set last_judged_at = last_judged_at - interval '1 minute'
		from event_rule r where r.id = l.rule_id and r.name = 'judge'`); err != nil {
		t.Fatal(err)
	}
	aged := judged()
	observe("bar-1", "down", now.Add(-time.Hour)) // history: older than the series' newest
	if got := judged(); !sameTime(got, aged) {
		t.Fatalf("a sample older than its series' newest stamped the rule %v (was %v)", got, aged)
	}
	off := false
	if _, err := f.gw.UpdateEventRule(ctx, "", "judge", storage.EventRulePatch{Enabled: &off}, all); err != nil {
		t.Fatal(err)
	}
	observe("bar-1", "down", now.Add(2*time.Second))
	if got := judged(); !sameTime(got, aged) {
		t.Fatalf("a disabled rule was stamped %v (was %v)", got, aged)
	}
	on := true
	if _, err := f.gw.UpdateEventRule(ctx, "", "judge", storage.EventRulePatch{Enabled: &on}, all); err != nil {
		t.Fatal(err)
	}
	observe("bar-1", "up", now.Add(3*time.Second))
	if got := judged(); got == nil || !got.After(*aged) {
		t.Fatalf("a batch past the bound left the stamp at %v, want it moved past %v", got, aged)
	}
}

// A batch never queues on a rule's stamp: while another transaction holds
// the stamp row, a batch past the bound skips it and commits at once.
func TestRuleStampNeverWaits(t *testing.T) {
	r := newStreakRig(t, "busy", 0)
	ctx := context.Background()
	r.observe(t, "tcp", "down", time.Now().UTC())
	if _, err := r.f.conn.Exec(ctx, `update event_rule_live l set last_judged_at = last_judged_at - interval '1 minute'
		from event_rule e where e.id = l.rule_id and e.name = 'busy'`); err != nil {
		t.Fatal(err)
	}
	other, err := pgx.Connect(ctx, r.f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close(ctx) }()
	tx, err := other.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select 1 from event_rule_live l join event_rule e on e.id = l.rule_id where e.name = 'busy' for update of l`); err != nil {
		t.Fatal(err)
	}
	quick, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := r.f.gw.InsertPropertySamples(quick, []storage.PropertySampleWrite{{OwnerKind: "component", OwnerID: "bar-1",
		Key: "endpoint-reachable", Instance: "tcp", Value: "up", TS: time.Now().UTC()}}); err != nil {
		t.Fatalf("a batch waited on a stamp row another transaction holds: %v", err)
	}
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
