package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestLocationPlacementEnforcement proves the allowed_parent_types contract on
// CreateLocation: an empty set (the default for a custom type) allows any
// placement, a populated set allows a listed parent type and a "skip a level"
// placement, a root-allowed type creates at root, and an out-of-order
// placement is refused with a PlacementError naming both types.
//
// The hierarchy fixture is built from custom types created in this test
// (t-campus/t-building/t-floor/t-room) rather than the official
// campus/building/floor/room registry, to keep this test independent of the
// seed's exact values, while exercising the same shape the boot seed ships
// (campus={root}, building={root,campus}, floor={building,campus},
// room={floor,building,campus}).
func TestLocationPlacementEnforcement(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A custom type with no allowed_parent_types (unconstrained): a root
	// placement succeeds with no restriction.
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{ID: "pod", DisplayName: "Pod"}); err != nil {
		t.Fatalf("create pod type: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "pod-root", LocationType: "pod"}, all); err != nil {
		t.Fatalf("unconstrained root placement: %v", err)
	}

	// The fixture hierarchy: t-campus={root}, t-building={root,t-campus},
	// t-floor={t-building,t-campus}, t-room={t-floor,t-building,t-campus}.
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{ID: "t-campus", DisplayName: "T-Campus", AllowedParentTypes: []string{storage.RootPlacement}}); err != nil {
		t.Fatalf("create t-campus type: %v", err)
	}
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{ID: "t-building", DisplayName: "T-Building", AllowedParentTypes: []string{storage.RootPlacement, "t-campus"}}); err != nil {
		t.Fatalf("create t-building type: %v", err)
	}
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{ID: "t-floor", DisplayName: "T-Floor", AllowedParentTypes: []string{"t-building", "t-campus"}}); err != nil {
		t.Fatalf("create t-floor type: %v", err)
	}
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{ID: "t-room", DisplayName: "T-Room", AllowedParentTypes: []string{"t-floor", "t-building", "t-campus"}}); err != nil {
		t.Fatalf("create t-room type: %v", err)
	}

	// t-campus is root-allowed: a root placement succeeds.
	mustCreate(t, gw, storage.LocationSpec{Name: "hq", LocationType: "t-campus"}, all)
	mustCreate(t, gw, storage.LocationSpec{Name: "hq-b1", LocationType: "t-building", ParentName: strptr("hq")}, all)

	// A room may skip straight under a building (listed) or a campus (listed):
	// both succeed.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq-r1", LocationType: "t-room", ParentName: strptr("hq-b1")}, all); err != nil {
		t.Fatalf("room under building (skip floor, allowed): %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq-r2", LocationType: "t-room", ParentName: strptr("hq")}, all); err != nil {
		t.Fatalf("room under campus (skip floor+building, allowed): %v", err)
	}

	// Out of order: a floor under a room is refused (room is not in floor's
	// allowed_parent_types); the error names both types.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "bad-floor", LocationType: "t-floor", ParentName: strptr("hq-r1")}, all); err == nil {
		t.Fatal("floor under room = nil error, want PlacementError")
	} else {
		var placementErr *storage.PlacementError
		if !errors.As(err, &placementErr) {
			t.Fatalf("floor under room err = %v (%T), want *storage.PlacementError", err, err)
		}
		if placementErr.ChildType != "t-floor" || placementErr.ParentType != "t-room" {
			t.Errorf("placement error = %+v, want child=t-floor parent=t-room", placementErr)
		}
		if !errors.Is(err, storage.ErrPlacementNotAllowed) {
			t.Error("placement error does not match storage.ErrPlacementNotAllowed via errors.Is")
		}
	}

	// t-campus's allowed set is {root} only: a non-root placement is refused.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "bad-campus", LocationType: "t-campus", ParentName: strptr("hq-b1")}, all); !errors.Is(err, storage.ErrPlacementNotAllowed) {
		t.Fatalf("campus under building err = %v, want ErrPlacementNotAllowed", err)
	}
}

// TestLocationRootPlacementRejected proves the root-rejection branch of
// validatePlacement: a location_type whose allowed_parent_types does not
// contain "root" is refused a root placement (no parent) on create, and the
// PlacementError names the rejected child type with no parent type.
func TestLocationRootPlacementRejected(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{ID: "t-noroot", DisplayName: "T-Noroot", AllowedParentTypes: []string{"t-noroot"}}); err != nil {
		t.Fatalf("create t-noroot type: %v", err)
	}

	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "bad-root", LocationType: "t-noroot"}, all); err == nil {
		t.Fatal("root placement for a type without root in allowed_parent_types = nil error, want PlacementError")
	} else {
		var placementErr *storage.PlacementError
		if !errors.As(err, &placementErr) {
			t.Fatalf("root placement err = %v (%T), want *storage.PlacementError", err, err)
		}
		if placementErr.ChildType != "t-noroot" || placementErr.ParentType != "" {
			t.Errorf("placement error = %+v, want child=t-noroot parent=\"\"", placementErr)
		}
		if !errors.Is(err, storage.ErrPlacementNotAllowed) {
			t.Error("placement error does not match storage.ErrPlacementNotAllowed via errors.Is")
		}
	}
}

// TestLocationReparentEnforcement proves the same allowed_parent_types contract
// on the move path (UpdateLocation's ParentName patch): a valid move succeeds,
// an out-of-order move is refused with the same PlacementError, a move that
// would create a cycle (under self or a descendant) is refused distinctly, and
// an existing (grandfathered) placement a type's set no longer allows is
// untouched by an unrelated field update.
//
// Like TestLocationPlacementEnforcement, the placement half of this fixture
// uses custom types (t-campus/t-building/t-room) with an explicit
// allowed_parent_types rather than the official campus/building/room
// registry, keeping the move path's placement enforcement testable
// independent of the seed's exact values (see that test's doc comment).
func TestLocationReparentEnforcement(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{ID: "t-campus", DisplayName: "T-Campus", AllowedParentTypes: []string{storage.RootPlacement}}); err != nil {
		t.Fatalf("create t-campus type: %v", err)
	}
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{ID: "t-building", DisplayName: "T-Building", AllowedParentTypes: []string{storage.RootPlacement, "t-campus"}}); err != nil {
		t.Fatalf("create t-building type: %v", err)
	}
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{ID: "t-room", DisplayName: "T-Room", AllowedParentTypes: []string{"t-building", "t-campus"}}); err != nil {
		t.Fatalf("create t-room type: %v", err)
	}

	mustCreate(t, gw, storage.LocationSpec{Name: "hq", LocationType: "t-campus"}, all)
	mustCreate(t, gw, storage.LocationSpec{Name: "lab", LocationType: "t-campus"}, all)
	mustCreate(t, gw, storage.LocationSpec{Name: "hq-b1", LocationType: "t-building", ParentName: strptr("hq")}, all)
	mustCreate(t, gw, storage.LocationSpec{Name: "hq-r1", LocationType: "t-room", ParentName: strptr("hq-b1")}, all)

	// A valid move: hq-b1 (t-building, allowed={root,t-campus}) moves from hq
	// to lab, both t-campuses.
	if _, err := gw.UpdateLocation(ctx, "", "hq-b1", storage.LocationPatch{ParentName: strptr("lab")}, all, all); err != nil {
		t.Fatalf("valid move: %v", err)
	}

	// Out of order: moving hq-b1 (t-building) under hq-r1 (t-room) is refused,
	// t-room is not in t-building's allowed_parent_types, and the error names
	// both. hq-r1 is also hq-b1's own child at this point, so this move is
	// simultaneously a structural cycle; the placement violation is expected
	// to take precedence (validatePlacement runs before the cycle guard), so
	// the more specific PlacementError, not ErrLocationCycle, is what the
	// caller sees.
	if _, err := gw.UpdateLocation(ctx, "", "hq-b1", storage.LocationPatch{ParentName: strptr("hq-r1")}, all, all); err == nil {
		t.Fatal("move building under room = nil error, want PlacementError")
	} else {
		var placementErr *storage.PlacementError
		if !errors.As(err, &placementErr) {
			t.Fatalf("move building under room err = %v, want *storage.PlacementError", err)
		}
		if placementErr.ChildType != "t-building" || placementErr.ParentType != "t-room" {
			t.Errorf("placement error = %+v, want child=t-building parent=t-room", placementErr)
		}
	}

	// Cycle guard, isolated from placement (an unconstrained custom type): a
	// location cannot move under itself or its own descendant.
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{ID: "pod", DisplayName: "Pod"}); err != nil {
		t.Fatalf("create pod type: %v", err)
	}
	mustCreate(t, gw, storage.LocationSpec{Name: "pod-a", LocationType: "pod"}, all)
	mustCreate(t, gw, storage.LocationSpec{Name: "pod-b", LocationType: "pod", ParentName: strptr("pod-a")}, all)
	if _, err := gw.UpdateLocation(ctx, "", "pod-a", storage.LocationPatch{ParentName: strptr("pod-b")}, all, all); !errors.Is(err, storage.ErrLocationCycle) {
		t.Fatalf("move pod-a under its own child err = %v, want ErrLocationCycle", err)
	}
	if _, err := gw.UpdateLocation(ctx, "", "pod-a", storage.LocationPatch{ParentName: strptr("pod-a")}, all, all); !errors.Is(err, storage.ErrLocationCycle) {
		t.Fatalf("move pod-a under itself err = %v, want ErrLocationCycle", err)
	}

	// Grandfathered: constrain pod's allowed_parent_types to {root} after pod-b
	// is already placed under pod-a, which now violates the new set. An
	// unrelated field update on pod-b still succeeds: enforcement is
	// forward-only, not retroactive.
	if _, err := gw.UpdateLocationType(ctx, "", "pod", storage.LocationTypePatch{AllowedParentTypes: &[]string{storage.RootPlacement}}); err != nil {
		t.Fatalf("constrain pod type: %v", err)
	}
	newName := "Pod B renamed"
	if _, err := gw.UpdateLocation(ctx, "", "pod-b", storage.LocationPatch{DisplayName: &newName}, all, all); err != nil {
		t.Fatalf("grandfathered unrelated update: %v", err)
	}

	// A fresh move re-validates: pod-b (still noncompliant) explicitly moved
	// back under pod-a is now refused, since pod no longer allows "pod" as a
	// parent type.
	if _, err := gw.UpdateLocation(ctx, "", "pod-b", storage.LocationPatch{ParentName: strptr("pod-a")}, all, all); !errors.Is(err, storage.ErrPlacementNotAllowed) {
		t.Fatalf("re-move pod-b under pod-a after constraining err = %v, want ErrPlacementNotAllowed", err)
	}
}
