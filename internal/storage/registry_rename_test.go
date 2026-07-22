package storage_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// A registry handle can be renamed with every reference intact. This is the whole
// point of giving the registries uuid primary keys, and it is the test each slice
// of the epic writes first.
//
// Before this, a product id was the primary key, so a typo or a rebrand could not
// be corrected: `cisco-room-bar` was permanent, and every referencing row would
// have had to be rewritten to change it.
func TestRegistryHandleRenameKeepsReferences(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn,
		storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	if _, err := gw.CreateVendor(ctx, "", storage.Vendor{Name: "acme", DisplayName: "Acme", Kind: "manufacturer"}); err != nil {
		t.Fatalf("vendor: %v", err)
	}
	// A product referencing that vendor, a sub-product referencing the product, a
	// property contract on it, and a component instance of it: one of every
	// inbound reference this slice converted.
	if _, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "acme-bar", DisplayName: "Acme Bar", Kind: "device",
		VendorID: strptr("acme"), Capabilities: []string{"microphone"}}); err != nil {
		t.Fatalf("product: %v", err)
	}
	if _, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "acme-bar-mini", DisplayName: "Acme Bar Mini", Kind: "device",
		ParentProductID: strptr("acme-bar")}); err != nil {
		t.Fatalf("sub-product: %v", err)
	}
	if _, err := gw.SetProductProperty(ctx, "", "acme-bar", storage.ProductPropertySpec{
		PropertyName: "serial_number", Required: true}); err != nil {
		t.Fatalf("property: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "bar-1", ProductName: strptr("acme-bar")}, all); err != nil {
		t.Fatalf("component: %v", err)
	}

	// The rename. Nothing else moves.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `update product set name = 'acme-soundbar' where name = 'acme-bar'`); err != nil {
		t.Fatalf("rename product: %v", err)
	}
	if _, err := conn.Exec(ctx, `update vendor set name = 'acme-av' where name = 'acme'`); err != nil {
		t.Fatalf("rename vendor: %v", err)
	}

	// Every reference still resolves, and now reads the NEW handle.
	got, err := gw.GetProduct(ctx, "acme-soundbar")
	if err != nil {
		t.Fatalf("get renamed product: %v", err)
	}
	if got.VendorName == nil || *got.VendorName != "acme-av" {
		t.Errorf("vendor reads %v, want acme-av: the arc should follow the rename", got.VendorName)
	}
	if len(got.Capabilities) != 1 || got.Capabilities[0] != "microphone" {
		t.Errorf("capabilities = %v, want the set intact through the rename", got.Capabilities)
	}
	sub, err := gw.GetProduct(ctx, "acme-bar-mini")
	if err != nil {
		t.Fatalf("get sub-product: %v", err)
	}
	if sub.ParentProductName == nil || *sub.ParentProductName != "acme-soundbar" {
		t.Errorf("parent reads %v, want acme-soundbar", sub.ParentProductName)
	}
	props, err := gw.ListProductProperties(ctx, "acme-soundbar")
	if err != nil {
		t.Fatalf("list properties: %v", err)
	}
	if len(props) != 1 || props[0].PropertyName != "serial_number" {
		t.Errorf("contract = %v, want serial_number still declared", props)
	}
	comp, err := gw.GetComponent(ctx, "bar-1", all)
	if err != nil {
		t.Fatalf("get component: %v", err)
	}
	if comp.ProductHandle == nil || *comp.ProductHandle != "acme-soundbar" {
		t.Errorf("component product reads %v, want acme-soundbar", comp.ProductHandle)
	}
}
