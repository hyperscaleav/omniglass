package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestLocationTypeCRUD(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Create a custom type; it is official=false.
	lt, err := gw.CreateLocationType(ctx, "", storage.LocationType{ID: "wing", DisplayName: "Wing", Icon: "layers"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if lt.Official {
		t.Fatalf("new type official=true, want false")
	}

	// Duplicate id is ErrTypeExists.
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{ID: "wing", DisplayName: "Dup"}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}

	// "root" is reserved for the allowed_parent_types sentinel: creating a type
	// with that id is refused, so a real type can never collide with it.
	if _, err := gw.CreateLocationType(ctx, "", storage.LocationType{ID: "root", DisplayName: "Root"}); !errors.Is(err, storage.ErrReservedTypeID) {
		t.Fatalf("create root-id type err = %v, want ErrReservedTypeID", err)
	}

	// Update mutates display_name; icon unchanged when omitted.
	name := "West Wing"
	if _, err := gw.UpdateLocationType(ctx, "", "wing", storage.LocationTypePatch{DisplayName: &name}); err != nil {
		t.Fatalf("update: %v", err)
	}

	// A shipped location type is operator-owned example content: the estate shapes
	// its own place vocabulary, so it is editable.
	if _, err := gw.UpdateLocationType(ctx, "", "campus", storage.LocationTypePatch{DisplayName: &name}); err != nil {
		t.Fatalf("update a shipped location type: %v, want it editable", err)
	}

	// The official read-only guard still stands for a row that IS official, so
	// prove the mechanism on one.
	if err := gw.UpsertLocationType(ctx, storage.LocationType{ID: "canon", Official: true, DisplayName: "Canonical"}); err != nil {
		t.Fatalf("seed an official location type: %v", err)
	}
	if _, err := gw.UpdateLocationType(ctx, "", "canon", storage.LocationTypePatch{DisplayName: &name}); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("update official err = %v, want ErrTypeOfficial", err)
	}
	if err := gw.DeleteLocationType(ctx, "", "canon"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrTypeOfficial", err)
	}

	// In-use delete is refused: place a location of type wing, then delete.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "w1", LocationType: "wing"}, all); err != nil {
		t.Fatalf("create location: %v", err)
	}
	if err := gw.DeleteLocationType(ctx, "", "wing"); !errors.Is(err, storage.ErrTypeInUse) {
		t.Fatalf("delete in-use err = %v, want ErrTypeInUse", err)
	}

	// Unknown id is ErrTypeNotFound.
	if err := gw.DeleteLocationType(ctx, "", "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrTypeNotFound", err)
	}
}
