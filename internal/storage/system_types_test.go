package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestSystemTypeCRUD(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	st, err := gw.CreateSystemType(ctx, "", storage.SystemType{ID: "kiosk", DisplayName: "Kiosk", Rank: 15})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if st.Official {
		t.Fatalf("new type official=true, want false")
	}
	if _, err := gw.CreateSystemType(ctx, "", storage.SystemType{ID: "kiosk", DisplayName: "Dup"}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}
	name := "Info Kiosk"
	if _, err := gw.UpdateSystemType(ctx, "", "kiosk", storage.SystemTypePatch{DisplayName: &name}); err != nil {
		t.Fatalf("update: %v", err)
	}
	// Place a system of type kiosk, delete refused (in use).
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "k1", SystemType: "kiosk"}, all); err != nil {
		t.Fatalf("create system: %v", err)
	}
	if err := gw.DeleteSystemType(ctx, "", "kiosk"); !errors.Is(err, storage.ErrTypeInUse) {
		t.Fatalf("delete in-use err = %v, want ErrTypeInUse", err)
	}

	// Official rows are read-only.
	if _, err := gw.UpdateSystemType(ctx, "", "meeting-room", storage.SystemTypePatch{DisplayName: &name}); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("update official err = %v, want ErrTypeOfficial", err)
	}
	if err := gw.DeleteSystemType(ctx, "", "meeting-room"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrTypeOfficial", err)
	}

	// Unknown id is ErrTypeNotFound.
	if err := gw.DeleteSystemType(ctx, "", "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrTypeNotFound", err)
	}
}
