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
	root, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av-root"}, all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av-child", ParentName: strptr("av-root")}, all); err != nil {
		t.Fatal(err)
	}

	// Rename the parent.
	newName := "av-root-renamed"
	up, err := gw.RenameSystem(ctx, "", "av-root", newName, all, all)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if up.Name != newName {
		t.Fatalf("name = %q, want %q", up.Name, newName)
	}
	if up.ID != root.ID {
		t.Fatalf("rename changed id: got %q, want %q (a rename moves the name, never the identity)", up.ID, root.ID)
	}

	// Reachable afterwards by the NEW name and by its uuid, not by the old one.
	if got, err := gw.GetSystem(ctx, newName, all); err != nil || got.ID != root.ID {
		t.Fatalf("get by new name = %v, %v; want the same row", got, err)
	}
	if got, err := gw.GetSystem(ctx, root.ID, all); err != nil || got.Name != newName {
		t.Fatalf("get by uuid = %v, %v; want the row under its new name", got, err)
	}
	if _, err := gw.GetSystem(ctx, "av-root", all); !errors.Is(err, storage.ErrSystemNotFound) {
		t.Fatalf("get by old name = %v, want ErrSystemNotFound", err)
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

	// The old name is free; a create can reuse it.
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av-root"}, all); err != nil {
		t.Fatalf("old name should be free after rename: %v", err)
	}

	// Renaming onto a taken name -> ErrSystemExists (the API's 409). The
	// collision has to land in av-child's OWN placement bucket (#627 scopes
	// name uniqueness to placement): av-child is a child of av-root-renamed
	// (system_parent_name_key), so newName itself (the root/orphan bucket)
	// can no longer collide with it; a sibling under the same parent can.
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av-sibling", ParentName: strptr(newName)}, all); err != nil {
		t.Fatalf("sibling for dup-rename case: %v", err)
	}
	if _, err := gw.RenameSystem(ctx, "", "av-child", "av-sibling", all, all); !errors.Is(err, storage.ErrSystemExistsUnderParent) {
		t.Fatalf("dup rename err = %v, want ErrSystemExistsUnderParent", err)
	}

	// Bad slug -> ErrInvalidEntityName (before touching the DB).
	if _, err := gw.RenameSystem(ctx, "", "av-child", "Bad Name", all, all); !errors.Is(err, storage.ErrInvalidEntityName) {
		t.Fatalf("bad-format rename err = %v, want ErrInvalidEntityName", err)
	}

	// A uuid-shaped name is its own refusal: it satisfies the slug rule completely,
	// and admitting it would make a name indistinguishable from an id.
	if _, err := gw.RenameSystem(ctx, "", "av-child", "019f8754-461f-7b82-b5f2-fc4bbe1c3765", all, all); !errors.Is(err, storage.ErrEntityNameIsUUID) {
		t.Fatalf("uuid-shaped rename err = %v, want ErrEntityNameIsUUID", err)
	}

	// A missing entity is the ordinary not-found, so the API answers 404.
	if _, err := gw.RenameSystem(ctx, "", "no-such-system", "whatever", all, all); !errors.Is(err, storage.ErrSystemNotFound) {
		t.Fatalf("rename of a missing system = %v, want ErrSystemNotFound", err)
	}

	// Create-tightening: the shared validator gates create too, not just rename.
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "Bad Name"}, all); !errors.Is(err, storage.ErrInvalidEntityName) {
		t.Fatalf("bad-format create err = %v, want ErrInvalidEntityName", err)
	}

	// SystemNameTaken checks the unplaced/root bucket here: av-root has
	// neither a parent nor a location.
	if taken, err := gw.SystemNameTaken(ctx, newName, nil, nil); err != nil || !taken {
		t.Fatalf("SystemNameTaken(%q) = %v,%v want true,nil", newName, taken, err)
	}
	if taken, err := gw.SystemNameTaken(ctx, "nope-not-here", nil, nil); err != nil || taken {
		t.Fatalf("SystemNameTaken(free) = %v,%v want false,nil", taken, err)
	}

	assertOneRenameAudit(t, ctx, gw, "system", root.ID)
}
