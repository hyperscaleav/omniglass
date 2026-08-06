package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestCatalogSchemaBareRequiredRefused drives the bare-required refusal (#595)
// through every schema-bearing catalog write: a schema whose required list
// names a property absent from its properties block is silently unenforced by
// the validator (huma checks required only over declared properties), so a
// catalog that stores it stores a schema that lies. Create and update on each
// of the three catalogs must refuse it through the catalog's invalid sentinel,
// naming the offending property. Before this slice all six sites accepted such
// a schema with only a well-formed-JSON check.
func TestCatalogSchemaBareRequiredRefused(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

	// The gap and its fix: required names "serial", properties declares nothing.
	bare := []byte(`{"type":"object","required":["serial"]}`)
	// The same required list with the property declared is a schema that means
	// what it says, so it must stay accepted.
	honest := []byte(`{"type":"object","required":["serial"],"properties":{"serial":{"type":"string"}}}`)

	t.Run("property_type create refuses, naming the property", func(t *testing.T) {
		_, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{
			Name: "bare-req-prop", DataType: "json", Validation: bare,
		})
		if !errors.Is(err, storage.ErrPropertyTypeInvalid) {
			t.Fatalf("create with bare required = %v, want ErrPropertyTypeInvalid", err)
		}
		if !strings.Contains(err.Error(), "serial") {
			t.Errorf("error %q does not name the offending property", err)
		}
	})

	t.Run("property_type update refuses, naming the property", func(t *testing.T) {
		if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{
			Name: "honest-prop", DataType: "json", Validation: honest,
		}); err != nil {
			t.Fatalf("create with an honest schema: %v", err)
		}
		_, err := gw.UpdatePropertyType(ctx, "", "honest-prop", storage.PropertyTypePatch{Validation: bare})
		if !errors.Is(err, storage.ErrPropertyTypeInvalid) {
			t.Fatalf("update with bare required = %v, want ErrPropertyTypeInvalid", err)
		}
		if !strings.Contains(err.Error(), "serial") {
			t.Errorf("error %q does not name the offending property", err)
		}
	})

	t.Run("event_type create refuses, naming the property", func(t *testing.T) {
		_, err := gw.CreateEventType(ctx, "", storage.EventTypeSpec{
			Name: "bare-req-event", PayloadSchema: bare,
		})
		if !errors.Is(err, storage.ErrEventTypeInvalid) {
			t.Fatalf("create with bare required = %v, want ErrEventTypeInvalid", err)
		}
		if !strings.Contains(err.Error(), "serial") {
			t.Errorf("error %q does not name the offending property", err)
		}
	})

	t.Run("event_type update refuses, naming the property", func(t *testing.T) {
		if _, err := gw.CreateEventType(ctx, "", storage.EventTypeSpec{
			Name: "honest-event", PayloadSchema: honest,
		}); err != nil {
			t.Fatalf("create with an honest schema: %v", err)
		}
		_, err := gw.UpdateEventType(ctx, "", "honest-event", storage.EventTypePatch{PayloadSchema: bare})
		if !errors.Is(err, storage.ErrEventTypeInvalid) {
			t.Fatalf("update with bare required = %v, want ErrEventTypeInvalid", err)
		}
		if !strings.Contains(err.Error(), "serial") {
			t.Errorf("error %q does not name the offending property", err)
		}
	})

	t.Run("command_type create refuses, naming the property", func(t *testing.T) {
		_, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
			Name: "bare-req-cmd", ParamsSchema: bare,
		})
		if !errors.Is(err, storage.ErrCommandTypeInvalid) {
			t.Fatalf("create with bare required = %v, want ErrCommandTypeInvalid", err)
		}
		if !strings.Contains(err.Error(), "serial") {
			t.Errorf("error %q does not name the offending property", err)
		}
	})

	t.Run("command_type update refuses, naming the property", func(t *testing.T) {
		if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
			Name: "honest-cmd", ParamsSchema: honest,
		}); err != nil {
			t.Fatalf("create with an honest schema: %v", err)
		}
		_, err := gw.UpdateCommandType(ctx, "", "honest-cmd", storage.CommandTypePatch{ParamsSchema: bare})
		if !errors.Is(err, storage.ErrCommandTypeInvalid) {
			t.Fatalf("update with bare required = %v, want ErrCommandTypeInvalid", err)
		}
		if !strings.Contains(err.Error(), "serial") {
			t.Errorf("error %q does not name the offending property", err)
		}
	})

	t.Run("the honest schema stays accepted on every catalog", func(t *testing.T) {
		if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{
			Name: "honest-prop-2", DataType: "json", Validation: honest,
		}); err != nil {
			t.Errorf("property create with an honest schema: %v", err)
		}
		if _, err := gw.CreateEventType(ctx, "", storage.EventTypeSpec{
			Name: "honest-event-2", PayloadSchema: honest,
		}); err != nil {
			t.Errorf("event create with an honest schema: %v", err)
		}
		if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
			Name: "honest-cmd-2", ParamsSchema: honest,
		}); err != nil {
			t.Errorf("command create with an honest schema: %v", err)
		}
	})
}
