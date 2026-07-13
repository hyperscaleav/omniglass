package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestComponentTypeCRUD(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	ct, err := gw.CreateComponentType(ctx, "", storage.ComponentType{ID: "relay", DisplayName: "Relay", Rank: 15})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ct.Official {
		t.Fatalf("new type official=true, want false")
	}
	if _, err := gw.CreateComponentType(ctx, "", storage.ComponentType{ID: "relay", DisplayName: "Dup"}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}
	name := "Relay Switch"
	if _, err := gw.UpdateComponentType(ctx, "", "relay", storage.ComponentTypePatch{DisplayName: &name}); err != nil {
		t.Fatalf("update: %v", err)
	}
	// Place a component of type relay, delete refused (in use).
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "r1", ComponentType: "relay"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	if err := gw.DeleteComponentType(ctx, "", "relay"); !errors.Is(err, storage.ErrTypeInUse) {
		t.Fatalf("delete in-use err = %v, want ErrTypeInUse", err)
	}

	// Official rows are read-only.
	if _, err := gw.UpdateComponentType(ctx, "", "display", storage.ComponentTypePatch{DisplayName: &name}); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("update official err = %v, want ErrTypeOfficial", err)
	}
	if err := gw.DeleteComponentType(ctx, "", "display"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrTypeOfficial", err)
	}

	// Unknown id is ErrTypeNotFound.
	if err := gw.DeleteComponentType(ctx, "", "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrTypeNotFound", err)
	}

	// Delete the unused relay component after freeing it from the component row,
	// then confirm the re-delete on an already-removed id is ErrTypeNotFound.
	if err := gw.DeleteComponent(ctx, "", "r1", all, all); err != nil {
		t.Fatalf("delete component: %v", err)
	}
	if err := gw.DeleteComponentType(ctx, "", "relay"); err != nil {
		t.Fatalf("delete unused: %v", err)
	}
	if err := gw.DeleteComponentType(ctx, "", "relay"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("re-delete err = %v, want ErrTypeNotFound", err)
	}
}
