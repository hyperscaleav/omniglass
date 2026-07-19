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

	// "display" is an official seeded component_type. A display_name is optional
	// and presentation-only; the raw name stays the unique key.
	fd, err := gw.CreateFieldDefinition(ctx, "", storage.FieldDefinitionSpec{
		ComponentType: "display",
		Name:          "asset_tag",
		DisplayName:   "Asset tag",
		DataType:      "string",
		DefaultValue:  nil,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if fd.ID == "" || fd.Name != "asset_tag" || fd.DisplayName != "Asset tag" || fd.ComponentType != "display" {
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

	// update the data_type, default, and display_name.
	def := json.RawMessage(`"unknown"`)
	up, err := gw.UpdateFieldDefinition(ctx, "", fd.ID, "string", "Asset Tag (label)", def)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if string(up.DefaultValue) != `"unknown"` || up.DisplayName != "Asset Tag (label)" {
		t.Fatalf("update did not apply: %+v", up)
	}

	// the same validation gates update: a mismatched default is refused.
	if _, err := gw.UpdateFieldDefinition(ctx, "", fd.ID, "int", "", json.RawMessage(`"nope"`)); !errors.Is(err, storage.ErrInvalidValue) {
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
		ComponentType: "display", Name: "diagonal_inches", DataType: "int",
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

	// Before any value is set, the effective value is the default, and ValueID is
	// empty (nothing to clear).
	eff, err := gw.EffectiveFields(ctx, "lobby-display", all)
	if err != nil {
		t.Fatalf("effective: %v", err)
	}
	if len(eff) != 1 || eff[0].IsSet || string(eff[0].Value) != `50` {
		t.Fatalf("want default 50 unset, got %+v", eff)
	}
	if eff[0].ValueID != "" {
		t.Fatalf("want empty ValueID when unset, got %q", eff[0].ValueID)
	}

	// Set an override on the component.
	fv, err := gw.SetFieldValue(ctx, "", "lobby-display", "diagonal_inches", json.RawMessage(`80`), all)
	if err != nil {
		t.Fatalf("set value: %v", err)
	}
	eff, _ = gw.EffectiveFields(ctx, "lobby-display", all)
	if !eff[0].IsSet || string(eff[0].Value) != `80` || string(eff[0].SetValue) != `80` {
		t.Fatalf("want set 80, got %+v", eff)
	}
	// ValueID carries the field_value id, so the surface can clear the override
	// back to the type default.
	if eff[0].ValueID != fv.ID {
		t.Fatalf("want ValueID %q, got %q", fv.ID, eff[0].ValueID)
	}

	// A value that does not match the field's data_type is rejected (int field,
	// string value).
	if _, err := gw.SetFieldValue(ctx, "", "lobby-display", "diagonal_inches", json.RawMessage(`"bright"`), all); !errors.Is(err, storage.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue, got %v", err)
	}

	// A field defined on "display" cannot be set on a "camera" component.
	if _, err := gw.SetFieldValue(ctx, "", "lobby-cam", "diagonal_inches", json.RawMessage(`10`), all); !errors.Is(err, storage.ErrFieldNotApplicable) {
		t.Fatalf("want ErrFieldNotApplicable, got %v", err)
	}

	// A camera has no display fields, so its effective set is empty.
	camEff, _ := gw.EffectiveFields(ctx, "lobby-cam", all)
	if len(camEff) != 0 {
		t.Fatalf("want no fields on camera, got %+v", camEff)
	}
	_ = fd
}

// TestSetFieldValueUpsert covers the idempotent set: the first set creates, a
// second set with a different value patches it in place (no conflict, same id),
// and a set with the unchanged value is a no-op that writes no audit row.
func TestSetFieldValueUpsert(t *testing.T) {
	gw := fieldGateway(t)
	ctx := context.Background()

	if _, err := gw.CreateFieldDefinition(ctx, "", storage.FieldDefinitionSpec{
		ComponentType: "display", Name: "diagonal_inches", DataType: "int",
		DefaultValue: json.RawMessage(`50`),
	}); err != nil {
		t.Fatalf("define: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "lobby-display", ComponentType: "display",
	}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}

	// First set creates the value.
	fv1, err := gw.SetFieldValue(ctx, "", "lobby-display", "diagonal_inches", json.RawMessage(`80`), all)
	if err != nil {
		t.Fatalf("first set: %v", err)
	}
	// A second set with a different value patches in place: no conflict, same row id.
	fv2, err := gw.SetFieldValue(ctx, "", "lobby-display", "diagonal_inches", json.RawMessage(`90`), all)
	if err != nil {
		t.Fatalf("second set (upsert) should patch, got: %v", err)
	}
	if fv2.ID != fv1.ID {
		t.Fatalf("upsert changed the row id: %q -> %q", fv1.ID, fv2.ID)
	}
	if eff, _ := gw.EffectiveFields(ctx, "lobby-display", all); string(eff[0].Value) != `90` || !eff[0].IsSet {
		t.Fatalf("want effective 90 set after upsert, got %+v", eff)
	}

	// The audit records a create then an update for this field_value, not two creates.
	if verbs := fieldValueAuditVerbs(t, gw, fv1.ID); len(verbs) != 2 || verbs[0] != "create" || verbs[1] != "update" {
		t.Fatalf("want audit [create update], got %v", verbs)
	}

	// A set with the unchanged value is a no-op: no third audit row.
	if _, err := gw.SetFieldValue(ctx, "", "lobby-display", "diagonal_inches", json.RawMessage(`90`), all); err != nil {
		t.Fatalf("no-op set: %v", err)
	}
	if verbs := fieldValueAuditVerbs(t, gw, fv1.ID); len(verbs) != 2 {
		t.Fatalf("no-op set must not audit, got %v", verbs)
	}

	// The set still validates against the field's data_type.
	if _, err := gw.SetFieldValue(ctx, "", "lobby-display", "diagonal_inches", json.RawMessage(`"bright"`), all); !errors.Is(err, storage.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue, got %v", err)
	}
}

// fieldValueAuditVerbs returns the audit verbs recorded for a field_value id, in
// chronological order (ListAuditLog is newest-first).
func fieldValueAuditVerbs(t *testing.T, gw storage.Gateway, id string) []string {
	t.Helper()
	entries, err := gw.ListAuditLog(context.Background(), storage.AuditFilter{Resource: "field_value", Limit: 500})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var verbs []string
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].ResourceID == id {
			verbs = append(verbs, entries[i].Verb)
		}
	}
	return verbs
}

// TestFieldValueUpdateDelete covers the mutation half: an update revalidates
// against the field's fixed data_type and moves the effective value, a delete
// reverts the component to the definition's default, and a second delete on the
// same id is the non-disclosing not-found.
func TestFieldValueUpdateDelete(t *testing.T) {
	gw := fieldGateway(t)
	ctx := context.Background()

	// diagonal_inches:int default 50 on "display", set on a fresh display component.
	if _, err := gw.CreateFieldDefinition(ctx, "", storage.FieldDefinitionSpec{
		ComponentType: "display", Name: "diagonal_inches", DataType: "int",
		DefaultValue: json.RawMessage(`50`),
	}); err != nil {
		t.Fatalf("define: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "lobby-display", ComponentType: "display",
	}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	fv, err := gw.SetFieldValue(ctx, "", "lobby-display", "diagonal_inches", json.RawMessage(`80`), all)
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
