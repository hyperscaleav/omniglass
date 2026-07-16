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

// fieldGateway opens a plain Gateway and seeds the reference data (field
// definitions reference the official component_type registry).
func fieldGateway(t *testing.T) storage.Gateway {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return gw
}

func TestFieldDefinitionCRUD(t *testing.T) {
	gw := fieldGateway(t)
	ctx := context.Background()

	// "display" is an official seeded component_type.
	fd, err := gw.CreateFieldDefinition(ctx, "", storage.FieldDefinitionSpec{
		ComponentType: "display",
		Name:          "asset_tag",
		DataType:      "string",
		DefaultValue:  nil,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if fd.ID == "" || fd.Name != "asset_tag" || fd.ComponentType != "display" {
		t.Fatalf("unexpected definition: %+v", fd)
	}

	// unknown component_type is rejected (FK).
	if _, err := gw.CreateFieldDefinition(ctx, "", storage.FieldDefinitionSpec{
		ComponentType: "nope", Name: "x", DataType: "string",
	}); !errors.Is(err, storage.ErrUnknownComponentType) {
		t.Fatalf("want ErrUnknownComponentType, got %v", err)
	}

	// duplicate (component_type, name) conflicts.
	if _, err := gw.CreateFieldDefinition(ctx, "", storage.FieldDefinitionSpec{
		ComponentType: "display", Name: "asset_tag", DataType: "string",
	}); !errors.Is(err, storage.ErrFieldDefinitionConflict) {
		t.Fatalf("want ErrFieldDefinitionConflict, got %v", err)
	}

	// a default that does not satisfy the declared data_type is refused on create
	// (an int field cannot default to a JSON string).
	if _, err := gw.CreateFieldDefinition(ctx, "", storage.FieldDefinitionSpec{
		ComponentType: "display", Name: "bad_default", DataType: "int",
		DefaultValue: json.RawMessage(`"not-an-int"`),
	}); !errors.Is(err, storage.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue on create, got %v", err)
	}

	// update the data_type + default.
	def := json.RawMessage(`"unknown"`)
	up, err := gw.UpdateFieldDefinition(ctx, "", fd.ID, "string", def)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if string(up.DefaultValue) != `"unknown"` {
		t.Fatalf("default not updated: %s", up.DefaultValue)
	}

	// the same validation gates update: a mismatched default is refused.
	if _, err := gw.UpdateFieldDefinition(ctx, "", fd.ID, "int", json.RawMessage(`"nope"`)); !errors.Is(err, storage.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue on update, got %v", err)
	}

	list, err := gw.ListFieldDefinitions(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}

	if err := gw.DeleteFieldDefinition(ctx, "", fd.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := gw.DeleteFieldDefinition(ctx, "", fd.ID); !errors.Is(err, storage.ErrFieldDefinitionNotFound) {
		t.Fatalf("want ErrFieldDefinitionNotFound, got %v", err)
	}
}

// TestFieldValueEffective covers the value half: a component sets a literal for a
// field defined on its type, the effective read coalesces set-value-or-default,
// the value is type-checked against the field's data_type, and a field not on the
// component's own type is not applicable.
func TestFieldValueEffective(t *testing.T) {
	gw := fieldGateway(t)
	ctx := context.Background()

	// A field on the "display" type with a default.
	fd, err := gw.CreateFieldDefinition(ctx, "", storage.FieldDefinitionSpec{
		ComponentType: "display", Name: "brightness", DataType: "int",
		DefaultValue: json.RawMessage(`50`),
	})
	if err != nil {
		t.Fatalf("define: %v", err)
	}

	// A "display" component and a "camera" component ("display"/"camera" are
	// official seeded component_type ids; a root component needs an all create
	// scope, which `all` provides).
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "lobby-display", ComponentType: "display",
	}, all); err != nil {
		t.Fatalf("create display component: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "lobby-cam", ComponentType: "camera",
	}, all); err != nil {
		t.Fatalf("create camera component: %v", err)
	}

	// Before any value is set, the effective value is the default.
	eff, err := gw.EffectiveFields(ctx, "lobby-display", all)
	if err != nil {
		t.Fatalf("effective: %v", err)
	}
	if len(eff) != 1 || eff[0].IsSet || string(eff[0].Value) != `50` {
		t.Fatalf("want default 50 unset, got %+v", eff)
	}

	// Set an override on the component.
	if _, err := gw.CreateFieldValue(ctx, "", "lobby-display", "brightness", json.RawMessage(`80`), all); err != nil {
		t.Fatalf("set value: %v", err)
	}
	eff, _ = gw.EffectiveFields(ctx, "lobby-display", all)
	if !eff[0].IsSet || string(eff[0].Value) != `80` || string(eff[0].SetValue) != `80` {
		t.Fatalf("want set 80, got %+v", eff)
	}

	// A value that does not match the field's data_type is rejected (int field,
	// string value).
	if _, err := gw.CreateFieldValue(ctx, "", "lobby-display", "brightness", json.RawMessage(`"bright"`), all); !errors.Is(err, storage.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue, got %v", err)
	}

	// A field defined on "display" cannot be set on a "camera" component.
	if _, err := gw.CreateFieldValue(ctx, "", "lobby-cam", "brightness", json.RawMessage(`10`), all); !errors.Is(err, storage.ErrFieldNotApplicable) {
		t.Fatalf("want ErrFieldNotApplicable, got %v", err)
	}

	// A camera has no display fields, so its effective set is empty.
	camEff, _ := gw.EffectiveFields(ctx, "lobby-cam", all)
	if len(camEff) != 0 {
		t.Fatalf("want no fields on camera, got %+v", camEff)
	}
	_ = fd
}

// TestFieldValueUpdateDelete covers the mutation half: an update revalidates
// against the field's fixed data_type and moves the effective value, a delete
// reverts the component to the definition's default, and a second delete on the
// same id is the non-disclosing not-found.
func TestFieldValueUpdateDelete(t *testing.T) {
	gw := fieldGateway(t)
	ctx := context.Background()

	// brightness:int default 50 on "display", set on a fresh display component.
	if _, err := gw.CreateFieldDefinition(ctx, "", storage.FieldDefinitionSpec{
		ComponentType: "display", Name: "brightness", DataType: "int",
		DefaultValue: json.RawMessage(`50`),
	}); err != nil {
		t.Fatalf("define: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "lobby-display", ComponentType: "display",
	}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	fv, err := gw.CreateFieldValue(ctx, "", "lobby-display", "brightness", json.RawMessage(`80`), all)
	if err != nil {
		t.Fatalf("set value: %v", err)
	}

	// Update moves the effective value.
	if _, err := gw.UpdateFieldValue(ctx, "", fv.ID, json.RawMessage(`90`), all, all); err != nil {
		t.Fatalf("update: %v", err)
	}
	eff, _ := gw.EffectiveFields(ctx, "lobby-display", all)
	if len(eff) != 1 || !eff[0].IsSet || string(eff[0].Value) != `90` {
		t.Fatalf("want set 90, got %+v", eff)
	}

	// Update revalidates against the field's fixed data_type (int field, string value).
	if _, err := gw.UpdateFieldValue(ctx, "", fv.ID, json.RawMessage(`"bad"`), all, all); !errors.Is(err, storage.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue on update, got %v", err)
	}

	// Delete reverts the component to the definition's default.
	if err := gw.DeleteFieldValue(ctx, "", fv.ID, all, all); err != nil {
		t.Fatalf("delete: %v", err)
	}
	eff, _ = gw.EffectiveFields(ctx, "lobby-display", all)
	if len(eff) != 1 || eff[0].IsSet || string(eff[0].Value) != `50` {
		t.Fatalf("want default 50 unset after delete, got %+v", eff)
	}

	// A second delete on the same id is the non-disclosing not-found.
	if err := gw.DeleteFieldValue(ctx, "", fv.ID, all, all); !errors.Is(err, storage.ErrFieldValueNotFound) {
		t.Fatalf("want ErrFieldValueNotFound, got %v", err)
	}
}
