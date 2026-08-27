package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/updatemask"
)

// The write mask at the storage layer (#666): SetSystemRole writes the fields
// spec.Write names, and when the caller names none (every caller that predates
// the mask), the fields the spec itself populated. The declaration used to
// wholesale-replace most of the row, which is what took impact and
// position_labels with it on an unrelated edit (#639) and left capacity with no
// clear path at all (#638).

func roleMaskGateway(t *testing.T) (*storage.PG, context.Context, scope.Set) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return gw, ctx, scope.Set{All: true}
}

// TestUnrelatedEditLeavesTheRestOfTheDeclarationAlone is #639, both halves at
// once: an edit that carries only a label must not reset the impact to
// degraded (which moves the system's verdict rollup with no visible field and
// no warning) and must not drop the position labels. capacity and the typed
// slot go with them, so the whole declaration now reads one way instead of
// field by field.
func TestUnrelatedEditLeavesTheRestOfTheDeclarationAlone(t *testing.T) {
	gw, ctx, all := roleMaskGateway(t)
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "mask-preserve-sys"}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}

	pinned := storagetest.MintProduct(t, ctx, gw, "display").Name
	cap3 := 3
	if _, err := gw.SetSystemRole(ctx, "", "system", "mask-preserve-sys", storage.SystemRoleSpec{
		Name: "main-display", Label: "Main Display", Quorum: 2, Impact: "outage",
		Capacity: &cap3, PositionLabels: []string{"left", "right"},
		AcceptedTypes: []string{"display"}, PinnedProducts: []string{pinned},
	}); err != nil {
		t.Fatalf("declare: %v", err)
	}

	r, err := gw.SetSystemRole(ctx, "", "system", "mask-preserve-sys", storage.SystemRoleSpec{
		Name: "main-display", Label: "Main Display (renamed)",
	})
	if err != nil {
		t.Fatalf("edit label only: %v", err)
	}
	if r.Label != "Main Display (renamed)" {
		t.Fatalf("label = %q, want the edit to take", r.Label)
	}
	if r.Impact != "outage" {
		t.Fatalf("impact after an edit that never mentions it = %q, want outage: an omitted impact "+
			"falling back to degraded silently downgrades the system's verdict (#639)", r.Impact)
	}
	if len(r.PositionLabels) != 2 || r.PositionLabels[0] != "left" {
		t.Fatalf("position_labels after an unrelated edit = %v, want [left right] (#639)", r.PositionLabels)
	}
	if r.Capacity == nil || *r.Capacity != 3 {
		t.Fatalf("capacity after an unrelated edit = %v, want it to survive at 3", r.Capacity)
	}
	if len(r.AcceptedTypes) != 1 || r.AcceptedTypes[0] != "display" {
		t.Fatalf("accepted_types after an unrelated edit = %v, want [display]: an empty set is not "+
			"a populated field, so it means unchanged, not cleared", r.AcceptedTypes)
	}
	if len(r.PinnedProducts) != 1 || r.PinnedProducts[0] != pinned {
		t.Fatalf("pinned_products after an unrelated edit = %v, want [%s]", r.PinnedProducts, pinned)
	}
	if quorum := r.Quorum; quorum != 2 {
		t.Fatalf("quorum after an unrelated edit = %d, want 2", quorum)
	}
}

// TestExplicitMaskClearsCapacity is #638: a capacity has no empty-string
// sentinel to clear it with, so before the mask an operator who set one could
// only ever narrow it, and raw SQL was the only way back to unbounded.
func TestExplicitMaskClearsCapacity(t *testing.T) {
	gw, ctx, all := roleMaskGateway(t)
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "mask-clear-sys"}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}

	cap3 := 3
	if _, err := gw.SetSystemRole(ctx, "", "system", "mask-clear-sys", storage.SystemRoleSpec{
		Name: "main-display", Label: "Main Display", Impact: "outage", Capacity: &cap3,
	}); err != nil {
		t.Fatalf("declare with capacity: %v", err)
	}

	r, err := gw.SetSystemRole(ctx, "", "system", "mask-clear-sys", storage.SystemRoleSpec{
		Name:  "main-display",
		Write: updatemask.Of(storage.RoleFieldCapacity),
	})
	if err != nil {
		t.Fatalf("clear capacity through the mask: %v", err)
	}
	if r.Capacity != nil {
		t.Fatalf("capacity = %v, want it cleared back to unbounded: a masked field carrying its "+
			"zero value clears (#638)", *r.Capacity)
	}
	if r.Impact != "outage" || r.Label != "Main Display" {
		t.Fatalf("role after a capacity-only mask = %+v, want every other field untouched", r)
	}
}

// TestExplicitMaskClearsTheTypedSlotAndLabels covers the other side of the doc
// change the mask forces: an empty list is not a populated field, so clearing
// one now means naming it in the mask rather than sending [].
func TestExplicitMaskClearsTheTypedSlotAndLabels(t *testing.T) {
	gw, ctx, all := roleMaskGateway(t)
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "mask-clear-list-sys"}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	pinned := storagetest.MintProduct(t, ctx, gw, "display").Name
	if _, err := gw.SetSystemRole(ctx, "", "system", "mask-clear-list-sys", storage.SystemRoleSpec{
		Name: "main-display", Label: "Main Display",
		PositionLabels: []string{"left"}, AcceptedTypes: []string{"display"},
		PinnedProducts: []string{pinned},
	}); err != nil {
		t.Fatalf("declare: %v", err)
	}

	r, err := gw.SetSystemRole(ctx, "", "system", "mask-clear-list-sys", storage.SystemRoleSpec{
		Name: "main-display",
		Write: updatemask.Of(storage.RoleFieldPositionLabels, storage.RoleFieldAcceptedTypes,
			storage.RoleFieldPinnedProducts),
	})
	if err != nil {
		t.Fatalf("clear the lists through the mask: %v", err)
	}
	if len(r.PositionLabels) != 0 || len(r.AcceptedTypes) != 0 || len(r.PinnedProducts) != 0 {
		t.Fatalf("role after a list-clearing mask = %+v, want all three lists empty", r)
	}
	if r.Label != "Main Display" {
		t.Fatalf("label = %q, want it untouched by a mask that does not name it", r.Label)
	}
}

// TestStarMaskReplacesTheWholeDeclaration proves "*" is the equivalent of the
// PUT this route used to be: every field the body omits goes back to its
// default, which is exactly the wholesale replace the old verb did on every
// write whether the caller meant it or not.
func TestStarMaskReplacesTheWholeDeclaration(t *testing.T) {
	gw, ctx, all := roleMaskGateway(t)
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "mask-star-sys"}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	cap3 := 3
	if _, err := gw.SetSystemRole(ctx, "", "system", "mask-star-sys", storage.SystemRoleSpec{
		Name: "main-display", Label: "Main Display", Quorum: 2, Impact: "outage", Capacity: &cap3,
		PositionLabels: []string{"left", "right"}, AcceptedTypes: []string{"display"},
	}); err != nil {
		t.Fatalf("declare: %v", err)
	}

	full, err := updatemask.Resolve([]string{updatemask.Star}, storage.RolePatchFields, nil)
	if err != nil {
		t.Fatalf("resolve star: %v", err)
	}
	r, err := gw.SetSystemRole(ctx, "", "system", "mask-star-sys", storage.SystemRoleSpec{
		Name: "main-display", Write: full,
	})
	if err != nil {
		t.Fatalf("full replacement: %v", err)
	}
	if r.Impact != "degraded" || r.Quorum != 1 || r.Capacity != nil ||
		len(r.PositionLabels) != 0 || len(r.AcceptedTypes) != 0 {
		t.Fatalf("role after a full replacement = %+v, want every omitted field back to its default", r)
	}
	if r.Label != "main-display" {
		t.Fatalf("label after a full replacement = %q, want the role name (its default)", r.Label)
	}
}

// TestMaskedCreateTakesDefaultsForWhatItDoesNotName pins the create half: with
// nothing to preserve, a field outside the mask lands on its default rather
// than on whatever the body happened to carry.
func TestMaskedCreateTakesDefaultsForWhatItDoesNotName(t *testing.T) {
	gw, ctx, all := roleMaskGateway(t)
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "mask-create-sys"}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	cap5 := 5
	r, err := gw.SetSystemRole(ctx, "", "system", "mask-create-sys", storage.SystemRoleSpec{
		Name: "main-display", Label: "Main Display", Quorum: 4, Impact: "outage", Capacity: &cap5,
		Write: updatemask.Of(storage.RoleFieldLabel),
	})
	if err != nil {
		t.Fatalf("masked create: %v", err)
	}
	if r.Label != "Main Display" {
		t.Fatalf("label = %q, want the one field the mask named", r.Label)
	}
	if r.Quorum != 1 || r.Impact != "degraded" || r.Capacity != nil {
		t.Fatalf("masked create = %+v, want the unnamed fields on their defaults, not on the body's values", r)
	}
}

// TestAnEmptyMaskWritesNothing is the present-but-empty reading: the caller
// named no fields, so nothing moves, however full the rest of the body is.
func TestAnEmptyMaskWritesNothing(t *testing.T) {
	gw, ctx, all := roleMaskGateway(t)
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "mask-empty-sys"}, all, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	if _, err := gw.SetSystemRole(ctx, "", "system", "mask-empty-sys", storage.SystemRoleSpec{
		Name: "main-display", Label: "Main Display", Quorum: 2, Impact: "outage",
	}); err != nil {
		t.Fatalf("declare: %v", err)
	}
	empty, err := updatemask.Resolve([]string{}, storage.RolePatchFields, nil)
	if err != nil {
		t.Fatalf("resolve an empty mask: %v", err)
	}
	r, err := gw.SetSystemRole(ctx, "", "system", "mask-empty-sys", storage.SystemRoleSpec{
		Name: "main-display", Label: "Ignored", Quorum: 9, Impact: "none", Write: empty,
	})
	if err != nil {
		t.Fatalf("write with an empty mask: %v", err)
	}
	if r.Label != "Main Display" || r.Quorum != 2 || r.Impact != "outage" {
		t.Fatalf("role after an empty mask = %+v, want it exactly as declared", r)
	}
}
