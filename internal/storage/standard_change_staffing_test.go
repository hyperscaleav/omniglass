package storage_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/db"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// Changing a system's standard carries its staffing onto the roles that still
// exist and releases the rest (#506, ADR-0506). The operator's view of it is
// driven over the wire by internal/api/standard_change_staffing_e2e_test.go;
// these are the gateway halves the wire cannot see: positions and capacity,
// the audit trail, the order against the health recompute, the serialization
// against a staffing write, and the one-time release of the strands an earlier
// release already made.

// declareRole declares a role on a standard the test owns.
func (f *healthFixture) declareRole(t *testing.T, ctx context.Context, standard string, spec storage.SystemRoleSpec) {
	t.Helper()
	if err := f.gw.UpsertStandard(ctx, storage.Standard{Name: standard, Label: standard}); err != nil {
		t.Fatalf("standard %s: %v", standard, err)
	}
	if _, err := f.gw.SetSystemRole(ctx, "", "standard", standard, spec); err != nil {
		t.Fatalf("declare %s/%s: %v", standard, spec.Name, err)
	}
}

// mustBar makes a component of the fixture's video-bar product.
func (f *healthFixture) mustBar(t *testing.T, ctx context.Context, name string) {
	t.Helper()
	product := f.bar.Name
	if _, err := f.gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name, ProductName: &product}, f.all, all, all, all); err != nil {
		t.Fatalf("component %s: %v", name, err)
	}
}

func (f *healthFixture) moveStandard(t *testing.T, ctx context.Context, system, standard string) {
	t.Helper()
	if _, err := f.gw.UpdateSystem(ctx, "", system, storage.SystemPatch{StandardID: &standard}, f.all, f.all); err != nil {
		t.Fatalf("move %s onto %q: %v", system, standard, err)
	}
}

func (f *healthFixture) effectiveRole(t *testing.T, ctx context.Context, system, role string) storage.EffectiveRole {
	t.Helper()
	roles, err := f.gw.EffectiveRoles(ctx, system, f.all)
	if err != nil {
		t.Fatalf("effective roles %s: %v", system, err)
	}
	for _, r := range roles {
		if r.Name == role {
			return r
		}
	}
	t.Fatalf("%s has no role %q (roles %+v)", system, role, roles)
	return storage.EffectiveRole{}
}

func (f *healthFixture) count(t *testing.T, ctx context.Context, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// assignmentID is the id of the one assignment a component holds in a system.
func (f *healthFixture) assignmentID(t *testing.T, ctx context.Context, system, component string) string {
	t.Helper()
	var id string
	if err := f.conn.QueryRow(ctx, `
		select ra.id from system_role_assignment ra
		where ra.system_id = (select id from system where name = $1)
		  and ra.component_id = (select id from component where name = $2)`, system, component).Scan(&id); err != nil {
		t.Fatalf("assignment of %s in %s: %v", component, system, err)
	}
	return id
}

// strandedAssignments counts the assignments whose role is neither the
// system's own nor its current standard's: the rows #506 left behind.
const strandedAssignments = `
	select count(*) from system_role_assignment ra
	join system s on s.id = ra.system_id
	join system_role r on r.id = ra.role_id
	where not (r.owner_kind = 'system' and r.system_id = s.id)
	  and not (r.owner_kind = 'standard' and r.standard_id is not distinct from s.standard_id)`

// TestStandardChangeCarriesStaffingOntoTheRolesThatStillExist: the fixture's
// bar fills health-huddle's table-mic; moving the system onto a standard that
// declares a table-mic it still fits carries it across, audited as one update
// of its assignment, and the health the move records already counts it.
func TestStandardChangeCarriesStaffingOntoTheRolesThatStillExist(t *testing.T) {
	ctx := context.Background()
	f := newHealthFixture(t)
	f.declareRole(t, ctx, "huddle-next", storage.SystemRoleSpec{
		Name: "table-mic", Label: "Table microphone", Quorum: 1, AcceptedTypes: []string{"video-bar"}, Impact: "degraded",
	})
	// A role the system declares itself is not the standard's to carry.
	if _, err := f.gw.SetSystemRole(ctx, "", "system", "hq-huddle", storage.SystemRoleSpec{Name: "spare", Label: "Spare", Quorum: 1}); err != nil {
		t.Fatalf("the system's own role: %v", err)
	}
	f.mustBar(t, ctx, "bar-own")
	if err := f.gw.AssignRole(ctx, "", "hq-huddle", "spare", "bar-own", f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("staff the system's own role: %v", err)
	}
	f.mustAgreeWithRecord(t, ctx, "hq-huddle", "healthy")
	carried := f.assignmentID(t, ctx, "hq-huddle", "bar-1")

	f.moveStandard(t, ctx, "hq-huddle", "huddle-next")

	if mic := f.effectiveRole(t, ctx, "hq-huddle", "table-mic"); !slices.Equal(mic.AssignedTo, []string{"bar-1"}) || !mic.FromStandard {
		t.Fatalf("table-mic after the move = %+v, want bar-1 carried onto huddle-next's", mic)
	}
	if spare := f.effectiveRole(t, ctx, "hq-huddle", "spare"); !slices.Equal(spare.AssignedTo, []string{"bar-own"}) {
		t.Fatalf("the system's own spare after the move = %+v, want bar-own untouched", spare)
	}
	// The carry moved the row rather than replacing it, so its history holds.
	if got := f.assignmentID(t, ctx, "hq-huddle", "bar-1"); got != carried {
		t.Fatalf("bar-1's assignment is %s after the move, want the same row %s carried", got, carried)
	}
	var oldImage, newImage []byte
	if err := f.conn.QueryRow(ctx, `select old, new from audit_log
		where resource = 'system_role_assignment' and verb = 'update' and resource_id = $1
		order by id desc limit 1`, carried).Scan(&oldImage, &newImage); err != nil {
		t.Fatalf("the carry's audit row: %v", err)
	}
	if !strings.Contains(string(oldImage), "health-huddle") || !strings.Contains(string(newImage), "huddle-next") {
		t.Fatalf("the carry's audit = %s -> %s, want it to name both standards", oldImage, newImage)
	}
	// The recompute runs after the carry, so the record agrees with the read.
	f.mustAgreeWithRecord(t, ctx, "hq-huddle", "healthy")
	if n := f.count(t, ctx, strandedAssignments); n != 0 {
		t.Fatalf("%d assignment(s) stranded on a standard the system left", n)
	}
}

// TestStandardChangeKeepsPositionsWithinTheNewCapacity: a carried occupant
// keeps its position where it is free in the new role, lowest positions are
// carried first when the new role has less room, and an occupant past the new
// capacity is released, still a member, audited as a delete of its assignment.
func TestStandardChangeKeepsPositionsWithinTheNewCapacity(t *testing.T) {
	ctx := context.Background()
	f := newHealthFixture(t)
	three, one := 3, 1
	f.declareRole(t, ctx, "pair-wide", storage.SystemRoleSpec{Name: "pair", Label: "Pair", Quorum: 1, Capacity: &three, Impact: "degraded"})
	f.declareRole(t, ctx, "pair-open", storage.SystemRoleSpec{Name: "pair", Label: "Pair", Quorum: 1, Impact: "degraded"})
	f.declareRole(t, ctx, "pair-narrow", storage.SystemRoleSpec{Name: "pair", Label: "Pair", Quorum: 1, Capacity: &one, Impact: "degraded"})
	std, room := "pair-wide", "hq-r1"
	if _, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "cap-room", StandardID: &std, LocationName: &room}, f.all, all); err != nil {
		t.Fatalf("system: %v", err)
	}
	for _, c := range []string{"cap-1", "cap-2", "cap-3"} {
		f.mustBar(t, ctx, c)
		if err := f.gw.AssignRole(ctx, "", "cap-room", "pair", c, f.all, f.all, f.all, f.all); err != nil {
			t.Fatalf("assign %s: %v", c, err)
		}
	}
	if err := f.gw.UnassignRole(ctx, "", "cap-room", "pair", "cap-1", f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("vacate position 1: %v", err)
	}

	f.moveStandard(t, ctx, "cap-room", "pair-open")
	pair := f.effectiveRole(t, ctx, "cap-room", "pair")
	if !slices.Equal(pair.AssignedTo, []string{"cap-2", "cap-3"}) || !slices.Equal(pair.Positions, []int{2, 3}) {
		t.Fatalf("pair on pair-open = %v at %v, want cap-2 and cap-3 at their own positions 2 and 3", pair.AssignedTo, pair.Positions)
	}

	released := f.assignmentID(t, ctx, "cap-room", "cap-3")
	f.moveStandard(t, ctx, "cap-room", "pair-narrow")
	pair = f.effectiveRole(t, ctx, "cap-room", "pair")
	if !slices.Equal(pair.AssignedTo, []string{"cap-2"}) || !slices.Equal(pair.Positions, []int{1}) {
		t.Fatalf("pair on pair-narrow = %v at %v, want cap-2 alone at position 1 (its own 2 is past the capacity)", pair.AssignedTo, pair.Positions)
	}
	if n := f.count(t, ctx, `select count(*) from system_member where component_id = (select id from component where name = 'cap-3')`); n != 1 {
		t.Fatalf("cap-3 has %d memberships after its release, want it still a member", n)
	}
	if n := f.count(t, ctx, `select count(*) from audit_log where resource = 'system_role_assignment' and verb = 'delete' and resource_id = $1`, released); n != 1 {
		t.Fatalf("%d delete audit rows for cap-3's released assignment, want 1", n)
	}
	// A released member is free: it leaves the system and the estate.
	if err := f.gw.RemoveMember(ctx, "", "cap-room", "cap-3", f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("unbind the released member: %v", err)
	}
	if err := f.gw.DeleteComponent(ctx, "", "cap-3", f.all, f.all); err != nil {
		t.Fatalf("delete the released component: %v", err)
	}
}

// TestStaffingWaitsOutAStandardChange: a staffing write that starts while a
// standard change is open resolves its role after the change commits, never
// against the standard being left, so it cannot strand an assignment the
// change has already settled. The change is held open on a raw connection at
// the point the gateway's own change holds: the system's staffing lock taken
// exclusive and its standard repointed, the carry not yet committed.
func TestStaffingWaitsOutAStandardChange(t *testing.T) {
	ctx := context.Background()
	f := newHealthFixture(t)
	f.declareRole(t, ctx, "huddle-next", storage.SystemRoleSpec{
		Name: "table-mic", Label: "Table microphone", Quorum: 1, AcceptedTypes: []string{"video-bar"}, Impact: "degraded",
	})
	if err := f.gw.UnassignRole(ctx, "", "hq-huddle", "table-mic", "bar-1", f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("vacate table-mic: %v", err)
	}

	change, err := f.conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = change.Rollback(ctx) }()
	if _, err := change.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('system/staffing/' || (select id from system where name = 'hq-huddle'), 0))`); err != nil {
		t.Fatalf("take the staffing lock: %v", err)
	}
	if _, err := change.Exec(ctx, `update system set standard_id = (select id from standard where name = 'huddle-next')
		where name = 'hq-huddle'`); err != nil {
		t.Fatalf("open the change: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- f.gw.AssignRole(ctx, "", "hq-huddle", "table-mic", "bar-1", f.all, f.all, f.all, f.all)
	}()
	// Commit once the assign is waiting on the change, or once it has finished
	// without waiting (the defect), whichever comes first.
	var early error
	finished := false
	deadline := time.Now().Add(10 * time.Second)
	for !finished && time.Now().Before(deadline) {
		select {
		case early = <-done:
			finished = true
		default:
		}
		if finished {
			break
		}
		var waiting bool
		if err := change.QueryRow(ctx, `select exists (select 1 from pg_locks where not granted)`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := change.Commit(ctx); err != nil {
		t.Fatalf("commit the change: %v", err)
	}
	if !finished {
		early = <-done
	}
	if early != nil {
		t.Fatalf("assign across the change: %v", early)
	}
	var standard string
	if err := f.conn.QueryRow(ctx, `select coalesce(st.name, '') from system_role_assignment ra
		join system_role r on r.id = ra.role_id left join standard st on st.id = r.standard_id
		where ra.component_id = (select id from component where name = 'bar-1')`).Scan(&standard); err != nil {
		t.Fatalf("read the assignment: %v", err)
	}
	if standard != "huddle-next" {
		t.Fatalf("the assign landed on %q's table-mic, want huddle-next's: it resolved against the standard being left", standard)
	}
}

// strandedBackfill is the one-time release of the assignments a standard
// change stranded before #506, driven from the embedded migration text.
const strandedBackfill = "migrations/20261026120000_stranded_role_assignment_backfill.sql"

func TestStrandedAssignmentBackfillReleasesOnlyTheStrands(t *testing.T) {
	ctx := context.Background()
	f := newHealthFixture(t)
	raw, err := db.FS.ReadFile(strandedBackfill)
	if err != nil {
		t.Fatalf("read the backfill: %v", err)
	}
	up, down, ok := strings.Cut(string(raw), "-- migrate:down")
	if !ok {
		t.Fatal("the backfill has no -- migrate:down")
	}
	up = strings.TrimPrefix(up, "-- migrate:up")

	// A second room still on health-huddle keeps its staffing.
	std, room := "health-huddle", "hq-r1"
	if _, err := f.gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "kept-room", StandardID: &std, LocationName: &room}, f.all, all); err != nil {
		t.Fatalf("system: %v", err)
	}
	f.mustBar(t, ctx, "bar-kept")
	if err := f.gw.AssignRole(ctx, "", "kept-room", "table-mic", "bar-kept", f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("staff kept-room: %v", err)
	}
	// hq-huddle's own role keeps its staffing too.
	if _, err := f.gw.SetSystemRole(ctx, "", "system", "hq-huddle", storage.SystemRoleSpec{Name: "spare", Label: "Spare", Quorum: 1}); err != nil {
		t.Fatalf("the system's own role: %v", err)
	}
	f.mustBar(t, ctx, "bar-own")
	if err := f.gw.AssignRole(ctx, "", "hq-huddle", "spare", "bar-own", f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("staff the system's own role: %v", err)
	}
	// What a release before #506 left: hq-huddle repointed with bar-1 still on
	// health-huddle's table-mic, invisible and wedged.
	if err := f.gw.UpsertStandard(ctx, storage.Standard{Name: "huddle-bare", Label: "Bare"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.conn.Exec(ctx, `update system set standard_id = (select id from standard where name = 'huddle-bare') where name = 'hq-huddle'`); err != nil {
		t.Fatalf("strand: %v", err)
	}
	if err := f.gw.RemoveMember(ctx, "", "hq-huddle", "bar-1", f.all, f.all, f.all, f.all); !errors.Is(err, storage.ErrMemberOccupied) {
		t.Fatalf("unbinding the stranded member before the backfill = %v, want ErrMemberOccupied (the wedge)", err)
	}

	for run := 0; run < 2; run++ {
		if _, err := f.conn.Exec(ctx, up); err != nil {
			t.Fatalf("run %d of the backfill: %v", run, err)
		}
		if n := f.count(t, ctx, strandedAssignments); n != 0 {
			t.Fatalf("run %d: %d stranded assignment(s) left", run, n)
		}
		if n := f.count(t, ctx, `select count(*) from system_role_assignment`); n != 2 {
			t.Fatalf("run %d: %d assignments left, want kept-room's and hq-huddle's own", run, n)
		}
	}
	if _, err := f.conn.Exec(ctx, down); err != nil {
		t.Fatalf("the backfill's down: %v", err)
	}
	if err := f.gw.RemoveMember(ctx, "", "hq-huddle", "bar-1", f.all, f.all, f.all, f.all); err != nil {
		t.Fatalf("unbind the released member: %v", err)
	}
	if err := f.gw.DeleteComponent(ctx, "", "bar-1", f.all, f.all); err != nil {
		t.Fatalf("delete the released component: %v", err)
	}
}
