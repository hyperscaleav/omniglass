package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestEffectiveSlotsAndAssignment proves slots resolve from both arcs (inherited
// from the standard, ad-hoc on the system), that staffing is visible, and that the
// typed-slot guard refuses a component whose product's component_type is not
// within an accepted type's subtree, naming both what the component is and what
// the slot wants.
func TestEffectiveSlotsAndAssignment(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// The test owns its own standard rather than piggybacking on a seeded one, so
	// what the boot seed happens to declare cannot change what this asserts. It
	// wants a table mic (a video-bar, quorum 2); the system itself also
	// declares an ad-hoc display slot.
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: "test-huddle", Label: "Test Huddle"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	std := "test-huddle"
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "hq-huddle", StandardID: &std}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	var micSlot string
	if err := conn.QueryRow(ctx, `
		insert into system_slot (owner_kind, standard_id, name, label, quorum)
		values ('standard',(select id from standard where name = 'test-huddle'),'table-mic','Table microphone',2)
		returning id`).Scan(&micSlot); err != nil {
		t.Fatalf("declare standard slot: %v", err)
	}
	// Scoped to the slot id this test just created: matching on name alone would
	// reach across owners and constrain what any other standard may declare.
	if _, err := conn.Exec(ctx, `
		insert into system_slot_type (slot_id, component_type_id)
		select $1, id from component_type where name = 'video-bar'`, micSlot); err != nil {
		t.Fatalf("require accepted type: %v", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into system_slot (owner_kind, system_id, name, label)
		select 'system', id, 'wall-display', 'Wall display' from system where name = 'hq-huddle'`); err != nil {
		t.Fatalf("declare ad-hoc slot: %v", err)
	}

	slots, err := gw.EffectiveSlots(ctx, "hq-huddle", all)
	if err != nil {
		t.Fatalf("effective slots: %v", err)
	}
	if len(slots) != 2 {
		t.Fatalf("slots = %d, want the inherited one plus the ad-hoc one: %+v", len(slots), slots)
	}
	bySlot := map[string]storage.EffectiveSlot{}
	for _, r := range slots {
		bySlot[r.Name] = r
	}
	if mic := bySlot["table-mic"]; !mic.FromStandard || mic.Quorum != 2 || mic.Understaffed() != 2 {
		t.Fatalf("table-mic = %+v, want inherited, quorum 2, understaffed 2", mic)
	}
	if disp := bySlot["wall-display"]; disp.FromStandard {
		t.Fatalf("wall-display should be ad-hoc, got FromStandard=true")
	}

	// A minted video-bar product fills the table-mic slot.
	bar := storagetest.MintProduct(t, ctx, gw, "video-bar").Name
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1", ProductName: &bar}, all, all, all, all); err != nil {
		t.Fatalf("create bar: %v", err)
	}
	if err := gw.AssignSlot(ctx, "", "hq-huddle", "table-mic", "bar-1", all, all, all, all); err != nil {
		t.Fatalf("assign a satisfying component: %v", err)
	}
	slots, _ = gw.EffectiveSlots(ctx, "hq-huddle", all)
	for _, r := range slots {
		if r.Name == "table-mic" {
			if r.Assigned() != 1 || r.Understaffed() != 1 {
				t.Fatalf("after one assignment: %+v, want 1 assigned and still 1 short of quorum", r)
			}
		}
	}
	// Idempotent: assigning the same component again does not duplicate.
	if err := gw.AssignSlot(ctx, "", "hq-huddle", "table-mic", "bar-1", all, all, all, all); err != nil {
		t.Fatalf("re-assign: %v", err)
	}
	slots, _ = gw.EffectiveSlots(ctx, "hq-huddle", all)
	for _, r := range slots {
		if r.Name == "table-mic" && r.Assigned() != 1 {
			t.Fatalf("re-assign duplicated: %d assigned", r.Assigned())
		}
	}

	// A display is not within the video-bar subtree: refused, and the refusal
	// names both what the component is and what the slot wants.
	qm := storagetest.MintProduct(t, ctx, gw, "display").Name
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1", ProductName: &qm}, all, all, all, all); err != nil {
		t.Fatalf("create panel: %v", err)
	}
	err = gw.AssignSlot(ctx, "", "hq-huddle", "table-mic", "panel-1", all, all, all, all)
	var short *storage.TypeShortfall
	if !errors.As(err, &short) {
		t.Fatalf("assign a non-satisfying component: err = %v, want TypeShortfall", err)
	}
	if short.Component != "panel-1" || short.ComponentType != "display" || short.Slot != "Table microphone" || !hasAll(short.WantTypes, "video-bar") {
		t.Fatalf("shortfall = %+v, want panel-1/display refused against slot Table microphone wanting video-bar", short)
	}

	// A second video-bar reaches quorum.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-2", ProductName: &bar}, all, all, all, all); err != nil {
		t.Fatalf("create second bar: %v", err)
	}
	if err := gw.AssignSlot(ctx, "", "hq-huddle", "table-mic", "bar-2", all, all, all, all); err != nil {
		t.Fatalf("assign a second satisfying component: %v", err)
	}
	slots, _ = gw.EffectiveSlots(ctx, "hq-huddle", all)
	for _, r := range slots {
		if r.Name == "table-mic" && (r.Assigned() != 2 || r.Understaffed() != 0) {
			t.Fatalf("after staffing to quorum: %+v, want 2 assigned and fully staffed", r)
		}
	}

	// Unassign removes it; unassigning again is an explicit miss.
	if err := gw.UnassignSlot(ctx, "", "hq-huddle", "table-mic", "bar-2", all, all, all, all); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if err := gw.UnassignSlot(ctx, "", "hq-huddle", "table-mic", "bar-2", all, all, all, all); !errors.Is(err, storage.ErrAssignmentMissing) {
		t.Fatalf("unassign twice: err = %v, want ErrAssignmentMissing", err)
	}

	// A component staffing a slot cannot be deleted out from under the system. The
	// refusal is a conflict (409), not an opaque server error: the restrict FK fires
	// from outside the structural child check, so without mapping it this is a 500.
	err = gw.DeleteComponent(ctx, "", "bar-1", all, all)
	if !errors.Is(err, storage.ErrReferenced) {
		t.Fatalf("delete a component staffing a slot: err = %v, want ErrReferenced", err)
	}
	// It must not blame child components. This component has none, so an operator
	// told that goes looking for something that is not there. The delete path cannot
	// tell which reference stopped it, so it says that it is referenced and no more.
	if strings.Contains(err.Error(), "child") {
		t.Fatalf("refusal blames children it cannot know about: %q", err)
	}
	// Unassigned, it deletes cleanly.
	if err := gw.UnassignSlot(ctx, "", "hq-huddle", "table-mic", "bar-1", all, all, all, all); err != nil {
		t.Fatalf("unassign before delete: %v", err)
	}
	if err := gw.DeleteComponent(ctx, "", "bar-1", all, all); err != nil {
		t.Fatalf("delete an unassigned component: %v", err)
	}

	// A component that does not exist is a not-found, NOT a type shortfall. An
	// absent component has no product to classify, so without an existence
	// check the operator gets a confusing type refusal for what is really a
	// typo.
	if err := gw.AssignSlot(ctx, "", "hq-huddle", "table-mic", "no-such-component", all, all, all, all); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Fatalf("assign an unknown component: err = %v, want ErrComponentNotFound", err)
	}

	// An unknown slot on a real system is a clear not-found, not a silent no-op.
	if err := gw.AssignSlot(ctx, "", "hq-huddle", "no-such-slot", "bar-1", all, all, all, all); !errors.Is(err, storage.ErrSlotNotFound) {
		t.Fatalf("assign to unknown slot: err = %v, want ErrSlotNotFound", err)
	}

	// A one-off system sees only what it declares itself.
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "one-off"}, all, all); err != nil {
		t.Fatalf("create one-off: %v", err)
	}
	if got, _ := gw.EffectiveSlots(ctx, "one-off", all); len(got) != 0 {
		t.Fatalf("one-off system slots = %+v, want none (it conforms to no standard)", got)
	}
}

// typedSlotSystem creates a fresh standalone system for a typed-slot guard
// test, isolated from whatever the boot seed happens to declare.
func typedSlotSystem(t *testing.T, ctx context.Context, gw storage.Gateway, all scope.Set, name string) {
	t.Helper()
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: name}, all, all); err != nil {
		t.Fatalf("create system %s: %v", name, err)
	}
}

// TestAssignRefusesWrongType proves the #626 guard's headline refusal: a
// component classified outside every type a slot accepts is refused with a
// 422-shaped error naming both parties in operator vocabulary, exactly the
// touch-panel-into-a-display-slot example from the task brief.
func TestAssignRefusesWrongType(t *testing.T) {
	t.Parallel()
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
	typedSlotSystem(t, ctx, gw, all, "wrong-type-sys")

	if _, err := gw.SetSystemSlot(ctx, "", "system", "wrong-type-sys", storage.SystemSlotSpec{
		Name: "display-left", Label: "Display (Left)", AcceptedTypes: []string{"display"},
	}); err != nil {
		t.Fatalf("declare slot: %v", err)
	}

	touch := storagetest.MintProduct(t, ctx, gw, "touch-panel").Name
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-1", ProductName: &touch}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	err = gw.AssignSlot(ctx, "", "wrong-type-sys", "display-left", "panel-1", all, all, all, all)
	var short *storage.TypeShortfall
	if !errors.As(err, &short) {
		t.Fatalf("assign a wrong-type component: err = %v, want TypeShortfall", err)
	}
	if short.Component != "panel-1" || short.ComponentType != "touch-panel" ||
		short.Slot != "Display (Left)" || !hasAll(short.WantTypes, "display") {
		t.Fatalf("shortfall = %+v, want panel-1/touch-panel refused against slot Display (Left) wanting display", short)
	}
}

// TestAssignAcceptsSubtype proves the guard walks the component_type subtree:
// a slot that accepts "display" also accepts "interactive-display", one of
// its declared children, without the slot naming the subtype itself.
func TestAssignAcceptsSubtype(t *testing.T) {
	t.Parallel()
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
	typedSlotSystem(t, ctx, gw, all, "subtype-sys")

	if _, err := gw.SetSystemSlot(ctx, "", "system", "subtype-sys", storage.SystemSlotSpec{
		Name: "display-left", Label: "Display (Left)", AcceptedTypes: []string{"display"},
	}); err != nil {
		t.Fatalf("declare slot: %v", err)
	}

	if _, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "test-interactive-display", Label: "Test Interactive Display",
		ComponentType: "interactive-display", Kind: "device",
	}); err != nil {
		t.Fatalf("create subtype product: %v", err)
	}
	sub := "test-interactive-display"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-2", ProductName: &sub}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	if err := gw.AssignSlot(ctx, "", "subtype-sys", "display-left", "panel-2", all, all, all, all); err != nil {
		t.Fatalf("assign a subtype component: %v, want success (interactive-display is within display's subtree)", err)
	}
}

// TestAssignProductPin proves the second half of the guard: when a slot pins
// specific products, a component of an otherwise-accepted type but a
// different product is refused naming the pinned products, and the pinned
// product itself is accepted.
func TestAssignProductPin(t *testing.T) {
	t.Parallel()
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
	typedSlotSystem(t, ctx, gw, all, "pin-sys")

	pinned := storagetest.MintProduct(t, ctx, gw, "display").Name
	if _, err := gw.SetSystemSlot(ctx, "", "system", "pin-sys", storage.SystemSlotSpec{
		Name: "main-display", Label: "Main Display",
		AcceptedTypes: []string{"display"}, PinnedProducts: []string{pinned},
	}); err != nil {
		t.Fatalf("declare slot: %v", err)
	}

	if _, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "other-display", Label: "Other Display", ComponentType: "display", Kind: "device",
	}); err != nil {
		t.Fatalf("create other display product: %v", err)
	}
	other := "other-display"
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-3", ProductName: &other}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	err = gw.AssignSlot(ctx, "", "pin-sys", "main-display", "panel-3", all, all, all, all)
	var pinShort *storage.ProductPinShortfall
	if !errors.As(err, &pinShort) {
		t.Fatalf("assign right type, wrong product: err = %v, want ProductPinShortfall", err)
	}
	if pinShort.Component != "panel-3" || pinShort.ComponentProduct != "other-display" ||
		pinShort.Slot != "Main Display" || !hasAll(pinShort.WantProducts, pinned) {
		t.Fatalf("shortfall = %+v, want panel-3/other-display refused against slot Main Display wanting the pinned product", pinShort)
	}

	qm := pinned
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "qm-2", ProductName: &qm}, all, all, all, all); err != nil {
		t.Fatalf("create pinned-product component: %v", err)
	}
	if err := gw.AssignSlot(ctx, "", "pin-sys", "main-display", "qm-2", all, all, all, all); err != nil {
		t.Fatalf("assign the pinned product: %v, want success", err)
	}
}

// TestSecondSlotSameComponentRefused proves the #626 one-slot-per-component
// rule: a component already filling one slot in a system is refused when
// assigned to a second, and the refusal names both the component and the
// slot it already holds, not just the status.
func TestSecondSlotSameComponentRefused(t *testing.T) {
	t.Parallel()
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
	typedSlotSystem(t, ctx, gw, all, "double-staff-sys")

	if _, err := gw.SetSystemSlot(ctx, "", "system", "double-staff-sys", storage.SystemSlotSpec{
		Name: "main-display", Label: "Main Display",
	}); err != nil {
		t.Fatalf("declare main-display: %v", err)
	}
	if _, err := gw.SetSystemSlot(ctx, "", "system", "double-staff-sys", storage.SystemSlotSpec{
		Name: "confidence-monitor", Label: "Confidence Monitor",
	}); err != nil {
		t.Fatalf("declare confidence-monitor: %v", err)
	}

	bar := storagetest.MintProduct(t, ctx, gw, "").Name
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "bar-1", ProductName: &bar}, all, all, all, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if err := gw.AssignSlot(ctx, "", "double-staff-sys", "main-display", "bar-1", all, all, all, all); err != nil {
		t.Fatalf("assign to first slot: %v", err)
	}

	err = gw.AssignSlot(ctx, "", "double-staff-sys", "confidence-monitor", "bar-1", all, all, all, all)
	var staffed *storage.ComponentStaffedShortfall
	if !errors.As(err, &staffed) {
		t.Fatalf("assign a second slot to the same component: err = %v, want ComponentStaffedShortfall", err)
	}
	if staffed.Component != "bar-1" || staffed.HeldSlot != "Main Display" || staffed.System != "double-staff-sys" {
		t.Fatalf("shortfall = %+v, want bar-1 refused because it already fills Main Display in double-staff-sys", staffed)
	}
	if !strings.Contains(err.Error(), "bar-1") || !strings.Contains(err.Error(), "Main Display") {
		t.Fatalf("shortfall message = %q, want it to name both bar-1 and Main Display", err.Error())
	}

	// Re-assigning to the SAME slot it already holds stays idempotent: the
	// exclusion is slot_id <> the target slot, not "already staffed at all".
	if err := gw.AssignSlot(ctx, "", "double-staff-sys", "main-display", "bar-1", all, all, all, all); err != nil {
		t.Fatalf("re-assign to the slot it already holds: %v, want idempotent success", err)
	}
}

// TestCapacitySurvivesUnrelatedEdit proves capacity reaches all six write
// sites (#626): a declared capacity must not be silently cleared by a later
// edit that only touches an unrelated field, the same live defect impact
// carries today (Task 9 fixes that one; this test guards capacity from
// inheriting it).
func TestCapacitySurvivesUnrelatedEdit(t *testing.T) {
	t.Parallel()
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
	typedSlotSystem(t, ctx, gw, all, "capacity-persist-sys")

	cap3 := 3
	if _, err := gw.SetSystemSlot(ctx, "", "system", "capacity-persist-sys", storage.SystemSlotSpec{
		Name: "main-display", Label: "Main Display", Capacity: &cap3,
	}); err != nil {
		t.Fatalf("declare with capacity: %v", err)
	}

	r, err := gw.SetSystemSlot(ctx, "", "system", "capacity-persist-sys", storage.SystemSlotSpec{
		Name: "main-display", Label: "Main Display (renamed)",
	})
	if err != nil {
		t.Fatalf("edit label only: %v", err)
	}
	if r.Capacity == nil || *r.Capacity != 3 {
		t.Fatalf("capacity after an unrelated edit = %v, want it to survive at 3", r.Capacity)
	}
	if r.Label != "Main Display (renamed)" {
		t.Fatalf("label = %q, want the edit to take", r.Label)
	}
}

// TestCapacityBelowQuorumRefused proves the declaration-alone refusal (422):
// a capacity below the slot's own quorum can never be satisfied by any
// staffing, so it is refused at declare time rather than left to confuse an
// operator staffing to quorum later.
func TestCapacityBelowQuorumRefused(t *testing.T) {
	t.Parallel()
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
	typedSlotSystem(t, ctx, gw, all, "capacity-quorum-sys")

	badCap := 1
	_, err = gw.SetSystemSlot(ctx, "", "system", "capacity-quorum-sys", storage.SystemSlotSpec{
		Name: "main-display", Label: "Main Display", Quorum: 2, Capacity: &badCap,
	})
	if !errors.Is(err, storage.ErrCapacityBelowQuorum) {
		t.Fatalf("declare capacity below quorum: err = %v, want ErrCapacityBelowQuorum", err)
	}
}

// TestLoweringCapacityBelowCountRefusedAcrossInheritingSystems proves the
// other-rows refusal (409): a standard-owned slot is staffed independently
// in every conforming system, so lowering its capacity must be refused if
// ANY conforming system exceeds the new cap, even when the system that
// triggered the edit would itself be fine.
func TestLoweringCapacityBelowCountRefusedAcrossInheritingSystems(t *testing.T) {
	t.Parallel()
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

	std := "pair-cap-standard"
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: std, Label: "Pair Cap"}); err != nil {
		t.Fatalf("create standard: %v", err)
	}
	if _, err := gw.SetSystemSlot(ctx, "", "standard", std, storage.SystemSlotSpec{
		Name: "table-mic", Label: "Table Mic", AcceptedTypes: []string{"video-bar"},
	}); err != nil {
		t.Fatalf("declare standard slot: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "cap-sys-a", StandardID: &std}, all, all); err != nil {
		t.Fatalf("create system a: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "cap-sys-b", StandardID: &std}, all, all); err != nil {
		t.Fatalf("create system b: %v", err)
	}

	bar := storagetest.MintProduct(t, ctx, gw, "video-bar").Name
	newBar := func(name string) {
		t.Helper()
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name, ProductName: &bar}, all, all, all, all); err != nil {
			t.Fatalf("create component %s: %v", name, err)
		}
	}

	// System A stays under any cap we will try; system B does not.
	newBar("a-bar-1")
	if err := gw.AssignSlot(ctx, "", "cap-sys-a", "table-mic", "a-bar-1", all, all, all, all); err != nil {
		t.Fatalf("assign a-bar-1: %v", err)
	}
	for _, name := range []string{"b-bar-1", "b-bar-2", "b-bar-3"} {
		newBar(name)
		if err := gw.AssignSlot(ctx, "", "cap-sys-b", "table-mic", name, all, all, all, all); err != nil {
			t.Fatalf("assign %s: %v", name, err)
		}
	}

	cap2 := 2
	_, err = gw.SetSystemSlot(ctx, "", "standard", std, storage.SystemSlotSpec{
		Name: "table-mic", Label: "Table Mic", AcceptedTypes: []string{"video-bar"}, Capacity: &cap2,
	})
	var short *storage.CapacityShortfall
	if !errors.As(err, &short) {
		t.Fatalf("lower capacity below the assigned count in one conforming system: err = %v, want CapacityShortfall", err)
	}
	if short.System != "cap-sys-b" || short.Have != 3 || short.Want != 2 {
		t.Fatalf("shortfall = %+v, want cap-sys-b with 3 exceeding a new cap of 2", short)
	}
	// The refusal carries every exceeding system with its id, so the API can
	// name only one the caller may read (#1216).
	if len(short.Exceeding) != 1 || short.Exceeding[0].System != "cap-sys-b" || short.Exceeding[0].SystemID == "" {
		t.Fatalf("exceeding = %+v, want only cap-sys-b, with its id", short.Exceeding)
	}

	// At a cap of zero both exceed, worst first.
	cap0 := 0
	_, err = gw.SetSystemSlot(ctx, "", "standard", std, storage.SystemSlotSpec{
		Name: "table-mic", Label: "Table Mic", AcceptedTypes: []string{"video-bar"}, Capacity: &cap0,
	})
	if !errors.As(err, &short) {
		t.Fatalf("lower capacity to zero: err = %v, want CapacityShortfall", err)
	}
	if len(short.Exceeding) != 2 || short.Exceeding[0].System != "cap-sys-b" || short.Exceeding[1].System != "cap-sys-a" {
		t.Fatalf("exceeding = %+v, want cap-sys-b then cap-sys-a", short.Exceeding)
	}
}

func hasAll(got []string, want ...string) bool {
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
