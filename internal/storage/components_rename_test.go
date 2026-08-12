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
	root, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-root"}, all, all, all, all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-child", ParentName: strptr("disp-root")}, all, all, all, all); err != nil {
		t.Fatal(err)
	}

	// Rename the parent.
	newName := "disp-root-renamed"
	up, err := gw.RenameComponent(ctx, "", "disp-root", newName, all, all)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if up.Name != newName {
		t.Fatalf("name = %q, want %q", up.Name, newName)
	}
	if up.ID != root.ID {
		t.Fatalf("rename changed id: got %q, want %q (a rename moves the name, never the identity)", up.ID, root.ID)
	}

	// The entity is reachable afterwards by the NEW name and by its uuid, and no
	// longer by the old one. This is the whole observable effect of a rename.
	if got, err := gw.GetComponent(ctx, newName, all); err != nil || got.ID != root.ID {
		t.Fatalf("get by new name = %v, %v; want the same row", got, err)
	}
	if got, err := gw.GetComponent(ctx, root.ID, all); err != nil || got.Name != newName {
		t.Fatalf("get by uuid = %v, %v; want the row under its new name", got, err)
	}
	if _, err := gw.GetComponent(ctx, "disp-root", all); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Fatalf("get by old name = %v, want ErrComponentNotFound", err)
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

	// The old name is free; a create can reuse it.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-root"}, all, all, all, all); err != nil {
		t.Fatalf("old name should be free after rename: %v", err)
	}

	// Renaming onto a taken name -> ErrComponentExists (the API's 409). The
	// collision has to land in disp-child's OWN placement bucket (#627 scopes
	// name uniqueness to placement): disp-child is a child of
	// disp-root-renamed (component_parent_name_key), so newName itself (the
	// root/orphan bucket) can no longer collide with it; a sibling under the
	// same parent can.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-sibling", ParentName: strptr(newName)}, all, all, all, all); err != nil {
		t.Fatalf("sibling for dup-rename case: %v", err)
	}
	if _, err := gw.RenameComponent(ctx, "", "disp-child", "disp-sibling", all, all); !errors.Is(err, storage.ErrComponentExistsUnderParent) {
		t.Fatalf("dup rename err = %v, want ErrComponentExistsUnderParent", err)
	}

	// Bad slug -> ErrInvalidEntityName (before touching the DB).
	if _, err := gw.RenameComponent(ctx, "", "disp-child", "Bad Name", all, all); !errors.Is(err, storage.ErrInvalidEntityName) {
		t.Fatalf("bad-format rename err = %v, want ErrInvalidEntityName", err)
	}

	// A uuid-shaped name is its own refusal: it satisfies the slug rule completely,
	// and admitting it would make a name indistinguishable from an id.
	if _, err := gw.RenameComponent(ctx, "", "disp-child", "019f8754-461f-7b82-b5f2-fc4bbe1c3765", all, all); !errors.Is(err, storage.ErrEntityNameIsUUID) {
		t.Fatalf("uuid-shaped rename err = %v, want ErrEntityNameIsUUID", err)
	}

	// A missing entity is the ordinary not-found, so the API answers 404.
	if _, err := gw.RenameComponent(ctx, "", "no-such-component", "whatever", all, all); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Fatalf("rename of a missing component = %v, want ErrComponentNotFound", err)
	}

	// Create-tightening: the shared validator gates create too, not just rename.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "Bad Name"}, all, all, all, all); !errors.Is(err, storage.ErrInvalidEntityName) {
		t.Fatalf("bad-format create err = %v, want ErrInvalidEntityName", err)
	}

	// ComponentNameTaken checks the unplaced/root bucket here: disp-root has
	// neither a parent nor a location.
	if taken, err := gw.ComponentNameTaken(ctx, newName, nil, nil); err != nil || !taken {
		t.Fatalf("ComponentNameTaken(%q) = %v,%v want true,nil", newName, taken, err)
	}
	if taken, err := gw.ComponentNameTaken(ctx, "nope-not-here", nil, nil); err != nil || taken {
		t.Fatalf("ComponentNameTaken(free) = %v,%v want false,nil", taken, err)
	}

	assertOneRenameAudit(t, ctx, gw, "component", root.ID)
}

// assertOneRenameAudit is the audit half every rename owes: exactly one row, verb
// "rename", keyed on the entity's primary key. Keyed on the uuid because the row
// records a change of name, and a trail keyed on the thing that just changed is a
// trail the change orphans.
func assertOneRenameAudit(t *testing.T, ctx context.Context, gw *storage.PG, resource, id string) {
	t.Helper()
	rows, err := gw.ListAuditLog(ctx, storage.AuditFilter{Resource: resource, Verb: "rename", Limit: 100})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var seen int
	for _, r := range rows {
		if r.ResourceID != id {
			t.Errorf("a %s rename audit row keys on %q, not the entity uuid %q", resource, r.ResourceID, id)
			continue
		}
		seen++
		if len(r.Old) == 0 || len(r.New) == 0 {
			t.Errorf("%s rename audit row carries old=%d new=%d bytes, want both row images", resource, len(r.Old), len(r.New))
		}
	}
	if seen != 1 {
		t.Errorf("%d rename audit rows keyed on %q, want exactly 1", seen, id)
	}
}
