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
// campus/building/floor/room registry: the boot seed does not populate the
// official types' allowed_parent_types until a later slice task, so this
// keeps the test independent of that not-yet-shipped seed data while
// exercising the exact same shape (campus={root}, building={root,campus},
// floor={building,campus}, room={floor,building,campus}).
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
