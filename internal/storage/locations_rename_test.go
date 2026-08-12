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
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av-placed", LocationName: strptr("hq-root")}, all, all); err != nil {
		t.Fatal(err)
	}

	// Rename the location.
	newName := "hq-root-renamed"
	up, err := gw.RenameLocation(ctx, "", "hq-root", newName, all, all)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if up.Name != newName {
		t.Fatalf("name = %q, want %q", up.Name, newName)
	}
	if up.ID != root.ID {
		t.Fatalf("rename changed id: got %q, want %q (a rename is a one-column update, not a new row)", up.ID, root.ID)
	}

	// Reachable afterwards by the NEW name and by its uuid, not by the old one.
	if got, err := gw.GetLocation(ctx, newName, all); err != nil || got.ID != root.ID {
		t.Fatalf("get by new name = %v, %v; want the same row", got, err)
	}
	if got, err := gw.GetLocation(ctx, root.ID, all); err != nil || got.Name != newName {
		t.Fatalf("get by uuid = %v, %v; want the row under its new name", got, err)
	}
	if _, err := gw.GetLocation(ctx, "hq-root", all); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Fatalf("get by old name = %v, want ErrLocationNotFound", err)
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

	// Renaming onto a taken name -> ErrLocationExistsAtRoot (the API's 409):
	// both hq-root-renamed and the reused hq-root are roots (no parent), so
	// this collides in location_root_name_key.
	if _, err := gw.RenameLocation(ctx, "", "hq-root-renamed", "hq-root", all, all); !errors.Is(err, storage.ErrLocationExistsAtRoot) {
		t.Fatalf("dup rename err = %v, want ErrLocationExistsAtRoot", err)
	}

	// Bad slug -> ErrInvalidEntityName (before touching the DB).
	if _, err := gw.RenameLocation(ctx, "", "hq-root-renamed", "Bad Name", all, all); !errors.Is(err, storage.ErrInvalidEntityName) {
		t.Fatalf("bad-format rename err = %v, want ErrInvalidEntityName", err)
	}

	// A uuid-shaped name is its own refusal: it satisfies the slug rule completely,
	// and admitting it would make a name indistinguishable from an id.
	if _, err := gw.RenameLocation(ctx, "", "hq-root-renamed", "019f8754-461f-7b82-b5f2-fc4bbe1c3765", all, all); !errors.Is(err, storage.ErrEntityNameIsUUID) {
		t.Fatalf("uuid-shaped rename err = %v, want ErrEntityNameIsUUID", err)
	}

	// A missing entity is the ordinary not-found, so the API answers 404.
	if _, err := gw.RenameLocation(ctx, "", "no-such-location", "whatever", all, all); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Fatalf("rename of a missing location = %v, want ErrLocationNotFound", err)
	}

	// Create-tightening: the shared validator gates create too, not just rename.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "Bad Name", LocationType: "campus"}, all); !errors.Is(err, storage.ErrInvalidEntityName) {
		t.Fatalf("bad-format create err = %v, want ErrInvalidEntityName", err)
	}

	// LocationNameTaken checks the root bucket here: hq-root has no parent.
	if taken, err := gw.LocationNameTaken(ctx, newName, nil); err != nil || !taken {
		t.Fatalf("LocationNameTaken(%q) = %v,%v want true,nil", newName, taken, err)
	}
	if taken, err := gw.LocationNameTaken(ctx, "nope-not-here", nil); err != nil || taken {
		t.Fatalf("LocationNameTaken(free) = %v,%v want false,nil", taken, err)
	}

	assertOneRenameAudit(t, ctx, gw, "location", root.ID)
}
