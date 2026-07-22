package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// memberFixture builds a room with two systems in it and three components, none
// of them members yet. Two systems is the point: membership is many-valued, and a
// single-system fixture cannot tell "belongs to the one system there is" from
// "belongs to this system in particular".
type memberFixture struct {
	gw  *storage.PG
	all scope.Set
}

func newMemberFixture(t *testing.T, ctx context.Context) *memberFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	f := &memberFixture{gw: gw, all: scope.Set{All: true}}

	for _, id := range []string{"member-standard"} {
		if err := gw.UpsertStandard(ctx, storage.Standard{Name: id, DisplayName: "Member Standard"}); err != nil {
			t.Fatalf("standard: %v", err)
		}
	}
	std := "member-standard"
	for _, s := range []string{"room-a", "room-b"} {
		if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: s, StandardID: &std}, f.all); err != nil {
			t.Fatalf("system %s: %v", s, err)
		}
	}
	bar := "cisco-room-bar"
	for _, c := range []string{"dsp", "mic-a", "mic-b"} {
		product := bar
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
			Name: c, ProductName: &product}, f.all); err != nil {
			t.Fatalf("component %s: %v", c, err)
		}
	}
	return f
}

// The shared device is the case the old single pointer could not express at all.
func TestMembershipIsManyValued(t *testing.T) {
	ctx := context.Background()
	f := newMemberFixture(t, ctx)

	for _, s := range []string{"room-a", "room-b"} {
		if err := f.gw.AddMember(ctx, "", s, "dsp", f.all); err != nil {
			t.Fatalf("add dsp to %s: %v", s, err)
		}
	}
	got, err := f.gw.ComponentMemberships(ctx, "dsp", f.all)
	if err != nil {
		t.Fatalf("memberships: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("dsp memberships = %d, want 2: a shared device belongs to every system it serves", len(got))
	}

	// And from the other direction, a system lists who is in it.
	members, err := f.gw.ListMembers(ctx, "room-a", f.all)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 1 || members[0].ComponentID != "dsp" {
		t.Fatalf("room-a members = %+v, want just dsp", members)
	}
}

// "Is this component shared" and "is this its default" are different questions,
// and a row carries both because one cannot be derived from the other. The case
// that proves it: a component whose default IS this system while it also serves
// another. Reading sharing off the primary flag would call that one exclusive,
// which is how the console first got it wrong.
func TestMemberCarriesHowManySystemsItServes(t *testing.T) {
	ctx := context.Background()
	f := newMemberFixture(t, ctx)

	for _, s := range []string{"room-a", "room-b"} {
		if err := f.gw.AddMember(ctx, "", s, "dsp", f.all); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	if err := f.gw.AddMember(ctx, "", "room-a", "mic-a", f.all); err != nil {
		t.Fatalf("add: %v", err)
	}

	members, err := f.gw.ListMembers(ctx, "room-a", f.all)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := map[string]storage.Member{}
	for _, m := range members {
		got[m.ComponentID] = m
	}
	// dsp took its default here (first membership) AND serves room-b.
	if d := got["dsp"]; !d.IsPrimary || d.SystemCount != 2 {
		t.Errorf("dsp in room-a = primary %v, system count %d; want primary with a count of 2: "+
			"holding the default here says nothing about whether it serves elsewhere",
			d.IsPrimary, d.SystemCount)
	}
	if m := got["mic-a"]; !m.IsPrimary || m.SystemCount != 1 {
		t.Errorf("mic-a = primary %v, system count %d, want primary with a count of 1",
			m.IsPrimary, m.SystemCount)
	}
}

// Adding the same membership twice is the same membership, not a second one.
func TestAddMemberIsIdempotent(t *testing.T) {
	ctx := context.Background()
	f := newMemberFixture(t, ctx)

	for range 2 {
		if err := f.gw.AddMember(ctx, "", "room-a", "mic-a", f.all); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	got, err := f.gw.ComponentMemberships(ctx, "mic-a", f.all)
	if err != nil {
		t.Fatalf("memberships: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("memberships after two adds = %d, want 1", len(got))
	}
}

// A component's FIRST membership is its default with no operator involvement: the
// single-system case, which is almost every component, must never surface the
// concept at all.
func TestFirstMembershipIsPrimary(t *testing.T) {
	ctx := context.Background()
	f := newMemberFixture(t, ctx)

	if err := f.gw.AddMember(ctx, "", "room-a", "mic-a", f.all); err != nil {
		t.Fatalf("add: %v", err)
	}
	got, _ := f.gw.ComponentMemberships(ctx, "mic-a", f.all)
	if len(got) != 1 || !got[0].IsPrimary {
		t.Fatalf("sole membership = %+v, want it primary without being asked", got)
	}

	// A second membership does not steal the default.
	if err := f.gw.AddMember(ctx, "", "room-b", "mic-a", f.all); err != nil {
		t.Fatalf("add second: %v", err)
	}
	got, _ = f.gw.ComponentMemberships(ctx, "mic-a", f.all)
	var primaries []string
	for _, m := range got {
		if m.IsPrimary {
			primaries = append(primaries, m.SystemID)
		}
	}
	if len(primaries) != 1 || primaries[0] != "room-a" {
		t.Fatalf("primaries = %v, want exactly [room-a]: a later membership must not steal the default", primaries)
	}
}

// Exactly one answer to "which is the default", moved rather than duplicated.
func TestSetPrimaryMemberMovesTheDefault(t *testing.T) {
	ctx := context.Background()
	f := newMemberFixture(t, ctx)

	for _, s := range []string{"room-a", "room-b"} {
		if err := f.gw.AddMember(ctx, "", s, "dsp", f.all); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	if err := f.gw.SetPrimaryMember(ctx, "", "room-b", "dsp", f.all); err != nil {
		t.Fatalf("set primary: %v", err)
	}
	got, _ := f.gw.ComponentMemberships(ctx, "dsp", f.all)
	var primaries []string
	for _, m := range got {
		if m.IsPrimary {
			primaries = append(primaries, m.SystemID)
		}
	}
	if len(primaries) != 1 || primaries[0] != "room-b" {
		t.Fatalf("primaries = %v, want exactly [room-b]", primaries)
	}
}

// Staffing a role IS membership: an operator should never have to say it twice.
func TestAssignRoleCreatesTheMembership(t *testing.T) {
	ctx := context.Background()
	f := newMemberFixture(t, ctx)

	if _, err := f.gw.SetSystemRole(ctx, "", "system", "room-a", storage.SystemRoleSpec{
		Name: "mic", DisplayName: "Mic", Quorum: 1,
		Capabilities: []string{"microphone"}, Impact: "degraded",
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}
	if err := f.gw.AssignRole(ctx, "", "room-a", "mic", "mic-a", f.all); err != nil {
		t.Fatalf("assign: %v", err)
	}
	got, err := f.gw.ComponentMemberships(ctx, "mic-a", f.all)
	if err != nil {
		t.Fatalf("memberships: %v", err)
	}
	if len(got) != 1 || got[0].SystemID != "room-a" {
		t.Fatalf("memberships after assign = %+v, want one in room-a: staffing a role is membership", got)
	}
}

// A membership still holding a role assignment is load-bearing. Removing it would
// leave the system staffed by a non-member, so it is refused until the role is
// given up, the same shape as every other occupied-delete in the gateway.
func TestRemoveMemberRefusedWhileStaffingARole(t *testing.T) {
	ctx := context.Background()
	f := newMemberFixture(t, ctx)

	if _, err := f.gw.SetSystemRole(ctx, "", "system", "room-a", storage.SystemRoleSpec{
		Name: "mic", DisplayName: "Mic", Quorum: 1,
		Capabilities: []string{"microphone"}, Impact: "degraded",
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}
	if err := f.gw.AssignRole(ctx, "", "room-a", "mic", "mic-a", f.all); err != nil {
		t.Fatalf("assign: %v", err)
	}
	err := f.gw.RemoveMember(ctx, "", "room-a", "mic-a", f.all)
	if !errors.Is(err, storage.ErrMemberOccupied) {
		t.Fatalf("remove a member still staffing a role = %v, want ErrMemberOccupied", err)
	}

	// Given up the role, it leaves cleanly.
	if err := f.gw.UnassignRole(ctx, "", "room-a", "mic", "mic-a", f.all); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if err := f.gw.RemoveMember(ctx, "", "room-a", "mic-a", f.all); err != nil {
		t.Fatalf("remove after unassign: %v", err)
	}
}

// A membership is a binding between two things and is meaningless once either is
// gone, so it cascades from both ends. It deliberately does NOT make a component
// undeletable: the guard that matters, refusing to delete a component that fills a
// job, belongs to role_assignment, and duplicating it here would add a step to
// every component removal while protecting nothing new.
func TestMembershipCascadesFromBothEnds(t *testing.T) {
	ctx := context.Background()
	f := newMemberFixture(t, ctx)

	// Deleting the system takes its memberships with it.
	if err := f.gw.AddMember(ctx, "", "room-a", "mic-a", f.all); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := f.gw.DeleteSystem(ctx, "", "room-a", f.all, f.all); err != nil {
		t.Fatalf("delete system: %v", err)
	}
	got, err := f.gw.ComponentMemberships(ctx, "mic-a", f.all)
	if err != nil {
		t.Fatalf("memberships: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("memberships after the system was deleted = %+v, want none", got)
	}

	// A plain member deletes cleanly: membership alone is an inventory fact.
	if err := f.gw.AddMember(ctx, "", "room-b", "mic-b", f.all); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := f.gw.DeleteComponent(ctx, "", "mic-b", f.all, f.all); err != nil {
		t.Fatalf("delete a component that only holds a membership: %v", err)
	}

	// Staffing a role is what makes it undeletable, and that is role_assignment's
	// guard, unchanged by this slice.
	if _, err := f.gw.SetSystemRole(ctx, "", "system", "room-b", storage.SystemRoleSpec{
		Name: "mic", DisplayName: "Mic", Quorum: 1,
		Capabilities: []string{"microphone"}, Impact: "degraded",
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}
	if err := f.gw.AssignRole(ctx, "", "room-b", "mic", "dsp", f.all); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if err := f.gw.DeleteComponent(ctx, "", "dsp", f.all, f.all); !errors.Is(err, storage.ErrReferenced) {
		t.Fatalf("delete a component staffing a role = %v, want ErrReferenced", err)
	}
}

// Membership is scoped like every other estate relation: a system out of the
// caller's scope is a non-disclosing not-found, never a partial answer.
func TestMembershipIsScoped(t *testing.T) {
	ctx := context.Background()
	f := newMemberFixture(t, ctx)

	none := scope.Set{}
	if err := f.gw.AddMember(ctx, "", "room-a", "mic-a", none); !errors.Is(err, storage.ErrSystemNotFound) {
		t.Fatalf("add out of scope = %v, want ErrSystemNotFound", err)
	}
}
