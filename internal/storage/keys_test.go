package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestKeyCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Create a custom key.
	k, err := gw.CreateKey(ctx, "", storage.KeySpec{
		Name: "rack_unit", DataType: "int", DisplayName: "Rack unit",
		Validation: json.RawMessage(`{"minimum":1,"maximum":48}`), Description: "U position.",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if k.Official {
		t.Fatalf("new key official=true, want false")
	}

	// Get it back.
	got, err := gw.GetKey(ctx, "rack_unit")
	if err != nil || got.DataType != "int" || got.DisplayName != "Rack unit" {
		t.Fatalf("get: %v (%+v)", err, got)
	}

	// List includes the custom key and the seeded official ones.
	keys, err := gw.ListKeys(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	names := map[string]bool{}
	for _, kk := range keys {
		names[kk.Name] = true
	}
	if !names["rack_unit"] || !names["serial_number"] || !names["icmp.reachable"] {
		t.Fatalf("list missing keys: %v", names)
	}

	// Update a mutable field.
	label := "Rack Unit (U)"
	if _, err := gw.UpdateKey(ctx, "", "rack_unit", storage.KeyPatch{DisplayName: &label}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got, _ := gw.GetKey(ctx, "rack_unit"); got.DisplayName != label {
		t.Fatalf("update not applied: %q", got.DisplayName)
	}

	// A duplicate name is ErrKeyExists.
	if _, err := gw.CreateKey(ctx, "", storage.KeySpec{Name: "rack_unit", DataType: "int"}); !errors.Is(err, storage.ErrKeyExists) {
		t.Fatalf("dup err = %v, want ErrKeyExists", err)
	}

	// A malformed name is ErrKeyInvalid.
	if _, err := gw.CreateKey(ctx, "", storage.KeySpec{Name: "Bad-Name", DataType: "string"}); !errors.Is(err, storage.ErrKeyInvalid) {
		t.Fatalf("bad name err = %v, want ErrKeyInvalid", err)
	}

	// Official (seeded) keys are read-only.
	if _, err := gw.UpdateKey(ctx, "", "serial_number", storage.KeyPatch{DisplayName: &label}); !errors.Is(err, storage.ErrKeyOfficial) {
		t.Fatalf("update official err = %v, want ErrKeyOfficial", err)
	}
	if err := gw.DeleteKey(ctx, "", "serial_number"); !errors.Is(err, storage.ErrKeyOfficial) {
		t.Fatalf("delete official err = %v, want ErrKeyOfficial", err)
	}

	// An unknown key is ErrKeyNotFound.
	if err := gw.DeleteKey(ctx, "", "nope"); !errors.Is(err, storage.ErrKeyNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrKeyNotFound", err)
	}
	if _, err := gw.GetKey(ctx, "nope"); !errors.Is(err, storage.ErrKeyNotFound) {
		t.Fatalf("get unknown err = %v, want ErrKeyNotFound", err)
	}

	// Delete the custom key; a re-delete is ErrKeyNotFound.
	if err := gw.DeleteKey(ctx, "", "rack_unit"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := gw.DeleteKey(ctx, "", "rack_unit"); !errors.Is(err, storage.ErrKeyNotFound) {
		t.Fatalf("re-delete err = %v, want ErrKeyNotFound", err)
	}
}
