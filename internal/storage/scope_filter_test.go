package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestScopedListByRoot proves the location scope filter: a scope root that is a
// real location id returns that location plus its subtree (not its siblings), and
// a malformed root (a name, not a uuid, as a mis-created grant would store) yields
// an empty list rather than erroring the whole query. Skipped under -short.
func TestScopedListByRoot(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	// boi (root) with a child, and sjc (a separate root).
	boi, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "boi", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create boi: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "17c", LocationType: "building", ParentName: ptr("boi")}, all); err != nil {
		t.Fatalf("create 17c: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "sjc", LocationType: "campus"}, all); err != nil {
		t.Fatalf("create sjc: %v", err)
	}

	// Scoped to boi's id: boi and its child, never sjc.
	got, err := gw.ListLocations(ctx, scope.Set{IDs: []string{boi.ID}})
	if err != nil {
		t.Fatalf("scoped list by id: %v", err)
	}
	names := map[string]bool{}
	for _, l := range got {
		names[l.Name] = true
	}
	if !names["boi"] || !names["17c"] || names["sjc"] {
		t.Fatalf("scope to boi should be {boi,17c}, got %v", names)
	}

	// A malformed root (a name, as a mis-created grant would store) must not error
	// the whole query; it contributes nothing.
	empty, err := gw.ListLocations(ctx, scope.Set{IDs: []string{"boi"}})
	if err != nil {
		t.Fatalf("scoped list by a bad root should not error, got %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("a non-uuid scope root should match nothing, got %v", empty)
	}
}

func ptr(s string) *string { return &s }
