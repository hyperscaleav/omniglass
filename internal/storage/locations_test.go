package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestLocationTypeRegistry is the round-trip for the location_type registry: an
// upsert installs a type, a second upsert by the same id updates it (idempotent,
// the boot-seed contract), and ListLocationTypes returns them ranked.
func TestLocationTypeRegistry(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()

	if err := gw.UpsertLocationType(ctx, storage.LocationType{
		ID: "building", Official: true, DisplayName: "Building", Rank: 20, Icon: "building",
	}); err != nil {
		t.Fatalf("upsert building: %v", err)
	}
	if err := gw.UpsertLocationType(ctx, storage.LocationType{
		ID: "campus", Official: true, DisplayName: "Campus", Rank: 10, Icon: "landmark",
	}); err != nil {
		t.Fatalf("upsert campus: %v", err)
	}
	// Re-upsert building with a new display_name and icon: idempotent update, not
	// a dup, and the icon is part of what the update carries.
	if err := gw.UpsertLocationType(ctx, storage.LocationType{
		ID: "building", Official: true, DisplayName: "Bldg", Rank: 20, Icon: "building-2",
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
	// Ordered by rank: campus (10) before building (20).
	if types[0].ID != "campus" || types[1].ID != "building" {
		t.Errorf("type order = %s,%s, want campus,building", types[0].ID, types[1].ID)
	}
	if types[1].DisplayName != "Bldg" {
		t.Errorf("building display_name = %q, want Bldg (the update took)", types[1].DisplayName)
	}
	// The icon round-trips, and the re-upsert updated it in place.
	if types[0].Icon != "landmark" {
		t.Errorf("campus icon = %q, want landmark", types[0].Icon)
	}
	if types[1].Icon != "building-2" {
		t.Errorf("building icon = %q, want building-2 (the update took)", types[1].Icon)
	}
}
