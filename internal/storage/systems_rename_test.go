package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestRenameSystem(t *testing.T) {
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

	// A system with a child, so we can prove the UUID FK survives the rename.
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av-root"}, all); err != nil {
		t.Fatal(err)
	}
	child, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av-child", ParentName: strptr("av-root")}, all)
	if err != nil {
		t.Fatal(err)
	}

	// Rename the parent.
	newName := "av-root-renamed"
	up, err := gw.UpdateSystem(ctx, "", "av-root", storage.SystemPatch{Name: &newName}, all, all)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if up.Name != newName {
		t.Fatalf("name = %q, want %q", up.Name, newName)
	}

	// The child's parent_id (a UUID FK) is untouched: the child still resolves and
	// its parent is the same row.
	got, err := gw.GetSystem(ctx, "av-child", all)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentID == nil || *got.ParentID != up.ID {
		t.Fatalf("child parent_id = %v, want %q (rename must not touch UUID FKs)", got.ParentID, up.ID)
	}
	_ = child

	// The old name is free; a create can reuse it.
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av-root"}, all); err != nil {
		t.Fatalf("old name should be free after rename: %v", err)
	}

	// Renaming onto a taken name -> ErrSystemExists.
	if _, err := gw.UpdateSystem(ctx, "", "av-child", storage.SystemPatch{Name: &newName}, all, all); !errors.Is(err, storage.ErrSystemExists) {
		t.Fatalf("dup rename err = %v, want ErrSystemExists", err)
	}

	// Bad slug -> ErrInvalidName (before touching the DB).
	bad := "Bad Name"
	if _, err := gw.UpdateSystem(ctx, "", "av-child", storage.SystemPatch{Name: &bad}, all, all); !errors.Is(err, storage.ErrInvalidName) {
		t.Fatalf("bad-format rename err = %v, want ErrInvalidName", err)
	}

	// Create-tightening: the shared validator gates create too, not just rename.
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "Bad Name"}, all); !errors.Is(err, storage.ErrInvalidName) {
		t.Fatalf("bad-format create err = %v, want ErrInvalidName", err)
	}

	// SystemNameTaken is scope-blind existence.
	if taken, err := gw.SystemNameTaken(ctx, newName); err != nil || !taken {
		t.Fatalf("SystemNameTaken(%q) = %v,%v want true,nil", newName, taken, err)
	}
	if taken, err := gw.SystemNameTaken(ctx, "nope-not-here"); err != nil || taken {
		t.Fatalf("SystemNameTaken(free) = %v,%v want false,nil", taken, err)
	}
}
