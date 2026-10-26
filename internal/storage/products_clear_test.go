package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestProductPatchClearsANullableField pins #260 on the product, the first
// registry to get it: every nullable column a product patch can write follows
// the house three-state string convention (api.md): an omitted field (nil) is
// unchanged, an explicit "" CLEARS the column back to SQL NULL, and a value
// sets it. Before, vendor_id, driver_id, parent_product_id and icon were written
// with coalesce($n, col), so nothing could clear them: the console's "None"
// reverted on refetch, and an empty icon was stored as ” rather than NULL.
func TestProductPatchClearsANullableField(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "acme-base", Label: "Acme Base", Kind: "device", ComponentType: "video-bar"}); err != nil {
		t.Fatalf("parent: %v", err)
	}
	if _, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "acme-bar", Label: "Acme Bar", Kind: "device", ComponentType: "video-bar",
		VendorID: strptr("kestrel"), DriverID: strptr("kestrel-api"),
		ParentProductID: strptr("acme-base"), Icon: strptr("speaker"), LabelRule: strptr("{{.Name}}"),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// An unrelated edit leaves every nullable field alone (nil is unchanged).
	m, err := gw.UpdateProduct(ctx, "", "acme-bar", storage.ProductPatch{Label: strptr("Acme Bar Pro")})
	if err != nil {
		t.Fatalf("label edit: %v", err)
	}
	if m.VendorID == nil || m.DriverID == nil || m.ParentProductID == nil || m.Icon == nil || m.LabelRule == nil {
		t.Fatalf("a label edit cleared a field it did not name: %+v", m)
	}

	// The clear: an explicit "" on each nullable field.
	m, err = gw.UpdateProduct(ctx, "", "acme-bar", storage.ProductPatch{
		VendorID: strptr(""), DriverID: strptr(""), ParentProductID: strptr(""), Icon: strptr(""),
	})
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if m.VendorID != nil || m.VendorName != nil {
		t.Errorf("vendor after clear = %v/%v, want none", m.VendorID, m.VendorName)
	}
	if m.DriverID != nil || m.DriverName != nil {
		t.Errorf("driver after clear = %v/%v, want none", m.DriverID, m.DriverName)
	}
	if m.ParentProductID != nil || m.ParentProductName != nil {
		t.Errorf("parent after clear = %v/%v, want none", m.ParentProductID, m.ParentProductName)
	}
	if m.Icon != nil {
		t.Errorf("icon after clear = %q, want none (inherit the component_type's)", *m.Icon)
	}
	if m.LabelRule == nil {
		t.Errorf("label_rule cleared by a patch that did not name it")
	}
	// Cleared means SQL NULL, not the empty string: an icon of '' is a value
	// every reader has to undo, and a uuid column cannot hold one at all.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var nulls int
	if err := conn.QueryRow(ctx, `select num_nulls(vendor_id, driver_id, parent_product_id, icon)
		from product where name = 'acme-bar'`).Scan(&nulls); err != nil {
		t.Fatalf("read raw columns: %v", err)
	}
	if nulls != 4 {
		t.Errorf("raw nulls after clear = %d, want all four columns NULL", nulls)
	}

	// And a value still sets, by handle, after the clear.
	m, err = gw.UpdateProduct(ctx, "", "acme-bar", storage.ProductPatch{VendorID: strptr("kestrel"), Icon: strptr("mic")})
	if err != nil {
		t.Fatalf("set again: %v", err)
	}
	if m.VendorName == nil || *m.VendorName != "kestrel" || m.Icon == nil || *m.Icon != "mic" {
		t.Errorf("set after clear = vendor %v icon %v, want kestrel and mic", m.VendorName, m.Icon)
	}
}
