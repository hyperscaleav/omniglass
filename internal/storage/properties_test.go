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

func TestPropertyCRUD(t *testing.T) {
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

	// Create a custom property.
	prop, err := gw.CreateProperty(ctx, "", storage.PropertySpec{
		Name: "rack_unit", DataType: "int", DisplayName: "Rack unit",
		Validation: json.RawMessage(`{"minimum":1,"maximum":48}`), Description: "U position.",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if prop.Official {
		t.Fatalf("new property official=true, want false")
	}

	// Get it back.
	got, err := gw.GetProperty(ctx, "rack_unit")
	if err != nil || got.DataType != "int" || got.DisplayName != "Rack unit" {
		t.Fatalf("get: %v (%+v)", err, got)
	}

	// List includes the custom property and the seeded official ones.
	props, err := gw.ListProperties(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	names := map[string]bool{}
	for _, pp := range props {
		names[pp.Name] = true
	}
	if !names["rack_unit"] || !names["serial_number"] || !names["icmp.reachable"] {
		t.Fatalf("list missing properties: %v", names)
	}

	// Update a mutable field.
	label := "Rack Unit (U)"
	if _, err := gw.UpdateProperty(ctx, "", "rack_unit", storage.PropertyPatch{DisplayName: &label}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got, _ := gw.GetProperty(ctx, "rack_unit"); got.DisplayName != label {
		t.Fatalf("update not applied: %q", got.DisplayName)
	}

	// A duplicate name is ErrPropertyExists.
	if _, err := gw.CreateProperty(ctx, "", storage.PropertySpec{Name: "rack_unit", DataType: "int"}); !errors.Is(err, storage.ErrPropertyExists) {
		t.Fatalf("dup err = %v, want ErrPropertyExists", err)
	}

	// A malformed name is ErrPropertyInvalid.
	if _, err := gw.CreateProperty(ctx, "", storage.PropertySpec{Name: "Bad-Name", DataType: "string"}); !errors.Is(err, storage.ErrPropertyInvalid) {
		t.Fatalf("bad name err = %v, want ErrPropertyInvalid", err)
	}

	// Official (seeded) properties are read-only.
	if _, err := gw.UpdateProperty(ctx, "", "serial_number", storage.PropertyPatch{DisplayName: &label}); !errors.Is(err, storage.ErrPropertyOfficial) {
		t.Fatalf("update official err = %v, want ErrPropertyOfficial", err)
	}
	if err := gw.DeleteProperty(ctx, "", "serial_number"); !errors.Is(err, storage.ErrPropertyOfficial) {
		t.Fatalf("delete official err = %v, want ErrPropertyOfficial", err)
	}

	// An unknown property is ErrPropertyNotFound.
	if err := gw.DeleteProperty(ctx, "", "nope"); !errors.Is(err, storage.ErrPropertyNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrPropertyNotFound", err)
	}
	if _, err := gw.GetProperty(ctx, "nope"); !errors.Is(err, storage.ErrPropertyNotFound) {
		t.Fatalf("get unknown err = %v, want ErrPropertyNotFound", err)
	}

	// Delete the custom property; a re-delete is ErrPropertyNotFound.
	if err := gw.DeleteProperty(ctx, "", "rack_unit"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := gw.DeleteProperty(ctx, "", "rack_unit"); !errors.Is(err, storage.ErrPropertyNotFound) {
		t.Fatalf("re-delete err = %v, want ErrPropertyNotFound", err)
	}
}
