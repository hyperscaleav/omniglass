package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The members-outside-a-slot rule (#2459, ADR-2459), held against real
// Postgres: an open alarm on a member that fills no slot degrades its system,
// never more; slots alone decide outage; the place above rolls up the new
// verdict unchanged; and every write that can move the rule recomputes.

// handBuilt adds a system with no standard (so no slots) to the fixture's room,
// with one member, amp-1, and returns nothing: the tests speak names.
func (f *healthFixture) handBuilt(t *testing.T, ctx context.Context) {
	t.Helper()
	room := "hq-r1"
	if _, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "hand", LocationName: &room}, f.all, all); err != nil {
		t.Fatalf("create hand-built system: %v", err)
	}
	f.mustComponent(t, ctx, "amp-1")
	if err := f.gw.AddMember(ctx, "", "hand", "amp-1", f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("add member: %v", err)
	}
}

func (f *healthFixture) mustComponent(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	if _, err := f.gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name, ProductName: &f.bar.Name}, f.all, all, all, all); err != nil {
		t.Fatalf("create component %s: %v", name, err)
	}
}

func (f *healthFixture) report(t *testing.T, ctx context.Context, system string) *storage.HealthReport {
	t.Helper()
	rep, err := f.gw.SystemHealth(ctx, system, storage.HealthRange{}, f.all)
	if err != nil {
		t.Fatalf("system health %s: %v", system, err)
	}
	mustAgree(t, rep)
	return rep
}

func member(rep *storage.HealthReport, name string) *storage.HealthMember {
	for i := range rep.Members {
		if rep.Members[i].Name == name {
			return &rep.Members[i]
		}
	}
	return nil
}

func TestOutsideMemberAlarmDegradesItsSystem(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	f.handBuilt(t, ctx)

	if rep := f.report(t, ctx, "hand"); rep.Verdict != "healthy" {
		t.Fatalf("before the alarm: %q, want healthy", rep.Verdict)
	}

	// A high alarm takes amp-1 to outage on its own arc; its system, which
	// declares nothing it would lose, reads degraded and no further.
	alarm, err := f.gw.RaiseAlarm(ctx, "", "amp-1", storage.AlarmSpec{Severity: "high", Message: "amplifier powered off"})
	if err != nil {
		t.Fatalf("raise alarm: %v", err)
	}
	if _, v := f.recorded(t, ctx, "component", "amp-1"); v != "outage" {
		t.Fatalf("component = %q, want outage", v)
	}
	if _, v := f.recorded(t, ctx, "system", "hand"); v != "degraded" {
		t.Fatalf("system recorded %q, want degraded", v)
	}
	for _, loc := range []string{"hq-r1", "hq-b1", "hq"} {
		if _, v := f.recorded(t, ctx, "location", loc); v != "degraded" {
			t.Errorf("location %s recorded %q, want degraded: the place rolls up the new verdict unchanged", loc, v)
		}
	}
	rep := f.report(t, ctx, "hand")
	if rep.Verdict != "degraded" {
		t.Fatalf("report verdict %q, want degraded", rep.Verdict)
	}
	if !strings.Contains(rep.Reason, "1 member outside a slot has an open alarm (amp-1)") {
		t.Errorf("reason %q does not name the member", rep.Reason)
	}
	m := member(rep, "amp-1")
	if m == nil || !m.OutsideSlot || m.Health != "outage" || len(m.Alarms) != 1 || m.Alarms[0].ID != alarm.ID {
		t.Fatalf("member = %+v, want amp-1 outside a slot, outage, carrying the alarm", m)
	}

	// Clearing returns it, and the history holds both edges.
	if err := f.gw.ClearAlarm(ctx, "", "amp-1", alarm.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	rep = f.report(t, ctx, "hand")
	if rep.Verdict != "healthy" {
		t.Fatalf("after clear %q, want healthy", rep.Verdict)
	}
	if got := verdictSeq(rep.Transitions); !sameSeq(got, []string{"healthy", "degraded", "healthy"}) {
		t.Fatalf("transitions = %v, want healthy, degraded, healthy", got)
	}
	if _, v := f.recorded(t, ctx, "location", "hq"); v != "healthy" {
		t.Errorf("campus %q, want healthy after the clear", v)
	}
}

// Severity does not reach past the member: a warning degrades the system
// exactly as a disaster does.
func TestOutsideMemberSeverityDoesNotMatter(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	f.handBuilt(t, ctx)
	if _, err := f.gw.RaiseAlarm(ctx, "", "amp-1", storage.AlarmSpec{Severity: "warning", Message: "fan"}); err != nil {
		t.Fatalf("raise: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hand"); v != "degraded" {
		t.Fatalf("system %q, want degraded", v)
	}
}

// A slotted outage still decides outage, a system holding both kinds takes the
// worse, and clearing the slotted one leaves the outside one's degraded.
func TestSlottedOutageStillDecidesOverAnOutsideAlarm(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	if _, err := f.gw.SetSystemSlot(ctx, "", "system", "hq-huddle", storage.SystemSlotSpec{
		Name: "display", Label: "Display", Quorum: 1, AcceptedTypes: []string{"video-bar"}, Impact: "outage",
	}); err != nil {
		t.Fatalf("declare display: %v", err)
	}
	f.mustComponent(t, ctx, "disp-1")
	if err := f.gw.AssignSlot(ctx, "", "hq-huddle", "display", "disp-1", f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("assign: %v", err)
	}
	f.mustComponent(t, ctx, "amp-2")
	if err := f.gw.AddMember(ctx, "", "hq-huddle", "amp-2", f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("add member: %v", err)
	}

	slotted, err := f.gw.RaiseAlarm(ctx, "", "disp-1", storage.AlarmSpec{Severity: "high", Message: "display off"})
	if err != nil {
		t.Fatalf("raise slotted: %v", err)
	}
	if _, err := f.gw.RaiseAlarm(ctx, "", "amp-2", storage.AlarmSpec{Severity: "high", Message: "amp off"}); err != nil {
		t.Fatalf("raise outside: %v", err)
	}
	rep := f.report(t, ctx, "hq-huddle")
	if rep.Verdict != "outage" {
		t.Fatalf("both kinds: %q, want outage (the worse)", rep.Verdict)
	}
	if m := member(rep, "disp-1"); m == nil || m.OutsideSlot {
		t.Errorf("disp-1 = %+v, want a slotted member", m)
	}
	if !strings.HasPrefix(rep.Reason, "slot display is impaired") || !strings.Contains(rep.Reason, "(amp-2)") {
		t.Errorf("reason %q, want the slot first and the outside member after", rep.Reason)
	}

	if err := f.gw.ClearAlarm(ctx, "", "disp-1", slotted.ID); err != nil {
		t.Fatalf("clear slotted: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "degraded" {
		t.Fatalf("after clearing the slotted outage: %q, want degraded (the outside member)", v)
	}
}

// Membership writes move the rule: binding an alarmed component degrades the
// system, unbinding it heals it, and deleting an alarmed member heals it too.
func TestMembershipWritesRecomputeTheOutsideRule(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	f.handBuilt(t, ctx)
	f.mustComponent(t, ctx, "amp-3")
	if _, err := f.gw.RaiseAlarm(ctx, "", "amp-3", storage.AlarmSpec{Severity: "high", Message: "off"}); err != nil {
		t.Fatalf("raise: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hand"); v != "healthy" {
		t.Fatalf("not yet a member: %q, want healthy", v)
	}
	if err := f.gw.AddMember(ctx, "", "hand", "amp-3", f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hand"); v != "degraded" {
		t.Fatalf("after binding an alarmed member: %q, want degraded", v)
	}
	if err := f.gw.RemoveMember(ctx, "", "hand", "amp-3", f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hand"); v != "healthy" {
		t.Fatalf("after unbinding: %q, want healthy", v)
	}

	if _, err := f.gw.RaiseAlarm(ctx, "", "amp-1", storage.AlarmSpec{Severity: "high", Message: "off"}); err != nil {
		t.Fatalf("raise amp-1: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hand"); v != "degraded" {
		t.Fatalf("amp-1 alarmed: %q, want degraded", v)
	}
	if err := f.gw.DeleteComponent(ctx, "", "amp-1", f.all, f.all); err != nil {
		t.Fatalf("delete component: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hand"); v != "healthy" {
		t.Fatalf("after deleting the alarmed member: %q, want healthy", v)
	}
	if _, v := f.recorded(t, ctx, "location", "hq"); v != "healthy" {
		t.Fatalf("campus after the delete: %q, want healthy", v)
	}
}

// A slot staffed by a component whose product is collected through a driver
// but which has no endpoint yet (the template case: "still to attach") reads
// incomplete, and attaching it makes the system healthy (#2459).
func TestOccupantStillToAttachReadsIncomplete(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	if _, err := f.conn.Exec(ctx, `update product set driver_id = (select id from driver order by name limit 1) where name = $1`, f.bar.Name); err != nil {
		t.Fatalf("give the product a driver: %v", err)
	}
	// bar-1 is the fixture's staffed table mic: re-assigning recomputes, and
	// its product is now collectable with no endpoint on it.
	if err := f.gw.UnassignSlot(ctx, "", "hq-huddle", "table-mic", "bar-1", f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if err := f.gw.AssignSlot(ctx, "", "hq-huddle", "table-mic", "bar-1", f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("assign: %v", err)
	}
	rep := f.report(t, ctx, "hq-huddle")
	if rep.Verdict != "incomplete" {
		t.Fatalf("verdict %q, want incomplete: its one occupant is still to attach", rep.Verdict)
	}
	if m := member(rep, "bar-1"); m == nil || !m.ToAttach {
		t.Fatalf("member = %+v, want bar-1 marked still to attach", m)
	}
	if !strings.Contains(rep.Reason, "still to attach (bar-1)") {
		t.Errorf("reason %q does not name the component to attach", rep.Reason)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "incomplete" {
		t.Fatalf("recorded %q, want incomplete", v)
	}

	comp := "bar-1"
	if _, err := f.gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: "edge"}, f.all, f.all); err != nil {
		t.Fatalf("outpost: %v", err)
	}
	edge := "edge"
	ep, err := f.gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Transport: "icmp", Component: &comp, Outpost: &edge, Params: []byte(`{"target":"10.0.0.1"}`)}, all)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "healthy" {
		t.Fatalf("after attaching: %q, want healthy", v)
	}
	if err := f.gw.DeleteEndpoint(ctx, "", ep.ID, all, all); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if _, v := f.recorded(t, ctx, "system", "hq-huddle"); v != "incomplete" {
		t.Fatalf("after detaching: %q, want incomplete", v)
	}
}
