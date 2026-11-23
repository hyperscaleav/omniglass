package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A member outside every slot is shown on its system, not rolled up (#2459,
// ADR-2459), held against real Postgres: its open alarm leaves the system's
// recorded health and the places above it as they were, and the system health
// read lists the member, outside a slot, with that alarm. A slotted member says
// which slot it fills.

// handBuilt adds a system with no standard (so no slots) to the fixture's room,
// with one member, amp-1.
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
	rep, err := f.gw.SystemHealth(ctx, system, storage.HealthRange{}, f.all, f.all)
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

func TestOutsideMemberAlarmIsShownNotRolledUp(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	f.handBuilt(t, ctx)

	systemRows, _ := f.recorded(t, ctx, "system", "hand")
	campusRows, _ := f.recorded(t, ctx, "location", "hq")

	alarm, err := f.gw.RaiseAlarm(ctx, "", "amp-1", storage.AlarmSpec{Severity: "high", Message: "amplifier powered off"})
	if err != nil {
		t.Fatalf("raise alarm: %v", err)
	}
	// The component's own health moves; nothing above it does.
	if _, v := f.recorded(t, ctx, "component", "amp-1"); v != "outage" {
		t.Fatalf("component = %q, want outage", v)
	}
	if n, v := f.recorded(t, ctx, "system", "hand"); v != "healthy" || n != systemRows {
		t.Fatalf("system recorded %d rows / %q, want %d / healthy: a member outside every slot moves nothing above it", n, v, systemRows)
	}
	if n, v := f.recorded(t, ctx, "location", "hq"); v != "healthy" || n != campusRows {
		t.Fatalf("campus recorded %d rows / %q, want %d / healthy", n, v, campusRows)
	}

	rep := f.report(t, ctx, "hand")
	if rep.Verdict != "healthy" {
		t.Fatalf("report verdict %q, want healthy", rep.Verdict)
	}
	if rep.Reason != "no slot is impaired" {
		t.Errorf("reason %q: a member outside a slot is never a cause", rep.Reason)
	}
	m := member(rep, "amp-1")
	if m == nil || !m.OutsideSlot || m.Slot != "" || m.Health != "outage" || len(m.Alarms) != 1 || m.Alarms[0].ID != alarm.ID {
		t.Fatalf("member = %+v, want amp-1 outside a slot, outage, carrying its alarm", m)
	}

	if err := f.gw.ClearAlarm(ctx, "", "amp-1", alarm.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if m := member(f.report(t, ctx, "hand"), "amp-1"); m == nil || len(m.Alarms) != 0 {
		t.Fatalf("after clear member = %+v, want no open alarms", m)
	}
}

// A slotted member carries the slot it fills, and its system's reason names the
// slot behind the verdict; an alarmed member outside a slot beside it changes
// neither.
func TestSlottedMemberNamesItsSlotAndTheReasonNamesTheSlot(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	f.mustComponent(t, ctx, "amp-2")
	if err := f.gw.AddMember(ctx, "", "hq-huddle", "amp-2", f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if _, err := f.gw.RaiseAlarm(ctx, "", "amp-2", storage.AlarmSpec{Severity: "high", Message: "amp off"}); err != nil {
		t.Fatalf("raise outside: %v", err)
	}
	if _, err := f.gw.RaiseAlarm(ctx, "", "bar-1", storage.AlarmSpec{Severity: "high", Message: "bar off"}); err != nil {
		t.Fatalf("raise slotted: %v", err)
	}
	rep := f.report(t, ctx, "hq-huddle")
	if rep.Verdict != "degraded" {
		t.Fatalf("verdict %q, want degraded (the table-mic slot's impact)", rep.Verdict)
	}
	if rep.Reason != "slot table-mic is impaired (0 of 1 occupying)" {
		t.Errorf("reason %q, want the slot alone", rep.Reason)
	}
	if m := member(rep, "bar-1"); m == nil || m.OutsideSlot || m.Slot != "table-mic" || len(m.Alarms) != 1 {
		t.Errorf("bar-1 = %+v, want in table-mic with its alarm", m)
	}
	if m := member(rep, "amp-2"); m == nil || !m.OutsideSlot || len(m.Alarms) != 1 {
		t.Errorf("amp-2 = %+v, want outside a slot with its alarm", m)
	}
}

// A caller who may read the system but none of its components gets the verdict
// and its slots, never a member's name, id or alarms: the member list follows
// the component read scope, the same rule the fleet view's dots follow (#458).
func TestSystemHealthMembersStayInComponentScope(t *testing.T) {
	t.Parallel()
	f := newHealthFixture(t)
	ctx := context.Background()
	f.handBuilt(t, ctx)
	if _, err := f.gw.RaiseAlarm(ctx, "", "amp-1", storage.AlarmSpec{Severity: "high", Message: "amplifier powered off"}); err != nil {
		t.Fatalf("raise alarm: %v", err)
	}

	rep, err := f.gw.SystemHealth(ctx, "hand", storage.HealthRange{}, f.all, scope.Set{})
	if err != nil {
		t.Fatalf("system health with no component read: %v", err)
	}
	if len(rep.Members) != 0 {
		t.Fatalf("members = %+v, want none for a caller who may read no component", rep.Members)
	}
	if rep.Verdict != "healthy" {
		t.Errorf("verdict %q, want healthy: the verdict does not depend on who reads it", rep.Verdict)
	}

	if m := member(f.report(t, ctx, "hand"), "amp-1"); m == nil {
		t.Fatal("a caller who may read every component does not see amp-1")
	}
}
