package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestRenameLocation(t *testing.T) {
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

	// A location with a system placed in it, so we can prove the placement's
	// location_id (a UUID FK) survives the rename (it references the id, not the name).
	root, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq-root", LocationType: "campus"}, all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av-placed", SystemType: "meeting-room", LocationName: strptr("hq-root")}, all); err != nil {
		t.Fatal(err)
	}

	// Rename the location.
	newName := "hq-root-renamed"
	up, err := gw.UpdateLocation(ctx, "", "hq-root", storage.LocationPatch{Name: &newName}, all, all)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if up.Name != newName {
		t.Fatalf("name = %q, want %q", up.Name, newName)
	}
	if up.ID != root.ID {
		t.Fatalf("rename changed id: got %q, want %q (a rename is a one-column update, not a new row)", up.ID, root.ID)
	}

	// The placed system's location_id (a UUID FK) is untouched: it still resolves to
	// the same location row.
	got, err := gw.GetSystem(ctx, "av-placed", all)
	if err != nil {
		t.Fatal(err)
	}
	if got.LocationID == nil || *got.LocationID != up.ID {
		t.Fatalf("placed system location_id = %v, want %q (rename must not touch UUID FKs)", got.LocationID, up.ID)
	}

	// The old name is free; a create can reuse it.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq-root", LocationType: "campus"}, all); err != nil {
		t.Fatalf("old name should be free after rename: %v", err)
	}

	// Renaming onto a taken name -> ErrLocationExists.
	if _, err := gw.UpdateLocation(ctx, "", "hq-root-renamed", storage.LocationPatch{Name: strptr("hq-root")}, all, all); !errors.Is(err, storage.ErrLocationExists) {
		t.Fatalf("dup rename err = %v, want ErrLocationExists", err)
	}

	// Bad slug -> ErrInvalidName (before touching the DB).
	bad := "Bad Name"
	if _, err := gw.UpdateLocation(ctx, "", "hq-root-renamed", storage.LocationPatch{Name: &bad}, all, all); !errors.Is(err, storage.ErrInvalidName) {
		t.Fatalf("bad-format rename err = %v, want ErrInvalidName", err)
	}

	// Create-tightening: the shared validator gates create too, not just rename.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "Bad Name", LocationType: "campus"}, all); !errors.Is(err, storage.ErrInvalidName) {
		t.Fatalf("bad-format create err = %v, want ErrInvalidName", err)
	}

	// LocationNameTaken is scope-blind existence.
	if taken, err := gw.LocationNameTaken(ctx, newName); err != nil || !taken {
		t.Fatalf("LocationNameTaken(%q) = %v,%v want true,nil", newName, taken, err)
	}
	if taken, err := gw.LocationNameTaken(ctx, "nope-not-here"); err != nil || taken {
		t.Fatalf("LocationNameTaken(free) = %v,%v want false,nil", taken, err)
	}
}
