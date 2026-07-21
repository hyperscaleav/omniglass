package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestCapabilityCRUD(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Create a custom capability; it is official=false.
	c, err := gw.CreateCapability(ctx, "", storage.Capability{
		ID: "projector", DisplayName: "Projector",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.ID != "projector" {
		t.Fatalf("create id = %q, want projector", c.ID)
	}
	if c.Official {
		t.Fatalf("new capability official=true, want false")
	}

	// Duplicate id is ErrTypeExists.
	if _, err := gw.CreateCapability(ctx, "", storage.Capability{ID: "projector", DisplayName: "Dup"}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}

	// Get + list.
	got, err := gw.GetCapability(ctx, "projector")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DisplayName != "Projector" {
		t.Fatalf("get display_name = %q, want Projector", got.DisplayName)
	}
	all, err := gw.ListCapabilities(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found *storage.Capability
	for i := range all {
		if all[i].ID == "projector" {
			found = &all[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("list does not contain projector; got %d rows", len(all))
	}
	if found.DisplayName != "Projector" {
		t.Fatalf("list projector display_name = %q, want Projector", found.DisplayName)
	}
	if found.Official {
		t.Fatalf("list projector official=true, want false")
	}

	// Update patch (display name).
	dn := "Projector Display"
	upd, err := gw.UpdateCapability(ctx, "", "projector", storage.CapabilityPatch{DisplayName: &dn})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.DisplayName != "Projector Display" {
		t.Fatalf("update display_name = %q, want Projector Display", upd.DisplayName)
	}

	// Official rows are read-only.
	if err := gw.UpsertCapability(ctx, storage.Capability{ID: "official-cap", DisplayName: "Official Cap", Official: true}); err != nil {
		t.Fatalf("upsert official: %v", err)
	}
	if _, err := gw.UpdateCapability(ctx, "", "official-cap", storage.CapabilityPatch{DisplayName: &dn}); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("update official err = %v, want ErrTypeOfficial", err)
	}
	if err := gw.DeleteCapability(ctx, "", "official-cap"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrTypeOfficial", err)
	}

	// Unknown id is ErrTypeNotFound.
	if _, err := gw.GetCapability(ctx, "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get unknown err = %v, want ErrTypeNotFound", err)
	}
	if err := gw.DeleteCapability(ctx, "", "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrTypeNotFound", err)
	}

	// Delete a custom row, then confirm it is gone.
	if err := gw.DeleteCapability(ctx, "", "projector"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := gw.GetCapability(ctx, "projector"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get after delete err = %v, want ErrTypeNotFound", err)
	}
}
