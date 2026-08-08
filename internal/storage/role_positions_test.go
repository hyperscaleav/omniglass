package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// declareTableMic is the fixture every position test shares: a fresh ad-hoc
// system with a quorum-2 table-mic role accepting any video-bar, so a test
// only names its own components.
func declareTableMic(t *testing.T, ctx context.Context, gw storage.Gateway, all scope.Set, system string, quorum int) {
	t.Helper()
	typedSlotSystem(t, ctx, gw, all, system)
	if _, err := gw.SetSystemRole(ctx, "", "system", system, storage.SystemRoleSpec{
		Name: "table-mic", DisplayName: "Table Mic", Quorum: quorum, AcceptedTypes: []string{"video-bar"},
	}); err != nil {
		t.Fatalf("declare table-mic on %s: %v", system, err)
	}
}

// newBarInto creates a video-bar component and immediately asserts the
// create succeeded, the shared shape every position test's fixtures need.
func newBarInto(t *testing.T, ctx context.Context, gw storage.Gateway, all scope.Set, name string) {
	t.Helper()
	bar := "cisco-room-bar"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name, ProductName: &bar}, all); err != nil {
		t.Fatalf("create component %s: %v", name, err)
	}
}

func assignedTo(t *testing.T, roles []storage.EffectiveRole, name string) []string {
	t.Helper()
	for _, r := range roles {
		if r.Name == name {
			return r.AssignedTo
		}
	}
	t.Fatalf("role %q not found among %+v", name, roles)
	return nil
}

// TestAssignedToIsPositionOrdered proves the #626 ordered read: assignment
// order, not alphabetical order, decides how a role's occupants list. Both
// resolved reads (EffectiveRoles, the declaration-plus-staffing read, and
// SystemHealth, the verdict read) must agree, since they resolve the same
// underlying assignments and a divergence between them would be a silent
// display bug on one and a verdict bug on the other.
func TestAssignedToIsPositionOrdered(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	declareTableMic(t, ctx, gw, all, "position-order-sys", 2)
	newBarInto(t, ctx, gw, all, "zeta")
	newBarInto(t, ctx, gw, all, "alpha")

	if err := gw.AssignRole(ctx, "", "position-order-sys", "table-mic", "zeta", all); err != nil {
		t.Fatalf("assign zeta: %v", err)
	}
	if err := gw.AssignRole(ctx, "", "position-order-sys", "table-mic", "alpha", all); err != nil {
		t.Fatalf("assign alpha: %v", err)
	}

	roles, err := gw.EffectiveRoles(ctx, "position-order-sys", all)
	if err != nil {
		t.Fatalf("effective roles: %v", err)
	}
	if got := assignedTo(t, roles, "table-mic"); !sameSeq(got, []string{"zeta", "alpha"}) {
		t.Errorf("EffectiveRoles assigned_to = %v, want [zeta alpha] (assignment order), not alphabetical", got)
	}

	rep, err := gw.SystemHealth(ctx, "position-order-sys", time.Time{}, all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	var gotHealth []string
	for _, r := range rep.Roles {
		if r.Name == "table-mic" {
			gotHealth = r.AssignedTo
		}
	}
	if !sameSeq(gotHealth, []string{"zeta", "alpha"}) {
		t.Errorf("SystemHealth assigned_to = %v, want [zeta alpha] (assignment order), not alphabetical", gotHealth)
	}
}

// TestSwapIsAtomic proves SwapPositions exchanges two occupants' positions in
// one statement rather than raising the moment the first row lands on the
// other's slot (the reason the uniqueness constraint has to be deferrable,
// not a plain unique index: see the floor migration).
func TestSwapIsAtomic(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	declareTableMic(t, ctx, gw, all, "swap-sys", 2)
	newBarInto(t, ctx, gw, all, "first")
	newBarInto(t, ctx, gw, all, "second")

	if err := gw.AssignRole(ctx, "", "swap-sys", "table-mic", "first", all); err != nil {
		t.Fatalf("assign first: %v", err)
	}
	if err := gw.AssignRole(ctx, "", "swap-sys", "table-mic", "second", all); err != nil {
		t.Fatalf("assign second: %v", err)
	}
	roles, err := gw.EffectiveRoles(ctx, "swap-sys", all)
	if err != nil {
		t.Fatalf("effective roles: %v", err)
	}
	if got := assignedTo(t, roles, "table-mic"); !sameSeq(got, []string{"first", "second"}) {
		t.Fatalf("before swap = %v, want [first second]", got)
	}

	if err := gw.SwapPositions(ctx, "", "swap-sys", "table-mic", 1, 2, all); err != nil {
		t.Fatalf("swap positions: %v", err)
	}
	roles, err = gw.EffectiveRoles(ctx, "swap-sys", all)
	if err != nil {
		t.Fatalf("effective roles after swap: %v", err)
	}
	if got := assignedTo(t, roles, "table-mic"); !sameSeq(got, []string{"second", "first"}) {
		t.Fatalf("after swap = %v, want [second first]", got)
	}

	// A position nobody holds is a not-found, not a silent no-op.
	if err := gw.SwapPositions(ctx, "", "swap-sys", "table-mic", 1, 99, all); err == nil {
		t.Fatal("swap against an unoccupied position succeeded, want ErrAssignmentMissing")
	}
}

// TestConcurrentAssignsGetDistinctPositions proves the advisory lock (#626):
// two concurrent assigns to the same role must not compute the same
// next-free position and collide on the deferrable uniqueness constraint.
// Reachable through runTogether the same way health_invariant_test.go's
// concurrent-write proofs are.
func TestConcurrentAssignsGetDistinctPositions(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	declareTableMic(t, ctx, gw, all, "concurrent-position-sys", 2)
	newBarInto(t, ctx, gw, all, "race-a")
	newBarInto(t, ctx, gw, all, "race-b")

	runTogether(t,
		func() error { return gw.AssignRole(ctx, "", "concurrent-position-sys", "table-mic", "race-a", all) },
		func() error { return gw.AssignRole(ctx, "", "concurrent-position-sys", "table-mic", "race-b", all) },
	)

	roles, err := gw.EffectiveRoles(ctx, "concurrent-position-sys", all)
	if err != nil {
		t.Fatalf("effective roles: %v", err)
	}
	got := assignedTo(t, roles, "table-mic")
	if len(got) != 2 {
		t.Fatalf("assigned = %v, want both concurrent assigns to land at distinct positions", got)
	}
	set := map[string]bool{}
	for _, n := range got {
		set[n] = true
	}
	if !set["race-a"] || !set["race-b"] {
		t.Fatalf("assigned = %v, want both race-a and race-b (a collision would have left one refused)", got)
	}
}

// TestUnassignLeavesGapThenRefills proves "next free position" is the lowest
// unused positive integer, not max(position)+1: a vacated slot survives as a
// gap (no auto-compaction, so position_labels[n-1] keeps naming the same
// slot for the occupants that did not move) until the next assignment fills
// it, rather than growing without bound.
func TestUnassignLeavesGapThenRefills(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	declareTableMic(t, ctx, gw, all, "gap-sys", 1)
	for _, name := range []string{"one", "two", "three"} {
		newBarInto(t, ctx, gw, all, name)
	}
	for _, name := range []string{"one", "two", "three"} {
		if err := gw.AssignRole(ctx, "", "gap-sys", "table-mic", name, all); err != nil {
			t.Fatalf("assign %s: %v", name, err)
		}
	}
	roles, err := gw.EffectiveRoles(ctx, "gap-sys", all)
	if err != nil {
		t.Fatalf("effective roles: %v", err)
	}
	if got := assignedTo(t, roles, "table-mic"); !sameSeq(got, []string{"one", "two", "three"}) {
		t.Fatalf("before unassign = %v, want [one two three]", got)
	}

	// two held position 2; unassigning it leaves a gap rather than shifting
	// three down to fill it.
	if err := gw.UnassignRole(ctx, "", "gap-sys", "table-mic", "two", all); err != nil {
		t.Fatalf("unassign two: %v", err)
	}
	roles, err = gw.EffectiveRoles(ctx, "gap-sys", all)
	if err != nil {
		t.Fatalf("effective roles after unassign: %v", err)
	}
	if got := assignedTo(t, roles, "table-mic"); !sameSeq(got, []string{"one", "three"}) {
		t.Fatalf("after unassign = %v, want [one three] (three keeps its own position, no compaction)", got)
	}

	// The next assignment refills the vacated position (2), landing between
	// one and three rather than appending after three at position 4.
	newBarInto(t, ctx, gw, all, "four")
	if err := gw.AssignRole(ctx, "", "gap-sys", "table-mic", "four", all); err != nil {
		t.Fatalf("assign four: %v", err)
	}
	roles, err = gw.EffectiveRoles(ctx, "gap-sys", all)
	if err != nil {
		t.Fatalf("effective roles after refill: %v", err)
	}
	if got := assignedTo(t, roles, "table-mic"); !sameSeq(got, []string{"one", "four", "three"}) {
		t.Fatalf("after refill = %v, want [one four three] (four took the vacated position 2)", got)
	}
}

// TestAssignRefusesAtCapacity proves AssignRole enforces capacity by an
// explicit count, not merely as a side effect of a position collision:
// filling a role to its declared capacity refuses the next assign with a
// message naming the role and the cap, not "that position is already
// taken". An existing occupant of a full role stays idempotent.
func TestAssignRefusesAtCapacity(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	typedSlotSystem(t, ctx, gw, all, "capacity-full-sys")
	cap2 := 2
	if _, err := gw.SetSystemRole(ctx, "", "system", "capacity-full-sys", storage.SystemRoleSpec{
		Name: "table-mic", DisplayName: "Table Mic", Quorum: 1, AcceptedTypes: []string{"video-bar"}, Capacity: &cap2,
	}); err != nil {
		t.Fatalf("declare role: %v", err)
	}
	newBarInto(t, ctx, gw, all, "one")
	newBarInto(t, ctx, gw, all, "two")
	newBarInto(t, ctx, gw, all, "three")
	if err := gw.AssignRole(ctx, "", "capacity-full-sys", "table-mic", "one", all); err != nil {
		t.Fatalf("assign one: %v", err)
	}
	if err := gw.AssignRole(ctx, "", "capacity-full-sys", "table-mic", "two", all); err != nil {
		t.Fatalf("assign two: %v", err)
	}

	err = gw.AssignRole(ctx, "", "capacity-full-sys", "table-mic", "three", all)
	var full *storage.CapacityFullShortfall
	if !errors.As(err, &full) {
		t.Fatalf("assign past capacity: err = %v, want CapacityFullShortfall", err)
	}
	if full.Role != "Table Mic" || full.System != "capacity-full-sys" || full.Have != 2 || full.Capacity != 2 {
		t.Fatalf("shortfall = %+v, want Table Mic in capacity-full-sys full at 2 of 2", full)
	}
	if !strings.Contains(err.Error(), "Table Mic") || !strings.Contains(err.Error(), "2") {
		t.Fatalf("shortfall message = %q, want it to name the role and the cap", err.Error())
	}

	// The role stays idempotent for its existing occupants even while full.
	if err := gw.AssignRole(ctx, "", "capacity-full-sys", "table-mic", "one", all); err != nil {
		t.Fatalf("re-assign an existing occupant of a full role: %v, want idempotent success", err)
	}

	roles, err := gw.EffectiveRoles(ctx, "capacity-full-sys", all)
	if err != nil {
		t.Fatalf("effective roles: %v", err)
	}
	if got := assignedTo(t, roles, "table-mic"); !sameSeq(got, []string{"one", "two"}) {
		t.Fatalf("assigned after the refused overfill = %v, want still [one two]", got)
	}
}

// TestLoweringCapacityThenAssigningIsRefused drives the exact sequence the
// review found unenforced: assign three, vacate the middle one (leaving a
// gap below the position an eventual cap would sit at), lower capacity to
// something the remaining row count already satisfies, then attempt one
// more assign. The lowering itself must succeed (two rows fit a cap of
// two: SetSystemRole's pre-check and AssignRole's now both count
// assignment rows for the role, the same count, since a component fills a
// system_role_assignment row at most once per role and every row carries a
// unique, NOT NULL position, so rows, occupants, and occupied positions are
// the same number here). The next assign must not silently land in the
// vacated gap and push the role to three occupants against a declared
// capacity of two.
func TestLoweringCapacityThenAssigningIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	declareTableMic(t, ctx, gw, all, "capacity-bypass-sys", 1) // no capacity yet
	for _, name := range []string{"one", "two", "three", "four"} {
		newBarInto(t, ctx, gw, all, name)
	}
	for _, name := range []string{"one", "two", "three"} {
		if err := gw.AssignRole(ctx, "", "capacity-bypass-sys", "table-mic", name, all); err != nil {
			t.Fatalf("assign %s: %v", name, err)
		}
	}
	// two held position 2; vacate it, leaving a gap below where the new cap
	// will sit.
	if err := gw.UnassignRole(ctx, "", "capacity-bypass-sys", "table-mic", "two", all); err != nil {
		t.Fatalf("unassign two: %v", err)
	}

	// Lowering to 2 succeeds: two rows (one, three) fit a cap of two.
	cap2 := 2
	if _, err := gw.SetSystemRole(ctx, "", "system", "capacity-bypass-sys", storage.SystemRoleSpec{
		Name: "table-mic", DisplayName: "Table Mic", Quorum: 1, AcceptedTypes: []string{"video-bar"}, Capacity: &cap2,
	}); err != nil {
		t.Fatalf("lower capacity to 2: %v, want success (2 rows fit a cap of 2)", err)
	}

	// The bypass: without an explicit capacity check, the next assign finds
	// the vacated position 2 free (it is within the new bound
	// least(capacity=2, count+1=3)=2) and would insert there, landing at 3
	// occupants against a declared capacity of 2 with no error.
	err = gw.AssignRole(ctx, "", "capacity-bypass-sys", "table-mic", "four", all)
	var full *storage.CapacityFullShortfall
	if !errors.As(err, &full) {
		t.Fatalf("assign a fourth component after lowering capacity to 2: err = %v, want CapacityFullShortfall", err)
	}
	if full.Have != 2 || full.Capacity != 2 {
		t.Fatalf("shortfall = %+v, want 2 of 2", full)
	}

	roles, err := gw.EffectiveRoles(ctx, "capacity-bypass-sys", all)
	if err != nil {
		t.Fatalf("effective roles: %v", err)
	}
	if got := assignedTo(t, roles, "table-mic"); !sameSeq(got, []string{"one", "three"}) {
		t.Fatalf("assigned after the refused overfill = %v, want still [one three]", got)
	}
}
