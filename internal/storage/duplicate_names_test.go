package storage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// newDuplicateNameFixture opens a gateway on a throwaway database with
// component_name_key dropped by raw DDL, so two components can legitimately
// share a name the way they will once #627 scopes name uniqueness to placement.
// The migration that relaxes the constraint for real is a later task; this
// fixture only needs to prove the GATEWAY's own queries survive that world, not
// implement it.
func newDuplicateNameFixture(t *testing.T) (gw storage.Gateway, conn *pgx.Conn) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	if _, err := conn.Exec(ctx, `alter table component drop constraint component_name_key`); err != nil {
		t.Fatalf("drop component name constraint: %v", err)
	}

	gw, err = storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return gw, conn
}

// twoDisplayOnes creates two components both named "display-1", in different
// rooms, plus a system and a quorum-1 role to staff. It is the shared fixture
// TestDuplicateNamesDoNotBreakGatewayReads and TestBareNameAmbiguousRefuses both
// build on.
func twoDisplayOnes(t *testing.T, gw storage.Gateway) (compA, compB *storage.Component, sys *storage.System) {
	t.Helper()
	ctx := context.Background()
	all := scope.Set{All: true}

	roomA, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "room-a", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("room-a: %v", err)
	}
	roomB, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "room-b", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("room-b: %v", err)
	}
	compA, err = gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1", LocationName: strptr(roomA.Name)}, all)
	if err != nil {
		t.Fatalf("component in room-a: %v", err)
	}
	compB, err = gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "display-1", LocationName: strptr(roomB.Name)}, all)
	if err != nil {
		t.Fatalf("component in room-b: %v", err)
	}
	if compA.ID == compB.ID {
		t.Fatalf("the two components did not actually land on different rows")
	}

	sys, err = gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "duptest-sys"}, all)
	if err != nil {
		t.Fatalf("system: %v", err)
	}
	if _, err := gw.SetSystemRole(ctx, "", "system", sys.Name, storage.SystemRoleSpec{
		Name: "seat", DisplayName: "Seat", Quorum: 1,
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}
	return compA, compB, sys
}

// TestDuplicateNamesDoNotBreakGatewayReads is the regression #627 Task 10 exists
// to prevent: once two rows legitimately share a name, every gateway path that
// resolves a reference through an inline `(select id from X where name = $1)`
// scalar subquery raises SQLSTATE 21000 ("more than one row returned by a
// subquery used as an expression") the moment that name is ambiguous, a hard 500
// out of alarm reads, member writes, role assignment, health recompute, and
// interface create. Addressing each duplicate by its uuid (never by the shared
// bare name) must work cleanly and land on the right row.
func TestDuplicateNamesDoNotBreakGatewayReads(t *testing.T) {
	gw, conn := newDuplicateNameFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	compA, compB, sys := twoDisplayOnes(t, gw)

	// Alarm create, by uuid: must not 21000, and must land on the right row.
	alarm, err := gw.RaiseAlarm(ctx, "", compA.ID, storage.AlarmSpec{Severity: "critical", Message: "m", DedupKey: "k"})
	if err != nil {
		t.Fatalf("raise alarm on compA by id: %v", err)
	}
	if alarm.ComponentID != compA.ID {
		t.Fatalf("alarm landed on %s, want %s", alarm.ComponentID, compA.ID)
	}
	aAlarms, err := gw.ListAlarms(ctx, compA.ID, true)
	if err != nil {
		t.Fatalf("list alarms compA by id: %v", err)
	}
	if len(aAlarms) != 1 {
		t.Fatalf("compA alarms = %d, want 1", len(aAlarms))
	}
	bAlarms, err := gw.ListAlarms(ctx, compB.ID, true)
	if err != nil {
		t.Fatalf("list alarms compB by id: %v", err)
	}
	if len(bAlarms) != 0 {
		t.Fatalf("compB alarms = %d, want 0 (cross-contaminated by the shared name)", len(bAlarms))
	}

	// Member write, by uuid.
	if err := gw.AddMember(ctx, "", sys.Name, compA.ID, all); err != nil {
		t.Fatalf("add member by id: %v", err)
	}
	memsA, err := gw.ComponentMemberships(ctx, compA.ID, all)
	if err != nil {
		t.Fatalf("compA memberships: %v", err)
	}
	if len(memsA) != 1 {
		t.Fatalf("compA memberships = %d, want 1", len(memsA))
	}
	memsB, err := gw.ComponentMemberships(ctx, compB.ID, all)
	if err != nil {
		t.Fatalf("compB memberships: %v", err)
	}
	if len(memsB) != 0 {
		t.Fatalf("compB memberships = %d, want 0 (cross-contaminated by the shared name)", len(memsB))
	}

	// Role assignment, by uuid.
	if err := gw.AssignRole(ctx, "", sys.Name, "seat", compA.ID, all); err != nil {
		t.Fatalf("assign role by id: %v", err)
	}
	var assignedComponent string
	if err := conn.QueryRow(ctx,
		`select component_id from system_role_assignment where system_id = $1`, sys.ID,
	).Scan(&assignedComponent); err != nil {
		t.Fatalf("read assignment: %v", err)
	}
	if assignedComponent != compA.ID {
		t.Fatalf("assignment landed on %s, want %s", assignedComponent, compA.ID)
	}

	// Health recompute: already exercised in-transaction by RaiseAlarm and
	// AssignRole above (both call it); read the result back explicitly too.
	rep, err := gw.SystemHealth(ctx, sys.Name, time.Time{}, all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	if rep.Verdict == "" {
		t.Fatalf("system health verdict came back empty")
	}
	if len(rep.Roles) != 1 || len(rep.Roles[0].AssignedTo) != 1 {
		t.Fatalf("system health roles = %+v, want one role with one assignee", rep.Roles)
	}
	crep, err := gw.RaiseAlarm(ctx, "", compB.ID, storage.AlarmSpec{Severity: "critical", Message: "m2", DedupKey: "k2"})
	if err != nil {
		t.Fatalf("raise alarm on compB by id: %v", err)
	}
	_ = crep

	// Interface create, by uuid.
	it, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "tcp", Component: strptr(compA.ID)}, all)
	if err != nil {
		t.Fatalf("create interface by id: %v", err)
	}
	if it.ComponentID == nil || *it.ComponentID != compA.ID {
		t.Fatalf("interface component = %v, want %s", it.ComponentID, compA.ID)
	}
	ifs, err := gw.ListComponentInterfaces(ctx, compA.ID)
	if err != nil {
		t.Fatalf("list interfaces by id: %v", err)
	}
	if len(ifs) != 1 {
		t.Fatalf("compA interfaces = %d, want 1", len(ifs))
	}
	ifsB, err := gw.ListComponentInterfaces(ctx, compB.ID)
	if err != nil {
		t.Fatalf("list interfaces compB by id: %v", err)
	}
	if len(ifsB) != 0 {
		t.Fatalf("compB interfaces = %d, want 0 (cross-contaminated by the shared name)", len(ifsB))
	}
}

// TestBareNameAmbiguousRefuses proves scopedByName refuses an ambiguous bare
// name rather than silently picking one row and hiding the other: the read-side
// half of the same fix.
func TestBareNameAmbiguousRefuses(t *testing.T) {
	gw, _ := newDuplicateNameFixture(t)
	ctx := context.Background()
	all := scope.Set{All: true}
	compA, compB, _ := twoDisplayOnes(t, gw)

	_, err := gw.GetComponent(ctx, "display-1", all)
	var ambig *storage.ErrAmbiguousName
	if !errors.As(err, &ambig) {
		t.Fatalf("get by ambiguous bare name = %v, want *storage.ErrAmbiguousName", err)
	}
	if ambig.Kind != "component" {
		t.Errorf("kind = %q, want %q", ambig.Kind, "component")
	}
	if ambig.Ref != "display-1" {
		t.Errorf("ref = %q, want %q", ambig.Ref, "display-1")
	}
	if len(ambig.Candidates) != 2 {
		t.Fatalf("candidates = %v, want 2", ambig.Candidates)
	}
	got := map[string]bool{ambig.Candidates[0]: true, ambig.Candidates[1]: true}
	if !got[compA.ID] || !got[compB.ID] {
		t.Errorf("candidates = %v, want %s and %s", ambig.Candidates, compA.ID, compB.ID)
	}
}
