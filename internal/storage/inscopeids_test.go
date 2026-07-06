package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestInScopeIDs proves the batch scope-membership primitive that drives per-row
// UI action gating: given candidate row ids and a resolved action scope, it
// returns which rows are in scope, using the same subtree/exclude-root logic the
// enforcement uses. Skipped under -short.
func TestInScopeIDs(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := gw.UpsertLocationType(ctx, storage.LocationType{ID: "campus", DisplayName: "Campus", Official: true}); err != nil {
		t.Fatalf("seed type: %v", err)
	}
	all := scope.Set{All: true}
	root, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "root", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	child, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "child", LocationType: "campus", ParentName: &root.Name}, all)
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	other, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "other", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	ids := []string{root.ID, child.ID, other.ID}

	// All scope: every candidate is in.
	got, err := gw.InScopeIDs(ctx, "location", ids, all)
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	for _, id := range ids {
		if !got[id] {
			t.Fatalf("all scope should include %s", id)
		}
	}

	// Rooted scope at root: root + child in, other out.
	rooted := scope.Set{IDs: []string{root.ID}}
	got, err = gw.InScopeIDs(ctx, "location", ids, rooted)
	if err != nil {
		t.Fatalf("rooted: %v", err)
	}
	if !got[root.ID] || !got[child.ID] || got[other.ID] {
		t.Fatalf("rooted scope = %+v, want root+child, not other", got)
	}

	// Exclude-root at root: root OUT, child in, other out (the write-scope shape).
	excl := scope.Set{IDs: []string{root.ID}, ExcludeRootIDs: []string{root.ID}}
	got, err = gw.InScopeIDs(ctx, "location", ids, excl)
	if err != nil {
		t.Fatalf("exclude-root: %v", err)
	}
	if got[root.ID] || !got[child.ID] || got[other.ID] {
		t.Fatalf("exclude-root scope = %+v, want child only (root excluded)", got)
	}

	// Empty candidates and a non-tree resource are clean empties.
	if m, err := gw.InScopeIDs(ctx, "location", nil, all); err != nil || len(m) != 0 {
		t.Fatalf("empty ids: %+v err %v", m, err)
	}
	if m, err := gw.InScopeIDs(ctx, "principal", ids, all); err != nil || len(m) != 0 {
		t.Fatalf("non-tree resource: want empty, got %+v err %v", m, err)
	}
}
