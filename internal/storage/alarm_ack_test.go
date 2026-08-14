package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The acknowledgement is a fact about a PERSON, and an alarm's raised state is a
// fact about its CONDITION (ADR-0075: the one-open invariant is per (component,
// dedup_key)). These tests pin that the two never move each other: acknowledging
// leaves cleared_at and the component's verdict exactly where they were, and
// clearing neither grants nor erases an acknowledgement.

// ackActor inserts a service principal and returns its id, so the
// acknowledgement can be checked for WHO as well as WHEN. The read resolves a
// service principal to its own identifying column, the same answer the audit
// write denormalizes (internal/storage/principal_ident.go).
func ackActor(t *testing.T, dsn, name string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var pid string
	if err := conn.QueryRow(ctx, `insert into principal (kind) values ('service') returning id`).Scan(&pid); err != nil {
		t.Fatalf("insert principal: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into service (principal_id, name) values ($1, $2)`, pid, name); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	return pid
}

// ackFixture builds one component with one raised alarm on it.
func ackFixture(t *testing.T) (storage.Gateway, string, string, *storage.Alarm) {
	t.Helper()
	gw, dsn := secretGateway(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "codec-1"}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	a, err := gw.RaiseAlarm(ctx, "", "codec-1", storage.AlarmSpec{
		Severity: "critical", Message: "no signal", DedupKey: "no-signal",
	})
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	if a.Acknowledged() {
		t.Fatal("a freshly raised alarm is already acknowledged: nobody has looked at it yet")
	}
	return gw, dsn, "codec-1", a
}

// TestAcknowledgeRecordsWhoAndWhenWithoutTouchingRaisedState is the core of the
// slice: the acknowledgement records a person, and the alarm stays exactly as
// raised as it was.
func TestAcknowledgeRecordsWhoAndWhenWithoutTouchingRaisedState(t *testing.T) {
	gw, dsn, comp, raised := ackFixture(t)
	ctx := context.Background()
	actor := ackActor(t, dsn, "jordan-ops")

	acked, err := gw.AcknowledgeAlarm(ctx, actor, comp, raised.ID)
	if err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if !acked.Acknowledged() || acked.AcknowledgedAt == nil {
		t.Fatal("acknowledging recorded no acknowledgement time")
	}
	if acked.AcknowledgedBy != "jordan-ops" {
		t.Errorf("acknowledged by %q, want the acting principal's name %q", acked.AcknowledgedBy, "jordan-ops")
	}
	if acked.ClearedAt != nil || !acked.Active() {
		t.Error("acknowledging cleared the alarm: an alarm's raised state belongs to its condition, not to the person who looked at it")
	}
	if acked.ID != raised.ID || acked.DedupKey != raised.DedupKey {
		t.Errorf("acknowledging moved the incident identity: %s/%s then %s/%s", raised.ID, raised.DedupKey, acked.ID, acked.DedupKey)
	}

	// And the same is true when read back, not only in the returned row.
	list, err := gw.ListAlarms(ctx, comp, storage.AlarmFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || !list[0].Acknowledged() || !list[0].Active() {
		t.Fatalf("read back %+v, want one acknowledged alarm that is still active", list)
	}
}

// TestAcknowledgingRecordsNoHealthTransition pins the orthogonality at the health
// chain rather than at the row. Raising and clearing both recompute health in the
// same transaction, and the rollup records a verdict transition as a calculated
// `property` sample. Acknowledging must record nothing: the component is still in
// outage after somebody has looked at it, because acknowledging is not fixing,
// and a transition written here would stamp a recovery that never happened.
func TestAcknowledgingRecordsNoHealthTransition(t *testing.T) {
	gw, dsn, comp, raised := ackFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}

	component, err := gw.GetComponent(ctx, comp, all)
	if err != nil {
		t.Fatalf("get component: %v", err)
	}
	beforeRows, beforeVerdict := recordedHealth(t, dsn, component.ID)
	if beforeVerdict != "outage" {
		t.Fatalf("recorded verdict before the acknowledgement is %q, want outage: the fixture's critical alarm never landed", beforeVerdict)
	}

	if _, err := gw.AcknowledgeAlarm(ctx, ackActor(t, dsn, "jordan-ops"), comp, raised.ID); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}

	afterRows, afterVerdict := recordedHealth(t, dsn, component.ID)
	if afterRows != beforeRows {
		t.Errorf("the acknowledgement recorded %d health sample(s): acknowledging is not fixing, so there is no transition to record", afterRows-beforeRows)
	}
	if afterVerdict != "outage" {
		t.Errorf("recorded verdict %q, want outage: the critical alarm is still raised", afterVerdict)
	}
}

// TestAcknowledgingTwiceIsIdempotent pins the ruling on #728's second open
// decision. The fact recorded is "a human has seen this", which is monotonic:
// the FIRST sighting is the operationally meaningful one (time to acknowledge),
// so a second acknowledgement keeps the first person and the first time, returns
// the row rather than an error, and writes no second audit row, exactly as a
// re-raise of an open condition returns the existing incident (ADR-0075).
func TestAcknowledgingTwiceIsIdempotent(t *testing.T) {
	gw, dsn, comp, raised := ackFixture(t)
	ctx := context.Background()
	first := ackActor(t, dsn, "jordan-ops")
	second := ackActor(t, dsn, "sam-ops")

	one, err := gw.AcknowledgeAlarm(ctx, first, comp, raised.ID)
	if err != nil {
		t.Fatalf("acknowledge 1: %v", err)
	}
	two, err := gw.AcknowledgeAlarm(ctx, second, comp, raised.ID)
	if err != nil {
		t.Fatalf("acknowledge 2 must be a no-op, not a refusal: %v", err)
	}
	if !one.AcknowledgedAt.Equal(*two.AcknowledgedAt) {
		t.Errorf("the second acknowledgement moved the time from %v to %v", one.AcknowledgedAt, two.AcknowledgedAt)
	}
	if two.AcknowledgedBy != "jordan-ops" {
		t.Errorf("the second acknowledgement reassigned the alarm to %q; the first person to look is the one recorded", two.AcknowledgedBy)
	}
	if n := auditRows(t, dsn, "acknowledge", raised.ID); n != 1 {
		t.Errorf("%d acknowledge audit rows, want 1: the no-op leg changed nothing, so it records nothing", n)
	}
}

// TestClearingAndAcknowledgingAreOrthogonal walks all four combinations the
// definition names: acknowledged then cleared keeps the acknowledgement, cleared
// unacknowledged stays unacknowledged, and a cleared alarm can still be
// acknowledged (the record of who looked outlives the fix, exactly as the row
// itself does).
func TestClearingAndAcknowledgingAreOrthogonal(t *testing.T) {
	gw, dsn, comp, raised := ackFixture(t)
	ctx := context.Background()
	actor := ackActor(t, dsn, "jordan-ops")

	if _, err := gw.AcknowledgeAlarm(ctx, actor, comp, raised.ID); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if err := gw.ClearAlarm(ctx, actor, comp, raised.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	kept := findAlarm(t, gw, comp, raised.ID)
	if !kept.Acknowledged() {
		t.Error("clearing erased the acknowledgement: who looked at the incident outlives the fix, like the row does")
	}
	if kept.Active() {
		t.Error("the alarm is still active after clearing")
	}

	// A second condition that comes and goes with nobody looking at it.
	quiet, err := gw.RaiseAlarm(ctx, actor, comp, storage.AlarmSpec{
		Severity: "warning", Message: "fan degraded", DedupKey: "fan.degraded",
	})
	if err != nil {
		t.Fatalf("raise quiet: %v", err)
	}
	if err := gw.ClearAlarm(ctx, actor, comp, quiet.ID); err != nil {
		t.Fatalf("clear quiet: %v", err)
	}
	gone := findAlarm(t, gw, comp, quiet.ID)
	if gone.Acknowledged() {
		t.Error("clearing acknowledged the alarm on the operator's behalf: nobody ever looked at this one")
	}

	// And a cleared alarm is still acknowledgeable: the two facts are independent
	// in both directions, so the person reviewing the history can record that they
	// read it.
	late, err := gw.AcknowledgeAlarm(ctx, actor, comp, quiet.ID)
	if err != nil {
		t.Fatalf("acknowledge a cleared alarm: %v", err)
	}
	if !late.Acknowledged() || late.Active() {
		t.Errorf("acknowledging a cleared alarm gave %+v, want acknowledged and still cleared", late)
	}
}

// TestAcknowledgeWritesItsAuditRow pins ship-slice gate 10 for this write: a
// committed privileged change carries its audit row, in the same transaction.
func TestAcknowledgeWritesItsAuditRow(t *testing.T) {
	gw, dsn, comp, raised := ackFixture(t)
	ctx := context.Background()
	actor := ackActor(t, dsn, "jordan-ops")

	if _, err := gw.AcknowledgeAlarm(ctx, actor, comp, raised.ID); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var gotActor, gotResource, gotID, gotUsername string
	if err := conn.QueryRow(ctx, `
		select coalesce(actor_principal_id::text, ''), resource, coalesce(resource_id, ''), coalesce(actor_username, '')
		from audit_log where verb = 'acknowledge' order by ts desc limit 1`).
		Scan(&gotActor, &gotResource, &gotID, &gotUsername); err != nil {
		t.Fatalf("read audit row: %v", err)
	}
	if gotActor != actor || gotResource != "alarm" || gotID != raised.ID {
		t.Errorf("audit row = actor %s / %s / %s, want %s / alarm / %s", gotActor, gotResource, gotID, actor, raised.ID)
	}
	if gotUsername != "jordan-ops" {
		t.Errorf("audit row names %q, want the acting principal's name", gotUsername)
	}
}

// TestAcknowledgeMissesAreNamed keeps a typo, an id from another component, and
// a malformed address off the 500 path: each is a miss, and the alarm is not
// silently acknowledged elsewhere.
func TestAcknowledgeMissesAreNamed(t *testing.T) {
	gw, dsn, comp, raised := ackFixture(t)
	ctx := context.Background()
	actor := ackActor(t, dsn, "jordan-ops")
	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "codec-2"}, all, all, all, all); err != nil {
		t.Fatalf("second component: %v", err)
	}

	cases := []struct {
		name      string
		component string
		alarmID   string
	}{
		{"a malformed alarm id", comp, "not-a-uuid"},
		{"an alarm that does not exist", comp, uuid.NewString()},
		{"an alarm belonging to another component", "codec-2", raised.ID},
		{"a component that does not exist", "codec-404", raised.ID},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := gw.AcknowledgeAlarm(ctx, actor, c.component, c.alarmID); !errors.Is(err, storage.ErrAlarmNotFound) {
				t.Fatalf("acknowledge = %v, want ErrAlarmNotFound", err)
			}
		})
	}
	if findAlarm(t, gw, comp, raised.ID).Acknowledged() {
		t.Error("a missed acknowledgement landed on the real alarm anyway")
	}
}

// TestListAlarmsExpressesRaisedAndUnacknowledged pins the read side of the
// definition: the queue an operator actually works is what is raised and nobody
// has looked at, and the gateway can say it in one query rather than making
// every caller filter.
func TestListAlarmsExpressesRaisedAndUnacknowledged(t *testing.T) {
	gw, dsn, comp, raised := ackFixture(t)
	ctx := context.Background()
	actor := ackActor(t, dsn, "jordan-ops")

	seen, err := gw.RaiseAlarm(ctx, actor, comp, storage.AlarmSpec{
		Severity: "warning", Message: "fan degraded", DedupKey: "fan.degraded",
	})
	if err != nil {
		t.Fatalf("raise second: %v", err)
	}
	if _, err := gw.AcknowledgeAlarm(ctx, actor, comp, seen.ID); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	old, err := gw.RaiseAlarm(ctx, actor, comp, storage.AlarmSpec{
		Severity: "info", Message: "rebooted", DedupKey: "rebooted",
	})
	if err != nil {
		t.Fatalf("raise third: %v", err)
	}
	if err := gw.ClearAlarm(ctx, actor, comp, old.ID); err != nil {
		t.Fatalf("clear third: %v", err)
	}

	queue, err := gw.ListAlarms(ctx, comp, storage.AlarmFilter{Unacknowledged: true})
	if err != nil {
		t.Fatalf("list unacknowledged: %v", err)
	}
	if len(queue) != 1 || queue[0].ID != raised.ID {
		t.Fatalf("the raised-and-unacknowledged queue is %+v, want only %s", queue, raised.ID)
	}

	// The filter is about the acknowledgement, not about the clear: asking for the
	// history as well brings the cleared, never-acknowledged incident back.
	history, err := gw.ListAlarms(ctx, comp, storage.AlarmFilter{Unacknowledged: true, IncludeCleared: true})
	if err != nil {
		t.Fatalf("list unacknowledged history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("unacknowledged history is %+v, want the raised one and the cleared one nobody looked at", history)
	}
}

// findAlarm reads one alarm back out of the component's full history.
func findAlarm(t *testing.T, gw storage.Gateway, component, id string) storage.Alarm {
	t.Helper()
	list, err := gw.ListAlarms(context.Background(), component, storage.AlarmFilter{IncludeCleared: true})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, a := range list {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("alarm %s is not in %+v", id, list)
	return storage.Alarm{}
}

// recordedHealth reads the health samples the rollup has recorded for one
// component: how many, and the newest verdict. The rollup writes a calculated
// `property` sample only on a TRANSITION, so the count is what says whether a
// write recomputed health at all.
func recordedHealth(t *testing.T, dsn, componentID string) (int, string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var n int
	var latest string
	if err := conn.QueryRow(ctx, `
		select count(*), coalesce((array_agg(value #>> '{}' order by id desc))[1], '')
		from property
		where component_id = $1::uuid
		  and property_type_id = (select id from property_type where name = 'health')
		  and source_rule = 'health-rollup'`, componentID).Scan(&n, &latest); err != nil {
		t.Fatalf("read recorded health: %v", err)
	}
	return n, latest
}

// auditRows counts the audit rows a verb wrote against one resource id.
func auditRows(t *testing.T, dsn, verb, resourceID string) int {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx,
		`select count(*) from audit_log where verb = $1 and resource_id = $2`, verb, resourceID).Scan(&n); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return n
}
