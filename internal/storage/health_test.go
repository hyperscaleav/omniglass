package storage_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/health"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// healthFixture is the estate the health tests reason over: a location tree with
// a system in its deepest room, a standard that wants one table mic, and a room
// bar staffing it. Everything below asserts on how an alarm travels this chain.
type healthFixture struct {
	gw   *storage.PG
	conn *pgx.Conn
	all  scope.Set
}

func newHealthFixture(t *testing.T) *healthFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })

	f := &healthFixture{gw: gw, conn: conn, all: scope.Set{All: true}}

	// hq (campus) > hq-b1 (building) > hq-r1 (room), with the system in the room,
	// so the rollup has two ancestor hops to climb and a miss is visible.
	f.mustLocation(t, ctx, "hq", "campus", nil)
	f.mustLocation(t, ctx, "hq-b1", "building", ptrStr("hq"))
	f.mustLocation(t, ctx, "hq-r1", "room", ptrStr("hq-b1"))

	if err := gw.UpsertStandard(ctx, storage.Standard{Name: "health-huddle", DisplayName: "Health Huddle"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	std, room := "health-huddle", "hq-r1"
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "hq-huddle", StandardID: &std, LocationName: &room,
	}, f.all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	// One table mic, quorum 1, an impaired one degrades its system.
	if _, err := gw.SetSystemRole(ctx, "", "standard", "health-huddle", storage.SystemRoleSpec{
		Name: "table-mic", DisplayName: "Table microphone", Quorum: 1,
		AcceptedTypes: []string{"video-bar"}, Impact: "degraded",
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}

	// cisco-room-bar classifies as video-bar.
	bar := "cisco-room-bar"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1", ProductName: &bar}, f.all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if err := gw.AssignRole(ctx, "", "hq-huddle", "table-mic", "bar-1", f.all); err != nil {
		t.Fatalf("assign: %v", err)
	}
	return f
}

func (f *healthFixture) mustLocation(t *testing.T, ctx context.Context, name, kind string, parent *string) {
	t.Helper()
	if _, err := f.gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: name, LocationType: kind, ParentName: parent,
	}, f.all); err != nil {
		t.Fatalf("create location %s: %v", name, err)
	}
}

// recorded counts the health rows for one owner and returns the latest value.
// The count is what proves the transition-only discipline: the verdict alone
// cannot tell a write that changed something from one that wrote a duplicate.
func (f *healthFixture) recorded(t *testing.T, ctx context.Context, ownerKind, ownerID string) (int, string) {
	t.Helper()
	col := map[string]string{"component": "component_id", "system": "system_id", "location": "location_id"}[ownerKind]
	if col == "" {
		t.Fatalf("unknown owner kind %q", ownerKind)
	}
	var n int
	var latest *string
	// The arc stores the owner's id; the tests speak names, so the id resolves here.
	//
	// "Latest" is the highest id, NOT the newest ts, matching every production read
	// of a recorded verdict (recordHealth's transition check, subtreeSystemHealth,
	// SystemHealth). A health row's ts is clock_timestamp() evaluated in the SELECT
	// list, while its id comes from the identity sequence applied when the row is
	// inserted, so the timestamp is taken BEFORE the id is assigned. Two concurrent
	// inserts can therefore land with ts inverted relative to id, and a reader
	// ordering by ts then disagrees with the writer about which row is current.
	// This helper used to order by ts, which is why it reported verdicts the
	// gateway never produced (#356).
	owner := `(select id from ` + ownerKind + ` where name = $1)`
	if err := f.conn.QueryRow(ctx, `
		select count(*), (select value #>> '{}' from property
			where `+col+` = `+owner+` and property_type_id = (select id from property_type where name = 'health') order by id desc limit 1)
		from property where `+col+` = `+owner+` and property_type_id = (select id from property_type where name = 'health')`, ownerID).Scan(&n, &latest); err != nil {
		t.Fatalf("read recorded health %s/%s: %v", ownerKind, ownerID, err)
	}
	if latest == nil {
		return n, ""
	}
	return n, *latest
}

func ptrStr(s string) *string { return &s }

// verdictSeq flattens a transition series to just its values, which is what the
// assertions are actually about.
func verdictSeq(ts []storage.HealthTransition) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Value)
	}
	return out
}

func sameSeq(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestHealthTransitionsThroughTheChain is the slice's central proof. An alarm
// that takes a component down (its own verdict, no longer per-capability, #626)
// must move the component, its system, and every location above it, recording
// exactly one row each; a SECOND alarm that changes no verdict must record
// nothing at all; and clearing must move everything back. The second-alarm
// assertion is the transition-only property, which is what makes the history a
// record of edges rather than of writes.
func TestHealthTransitionsThroughTheChain(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	// Baseline. Staffing the role already recorded a healthy start for everything
	// in the chain, so the assertions below are on the DELTA an alarm causes.
	// The component's own arc reads the alarm severity directly (outage, once
	// the deciding alarm is critical); everything above it reads the role's
	// declared impact (degraded), a different axis entirely.
	type owner struct {
		kind, id, downWant string
	}
	chain := []owner{
		{"component", "bar-1", "outage"}, {"system", "hq-huddle", "degraded"},
		{"location", "hq-r1", "degraded"}, {"location", "hq-b1", "degraded"}, {"location", "hq", "degraded"},
	}
	before := map[owner]int{}
	for _, o := range chain {
		n, v := f.recorded(t, ctx, o.kind, o.id)
		if v != "healthy" {
			t.Fatalf("baseline %s/%s = %q, want healthy (the first value is always recorded)", o.kind, o.id, v)
		}
		before[o] = n
	}

	// The critical alarm takes bar-1 down, its only occupant, so the role drops
	// below quorum.
	alarm, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "critical", Message: "mic array not responding",
	})
	if err != nil {
		t.Fatalf("raise alarm: %v", err)
	}

	for _, o := range chain {
		n, v := f.recorded(t, ctx, o.kind, o.id)
		if v != o.downWant {
			t.Errorf("after the alarm %s/%s = %q, want %s", o.kind, o.id, v, o.downWant)
		}
		if got := n - before[o]; got != 1 {
			t.Errorf("%s/%s recorded %d rows for one transition, want exactly 1", o.kind, o.id, got)
		}
		before[o] = n
	}

	// A SECOND, lesser alarm that changes nothing: the component is already
	// outage from the first, worse wins so it stays outage, and the role is
	// already below quorum, so no verdict in the chain moves. Nothing may be
	// written. This is the property the whole recording design exists for.
	second, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "warning", Message: "speaker distortion",
	})
	if err != nil {
		t.Fatalf("raise second alarm: %v", err)
	}
	for _, o := range chain {
		n, _ := f.recorded(t, ctx, o.kind, o.id)
		if n != before[o] {
			t.Errorf("%s/%s recorded %d rows for a no-op recompute, want %d (transition-only)",
				o.kind, o.id, n, before[o])
		}
	}

	// Clearing the second alarm still leaves the first one active, so still
	// nothing moves.
	if err := f.gw.ClearAlarm(ctx, "", "bar-1", second.ID); err != nil {
		t.Fatalf("clear second alarm: %v", err)
	}
	for _, o := range chain {
		n, v := f.recorded(t, ctx, o.kind, o.id)
		if n != before[o] || v != o.downWant {
			t.Errorf("%s/%s = %d rows / %q after clearing a non-deciding alarm, want %d / %s",
				o.kind, o.id, n, v, before[o], o.downWant)
		}
	}

	// Clearing the deciding alarm returns the whole chain to healthy, one row each.
	if err := f.gw.ClearAlarm(ctx, "", "bar-1", alarm.ID); err != nil {
		t.Fatalf("clear alarm: %v", err)
	}
	for _, o := range chain {
		n, v := f.recorded(t, ctx, o.kind, o.id)
		if v != "healthy" {
			t.Errorf("after the clear %s/%s = %q, want healthy", o.kind, o.id, v)
		}
		if got := n - before[o]; got != 1 {
			t.Errorf("%s/%s recorded %d rows for the recovery, want exactly 1", o.kind, o.id, got)
		}
	}

	// Clearing an already-cleared alarm is an explicit miss, not a silent success.
	if err := f.gw.ClearAlarm(ctx, "", "bar-1", alarm.ID); !errors.Is(err, storage.ErrAlarmNotFound) {
		t.Errorf("clear twice: err = %v, want ErrAlarmNotFound", err)
	}
}

// TestHealthReportNamesTheCause is the reconciliation UX in one assertion: a
// degraded system must say WHICH role is impaired, WHICH assigned component is
// down, and WHICH alarm took it down. A verdict an operator cannot act on is not
// worth recording.
func TestHealthReportNamesTheCause(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	alarm, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "critical", Message: "mic array not responding",
	})
	if err != nil {
		t.Fatalf("raise alarm: %v", err)
	}

	rep, err := f.gw.SystemHealth(ctx, "hq-huddle", 0, f.all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	if rep.Verdict != "degraded" {
		t.Fatalf("system verdict = %q, want degraded (the role's impact, not the alarm's severity)", rep.Verdict)
	}
	if len(rep.Roles) != 1 {
		t.Fatalf("roles = %+v, want the one table-mic", rep.Roles)
	}
	role := rep.Roles[0]
	if role.Name != "table-mic" || !role.Impaired || role.Satisfying != 0 || role.Quorum != 1 {
		t.Fatalf("role = %+v, want table-mic impaired with 0 of 1 satisfying", role)
	}
	if role.Impact != "degraded" {
		t.Errorf("role impact = %q, want degraded", role.Impact)
	}
	if !hasAll(role.Down, "bar-1") || len(role.Down) != 1 {
		t.Errorf("down = %v, want exactly the assigned component the alarm took down (bar-1)", role.Down)
	}
	if len(role.Alarms) != 1 || role.Alarms[0].ID != alarm.ID {
		t.Fatalf("alarms = %+v, want the one that caused it (%s)", role.Alarms, alarm.ID)
	}
	if role.Alarms[0].Message != "mic array not responding" {
		t.Errorf("alarm message = %q, want the operator's own words back", role.Alarms[0].Message)
	}
	// The transitions are the edges, oldest first, and the series tells the whole
	// story of the fixture: healthy the moment the system was created (its standard
	// wanted nothing yet), degraded when the role was declared with nobody filling
	// it, healthy once the bar was assigned, degraded again now. Four writes, four
	// edges, no samples in between.
	if got := verdictSeq(rep.Transitions); !sameSeq(got, []string{"healthy", "degraded", "healthy", "degraded"}) {
		t.Fatalf("transitions = %v, want healthy (created), degraded (role declared unstaffed), healthy (staffed), degraded (alarm)", got)
	}

	// The location report is the drill-down: it names the system at fault rather
	// than repeating the role detail, which the system read already answers.
	loc, err := f.gw.LocationHealth(ctx, "hq", 0, f.all)
	if err != nil {
		t.Fatalf("location health: %v", err)
	}
	if loc.Verdict != "degraded" {
		t.Errorf("location verdict = %q, want degraded", loc.Verdict)
	}
	if len(loc.Systems) != 1 || loc.Systems[0].Name != "hq-huddle" || loc.Systems[0].Verdict != "degraded" {
		t.Fatalf("location systems = %+v, want hq-huddle degraded", loc.Systems)
	}
}

// TestHealthImpactAndQuorum proves the two declaration-side levers actually reach
// the verdict: a role whose impact is outage escalates its system past degraded,
// and one whose impact is none never escalates it at all. Both are recomputed at
// the declaration write, so changing impact alone moves recorded health.
func TestHealthImpactAndQuorum(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "critical", Message: "mic dead",
	}); err != nil {
		t.Fatalf("raise alarm: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "degraded" {
		t.Fatalf("system = %q, want degraded", v)
	}

	// Re-declaring the role with impact outage escalates the same broken component:
	// the slot it was filling is what decides, not the component.
	if _, err := f.gw.SetSystemRole(ctx, "", "standard", "health-huddle", storage.SystemRoleSpec{
		Name: "table-mic", DisplayName: "Table microphone", Quorum: 1,
		AcceptedTypes: []string{"video-bar"}, Impact: "outage",
	}); err != nil {
		t.Fatalf("re-declare role as outage: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "outage" {
		t.Fatalf("system after impact=outage = %q, want outage", v)
	}
	if _, v := f.recorded(t, ctx, "location", "hq"); v != "outage" {
		t.Errorf("location after impact=outage = %q, want outage (worst-wins climbs)", v)
	}

	// impact none: the role is still impaired, but its failure is declared not to
	// matter, so the system reads healthy again.
	if _, err := f.gw.SetSystemRole(ctx, "", "standard", "health-huddle", storage.SystemRoleSpec{
		Name: "table-mic", DisplayName: "Table microphone", Quorum: 1,
		AcceptedTypes: []string{"video-bar"}, Impact: "none",
	}); err != nil {
		t.Fatalf("re-declare role as none: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "healthy" {
		t.Fatalf("system after impact=none = %q, want healthy", v)
	}
	// The component itself is still alarmed: impact governs the system, never the
	// component's own condition.
	if _, v := f.recorded(t, ctx, "component", "bar-1"); v != "outage" {
		t.Errorf("component = %q, want outage regardless of the role's impact", v)
	}

	// An unknown impact is a named refusal, not a constraint violation.
	if _, err := f.gw.SetSystemRole(ctx, "", "standard", "health-huddle", storage.SystemRoleSpec{
		Name: "table-mic", Quorum: 1, Impact: "catastrophic",
	}); !errors.Is(err, storage.ErrRoleImpact) {
		t.Errorf("bad impact: err = %v, want ErrRoleImpact", err)
	}
}

// TestHealthMovesOnUnassign proves the trigger set covers the writes that REMOVE
// a link. Unassigning the last satisfying component impairs the role, and the
// assignment row is already gone by then, so a recompute that walked the
// component's assignments would silently miss the system it just left.
func TestHealthMovesOnUnassign(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "healthy" {
		t.Fatalf("baseline system = %q, want healthy", v)
	}
	if err := f.gw.UnassignRole(ctx, "", "hq-huddle", "table-mic", "bar-1", f.all); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "degraded" {
		t.Fatalf("system after unassigning the only component = %q, want degraded", v)
	}
	// Staffing it again recovers, which is the same trigger in the other direction.
	if err := f.gw.AssignRole(ctx, "", "hq-huddle", "table-mic", "bar-1", f.all); err != nil {
		t.Fatalf("re-assign: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "healthy" {
		t.Fatalf("system after re-staffing = %q, want healthy", v)
	}

	// An alarm is the other removal shape: the component stays assigned, but a
	// critical one stops it occupying the slot because its own verdict went to
	// outage.
	alarm, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "critical", Message: "mic dead",
	})
	if err != nil {
		t.Fatalf("raise alarm: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "degraded" {
		t.Fatalf("system after the assignee went down = %q, want degraded", v)
	}
	if err := f.gw.ClearAlarm(ctx, "", "bar-1", alarm.ID); err != nil {
		t.Fatalf("clear alarm: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "healthy" {
		t.Fatalf("system after the assignee recovered = %q, want healthy", v)
	}
}

// TestAlarmListingAndRefusals covers the alarm surface's own contract: the active
// set versus the history, and the request faults that must not read as server
// errors.
func TestAlarmListingAndRefusals(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	a1, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "warning", Message: "one",
	})
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "info", Message: "two"}); err != nil {
		t.Fatalf("raise second: %v", err)
	}
	if got, _ := f.gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{}); len(got) != 2 {
		t.Fatalf("active alarms = %d, want 2", len(got))
	}
	if err := f.gw.ClearAlarm(ctx, "", "bar-1", a1.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	active, _ := f.gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{})
	if len(active) != 1 || active[0].ClearedAt != nil {
		t.Fatalf("active after clearing one = %+v, want just the uncleared one", active)
	}
	all, _ := f.gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{IncludeCleared: true})
	if len(all) != 2 {
		t.Fatalf("history = %d, want 2: clearing keeps the row", len(all))
	}

	// Request faults, each its own named sentinel.
	if _, err := f.gw.RaiseAlarm(ctx, "", "no-such-component", storage.AlarmSpec{Severity: "info"}); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Errorf("raise on unknown component: err = %v, want ErrComponentNotFound", err)
	}
	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "apocalyptic"}); !errors.Is(err, storage.ErrAlarmSeverity) {
		t.Errorf("bad severity: err = %v, want ErrAlarmSeverity", err)
	}
	// A malformed id must not reach Postgres as a cast error.
	if err := f.gw.ClearAlarm(ctx, "", "bar-1", "not-a-uuid"); !errors.Is(err, storage.ErrAlarmNotFound) {
		t.Errorf("malformed alarm id: err = %v, want ErrAlarmNotFound", err)
	}
	if _, err := f.gw.ListAlarms(ctx, "no-such-component", storage.AlarmFilter{}); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Errorf("list on unknown component: err = %v, want ErrComponentNotFound", err)
	}
}

// pureRoles projects a report's role rows back onto the pure rollup's input,
// synthesizing one satisfying component for each the report counts as satisfying.
// It is how the consistency assertions below judge the served verdict with the
// same function the recompute uses, against the evidence the report itself shows.
func pureRoles(rows []storage.HealthRole) []health.Role {
	out := make([]health.Role, 0, len(rows))
	for _, r := range rows {
		role := health.Role{Name: r.Name, Quorum: r.Quorum, Impact: r.Impact}
		for i := 0; i < r.Satisfying; i++ {
			role.Assigned = append(role.Assigned, health.Component{
				Name: fmt.Sprintf("%s-%d", r.Name, i), Verdict: health.Healthy,
			})
		}
		out = append(out, role)
	}
	return out
}

// mustAgree is the invariant the report owes an operator: the headline verdict and
// the evidence printed beside it must be the same judgement. A report that says
// healthy next to an impaired outage role is worse than no report, because it is
// believed.
func mustAgree(t *testing.T, rep *storage.HealthReport) {
	t.Helper()
	want := health.SystemVerdict(pureRoles(rep.Roles)).String()
	if rep.Verdict != want {
		t.Fatalf("verdict %q contradicts its own roles (which roll up to %q): %+v", rep.Verdict, want, rep.Roles)
	}
}

// TestHealthReportOfAFreshSystem is the regression. A system created against a
// standard that already declares roles inherits them the instant it exists, and
// nobody is assigned, so it is broken before anyone touches it. Reading it must
// say so, and must not say healthy beside the impaired role it is printing.
func TestHealthReportOfAFreshSystem(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	// The standard and its role exist BEFORE the system does, which is the ordering
	// that used to leave nothing recorded for the system at read time.
	if err := f.gw.UpsertStandard(ctx, storage.Standard{Name: "health-podium", DisplayName: "Health Podium"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	if _, err := f.gw.SetSystemRole(ctx, "", "standard", "health-podium", storage.SystemRoleSpec{
		Name: "mic", DisplayName: "Microphone", Quorum: 1,
		AcceptedTypes: []string{"video-bar"}, Impact: "outage",
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}
	std, room := "health-podium", "hq-r1"
	if _, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "fresh", StandardID: &std, LocationName: &room,
	}, f.all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}

	rep, err := f.gw.SystemHealth(ctx, "fresh", 0, f.all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	if len(rep.Roles) != 1 || !rep.Roles[0].Impaired || rep.Roles[0].Impact != "outage" {
		t.Fatalf("roles = %+v, want the one inherited mic role, impaired at impact outage", rep.Roles)
	}
	if rep.Verdict != "outage" {
		t.Fatalf("verdict = %q, want outage: an unstaffed outage-impact role is not healthy", rep.Verdict)
	}
	mustAgree(t, rep)

	// Creating it recorded the opening edge, so the history has a defined start
	// rather than beginning at whatever later write happened to notice.
	n, v := f.recorded(t, ctx, "system", "fresh")
	if n != 1 || v != "outage" {
		t.Fatalf("recorded = %d rows / %q, want exactly 1 / outage (the opening verdict)", n, v)
	}
	if got := verdictSeq(rep.Transitions); !sameSeq(got, []string{"outage"}) {
		t.Fatalf("transitions = %v, want the single opening edge", got)
	}
	// The opening verdict climbed to the room the system was created in, and the
	// location report agrees with the systems it lists.
	loc, err := f.gw.LocationHealth(ctx, "hq-r1", 0, f.all)
	if err != nil {
		t.Fatalf("location health: %v", err)
	}
	if loc.Verdict != "outage" {
		t.Fatalf("location verdict = %q, want outage", loc.Verdict)
	}
	if _, v := f.recorded(t, ctx, "location", "hq"); v != "outage" {
		t.Errorf("campus = %q, want outage: the opening verdict rolls up like any other", v)
	}
}

// TestHealthSurvivesARename is the consequence of recording an opening verdict:
// every system now has a history from the moment it exists, so the rename the
// console offers has to carry that history rather than trip over it. The record
// addresses its owner by name, and a name is renameable, so the reference moves
// with the entity.
func TestHealthSurvivesARename(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	room := "hq-r1"
	if _, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "plain", LocationName: &room}, f.all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	before, v := f.recorded(t, ctx, "system", "plain")
	if before != 1 || v != "healthy" {
		t.Fatalf("opening record = %d rows / %q, want 1 / healthy", before, v)
	}

	renamed := "plain-renamed"
	if _, err := f.gw.RenameSystem(ctx, "", "plain", renamed, f.all, f.all); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if n, v := f.recorded(t, ctx, "system", renamed); n != before || v != "healthy" {
		t.Fatalf("history after the rename = %d rows / %q, want %d / healthy under the new name", n, v, before)
	}
	if n, _ := f.recorded(t, ctx, "system", "plain"); n != 0 {
		t.Errorf("old name still holds %d health rows, want none: the history moved, it was not copied", n)
	}
}

// TestHealthMovesOnStandardChange proves the standard swap is a trigger: it
// replaces the whole inherited role set, so the verdict can flip with no alarm and
// no assignment involved.
func TestHealthMovesOnStandardChange(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	if err := f.gw.UpsertStandard(ctx, storage.Standard{Name: "health-podium", DisplayName: "Health Podium"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	if _, err := f.gw.SetSystemRole(ctx, "", "standard", "health-podium", storage.SystemRoleSpec{
		Name: "mic", Quorum: 1, AcceptedTypes: []string{"video-bar"}, Impact: "outage",
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}
	// A second standard that claims nothing at all: conforming to it is what makes
	// the system healthy again.
	if err := f.gw.UpsertStandard(ctx, storage.Standard{Name: "health-plain", DisplayName: "Health Plain"}); err != nil {
		t.Fatalf("create plain standard: %v", err)
	}
	std, room := "health-podium", "hq-r1"
	if _, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "fresh", StandardID: &std, LocationName: &room,
	}, f.all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	before, v := f.recorded(t, ctx, "system", "fresh")
	if v != "outage" {
		t.Fatalf("opening verdict = %q, want outage", v)
	}

	plain := "health-plain"
	if _, err := f.gw.UpdateSystem(ctx, "", "fresh", storage.SystemPatch{StandardID: &plain}, f.all, f.all); err != nil {
		t.Fatalf("change standard: %v", err)
	}
	n, v := f.recorded(t, ctx, "system", "fresh")
	if v != "healthy" {
		t.Fatalf("verdict after conforming to a standard that wants nothing = %q, want healthy", v)
	}
	if n-before != 1 {
		t.Fatalf("recorded %d rows for the standard change, want exactly 1", n-before)
	}
	rep, err := f.gw.SystemHealth(ctx, "fresh", 0, f.all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	if len(rep.Roles) != 0 {
		t.Fatalf("roles = %+v, want none: the old standard's roles left with it", rep.Roles)
	}
	mustAgree(t, rep)

	// Clearing the standard entirely (the one-off conversion) is the same trigger,
	// and it changes no verdict here, so it must record nothing.
	clear := ""
	if _, err := f.gw.UpdateSystem(ctx, "", "fresh", storage.SystemPatch{StandardID: &clear}, f.all, f.all); err != nil {
		t.Fatalf("clear standard: %v", err)
	}
	if got, _ := f.recorded(t, ctx, "system", "fresh"); got != n {
		t.Errorf("recorded %d rows after a conversion that changed no verdict, want %d (transition-only)", got, n)
	}
}

// TestHealthIgnoresProductChange proves a product swap is NOT a health trigger
// any more (#626): a component's own verdict is purely its active alarms, so
// reclassifying it, even to a type no role would accept, moves nothing already
// recorded. The typed-slot guard is enforced at the NEXT AssignRole, not
// continuously against an existing assignment.
func TestHealthIgnoresProductChange(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	before, v := f.recorded(t, ctx, "system", "hq-huddle")
	if v != "healthy" {
		t.Fatalf("baseline system = %q, want healthy", v)
	}

	// samsung-qm55 classifies as display, not video-bar, but bar-1 is already
	// assigned and stays assigned: reclassifying it must record nothing.
	panel := "samsung-qm55"
	if _, err := f.gw.UpdateComponent(ctx, "", "bar-1", storage.ComponentPatch{ProductName: &panel}, f.all, f.all); err != nil {
		t.Fatalf("change product: %v", err)
	}
	if n, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "healthy" || n != before {
		t.Fatalf("system after a product swap = %d rows / %q, want %d / healthy (product no longer moves health)", n, v, before)
	}
	rep, err := f.gw.SystemHealth(ctx, "hq-huddle", 0, f.all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	if len(rep.Roles) != 1 || rep.Roles[0].Impaired || rep.Roles[0].Satisfying != 1 {
		t.Fatalf("role = %+v, want table-mic still satisfied by its unchanged assignment", rep.Roles)
	}
	mustAgree(t, rep)

	// Reclassifying back is the same no-op in the other direction.
	bar := "cisco-room-bar"
	if _, err := f.gw.UpdateComponent(ctx, "", "bar-1", storage.ComponentPatch{ProductName: &bar}, f.all, f.all); err != nil {
		t.Fatalf("restore product: %v", err)
	}
	if got, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "healthy" || got != before {
		t.Fatalf("system after restoring the product = %d rows / %q, want %d / healthy", got, v, before)
	}

	// An unknown product is still a named refusal, not a constraint violation:
	// that guard lives in UpdateComponent itself, unrelated to health.
	ghost := "no-such-product"
	if _, err := f.gw.UpdateComponent(ctx, "", "bar-1", storage.ComponentPatch{ProductName: &ghost}, f.all, f.all); !errors.Is(err, storage.ErrProductNotFound) {
		t.Errorf("unknown product: err = %v, want ErrProductNotFound", err)
	}
}

// TestHealthMovesOnRelocation proves a system's move recomputes BOTH ends. The
// location it arrived at is reachable from its row; the one it LEFT is not, and
// that one has just improved, which is an edge as real as any failure.
func TestHealthMovesOnRelocation(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	// A second branch of the tree to move into, so the departure is visible at the
	// building level and not just the room.
	f.mustLocation(t, ctx, "hq-b2", "building", ptrStr("hq"))
	f.mustLocation(t, ctx, "hq-r2", "room", ptrStr("hq-b2"))

	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "critical", Message: "mic dead",
	}); err != nil {
		t.Fatalf("raise alarm: %v", err)
	}
	for _, name := range []string{"hq-r1", "hq-b1", "hq"} {
		if _, v := f.recorded(t, ctx, "location", name); v != "degraded" {
			t.Fatalf("%s = %q, want degraded before the move", name, v)
		}
	}
	campus, _ := f.recorded(t, ctx, "location", "hq")

	room := "hq-r2"
	if _, err := f.gw.MoveSystem(ctx, "", "hq-huddle", storage.SystemMove{LocationName: &room}, f.all, f.all, all); err != nil {
		t.Fatalf("relocate: %v", err)
	}

	// The branch it left recovers, all the way up to the building.
	for _, name := range []string{"hq-r1", "hq-b1"} {
		if _, v := f.recorded(t, ctx, "location", name); v != "healthy" {
			t.Errorf("%s = %q after the system left, want healthy: the old location must be recomputed too", name, v)
		}
	}
	// The branch it arrived in takes the verdict on.
	for _, name := range []string{"hq-r2", "hq-b2"} {
		if _, v := f.recorded(t, ctx, "location", name); v != "degraded" {
			t.Errorf("%s = %q after the system arrived, want degraded", name, v)
		}
	}
	// The campus is above both branches, so nothing about it changed and nothing may
	// be written for it.
	if n, v := f.recorded(t, ctx, "location", "hq"); n != campus || v != "degraded" {
		t.Errorf("campus = %d rows / %q, want %d / degraded: the move did not change it", n, v, campus)
	}
	// The reports agree with the systems they list, on both ends of the move.
	for _, tc := range []struct{ name, want string }{{"hq-r1", "healthy"}, {"hq-r2", "degraded"}} {
		rep, err := f.gw.LocationHealth(ctx, tc.name, 0, f.all)
		if err != nil {
			t.Fatalf("location health %s: %v", tc.name, err)
		}
		if rep.Verdict != tc.want {
			t.Errorf("%s verdict = %q, want %q", tc.name, rep.Verdict, tc.want)
		}
		if got := health.RollUp(reportedVerdicts(rep.Systems)).String(); got != rep.Verdict {
			t.Errorf("%s verdict %q contradicts the systems it lists (%+v)", tc.name, rep.Verdict, rep.Systems)
		}
	}
}

// TestHealthMovesOnLocationMove is the location-tier twin of the relocate proof
// above (#642). A location that moves carries every system in its own subtree
// from one ancestor chain to another, so BOTH chains have to be recomputed: the
// chain it left loses that contribution (an improvement, an edge as real as any
// failure) and the chain it joins takes it on.
//
// Both sides are asserted, and each is asserted TWO levels up, which is what
// makes the test load-bearing in both directions at once. Asserting only the
// arrival would pass an implementation that never names the location left
// behind, which is half the defect; asserting only the direct parent on each
// side would pass one that named the two parents and never walked above them.
// The two branches hang off DIFFERENT roots so nothing is shared above the
// move: a common ancestor would read the same verdict either way and prove
// nothing.
func TestHealthMovesOnLocationMove(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	// north-campus > north-b1 > mover-room (the room that moves, holding a
	// degraded system), and south-campus > south-b1 > anchor-room (a healthy
	// system, so the arrival branch has a RECORDED healthy verdict to move off
	// of rather than no history at all).
	f.mustLocation(t, ctx, "north-campus", "campus", nil)
	f.mustLocation(t, ctx, "north-b1", "building", ptrStr("north-campus"))
	f.mustLocation(t, ctx, "mover-room", "room", ptrStr("north-b1"))
	f.mustLocation(t, ctx, "south-campus", "campus", nil)
	f.mustLocation(t, ctx, "south-b1", "building", ptrStr("south-campus"))
	f.mustLocation(t, ctx, "anchor-room", "room", ptrStr("south-b1"))

	anchorRoom := "anchor-room"
	if _, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "anchor-sys", LocationName: &anchorRoom,
	}, f.all, all); err != nil {
		t.Fatalf("create anchor system: %v", err)
	}
	// Conforming to the fixture's standard with nobody staffing its role is
	// what makes this system degraded, with no alarm needed.
	std, moverRoom := "health-huddle", "mover-room"
	if _, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "mover-sys", StandardID: &std, LocationName: &moverRoom,
	}, f.all, all); err != nil {
		t.Fatalf("create mover system: %v", err)
	}

	for _, tc := range []struct{ name, want string }{
		{"mover-room", "degraded"}, {"north-b1", "degraded"}, {"north-campus", "degraded"},
		{"anchor-room", "healthy"}, {"south-b1", "healthy"}, {"south-campus", "healthy"},
	} {
		if _, v := f.recorded(t, ctx, "location", tc.name); v != tc.want {
			t.Fatalf("%s = %q before the move, want %q", tc.name, v, tc.want)
		}
	}
	moverRows, _ := f.recorded(t, ctx, "location", "mover-room")
	anchorRows, _ := f.recorded(t, ctx, "location", "anchor-room")

	newParent := "south-b1"
	if _, err := f.gw.MoveLocation(ctx, "", "mover-room", storage.LocationMove{ParentName: &newParent}, f.all, f.all); err != nil {
		t.Fatalf("move location: %v", err)
	}

	// The chain it left recovers, the direct parent and the root above it.
	for _, name := range []string{"north-b1", "north-campus"} {
		if _, v := f.recorded(t, ctx, "location", name); v != "healthy" {
			t.Errorf("%s = %q after the room left, want healthy: the ancestors of the OLD parent must be recomputed too (series %v)",
				name, v, f.healthSeries(t, ctx, "location", name))
		}
	}
	// The chain it joined takes the verdict on, to the same depth.
	for _, name := range []string{"south-b1", "south-campus"} {
		if _, v := f.recorded(t, ctx, "location", name); v != "degraded" {
			t.Errorf("%s = %q after the room arrived, want degraded: the ancestors of the NEW parent must be recomputed too (series %v)",
				name, v, f.healthSeries(t, ctx, "location", name))
		}
	}
	// The moved room's own verdict folds its own subtree, which travelled with
	// it, so nothing about it changed and nothing may be written for it. Same
	// for the room that never moved.
	if n, v := f.recorded(t, ctx, "location", "mover-room"); n != moverRows || v != "degraded" {
		t.Errorf("mover-room = %d rows / %q, want %d / degraded: its own subtree did not change", n, v, moverRows)
	}
	if n, v := f.recorded(t, ctx, "location", "anchor-room"); n != anchorRows || v != "healthy" {
		t.Errorf("anchor-room = %d rows / %q, want %d / healthy: an unrelated room must not be rewritten", n, v, anchorRows)
	}

	// The record and the report agree on both ends: a location cannot read
	// healthy over a system it itself lists as degraded.
	for _, tc := range []struct{ name, want string }{
		{"north-b1", "healthy"}, {"north-campus", "healthy"},
		{"south-b1", "degraded"}, {"south-campus", "degraded"},
	} {
		rep, err := f.gw.LocationHealth(ctx, tc.name, 0, f.all)
		if err != nil {
			t.Fatalf("location health %s: %v", tc.name, err)
		}
		if rep.Verdict != tc.want {
			t.Errorf("%s reported verdict = %q, want %q (systems %+v)", tc.name, rep.Verdict, tc.want, rep.Systems)
		}
		if _, recorded := f.recorded(t, ctx, "location", tc.name); recorded != rep.Verdict {
			t.Errorf("%s recorded %q disagrees with the reported %q: a real transition went unrecorded (series %v)",
				tc.name, recorded, rep.Verdict, f.healthSeries(t, ctx, "location", tc.name))
		}
	}
	f.assertTransitionOnly(t, ctx)
}

// reportedVerdicts reads the drill-down's verdicts back for the consistency check.
func reportedVerdicts(systems []storage.HealthSystem) []health.Verdict {
	out := make([]health.Verdict, 0, len(systems))
	for _, s := range systems {
		out = append(out, health.ParseVerdict(s.Verdict))
	}
	return out
}

// TestHealthReadScope proves the health reads are scope-injected like every other
// read: a system or location outside the caller's scope is a non-disclosing
// not-found, never a forbidden that confirms it exists.
func TestHealthReadScope(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	none := scope.Set{}
	if _, err := f.gw.SystemHealth(ctx, "hq-huddle", 0, none); !errors.Is(err, storage.ErrSystemNotFound) {
		t.Errorf("out-of-scope system health: err = %v, want ErrSystemNotFound", err)
	}
	if _, err := f.gw.LocationHealth(ctx, "hq", 0, none); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Errorf("out-of-scope location health: err = %v, want ErrLocationNotFound", err)
	}
	if _, err := f.gw.SystemHealth(ctx, "no-such-system", 0, f.all); !errors.Is(err, storage.ErrSystemNotFound) {
		t.Errorf("unknown system health: err = %v, want ErrSystemNotFound", err)
	}
}

// TestAlarmImpairsComponentImpairsSlot is the new chain end to end: alarm ->
// component verdict -> occupied slot -> impact. table-mic wants quorum 1 and
// bar-1 is its only occupant, so an alarm on bar-1 (naming no capability: the
// concept is retired) takes the component down, the role loses its only
// occupant, and the system's verdict moves to the role's declared impact.
func TestAlarmImpairsComponentImpairsSlot(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "healthy" {
		t.Fatalf("baseline system = %q, want healthy", v)
	}

	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "critical", Message: "mic array not responding",
	}); err != nil {
		t.Fatalf("raise alarm: %v", err)
	}

	if _, v := f.recorded(t, ctx, "component", "bar-1"); v != "outage" {
		t.Errorf("component = %q, want outage: only a critical alarm takes it down (a degraded component still occupies)", v)
	}

	rep, err := f.gw.SystemHealth(ctx, "hq-huddle", 0, f.all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	if len(rep.Roles) != 1 || rep.Roles[0].Name != "table-mic" {
		t.Fatalf("roles = %+v, want the one table-mic", rep.Roles)
	}
	role := rep.Roles[0]
	if role.Satisfying != 0 {
		t.Errorf("satisfying = %d, want 0: the alarm impairs the component wholesale, not per capability", role.Satisfying)
	}
	if !role.Impaired {
		t.Errorf("role impaired = %v, want true: quorum 1 with 0 satisfying", role.Impaired)
	}
	if rep.Verdict != "degraded" {
		t.Fatalf("system verdict = %q, want degraded (the role's declared impact)", rep.Verdict)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "degraded" {
		t.Errorf("recorded system = %q, want degraded", v)
	}
}

// TestDegradedOccupantStillSatisfies pins the boundary #626 and the epic both
// state verbatim: "an occupant satisfies its slot when its component verdict
// is not down". Down means outage; a merely degraded component (an info or
// warning alarm) still occupies the slot it fills, so the role stays
// satisfied and the system's verdict does not move.
func TestDegradedOccupantStillSatisfies(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "warning", Message: "mic array a little quiet",
	}); err != nil {
		t.Fatalf("raise alarm: %v", err)
	}
	if _, v := f.recorded(t, ctx, "component", "bar-1"); v != "degraded" {
		t.Errorf("component = %q, want degraded (a warning alarm)", v)
	}

	rep, err := f.gw.SystemHealth(ctx, "hq-huddle", 0, f.all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	if len(rep.Roles) != 1 {
		t.Fatalf("roles = %+v, want the one table-mic", rep.Roles)
	}
	role := rep.Roles[0]
	if role.Satisfying != 1 {
		t.Errorf("satisfying = %d, want 1: a degraded occupant still occupies its slot", role.Satisfying)
	}
	if role.Impaired {
		t.Errorf("role impaired = %v, want false: quorum 1 is still met by the degraded occupant", role.Impaired)
	}
	if rep.Verdict != "healthy" {
		t.Fatalf("system verdict = %q, want healthy: the role never lost its occupant", rep.Verdict)
	}
	// A component's own verdict is recorded regardless of whether it fills a
	// role, but nothing about the system's own recorded arc should move: no
	// role went from satisfied to impaired, so no new transition is due.
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "healthy" {
		t.Errorf("recorded system = %q, want healthy: nothing may be written for a no-op", v)
	}
}

// TestAlarmOnSpareDoesNotShort proves the other half of the arithmetic: a role
// staffed past its quorum absorbs one down occupant without moving. Two
// components fill a quorum-1 role; an alarm on one of them takes it down, but
// the other still occupies the slot, so the role, and the system above it,
// hold healthy.
func TestAlarmOnSpareDoesNotShort(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	panel := "cisco-room-bar"
	if _, err := f.gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-2", ProductName: &panel}, f.all, all, all, all); err != nil {
		t.Fatalf("create second component: %v", err)
	}
	if err := f.gw.AssignRole(ctx, "", "hq-huddle", "table-mic", "bar-2", f.all); err != nil {
		t.Fatalf("assign spare: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "healthy" {
		t.Fatalf("baseline system with two occupants = %q, want healthy", v)
	}

	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "critical", Message: "mic array not responding",
	}); err != nil {
		t.Fatalf("raise alarm: %v", err)
	}
	if _, v := f.recorded(t, ctx, "component", "bar-1"); v != "outage" {
		t.Errorf("component = %q, want outage (a critical alarm)", v)
	}

	rep, err := f.gw.SystemHealth(ctx, "hq-huddle", 0, f.all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	role := rep.Roles[0]
	if role.Satisfying != 1 {
		t.Errorf("satisfying = %d, want 1: bar-2 still occupies the slot", role.Satisfying)
	}
	if role.Impaired {
		t.Errorf("role impaired = %v, want false: quorum 1 is still met by the spare", role.Impaired)
	}
	if rep.Verdict != "healthy" {
		t.Fatalf("system verdict = %q, want healthy: the down component is not the only occupant", rep.Verdict)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "healthy" {
		t.Errorf("recorded system = %q, want healthy: nothing may be written for a no-op", v)
	}
}

// TestShortAndUnderstaffedDivergeOnCriticalAlarm pins the vocabulary split
// (#626): the roles read's understaffed (EffectiveRole.Understaffed, quorum
// minus assignment rows) is health-blind assignment arithmetic, while the
// health read's short (HealthRole.Short, quorum minus components whose own
// verdict is not outage) is occupancy-aware. A warning alarm degrades the
// assignee but it still occupies its slot (Occupies is Verdict != Outage),
// so the two figures agree; a critical alarm takes the assignee out of its
// slot without touching the assignment row, so understaffed stays 0 while
// short moves to 1. Both are correct under their own definition, which is
// exactly what makes the divergence a feature and not a bug to reconcile.
func TestShortAndUnderstaffedDivergeOnCriticalAlarm(t *testing.T) {
	t.Run("a warning alarm does not diverge them", func(t *testing.T) {
		f := newHealthFixture(t)
		ctx := context.Background()

		if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
			Severity: "warning", Message: "mic array a little quiet",
		}); err != nil {
			t.Fatalf("raise alarm: %v", err)
		}

		roles, err := f.gw.EffectiveRoles(ctx, "hq-huddle", f.all)
		if err != nil {
			t.Fatalf("effective roles: %v", err)
		}
		if len(roles) != 1 || roles[0].Understaffed() != 0 {
			t.Fatalf("understaffed = %+v, want 0: the assignment row is untouched by a warning", roles)
		}

		rep, err := f.gw.SystemHealth(ctx, "hq-huddle", 0, f.all)
		if err != nil {
			t.Fatalf("system health: %v", err)
		}
		role := rep.Roles[0]
		if role.Short != 0 || role.Impaired {
			t.Fatalf("short = %d impaired = %v, want 0/false: a degraded component still occupies its slot", role.Short, role.Impaired)
		}
		if roles[0].Understaffed() != role.Short {
			t.Fatalf("understaffed (%d) and short (%d) must agree when nothing has stopped occupying its slot", roles[0].Understaffed(), role.Short)
		}
	})

	t.Run("a critical alarm diverges them", func(t *testing.T) {
		f := newHealthFixture(t)
		ctx := context.Background()

		if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
			Severity: "critical", Message: "mic array not responding",
		}); err != nil {
			t.Fatalf("raise alarm: %v", err)
		}

		roles, err := f.gw.EffectiveRoles(ctx, "hq-huddle", f.all)
		if err != nil {
			t.Fatalf("effective roles: %v", err)
		}
		if len(roles) != 1 || roles[0].Understaffed() != 0 {
			t.Fatalf("understaffed = %+v, want 0: the assignment row is untouched, understaffed is health-blind", roles)
		}

		rep, err := f.gw.SystemHealth(ctx, "hq-huddle", 0, f.all)
		if err != nil {
			t.Fatalf("system health: %v", err)
		}
		role := rep.Roles[0]
		if role.Short != 1 || !role.Impaired {
			t.Fatalf("short = %d impaired = %v, want 1/true: bar-1 no longer occupies its slot", role.Short, role.Impaired)
		}

		// Same role, same instant, two answers that are each correct under their
		// own definition: understaffed says the slot is fully assigned, short
		// says it is not actually staffed. The divergence itself is the point.
		if roles[0].Understaffed() == role.Short {
			t.Fatalf("understaffed (%d) and short (%d) must diverge on a critical alarm", roles[0].Understaffed(), role.Short)
		}
	})
}

// TestRaiseAlarmWithoutCapabilities proves the alarm-raise spec round-trips
// with no capability concept at all: the field retired with the rest of the
// family (#626), so a raise names only severity, message, and a dedup key.
func TestRaiseAlarmWithoutCapabilities(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	alarm, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "warning", Message: "mic array not responding", DedupKey: "mic-fault",
	})
	if err != nil {
		t.Fatalf("raise alarm: %v", err)
	}
	if alarm.ComponentID != "bar-1" || alarm.Severity != "warning" ||
		alarm.Message != "mic array not responding" || alarm.DedupKey != "mic-fault" || !alarm.Active() {
		t.Fatalf("alarm = %+v, want it to round-trip exactly what was raised", alarm)
	}

	got, err := f.gw.ListAlarms(ctx, "bar-1", storage.AlarmFilter{})
	if err != nil {
		t.Fatalf("list alarms: %v", err)
	}
	if len(got) != 1 || got[0].ID != alarm.ID {
		t.Fatalf("listed alarms = %+v, want the one just raised", got)
	}
}
