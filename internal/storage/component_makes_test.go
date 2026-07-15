package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestComponentMakeCRUD(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Create a custom make; it is official=false.
	m, err := gw.CreateComponentMake(ctx, "", storage.ComponentMake{
		ID: "acme", DisplayName: "Acme", Website: "https://acme.example",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.ID != "acme" {
		t.Fatalf("create id = %q, want acme", m.ID)
	}
	if m.Official {
		t.Fatalf("new make official=true, want false")
	}

	// Duplicate id is ErrTypeExists.
	if _, err := gw.CreateComponentMake(ctx, "", storage.ComponentMake{ID: "acme", DisplayName: "Dup"}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}

	// Get + list.
	got, err := gw.GetComponentMake(ctx, "acme")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DisplayName != "Acme" {
		t.Fatalf("get display_name = %q, want Acme", got.DisplayName)
	}
	all, err := gw.ListComponentMakes(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found *storage.ComponentMake
	for i := range all {
		if all[i].ID == "acme" {
			found = &all[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("list does not contain acme; got %d rows", len(all))
	}
	if found.DisplayName != "Acme" {
		t.Fatalf("list acme display_name = %q, want Acme", found.DisplayName)
	}
	if found.Official {
		t.Fatalf("list acme official=true, want false")
	}

	// Update patch (display name + support phone); icon/website unchanged when omitted.
	dn, ph := "Acme Inc.", "+1-555-0100"
	upd, err := gw.UpdateComponentMake(ctx, "", "acme", storage.ComponentMakePatch{DisplayName: &dn, SupportPhone: &ph})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.DisplayName != "Acme Inc." {
		t.Fatalf("update display_name = %q, want Acme Inc.", upd.DisplayName)
	}
	if upd.SupportPhone != "+1-555-0100" {
		t.Fatalf("update support_phone = %q, want +1-555-0100", upd.SupportPhone)
	}
	if upd.Website != "https://acme.example" {
		t.Fatalf("update website = %q, want unchanged https://acme.example", upd.Website)
	}

	// Official rows are read-only.
	if err := gw.UpsertComponentMake(ctx, storage.ComponentMake{ID: "official-co", DisplayName: "Official Co", Official: true}); err != nil {
		t.Fatalf("upsert official: %v", err)
	}
	if _, err := gw.UpdateComponentMake(ctx, "", "official-co", storage.ComponentMakePatch{DisplayName: &dn}); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("update official err = %v, want ErrTypeOfficial", err)
	}
	if err := gw.DeleteComponentMake(ctx, "", "official-co"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrTypeOfficial", err)
	}

	// Unknown id is ErrTypeNotFound.
	if _, err := gw.GetComponentMake(ctx, "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get unknown err = %v, want ErrTypeNotFound", err)
	}
	if err := gw.DeleteComponentMake(ctx, "", "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrTypeNotFound", err)
	}

	// Delete a custom row, then confirm it is gone.
	if err := gw.DeleteComponentMake(ctx, "", "acme"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := gw.GetComponentMake(ctx, "acme"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get after delete err = %v, want ErrTypeNotFound", err)
	}
}
