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
