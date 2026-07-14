package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestLocationTypeRegistry is the round-trip for the location_type registry: an
// upsert installs a type, a second upsert by the same id updates it (idempotent,
// the boot-seed contract), and ListLocationTypes returns them alphabetically by
// display_name.
func TestLocationTypeRegistry(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()

	if err := gw.UpsertLocationType(ctx, storage.LocationType{
		ID: "building", Official: true, DisplayName: "Building", Icon: "building",
	}); err != nil {
		t.Fatalf("upsert building: %v", err)
	}
	if err := gw.UpsertLocationType(ctx, storage.LocationType{
		ID: "campus", Official: true, DisplayName: "Campus", Icon: "landmark",
	}); err != nil {
		t.Fatalf("upsert campus: %v", err)
	}
	// Re-upsert building with a new display_name and icon: idempotent update, not
	// a dup, and the icon is part of what the update carries.
	if err := gw.UpsertLocationType(ctx, storage.LocationType{
		ID: "building", Official: true, DisplayName: "Bldg", Icon: "building-2",
	}); err != nil {
		t.Fatalf("re-upsert building: %v", err)
	}

	types, err := gw.ListLocationTypes(ctx)
	if err != nil {
		t.Fatalf("list types: %v", err)
	}
	if len(types) != 2 {
		t.Fatalf("got %d types, want 2: %+v", len(types), types)
	}
	// Ordered alphabetically by display_name: "Bldg" (building) before "Campus"
	// (campus), not insertion order and not id order.
	if types[0].ID != "building" || types[1].ID != "campus" {
		t.Errorf("type order = %s,%s, want building,campus", types[0].ID, types[1].ID)
	}
	if types[0].DisplayName != "Bldg" {
		t.Errorf("building display_name = %q, want Bldg (the update took)", types[0].DisplayName)
	}
	// The icon round-trips, and the re-upsert updated it in place.
	if types[0].Icon != "building-2" {
		t.Errorf("building icon = %q, want building-2 (the update took)", types[0].Icon)
	}
	if types[1].Icon != "landmark" {
		t.Errorf("campus icon = %q, want landmark", types[1].Icon)
	}

	// allowed_parent_types round-trips: a bare upsert (no set given) defaults to
	// an empty (non-nil) slice, not SQL null, and a populated set persists.
	if err := gw.UpsertLocationType(ctx, storage.LocationType{
		ID: "wing", Official: true, DisplayName: "Wing", Icon: "layers",
	}); err != nil {
		t.Fatalf("upsert wing (no allowed_parent_types): %v", err)
	}
	if err := gw.UpsertLocationType(ctx, storage.LocationType{
		ID: "room", Official: true, DisplayName: "Room", Icon: "door-open",
		AllowedParentTypes: []string{"wing", storage.RootPlacement},
	}); err != nil {
		t.Fatalf("upsert room (with allowed_parent_types): %v", err)
	}
	types, err = gw.ListLocationTypes(ctx)
	if err != nil {
		t.Fatalf("list types after allowed_parent_types upserts: %v", err)
	}
	byID := make(map[string]storage.LocationType, len(types))
	for _, lt := range types {
		byID[lt.ID] = lt
	}
	if got := byID["wing"].AllowedParentTypes; got == nil || len(got) != 0 {
		t.Errorf("wing allowed_parent_types = %#v, want empty (non-nil) slice", got)
	}
	if got := byID["room"].AllowedParentTypes; len(got) != 2 || got[0] != "wing" || got[1] != storage.RootPlacement {
		t.Errorf("room allowed_parent_types = %#v, want [wing root]", got)
	}
}
