package storage_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

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
	}, f.all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	// One table mic, quorum 1, an impaired one degrades its system. A second role
	// nobody can break confirms the rollup is worst-wins, not last-wins.
	if _, err := gw.SetSystemRole(ctx, "", "standard", "health-huddle", storage.SystemRoleSpec{
		Name: "table-mic", DisplayName: "Table microphone", Quorum: 1,
		Capabilities: []string{"microphone", "speaker"}, Impact: "degraded",
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}

	// cisco-room-bar provides microphone, speaker, camera, codec.
	bar := "cisco-room-bar"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1", ProductName: &bar}, f.all); err != nil {
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
		select count(*), (select value from state
			where `+col+` = `+owner+` and property_type_id = (select id from property_type where name = 'health') order by id desc limit 1)
		from state where `+col+` = `+owner+` and property_type_id = (select id from property_type where name = 'health')`, ownerID).Scan(&n, &latest); err != nil {
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
// that degrades a capability a role requires must move the component, its system,
// and every location above it, recording exactly one row each; a SECOND alarm
// that changes no verdict must record nothing at all; and clearing must move
// everything back. The second-alarm assertion is the transition-only property,
// which is what makes the history a record of edges rather than of writes.
func TestHealthTransitionsThroughTheChain(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	// Baseline. Staffing the role already recorded a healthy start for everything
	// in the chain, so the assertions below are on the DELTA an alarm causes.
	type owner struct{ kind, id string }
	chain := []owner{
		{"component", "bar-1"}, {"system", "hq-huddle"},
		{"location", "hq-r1"}, {"location", "hq-b1"}, {"location", "hq"},
	}
	before := map[owner]int{}
	for _, o := range chain {
		n, v := f.recorded(t, ctx, o.kind, o.id)
		if v != "healthy" {
			t.Fatalf("baseline %s/%s = %q, want healthy (the first value is always recorded)", o.kind, o.id, v)
		}
		before[o] = n
	}

	// The alarm takes away microphone, which the role requires, so the bar can no
	// longer fill it and the role drops below quorum.
	alarm, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "warning", Message: "mic array not responding", Capabilities: []string{"microphone"},
	})
	if err != nil {
		t.Fatalf("raise alarm: %v", err)
	}

	for _, o := range chain {
		n, v := f.recorded(t, ctx, o.kind, o.id)
		if v != "degraded" {
			t.Errorf("after the alarm %s/%s = %q, want degraded", o.kind, o.id, v)
		}
		if got := n - before[o]; got != 1 {
			t.Errorf("%s/%s recorded %d rows for one transition, want exactly 1", o.kind, o.id, got)
		}
		before[o] = n
	}

	// A SECOND alarm that changes nothing: the component is already degraded, the
	// role is already below quorum, so no verdict in the chain moves. Nothing may
	// be written. This is the property the whole recording design exists for.
	second, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "warning", Message: "speaker distortion", Capabilities: []string{"speaker"},
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

	// Clearing the second alarm still leaves the first one degrading microphone, so
	// still nothing moves.
	if err := f.gw.ClearAlarm(ctx, "", "bar-1", second.ID); err != nil {
		t.Fatalf("clear second alarm: %v", err)
	}
	for _, o := range chain {
		n, v := f.recorded(t, ctx, o.kind, o.id)
		if n != before[o] || v != "degraded" {
			t.Errorf("%s/%s = %d rows / %q after clearing a non-deciding alarm, want %d / degraded",
				o.kind, o.id, n, v, before[o])
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

// TestHealthIgnoresIrrelevantCapability proves the routing: an alarm that
// degrades a capability no role requires marks the COMPONENT (something is wrong
// with it) but must not touch the system or its locations. Without this, every
// alarm anywhere would paint the estate red and the verdict would mean nothing.
func TestHealthIgnoresIrrelevantCapability(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	sysBefore, _ := f.recorded(t, ctx, "system", "hq-huddle")
	locBefore, _ := f.recorded(t, ctx, "location", "hq")

	// The role requires microphone and speaker; camera is not among them.
	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "warning", Message: "camera lens fouled", Capabilities: []string{"camera"},
	}); err != nil {
		t.Fatalf("raise alarm: %v", err)
	}

	if _, v := f.recorded(t, ctx, "component", "bar-1"); v != "degraded" {
		t.Errorf("component = %q, want degraded (an active alarm is still wrong with it)", v)
	}
	if n, v := f.recorded(t, ctx, "system", "hq-huddle"); n != sysBefore || v != "healthy" {
		t.Errorf("system = %d rows / %q, want %d / healthy: no role required camera", n, v, sysBefore)
	}
	if n, v := f.recorded(t, ctx, "location", "hq"); n != locBefore || v != "healthy" {
		t.Errorf("location = %d rows / %q, want %d / healthy", n, v, locBefore)
	}
}

// TestHealthReportNamesTheCause is the reconciliation UX in one assertion: a
// degraded system must say WHICH role is impaired, WHICH required capability it
// lost, and WHICH alarm took it. A verdict an operator cannot act on is not worth
// recording.
func TestHealthReportNamesTheCause(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	alarm, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "critical", Message: "mic array not responding", Capabilities: []string{"microphone"},
	})
	if err != nil {
		t.Fatalf("raise alarm: %v", err)
	}

	rep, err := f.gw.SystemHealth(ctx, "hq-huddle", time.Time{}, f.all)
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
	if !hasAll(role.Degraded, "microphone") || len(role.Degraded) != 1 {
		t.Errorf("degraded = %v, want exactly the required capability the alarm took (microphone)", role.Degraded)
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
	loc, err := f.gw.LocationHealth(ctx, "hq", time.Time{}, f.all)
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
		Severity: "warning", Message: "mic dead", Capabilities: []string{"microphone"},
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
		Capabilities: []string{"microphone", "speaker"}, Impact: "outage",
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
		Capabilities: []string{"microphone", "speaker"}, Impact: "none",
	}); err != nil {
		t.Fatalf("re-declare role as none: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "healthy" {
		t.Fatalf("system after impact=none = %q, want healthy", v)
	}
	// The component itself is still alarmed: impact governs the system, never the
	// component's own condition.
	if _, v := f.recorded(t, ctx, "component", "bar-1"); v != "degraded" {
		t.Errorf("component = %q, want degraded regardless of the role's impact", v)
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

	// Suppressing a required capability on the component is the other removal
	// shape: it provides less, so it can no longer fill the role.
	if err := f.gw.SetComponentCapability(ctx, "", "bar-1", "microphone", false); err != nil {
		t.Fatalf("suppress capability: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "degraded" {
		t.Fatalf("system after suppressing a required capability = %q, want degraded", v)
	}
	if err := f.gw.ClearComponentCapability(ctx, "", "bar-1", "microphone"); err != nil {
		t.Fatalf("clear capability fact: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "healthy" {
		t.Fatalf("system after falling back to the product's set = %q, want healthy", v)
	}
}

// TestAlarmListingAndRefusals covers the alarm surface's own contract: the active
// set versus the history, and the request faults that must not read as server
// errors.
func TestAlarmListingAndRefusals(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	a1, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "warning", Message: "one", Capabilities: []string{"microphone"},
	})
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "info", Message: "two"}); err != nil {
		t.Fatalf("raise capability-less: %v", err)
	}
	if got, _ := f.gw.ListAlarms(ctx, "bar-1", false); len(got) != 2 {
		t.Fatalf("active alarms = %d, want 2", len(got))
	}
	if err := f.gw.ClearAlarm(ctx, "", "bar-1", a1.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	active, _ := f.gw.ListAlarms(ctx, "bar-1", false)
	if len(active) != 1 || active[0].ClearedAt != nil {
		t.Fatalf("active after clearing one = %+v, want just the uncleared one", active)
	}
	all, _ := f.gw.ListAlarms(ctx, "bar-1", true)
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
	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{
		Severity: "info", Capabilities: []string{"telepathy"},
	}); !errors.Is(err, storage.ErrAlarmRefNotFound) {
		t.Errorf("unknown capability: err = %v, want ErrAlarmRefNotFound", err)
	}
	// A malformed id must not reach Postgres as a cast error.
	if err := f.gw.ClearAlarm(ctx, "", "bar-1", "not-a-uuid"); !errors.Is(err, storage.ErrAlarmNotFound) {
		t.Errorf("malformed alarm id: err = %v, want ErrAlarmNotFound", err)
	}
	if _, err := f.gw.ListAlarms(ctx, "no-such-component", false); !errors.Is(err, storage.ErrComponentNotFound) {
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
		role := health.Role{Name: r.Name, Required: r.Required, Quorum: r.Quorum, Impact: r.Impact}
		for i := 0; i < r.Satisfying; i++ {
			role.Assigned = append(role.Assigned, health.Component{
				Name: fmt.Sprintf("%s-%d", r.Name, i), Provides: r.Required,
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
		Capabilities: []string{"microphone"}, Impact: "outage",
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}
	std, room := "health-podium", "hq-r1"
	if _, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{
		Name: "fresh", StandardID: &std, LocationName: &room,
	}, f.all); err != nil {
		t.Fatalf("create system: %v", err)
	}

	rep, err := f.gw.SystemHealth(ctx, "fresh", time.Time{}, f.all)
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
	loc, err := f.gw.LocationHealth(ctx, "hq-r1", time.Time{}, f.all)
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
	if _, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "plain", LocationName: &room}, f.all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	before, v := f.recorded(t, ctx, "system", "plain")
	if before != 1 || v != "healthy" {
		t.Fatalf("opening record = %d rows / %q, want 1 / healthy", before, v)
	}

	renamed := "plain-renamed"
	if _, err := f.gw.UpdateSystem(ctx, "", "plain", storage.SystemPatch{Name: &renamed}, f.all, f.all); err != nil {
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
		Name: "mic", Quorum: 1, Capabilities: []string{"microphone"}, Impact: "outage",
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
	}, f.all); err != nil {
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
	rep, err := f.gw.SystemHealth(ctx, "fresh", time.Time{}, f.all)
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

// TestHealthMovesOnProductChange proves the product swap is a trigger. The product
// supplies a component's default capabilities, so changing it can quietly unstaff a
// role: the assignment is still there, the component simply cannot do the job.
func TestHealthMovesOnProductChange(t *testing.T) {
	f := newHealthFixture(t)
	ctx := context.Background()

	before, v := f.recorded(t, ctx, "system", "hq-huddle")
	if v != "healthy" {
		t.Fatalf("baseline system = %q, want healthy", v)
	}

	// samsung-qm55 provides flat-panel-display only, so the bar stops providing the
	// microphone and speaker the table-mic role requires.
	panel := "samsung-qm55"
	if _, err := f.gw.UpdateComponent(ctx, "", "bar-1", storage.ComponentPatch{ProductName: &panel}, f.all, f.all); err != nil {
		t.Fatalf("change product: %v", err)
	}
	n, v := f.recorded(t, ctx, "system", "hq-huddle")
	if v != "degraded" {
		t.Fatalf("system after the assignee lost its capabilities = %q, want degraded", v)
	}
	if n-before != 1 {
		t.Fatalf("recorded %d rows for the product change, want exactly 1", n-before)
	}
	rep, err := f.gw.SystemHealth(ctx, "hq-huddle", time.Time{}, f.all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	if len(rep.Roles) != 1 || !rep.Roles[0].Impaired || rep.Roles[0].Satisfying != 0 {
		t.Fatalf("role = %+v, want table-mic impaired with nobody satisfying it", rep.Roles)
	}
	if len(rep.Roles[0].Degraded) != 0 {
		t.Errorf("degraded = %v, want empty: no alarm took anything, the component simply provides less",
			rep.Roles[0].Degraded)
	}
	mustAgree(t, rep)

	// Putting the original product back restores the role, which is the same
	// detection in the other direction.
	bar := "cisco-room-bar"
	if _, err := f.gw.UpdateComponent(ctx, "", "bar-1", storage.ComponentPatch{ProductName: &bar}, f.all, f.all); err != nil {
		t.Fatalf("restore product: %v", err)
	}
	if got, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "healthy" || got-n != 1 {
		t.Fatalf("system after restoring the product = %d rows / %q, want %d / healthy", got, v, n+1)
	}
	// A patch that names the product the component already has moves nothing, so it
	// must record nothing.
	if _, err := f.gw.UpdateComponent(ctx, "", "bar-1", storage.ComponentPatch{ProductName: &bar}, f.all, f.all); err != nil {
		t.Fatalf("re-set the same product: %v", err)
	}
	if got, _ := f.recorded(t, ctx, "system", "hq-huddle"); got != n+1 {
		t.Errorf("recorded %d rows for a product patch that changed nothing, want %d", got, n+1)
	}

	// Clearing the product outright leaves the component with only its own
	// capability facts, which here is nothing at all: the same trigger, the same
	// impairment.
	none := ""
	if _, err := f.gw.UpdateComponent(ctx, "", "bar-1", storage.ComponentPatch{ProductName: &none}, f.all, f.all); err != nil {
		t.Fatalf("clear product: %v", err)
	}
	if got, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "degraded" || got != n+2 {
		t.Errorf("system after clearing the product = %d rows / %q, want %d / degraded", got, v, n+2)
	}
	// An unknown product is a named refusal, not a constraint violation.
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
		Severity: "warning", Message: "mic dead", Capabilities: []string{"microphone"},
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
	if _, err := f.gw.UpdateSystem(ctx, "", "hq-huddle", storage.SystemPatch{LocationName: &room}, f.all, f.all); err != nil {
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
		rep, err := f.gw.LocationHealth(ctx, tc.name, time.Time{}, f.all)
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
	if _, err := f.gw.SystemHealth(ctx, "hq-huddle", time.Time{}, none); !errors.Is(err, storage.ErrSystemNotFound) {
		t.Errorf("out-of-scope system health: err = %v, want ErrSystemNotFound", err)
	}
	if _, err := f.gw.LocationHealth(ctx, "hq", time.Time{}, none); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Errorf("out-of-scope location health: err = %v, want ErrLocationNotFound", err)
	}
	if _, err := f.gw.SystemHealth(ctx, "no-such-system", time.Time{}, f.all); !errors.Is(err, storage.ErrSystemNotFound) {
		t.Errorf("unknown system health: err = %v, want ErrSystemNotFound", err)
	}
}
