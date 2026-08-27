package storage_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A platform-owned component name is minted from exactly two facts: the stem
// its product resolves to through the component_type chain, and the placement
// bucket its siblings sit in (ADR-0101's derivation, one tier down). So the
// acts that must re-mint one are the acts that CHANGE one of those two, and an
// act that changes neither has to leave the name where it is.
//
// Re-minting anyway is not a harmless no-op, which is what makes both of these
// defects rather than tidiness: allocation is lowest-free, so an ordinal freed
// by an earlier :rename means a re-mint MOVES the name (display-2 becomes
// display-1) with no rename requested and possibly no component:rename grant
// held (#696 on :move, #691 on the reclassify).
//
// Both directions are pinned here, because a guard that suppresses too much is
// the same defect from the other side: a real bucket change and a real
// reclassify still re-mint, and the label follows the name wherever it
// legitimately moves.

const ceilingMic = "lyra-arc-a2"

// TestAMoveThatRestatesItsPlacementKeepsTheName is #696's reproduction: a
// pre-filled move form sends the current placement back, the move API requires
// at least one field, so re-stating the current location is the ordinary path.
func TestAMoveThatRestatesItsPlacementKeepsTheName(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoom(t, gw, ctx, "restated-room")

	first, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(edge55), LocationName: &room}, all, all, all, all)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(edge55), LocationName: &room}, all, all, all, all)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if first.Name != "display-1" || second.Name != "display-2" {
		t.Fatalf("precondition names = %q, %q, want display-1, display-2", first.Name, second.Name)
	}
	if second.Label != "Display 2" {
		t.Fatalf("precondition label = %q, want %q", second.Label, "Display 2")
	}
	// An operator claims the first one's name, which frees ordinal 1 in this
	// bucket. Nothing about the second component changed.
	if _, err := gw.RenameComponent(ctx, "", first.ID, "house-left", all, all); err != nil {
		t.Fatalf("rename the first: %v", err)
	}

	moved, err := gw.MoveComponent(ctx, "", second.ID, storage.ComponentMove{LocationName: &room}, all, all, all)
	if err != nil {
		t.Fatalf("move restating the current location: %v", err)
	}
	if moved.Name != "display-2" {
		t.Fatalf("name after a :move that restated the current location = %q, want unchanged display-2 (no rename was requested, and component:move is not component:rename)", moved.Name)
	}
	if moved.Ordinal == nil || *moved.Ordinal != 2 {
		t.Fatalf("ordinal after the restated :move = %v, want unchanged 2", ordstr(moved.Ordinal))
	}
	if moved.Label != "Display 2" {
		t.Fatalf("label after the restated :move = %q, want unchanged %q (the label reads the ordinal, so it moves exactly when the name does)", moved.Label, "Display 2")
	}
}

// TestARelocateInsideOneParentBucketKeepsTheName is the case a comparison of
// the two placement POINTERS gets wrong: a parent wins over a location, so a
// parented component that relocates has changed a pointer and not the bucket.
func TestARelocateInsideOneParentBucketKeepsTheName(t *testing.T) {
	gw, ctx := seededGateway(t)
	roomA := makeRoom(t, gw, ctx, "parented-room-a")
	roomB := makeRoom(t, gw, ctx, "parented-room-b")
	rack := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "parented-rack"}, all)

	first, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(edge55), ParentName: &rack.Name, LocationName: &roomA}, all, all, all, all)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(edge55), ParentName: &rack.Name, LocationName: &roomA}, all, all, all, all)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if first.Name != "display-1" || second.Name != "display-2" {
		t.Fatalf("precondition names = %q, %q, want display-1, display-2 (both in the rack's bucket)", first.Name, second.Name)
	}
	if _, err := gw.RenameComponent(ctx, "", first.ID, "rack-house-left", all, all); err != nil {
		t.Fatalf("rename the first: %v", err)
	}

	moved, err := gw.MoveComponent(ctx, "", second.ID, storage.ComponentMove{LocationName: &roomB}, all, all, all)
	if err != nil {
		t.Fatalf("relocate the parented component: %v", err)
	}
	dest := mustGetLocation(t, gw, ctx, roomB)
	if moved.LocationID == nil || *moved.LocationID != dest.ID {
		t.Fatalf("location after the relocate = %v, want %q (the relocate itself must still happen)", moved.LocationID, dest.ID)
	}
	if moved.Name != "display-2" {
		t.Fatalf("name after relocating a PARENTED component = %q, want unchanged display-2 (its bucket is the rack, which did not move)", moved.Name)
	}
	if moved.Ordinal == nil || *moved.Ordinal != 2 {
		t.Fatalf("ordinal after relocating a parented component = %v, want unchanged 2", ordstr(moved.Ordinal))
	}
}

// TestAMoveBetweenBucketsStillReMints is the guard-rail on the other side: a
// move that really does change the bucket keeps re-minting, in both the
// location-to-location and the clear-to-unplaced directions.
func TestAMoveBetweenBucketsStillReMints(t *testing.T) {
	gw, ctx := seededGateway(t)
	roomA := makeRoom(t, gw, ctx, "moving-room-a")
	roomB := makeRoom(t, gw, ctx, "moving-room-b")

	// display-1 is already taken in the destination, so a re-mint is visible
	// rather than landing back on the name it started with.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(edge55), LocationName: &roomB}, all, all, all, all); err != nil {
		t.Fatalf("occupy the destination bucket: %v", err)
	}
	mover, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(edge55), LocationName: &roomA}, all, all, all, all)
	if err != nil {
		t.Fatalf("create the mover: %v", err)
	}
	if mover.Name != "display-1" {
		t.Fatalf("precondition mover name = %q, want display-1", mover.Name)
	}

	moved, err := gw.MoveComponent(ctx, "", mover.ID, storage.ComponentMove{LocationName: &roomB}, all, all, all)
	if err != nil {
		t.Fatalf("relocate: %v", err)
	}
	if moved.Name != "display-2" {
		t.Fatalf("name after a real relocate = %q, want display-2 (the destination's display-1 is taken)", moved.Name)
	}
	if moved.Ordinal == nil || *moved.Ordinal != 2 {
		t.Fatalf("ordinal after a real relocate = %v, want 2", ordstr(moved.Ordinal))
	}
	if moved.Label != "Display 2" {
		t.Fatalf("label after a real relocate = %q, want %q (the label follows the name where the name legitimately moves)", moved.Label, "Display 2")
	}

	// Unplaced is a bucket like any other: clearing the location changes it.
	orphaned, err := gw.MoveComponent(ctx, "", moved.ID, storage.ComponentMove{LocationName: strptr("")}, all, all, all)
	if err != nil {
		t.Fatalf("clear the location: %v", err)
	}
	if orphaned.Name != "display-1" {
		t.Fatalf("name after clearing the location = %q, want display-1 (the unplaced bucket is empty)", orphaned.Name)
	}
	if orphaned.Label != "Display 1" {
		t.Fatalf("label after clearing the location = %q, want %q", orphaned.Label, "Display 1")
	}
}

// TestAReclassifyToTheSameProductKeepsTheName is #691: the guard keys on the
// classification field being PRESENT in the patch rather than on its value
// changing, so a save that re-states the product a component already has
// re-mints its name.
func TestAReclassifyToTheSameProductKeepsTheName(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoom(t, gw, ctx, "reclassify-room")

	first, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(edge55), LocationName: &room}, all, all, all, all)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(edge55), LocationName: &room}, all, all, all, all)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if first.Name != "display-1" || second.Name != "display-2" {
		t.Fatalf("precondition names = %q, %q, want display-1, display-2", first.Name, second.Name)
	}
	if _, err := gw.RenameComponent(ctx, "", first.ID, "reclassify-house-left", all, all); err != nil {
		t.Fatalf("rename the first: %v", err)
	}

	after, err := gw.UpdateComponent(ctx, "", second.ID, storage.ComponentPatch{ProductName: strptr(edge55)}, all, all)
	if err != nil {
		t.Fatalf("save restating the current product: %v", err)
	}
	if after.Name != "display-2" {
		t.Fatalf("name after a save that restated the current product = %q, want unchanged display-2 (component:update is not component:rename)", after.Name)
	}
	if after.Ordinal == nil || *after.Ordinal != 2 {
		t.Fatalf("ordinal after the restated save = %v, want unchanged 2", ordstr(after.Ordinal))
	}
	if after.Label != "Display 2" {
		t.Fatalf("label after the restated save = %q, want unchanged %q", after.Label, "Display 2")
	}
}

// TestAReclassifyWithinOneStemKeepsTheName is the component tier's own fact,
// and the reason this guard is not a copy of the system tier's: a component
// reaches its stem through a PRODUCT, so two products classified under one
// component_type mint from the same stem. Swapping between them is a real
// reclassify that changes no input to the mint.
func TestAReclassifyWithinOneStemKeepsTheName(t *testing.T) {
	gw, ctx := seededGateway(t)
	for _, name := range []string{"stem-twin-a", "stem-twin-b"} {
		if _, err := gw.CreateProduct(ctx, "", storage.Product{
			Name: name, Label: name, Kind: "device", ComponentType: "display",
		}); err != nil {
			t.Fatalf("create product %s: %v", name, err)
		}
	}
	room := makeRoom(t, gw, ctx, "stem-twin-room")

	first, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr("stem-twin-a"), LocationName: &room}, all, all, all, all)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr("stem-twin-a"), LocationName: &room}, all, all, all, all)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if first.Name != "display-1" || second.Name != "display-2" {
		t.Fatalf("precondition names = %q, %q, want display-1, display-2 (both products classify under display)", first.Name, second.Name)
	}
	if _, err := gw.RenameComponent(ctx, "", first.ID, "twin-house-left", all, all); err != nil {
		t.Fatalf("rename the first: %v", err)
	}

	after, err := gw.UpdateComponent(ctx, "", second.ID, storage.ComponentPatch{ProductName: strptr("stem-twin-b")}, all, all)
	if err != nil {
		t.Fatalf("reclassify within one stem: %v", err)
	}
	if after.ProductHandle == nil || *after.ProductHandle != "stem-twin-b" {
		t.Fatalf("product after the reclassify = %v, want stem-twin-b (the reclassify itself must still happen)", after.ProductHandle)
	}
	if after.Name != "display-2" {
		t.Fatalf("name after a reclassify between two products of one component_type = %q, want unchanged display-2 (same stem, same bucket, nothing the mint reads changed)", after.Name)
	}
	if after.Ordinal == nil || *after.Ordinal != 2 {
		t.Fatalf("ordinal after a same-stem reclassify = %v, want unchanged 2", ordstr(after.Ordinal))
	}
}

// TestARealReclassifyStillReMintsAndTheLabelFollows is the guard-rail for the
// reclassify: a product swap that lands on a different stem still moves the
// name, and the label (which reads the type and the ordinal) moves with it.
func TestARealReclassifyStillReMintsAndTheLabelFollows(t *testing.T) {
	gw, ctx := seededGateway(t)
	room := makeRoom(t, gw, ctx, "real-reclassify-room")

	// A mic already in the bucket, so the reclassified row cannot land on
	// ordinal 1 and pass by accident.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(ceilingMic), LocationName: &room}, all, all, all, all); err != nil {
		t.Fatalf("create the sitting mic: %v", err)
	}
	c, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{ProductName: strptr(edge55), LocationName: &room}, all, all, all, all)
	if err != nil {
		t.Fatalf("create the display: %v", err)
	}
	if c.Name != "display-1" || c.Label != "Display 1" {
		t.Fatalf("precondition = %q / %q, want display-1 / Display 1", c.Name, c.Label)
	}

	after, err := gw.UpdateComponent(ctx, "", c.ID, storage.ComponentPatch{ProductName: strptr(ceilingMic)}, all, all)
	if err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if after.Name != "mic-2" {
		t.Fatalf("name after a real reclassify = %q, want mic-2", after.Name)
	}
	if after.Ordinal == nil || *after.Ordinal != 2 {
		t.Fatalf("ordinal after a real reclassify = %v, want 2", ordstr(after.Ordinal))
	}
	if after.Label != "Ceiling Microphone 2" {
		t.Fatalf("label after a real reclassify = %q, want %q (the label follows the name and the type)", after.Label, "Ceiling Microphone 2")
	}
}
