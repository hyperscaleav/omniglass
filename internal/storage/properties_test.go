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
	prop, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{
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
	got, err := gw.GetPropertyType(ctx, "rack_unit")
	if err != nil || got.DataType != "int" || got.DisplayName != "Rack unit" {
		t.Fatalf("get: %v (%+v)", err, got)
	}

	// List includes the custom property and the seeded official ones.
	props, err := gw.ListPropertyTypes(ctx)
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
	if _, err := gw.UpdatePropertyType(ctx, "", "rack_unit", storage.PropertyTypePatch{DisplayName: &label}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got, _ := gw.GetPropertyType(ctx, "rack_unit"); got.DisplayName != label {
		t.Fatalf("update not applied: %q", got.DisplayName)
	}

	// A duplicate name is ErrPropertyExists.
	if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: "rack_unit", DataType: "int"}); !errors.Is(err, storage.ErrPropertyTypeExists) {
		t.Fatalf("dup err = %v, want ErrPropertyExists", err)
	}

	// A malformed name is ErrPropertyInvalid.
	if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: "Bad-Name", DataType: "string"}); !errors.Is(err, storage.ErrPropertyTypeInvalid) {
		t.Fatalf("bad name err = %v, want ErrPropertyInvalid", err)
	}

	// Official (seeded) properties are read-only.
	if _, err := gw.UpdatePropertyType(ctx, "", "serial_number", storage.PropertyTypePatch{DisplayName: &label}); !errors.Is(err, storage.ErrPropertyTypeOfficial) {
		t.Fatalf("update official err = %v, want ErrPropertyOfficial", err)
	}
	if err := gw.DeletePropertyType(ctx, "", "serial_number"); !errors.Is(err, storage.ErrPropertyTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrPropertyOfficial", err)
	}

	// An unknown property is ErrPropertyNotFound.
	if err := gw.DeletePropertyType(ctx, "", "nope"); !errors.Is(err, storage.ErrPropertyTypeNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrPropertyNotFound", err)
	}
	if _, err := gw.GetPropertyType(ctx, "nope"); !errors.Is(err, storage.ErrPropertyTypeNotFound) {
		t.Fatalf("get unknown err = %v, want ErrPropertyNotFound", err)
	}

	// Delete the custom property; a re-delete is ErrPropertyNotFound.
	if err := gw.DeletePropertyType(ctx, "", "rack_unit"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := gw.DeletePropertyType(ctx, "", "rack_unit"); !errors.Is(err, storage.ErrPropertyTypeNotFound) {
		t.Fatalf("re-delete err = %v, want ErrPropertyNotFound", err)
	}
}
