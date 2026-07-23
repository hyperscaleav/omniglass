package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestDriverCRUD(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Create a custom driver; it is official=false.
	d, err := gw.CreateDriver(ctx, "", storage.Driver{
		Name: "acme-agent", DisplayName: "Acme Agent", Version: "2.0.0",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if d.Name != "acme-agent" {
		t.Fatalf("create name = %q, want acme-agent", d.Name)
	}
	if d.Official {
		t.Fatalf("new driver official=true, want false")
	}

	// Duplicate id is ErrTypeExists.
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "acme-agent", DisplayName: "Dup"}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}

	// Get + list.
	got, err := gw.GetDriver(ctx, "acme-agent")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DisplayName != "Acme Agent" {
		t.Fatalf("get display_name = %q, want Acme Agent", got.DisplayName)
	}
	all, err := gw.ListDrivers(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found *storage.Driver
	for i := range all {
		if all[i].Name == "acme-agent" {
			found = &all[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("list does not contain acme-agent; got %d rows", len(all))
	}
	if found.DisplayName != "Acme Agent" {
		t.Fatalf("list acme-agent display_name = %q, want Acme Agent", found.DisplayName)
	}
	if found.Official {
		t.Fatalf("list acme-agent official=true, want false")
	}

	// Update patch (display name); version unchanged when omitted.
	dn := "Acme Agent Pro"
	upd, err := gw.UpdateDriver(ctx, "", "acme-agent", storage.DriverPatch{DisplayName: &dn})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.DisplayName != "Acme Agent Pro" {
		t.Fatalf("update display_name = %q, want Acme Agent Pro", upd.DisplayName)
	}
	if upd.Version != "2.0.0" {
		t.Fatalf("update version = %q, want unchanged 2.0.0", upd.Version)
	}

	// Official rows are read-only.
	if err := gw.UpsertDriver(ctx, storage.Driver{Name: "official-driver", DisplayName: "Official Driver", Official: true}); err != nil {
		t.Fatalf("upsert official: %v", err)
	}
	if _, err := gw.UpdateDriver(ctx, "", "official-driver", storage.DriverPatch{DisplayName: &dn}); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("update official err = %v, want ErrTypeOfficial", err)
	}
	if err := gw.DeleteDriver(ctx, "", "official-driver"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrTypeOfficial", err)
	}

	// Unknown id is ErrTypeNotFound.
	if _, err := gw.GetDriver(ctx, "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get unknown err = %v, want ErrTypeNotFound", err)
	}
	if err := gw.DeleteDriver(ctx, "", "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrTypeNotFound", err)
	}

	// Delete a custom row, then confirm it is gone.
	if err := gw.DeleteDriver(ctx, "", "acme-agent"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := gw.GetDriver(ctx, "acme-agent"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get after delete err = %v, want ErrTypeNotFound", err)
	}
}
