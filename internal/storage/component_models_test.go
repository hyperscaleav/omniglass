package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func containsModel(all []storage.ComponentModel, id string) bool {
	for _, m := range all {
		if m.ID == id {
			return true
		}
	}
	return false
}

func TestComponentModelCRUD(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Create referencing a seeded make.
	m, err := gw.CreateComponentModel(ctx, "", storage.ComponentModel{
		ID: "acme-123a", DisplayName: "Acme 123A", MakeID: "crestron", ModelNumber: "123A", Family: "1xx-series",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.MakeID != "crestron" {
		t.Fatalf("create make_id = %q, want crestron", m.MakeID)
	}
	if m.Official {
		t.Fatalf("new model official=true, want false")
	}

	// Create with a nonexistent make: the FK rejects it.
	if _, err := gw.CreateComponentModel(ctx, "", storage.ComponentModel{
		ID: "bad", DisplayName: "Bad", MakeID: "nope", ModelNumber: "X",
	}); err == nil {
		t.Fatalf("create with unknown make_id: want error, got nil")
	}

	// Get + list contains our model.
	got, err := gw.GetComponentModel(ctx, "acme-123a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DisplayName != "Acme 123A" {
		t.Fatalf("get display_name = %q, want Acme 123A", got.DisplayName)
	}
	all, err := gw.ListComponentModels(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !containsModel(all, "acme-123a") {
		t.Fatalf("list does not contain acme-123a; got %d rows", len(all))
	}

	// Patch (family); other fields unchanged.
	fam := "2xx-series"
	upd, err := gw.UpdateComponentModel(ctx, "", "acme-123a", storage.ComponentModelPatch{Family: &fam})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Family != "2xx-series" {
		t.Fatalf("update family = %q, want 2xx-series", upd.Family)
	}
	if upd.ModelNumber != "123A" {
		t.Fatalf("update model_number = %q, want unchanged 123A", upd.ModelNumber)
	}

	// Official rows are read-only.
	if err := gw.UpsertComponentModel(ctx, storage.ComponentModel{
		ID: "official-model", DisplayName: "Official Model", MakeID: "crestron", Official: true,
	}); err != nil {
		t.Fatalf("upsert official: %v", err)
	}
	if _, err := gw.UpdateComponentModel(ctx, "", "official-model", storage.ComponentModelPatch{Family: &fam}); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("update official err = %v, want ErrTypeOfficial", err)
	}
	if err := gw.DeleteComponentModel(ctx, "", "official-model"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrTypeOfficial", err)
	}

	// Unknown id is ErrTypeNotFound.
	if _, err := gw.GetComponentModel(ctx, "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get unknown err = %v, want ErrTypeNotFound", err)
	}

	// Delete.
	if err := gw.DeleteComponentModel(ctx, "", "acme-123a"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := gw.GetComponentModel(ctx, "acme-123a"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get after delete err = %v, want ErrTypeNotFound", err)
	}
}
