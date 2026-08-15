package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestVendorCRUD(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Create a custom vendor; it is official=false and round-trips its kind.
	m, err := gw.CreateVendor(ctx, "", storage.Vendor{
		Name: "acme", Label: "Acme", Kind: "integrator", Website: "https://acme.example",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.Name != "acme" {
		t.Fatalf("create name = %q, want acme", m.ID)
	}
	if m.Kind != "integrator" {
		t.Fatalf("create kind = %q, want integrator", m.Kind)
	}
	if m.Official {
		t.Fatalf("new vendor official=true, want false")
	}

	// Duplicate id is ErrTypeExists.
	if _, err := gw.CreateVendor(ctx, "", storage.Vendor{Name: "acme", Label: "Dup", Kind: "manufacturer"}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}

	// Get + list.
	got, err := gw.GetVendor(ctx, "acme")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Label != "Acme" {
		t.Fatalf("get label = %q, want Acme", got.Label)
	}
	if got.Kind != "integrator" {
		t.Fatalf("get kind = %q, want integrator", got.Kind)
	}
	all, err := gw.ListVendors(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found *storage.Vendor
	for i := range all {
		if all[i].Name == "acme" {
			found = &all[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("list does not contain acme; got %d rows", len(all))
	}
	if found.Label != "Acme" {
		t.Fatalf("list acme label = %q, want Acme", found.Label)
	}
	if found.Kind != "integrator" {
		t.Fatalf("list acme kind = %q, want integrator", found.Kind)
	}
	if found.Official {
		t.Fatalf("list acme official=true, want false")
	}

	// Update patch (label + support phone + kind); icon/website unchanged when omitted.
	dn, ph, kd := "Acme Inc.", "+1-555-0100", "developer"
	upd, err := gw.UpdateVendor(ctx, "", "acme", storage.VendorPatch{Label: &dn, SupportPhone: &ph, Kind: &kd})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Label != "Acme Inc." {
		t.Fatalf("update label = %q, want Acme Inc.", upd.Label)
	}
	if upd.SupportPhone != "+1-555-0100" {
		t.Fatalf("update support_phone = %q, want +1-555-0100", upd.SupportPhone)
	}
	if upd.Kind != "developer" {
		t.Fatalf("update kind = %q, want developer", upd.Kind)
	}
	if upd.Website != "https://acme.example" {
		t.Fatalf("update website = %q, want unchanged https://acme.example", upd.Website)
	}

	// Official rows are read-only.
	if err := gw.UpsertVendor(ctx, storage.Vendor{Name: "official-co", Label: "Official Co", Kind: "manufacturer", Official: true}); err != nil {
		t.Fatalf("upsert official: %v", err)
	}
	if _, err := gw.UpdateVendor(ctx, "", "official-co", storage.VendorPatch{Label: &dn}); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("update official err = %v, want ErrTypeOfficial", err)
	}
	if err := gw.DeleteVendor(ctx, "", "official-co"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrTypeOfficial", err)
	}

	// Unknown id is ErrTypeNotFound.
	if _, err := gw.GetVendor(ctx, "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get unknown err = %v, want ErrTypeNotFound", err)
	}
	if err := gw.DeleteVendor(ctx, "", "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrTypeNotFound", err)
	}

	// Delete a custom row, then confirm it is gone.
	if err := gw.DeleteVendor(ctx, "", "acme"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := gw.GetVendor(ctx, "acme"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get after delete err = %v, want ErrTypeNotFound", err)
	}
}
