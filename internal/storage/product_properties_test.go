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

func TestProductPropertyCRUD(t *testing.T) {
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

	// A custom (official=false) product owns a mutable contract.
	if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: "acme-widget", DisplayName: "Acme Widget", Kind: "device"}); err != nil {
		t.Fatalf("create product: %v", err)
	}

	// A product with no contract lists empty, not an error.
	props, err := gw.ListProductProperties(ctx, "acme-widget")
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(props) != 0 {
		t.Fatalf("list empty = %d rows, want 0", len(props))
	}

	// Declare a required property with a default.
	pp, err := gw.SetProductProperty(ctx, "", "acme-widget", storage.ProductPropertySpec{
		PropertyTypeName: "serial_number", DefaultValue: json.RawMessage(`"SN-0"`), Required: true,
	})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if pp.ProductName != "acme-widget" || pp.PropertyTypeName != "serial_number" || !pp.Required {
		t.Fatalf("set = %+v, want acme-widget/serial_number required", pp)
	}
	if string(pp.DefaultValue) != `"SN-0"` {
		t.Fatalf("set default = %s, want \"SN-0\"", pp.DefaultValue)
	}
	props, err = gw.ListProductProperties(ctx, "acme-widget")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(props) != 1 || props[0].PropertyTypeName != "serial_number" || !props[0].Required || string(props[0].DefaultValue) != `"SN-0"` {
		t.Fatalf("list = %+v, want one required serial_number defaulting to \"SN-0\"", props)
	}

	// Setting the same property again updates the contract in place rather than
	// adding a second row (the (product, property) pair is the key).
	if _, err := gw.SetProductProperty(ctx, "", "acme-widget", storage.ProductPropertySpec{
		PropertyTypeName: "serial_number", DefaultValue: json.RawMessage(`"SN-1"`), Required: false,
	}); err != nil {
		t.Fatalf("re-set: %v", err)
	}
	props, err = gw.ListProductProperties(ctx, "acme-widget")
	if err != nil {
		t.Fatalf("list after re-set: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("list after re-set = %d rows, want 1", len(props))
	}
	if string(props[0].DefaultValue) != `"SN-1"` || props[0].Required {
		t.Fatalf("re-set = %+v, want default \"SN-1\" required=false", props[0])
	}

	// A contract may declare a property with no default at all; nil round-trips as
	// nil (a SQL NULL), not as the JSON literal null.
	noDefault, err := gw.SetProductProperty(ctx, "", "acme-widget", storage.ProductPropertySpec{PropertyTypeName: "firmware_version"})
	if err != nil {
		t.Fatalf("set without default: %v", err)
	}
	if noDefault.DefaultValue != nil {
		t.Fatalf("set without default = %s, want nil", noDefault.DefaultValue)
	}
	props, err = gw.ListProductProperties(ctx, "acme-widget")
	if err != nil {
		t.Fatalf("list two: %v", err)
	}
	// Ordered by property_type_name: firmware_version before serial_number.
	if len(props) != 2 || props[0].PropertyTypeName != "firmware_version" || props[1].PropertyTypeName != "serial_number" {
		t.Fatalf("list two = %+v, want firmware_version then serial_number", props)
	}
	if props[0].DefaultValue != nil {
		t.Fatalf("list default = %s, want nil", props[0].DefaultValue)
	}

	// An unknown property_type_name is a missing catalog reference, not a silent insert.
	if _, err := gw.SetProductProperty(ctx, "", "acme-widget", storage.ProductPropertySpec{PropertyTypeName: "nope.not_a_property"}); !errors.Is(err, storage.ErrPropertyTypeNotFound) {
		t.Fatalf("unknown property err = %v, want ErrPropertyNotFound", err)
	}

	// Delete clears one declaration; a re-delete is ErrTypeNotFound.
	if err := gw.DeleteProductProperty(ctx, "", "acme-widget", "firmware_version"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := gw.DeleteProductProperty(ctx, "", "acme-widget", "serial_number"); err != nil {
		t.Fatalf("delete second: %v", err)
	}
	props, err = gw.ListProductProperties(ctx, "acme-widget")
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(props) != 0 {
		t.Fatalf("list after delete = %d rows, want 0", len(props))
	}
	if err := gw.DeleteProductProperty(ctx, "", "acme-widget", "serial_number"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("re-delete err = %v, want ErrTypeNotFound", err)
	}

	// A seeded (official) product's contract is seed-owned and read-only.
	if _, err := gw.SetProductProperty(ctx, "", "cisco-room-bar", storage.ProductPropertySpec{PropertyTypeName: "serial_number"}); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("set official err = %v, want ErrTypeOfficial", err)
	}
	if err := gw.DeleteProductProperty(ctx, "", "cisco-room-bar", "serial_number"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrTypeOfficial", err)
	}

	// The boot-seed path writes the official product's contract without the guard
	// and without an audit, and is idempotent.
	if err := gw.UpsertProductProperty(ctx, "cisco-room-bar", storage.ProductPropertySpec{
		PropertyTypeName: "serial_number", DefaultValue: json.RawMessage(`"unset"`), Required: true,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := gw.UpsertProductProperty(ctx, "cisco-room-bar", storage.ProductPropertySpec{
		PropertyTypeName: "serial_number", DefaultValue: json.RawMessage(`"factory"`), Required: false,
	}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	props, err = gw.ListProductProperties(ctx, "cisco-room-bar")
	if err != nil {
		t.Fatalf("list official: %v", err)
	}
	// The boot seed already ships this product a contract, so assert the row this
	// upsert owns rather than the size of the whole list (which grows whenever the
	// seed declares another property).
	var serial *storage.ProductProperty
	for i := range props {
		if props[i].PropertyTypeName == "serial_number" {
			serial = &props[i]
		}
	}
	if serial == nil || string(serial.DefaultValue) != `"factory"` || serial.Required {
		t.Fatalf("upsert = %+v, want serial_number defaulting to \"factory\" required=false", props)
	}

	// An unknown product is ErrTypeNotFound on the audited path, and on the seed
	// path the FK reports the same missing product.
	if _, err := gw.SetProductProperty(ctx, "", "no-such-product", storage.ProductPropertySpec{PropertyTypeName: "serial_number"}); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("set unknown product err = %v, want ErrTypeNotFound", err)
	}
	if err := gw.UpsertProductProperty(ctx, "no-such-product", storage.ProductPropertySpec{PropertyTypeName: "serial_number"}); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("upsert unknown product err = %v, want ErrTypeNotFound", err)
	}
}
