package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestRenameComponent(t *testing.T) {
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

	// A component with a child, so we can prove the UUID FK survives the rename.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-root"}, all); err != nil {
		t.Fatal(err)
	}
	child, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-child", ParentName: strptr("disp-root")}, all)
	if err != nil {
		t.Fatal(err)
	}

	// Rename the parent.
	newName := "disp-root-renamed"
	up, err := gw.UpdateComponent(ctx, "", "disp-root", storage.ComponentPatch{Name: &newName}, all, all)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if up.Name != newName {
		t.Fatalf("name = %q, want %q", up.Name, newName)
	}

	// The child's parent_id (a UUID FK) is untouched: the child still resolves and
	// its parent is the same row.
	got, err := gw.GetComponent(ctx, "disp-child", all)
	if err != nil {
		t.Fatal(err)
	}
	if got.ParentID == nil || *got.ParentID != up.ID {
		t.Fatalf("child parent_id = %v, want %q (rename must not touch UUID FKs)", got.ParentID, up.ID)
	}
	_ = child

	// The old name is free; a create can reuse it.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-root"}, all); err != nil {
		t.Fatalf("old name should be free after rename: %v", err)
	}

	// Renaming onto a taken name -> ErrComponentExists.
	if _, err := gw.UpdateComponent(ctx, "", "disp-child", storage.ComponentPatch{Name: &newName}, all, all); !errors.Is(err, storage.ErrComponentExists) {
		t.Fatalf("dup rename err = %v, want ErrComponentExists", err)
	}

	// Bad slug -> ErrInvalidName (before touching the DB).
	bad := "Bad Name"
	if _, err := gw.UpdateComponent(ctx, "", "disp-child", storage.ComponentPatch{Name: &bad}, all, all); !errors.Is(err, storage.ErrInvalidName) {
		t.Fatalf("bad-format rename err = %v, want ErrInvalidName", err)
	}

	// Create-tightening: the shared validator gates create too, not just rename.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "Bad Name"}, all); !errors.Is(err, storage.ErrInvalidName) {
		t.Fatalf("bad-format create err = %v, want ErrInvalidName", err)
	}

	// ComponentNameTaken is scope-blind existence.
	if taken, err := gw.ComponentNameTaken(ctx, newName); err != nil || !taken {
		t.Fatalf("ComponentNameTaken(%q) = %v,%v want true,nil", newName, taken, err)
	}
	if taken, err := gw.ComponentNameTaken(ctx, "nope-not-here"); err != nil || taken {
		t.Fatalf("ComponentNameTaken(free) = %v,%v want false,nil", taken, err)
	}
}
