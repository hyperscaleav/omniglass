package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A snooze is a fact about an alarm's CONDITION (#1318, ADR-1318): the
// (component, dedup_key) identity of ADR-0075, set from any alarm of that
// condition and lasting until a deadline. These tests pin the record itself:
// who and until when, on every alarm of the condition and on no other, audited
// on the alarm it was called on, and orthogonal to acknowledgement, health and
// clearing.

// snoozeAudit reads the newest audit row a verb wrote on one alarm: its
// actor's name and its before and after documents.
func snoozeAudit(t *testing.T, dsn, verb, alarmID string) (actor string, before, after map[string]any) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var oldRaw, newRaw []byte
	if err := conn.QueryRow(ctx, `select coalesce(actor_username, ''), old, new from audit_log
		where verb = $1 and resource = 'alarm' and resource_id = $2 order by seq desc limit 1`, verb, alarmID).
		Scan(&actor, &oldRaw, &newRaw); err != nil {
		t.Fatalf("read %s audit row on %s: %v", verb, alarmID, err)
	}
	_ = json.Unmarshal(oldRaw, &before)
	_ = json.Unmarshal(newRaw, &after)
	return actor, before, after
}

func near(t *testing.T, what string, got *time.Time, want time.Time) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s is unset, want about %s", what, want)
	}
	if d := got.Sub(want); d < -time.Minute || d > time.Minute {
		t.Fatalf("%s = %s, want about %s", what, got, want)
	}
}

// TestSnoozeRecordsWhoAndUntilWhenOnTheCondition is the first acceptance
// behavior: an hour's snooze reads about an hour out, names the person who
// set it, and writes one snooze audit row on the alarm it was called on.
func TestSnoozeRecordsWhoAndUntilWhenOnTheCondition(t *testing.T) {
	gw, dsn, comp, raised := ackFixture(t)
	ctx := context.Background()
	actor := ackActor(t, dsn, "jordan-ops")

	got, err := gw.SnoozeAlarm(ctx, actor, comp, raised.ID, time.Hour)
	if err != nil {
		t.Fatalf("snooze: %v", err)
	}
	near(t, "snoozed_until", got.SnoozedUntil, time.Now().Add(time.Hour))
	if got.SnoozedBy != "jordan-ops" {
		t.Errorf("snoozed by %q, want the acting principal's name", got.SnoozedBy)
	}
	if got.Acknowledged() || !got.Active() {
		t.Errorf("snoozing moved the alarm: %+v; a snooze neither acknowledges nor clears", got)
	}
	list, err := gw.ListAlarms(ctx, comp, storage.AlarmFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].SnoozedUntil == nil || list[0].SnoozedBy != "jordan-ops" {
		t.Fatalf("read back %+v, want the snooze on the alarm", list)
	}
	if n := auditRows(t, dsn, "snooze", raised.ID); n != 1 {
		t.Fatalf("%d snooze audit rows, want 1", n)
	}
	who, before, after := snoozeAudit(t, dsn, "snooze", raised.ID)
	if who != "jordan-ops" || before != nil || after["snoozed_until"] == nil || after["dedup_key"] != "no-signal" {
		t.Errorf("snooze audit = %s %v -> %v, want jordan-ops, no before, the deadline and the condition after", who, before, after)
	}

	// The timeline reads the audit row with no change to its read.
	timeline, err := gw.AlarmTimeline(ctx, raised.ComponentID, raised.ID, storage.RecipientView{All: true})
	if err != nil {
		t.Fatal(err)
	}
	if last := timeline[len(timeline)-1]; last.Kind != "audit" || last.What != "snooze" || last.Actor != "jordan-ops" {
		t.Errorf("the timeline ends %+v, want the snooze by jordan-ops", last)
	}
}

// TestASnoozeCoversEveryAlarmOfItsConditionAndNoOther pins what the snooze
// attaches to (decision 1): a re-raise of the same condition is a new row,
// and it reads snoozed; another condition on the same component does not.
func TestASnoozeCoversEveryAlarmOfItsConditionAndNoOther(t *testing.T) {
	gw, dsn, comp, raised := ackFixture(t)
	ctx := context.Background()
	if _, err := gw.SnoozeAlarm(ctx, ackActor(t, dsn, "jordan-ops"), comp, raised.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := gw.ClearAlarm(ctx, "", comp, raised.ID); err != nil {
		t.Fatal(err)
	}
	again, err := gw.RaiseAlarm(ctx, "", comp, storage.AlarmSpec{Severity: "high", Message: "no signal", DedupKey: "no-signal"})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID == raised.ID {
		t.Fatal("the re-raise landed on the cleared row")
	}
	other, err := gw.RaiseAlarm(ctx, "", comp, storage.AlarmSpec{Severity: "warning", Message: "fan", DedupKey: "fan"})
	if err != nil {
		t.Fatal(err)
	}
	list, err := gw.ListAlarms(ctx, comp, storage.AlarmFilter{IncludeCleared: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		snoozed := a.SnoozedUntil != nil
		if want := a.DedupKey == "no-signal"; snoozed != want {
			t.Errorf("alarm %s (%s) snoozed = %v, want %v: clearing does not end a snooze, and it covers its condition only", a.ID, a.DedupKey, snoozed, want)
		}
	}
	if other.SnoozedUntil != nil {
		t.Error("a raise of another condition came back snoozed")
	}
}

// TestReSnoozingReplacesTheDeadlineWithOneAuditRow pins the thin cut: one
// snooze per condition, and a second one replaces its deadline (shorter or
// longer) with an audit row carrying the before and the after.
func TestReSnoozingReplacesTheDeadlineWithOneAuditRow(t *testing.T) {
	gw, dsn, comp, raised := ackFixture(t)
	ctx := context.Background()
	actor := ackActor(t, dsn, "jordan-ops")
	if _, err := gw.SnoozeAlarm(ctx, actor, comp, raised.ID, 4*time.Hour); err != nil {
		t.Fatal(err)
	}
	got, err := gw.SnoozeAlarm(ctx, actor, comp, raised.ID, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "the shortened deadline", got.SnoozedUntil, time.Now().Add(15*time.Minute))
	if n := auditRows(t, dsn, "snooze", raised.ID); n != 2 {
		t.Fatalf("%d snooze audit rows, want 2 (one per call)", n)
	}
	_, before, after := snoozeAudit(t, dsn, "snooze", raised.ID)
	if before["snoozed_until"] == nil || after["snoozed_until"] == nil || before["snoozed_until"] == after["snoozed_until"] {
		t.Errorf("re-snooze audit %v -> %v, want the old deadline before and the new one after", before, after)
	}
}

// TestUnsnoozeEndsItAndAnUnsnoozedConditionIsANoOp pins the early end's
// record, and that ending a snooze that is not there writes nothing.
func TestUnsnoozeEndsItAndAnUnsnoozedConditionIsANoOp(t *testing.T) {
	gw, dsn, comp, raised := ackFixture(t)
	ctx := context.Background()
	actor := ackActor(t, dsn, "jordan-ops")

	got, err := gw.UnsnoozeAlarm(ctx, actor, comp, raised.ID)
	if err != nil {
		t.Fatalf("unsnooze of an unsnoozed condition: %v", err)
	}
	if got.SnoozedUntil != nil || auditRows(t, dsn, "unsnooze", raised.ID) != 0 {
		t.Fatal("unsnoozing a condition that is not snoozed wrote something")
	}

	if _, err := gw.SnoozeAlarm(ctx, actor, comp, raised.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	got, err = gw.UnsnoozeAlarm(ctx, actor, comp, raised.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SnoozedUntil != nil || got.SnoozedBy != "" {
		t.Errorf("after unsnooze the alarm reads %+v, want no snooze", got)
	}
	who, before, _ := snoozeAudit(t, dsn, "unsnooze", raised.ID)
	if who != "jordan-ops" || before["snoozed_until"] == nil {
		t.Errorf("unsnooze audit by %q with before %v, want jordan-ops and the deadline it ended", who, before)
	}
}

// TestSnoozeIsOrthogonalToAcknowledgementAndHealth pins the orthogonality
// list: snoozing does not acknowledge or recompute health, acknowledging does
// not snooze, a snoozed alarm still degrades its component and still sits in
// the unacknowledged queue, and a cleared alarm can be snoozed.
func TestSnoozeIsOrthogonalToAcknowledgementAndHealth(t *testing.T) {
	gw, dsn, comp, raised := ackFixture(t)
	ctx := context.Background()
	actor := ackActor(t, dsn, "jordan-ops")

	rows, verdict := recordedHealth(t, dsn, raised.ComponentID)
	if _, err := gw.SnoozeAlarm(ctx, actor, comp, raised.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if afterRows, afterVerdict := recordedHealth(t, dsn, raised.ComponentID); afterRows != rows || afterVerdict != verdict {
		t.Errorf("snoozing recorded health (%d rows, %s), want none: a snooze only governs notifications", afterRows-rows, afterVerdict)
	}
	if v, err := gw.ComponentVerdict(ctx, raised.ComponentID); err != nil || v != "outage" {
		t.Errorf("a snoozed high alarm leaves its component %q (%v), want outage", v, err)
	}
	queue, err := gw.ListAlarms(ctx, comp, storage.AlarmFilter{Unacknowledged: true})
	if err != nil || len(queue) != 1 {
		t.Fatalf("the unacknowledged queue = %+v (%v), want the snoozed alarm in it", queue, err)
	}

	other, err := gw.RaiseAlarm(ctx, "", comp, storage.AlarmSpec{Severity: "warning", Message: "fan", DedupKey: "fan"})
	if err != nil {
		t.Fatal(err)
	}
	acked, err := gw.AcknowledgeAlarm(ctx, actor, comp, other.ID)
	if err != nil || acked.SnoozedUntil != nil {
		t.Fatalf("acknowledging returned %+v (%v), want it unsnoozed", acked, err)
	}

	if err := gw.ClearAlarm(ctx, "", comp, other.ID); err != nil {
		t.Fatal(err)
	}
	cleared, err := gw.SnoozeAlarm(ctx, actor, comp, other.ID, time.Hour)
	if err != nil || cleared.SnoozedUntil == nil || cleared.Active() {
		t.Fatalf("snoozing a cleared alarm = %+v (%v), want a snoozed, cleared alarm", cleared, err)
	}
}

// TestSnoozeRefusesABadDurationAndAMiss pins the bound (decision 3: one
// minute to seven days) and the misses: an id on another component, an
// unknown one, a malformed one.
func TestSnoozeRefusesABadDurationAndAMiss(t *testing.T) {
	gw, dsn, comp, raised := ackFixture(t)
	ctx := context.Background()
	actor := ackActor(t, dsn, "jordan-ops")
	for _, d := range []time.Duration{0, 59 * time.Second, 7*24*time.Hour + time.Second, -time.Hour} {
		if _, err := gw.SnoozeAlarm(ctx, actor, comp, raised.ID, d); !errors.Is(err, storage.ErrSnoozeDuration) {
			t.Errorf("snooze for %s: err %v, want ErrSnoozeDuration", d, err)
		}
	}
	for _, d := range []time.Duration{storage.SnoozeMin, storage.SnoozeMax} {
		if _, err := gw.SnoozeAlarm(ctx, actor, comp, raised.ID, d); err != nil {
			t.Errorf("snooze for %s (on the bound): %v", d, err)
		}
	}
	for _, id := range []string{"not-a-uuid", "01a15700-0000-7000-8000-000000000000"} {
		if _, err := gw.SnoozeAlarm(ctx, actor, comp, id, time.Hour); !errors.Is(err, storage.ErrAlarmNotFound) {
			t.Errorf("snooze %s: err %v, want ErrAlarmNotFound", id, err)
		}
		if _, err := gw.UnsnoozeAlarm(ctx, actor, comp, id); !errors.Is(err, storage.ErrAlarmNotFound) {
			t.Errorf("unsnooze %s: err %v, want ErrAlarmNotFound", id, err)
		}
	}
	if _, err := gw.SnoozeAlarm(ctx, actor, "no-such-component", raised.ID, time.Hour); !errors.Is(err, storage.ErrAlarmNotFound) {
		t.Errorf("snooze on an unknown component: err %v, want ErrAlarmNotFound", err)
	}
}

// TestUnsnoozingAnExpiredSnoozeEndsItAsTheSweepWould: a snooze past its
// deadline the sweep has not reached was ended by its deadline, not by the
// caller, so an unsnooze writes snooze-end from the platform (and the
// announcement it owes), never an unsnooze that ended nothing.
func TestUnsnoozingAnExpiredSnoozeEndsItAsTheSweepWould(t *testing.T) {
	gw, dsn, comp, raised := ackFixture(t)
	ctx := context.Background()
	actor := ackActor(t, dsn, "jordan-ops")
	if _, err := gw.SnoozeAlarm(ctx, actor, comp, raised.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := gw.(*storage.PG).ExportExpireSnoozes(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := gw.UnsnoozeAlarm(ctx, actor, comp, raised.ID)
	if err != nil || got.SnoozedUntil != nil {
		t.Fatalf("unsnooze of an expired snooze = %+v (%v)", got, err)
	}
	if n := auditRows(t, dsn, "unsnooze", raised.ID); n != 0 {
		t.Errorf("unsnoozing an expired snooze wrote %d unsnooze rows, want none", n)
	}
	if n := auditRows(t, dsn, "snooze-end", raised.ID); n != 1 {
		t.Errorf("unsnoozing an expired snooze wrote %d snooze-end rows, want the one its deadline owed", n)
	}
	// And a snooze after it audits no before: none was in force.
	if _, err := gw.SnoozeAlarm(ctx, actor, comp, raised.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, before, _ := snoozeAudit(t, dsn, "snooze", raised.ID); before != nil {
		t.Errorf("a snooze after an expired one audited before %v, want none", before)
	}
}

// TestConcurrentFirstSnoozesAuditTheReplacement: two first snoozes of one
// condition at once serialize on the condition, so exactly one of them audits
// no before and the other audits the deadline it replaced.
func TestConcurrentFirstSnoozesAuditTheReplacement(t *testing.T) {
	gw, dsn, comp, raised := ackFixture(t)
	ctx := context.Background()
	actor := ackActor(t, dsn, "jordan-ops")
	errs := make(chan error, 2)
	for _, d := range []time.Duration{time.Hour, 4 * time.Hour} {
		go func(d time.Duration) {
			_, err := gw.SnoozeAlarm(ctx, actor, comp, raised.ID, d)
			errs <- err
		}(d)
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var withBefore, without int
	if err := conn.QueryRow(ctx, `select count(*) filter (where old is not null and old <> 'null'::jsonb),
		count(*) filter (where old is null or old = 'null'::jsonb)
		from audit_log where verb = 'snooze' and resource_id = $1`, raised.ID).Scan(&withBefore, &without); err != nil {
		t.Fatal(err)
	}
	if withBefore != 1 || without != 1 {
		t.Errorf("two concurrent first snoozes audited %d with a before and %d without, want one of each", withBefore, without)
	}
}
