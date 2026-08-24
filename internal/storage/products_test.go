package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

func TestProductCRUD(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	vendor, driver := "kestrel", "kestrel-api"

	// Create a custom product with a vendor, driver, and kind. It is
	// official=false.
	m, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "room-bar", Label: "VRoom",
		VendorID: &vendor, DriverID: &driver, Kind: "device",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.Name != "room-bar" || m.Official {
		t.Fatalf("create = %+v, want name=room-bar official=false", m)
	}
	if m.Kind != "device" {
		t.Fatalf("create kind = %q, want device", m.Kind)
	}
	// Both arcs store uuids; the handles come back beside them.
	if m.VendorName == nil || *m.VendorName != "kestrel" || m.DriverName == nil || *m.DriverName != "kestrel-api" {
		t.Fatalf("create refs = vendor:%v driver:%v, want kestrel/kestrel-api", m.VendorName, m.DriverName)
	}

	got, err := gw.GetProduct(ctx, "room-bar")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "room-bar" {
		t.Fatalf("get = %+v, want room-bar", got)
	}

	all, err := gw.ListProducts(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found *storage.Product
	for i := range all {
		if all[i].Name == "room-bar" {
			found = &all[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("list does not contain room-bar; got %d rows", len(all))
	}

	// Duplicate id is ErrTypeExists.
	if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: "room-bar", Label: "Dup", Kind: "device"}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}

	// An unknown vendor reference is ErrProductRefNotFound (422-worthy).
	badVendor := "nonexistent-vendor"
	if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: "bad-ref", Label: "Bad", VendorID: &badVendor, Kind: "device"}); !errors.Is(err, storage.ErrProductRefNotFound) {
		t.Fatalf("bad vendor err = %v, want ErrProductRefNotFound", err)
	}

	// An out-of-set kind is ErrProductInvalidKind (422-worthy), rejected before
	// the DB CHECK.
	if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: "bad-kind", Label: "Bad", Kind: "gizmo"}); !errors.Is(err, storage.ErrProductInvalidKind) {
		t.Fatalf("bad kind err = %v, want ErrProductInvalidKind", err)
	}

	// Update changes label and kind; vendor is left untouched (nil patch
	// field).
	dn, kd := "Room Bar Pro", "app"
	upd, err := gw.UpdateProduct(ctx, "", "room-bar", storage.ProductPatch{
		Label: &dn,
		Kind:  &kd,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Label != "Room Bar Pro" || upd.Kind != "app" {
		t.Fatalf("update = %+v, want label=Room Bar Pro kind=app", upd)
	}
	if upd.VendorName == nil || *upd.VendorName != "kestrel" {
		t.Fatalf("update vendor = %v, want unchanged cisco", upd.VendorName)
	}

	// An out-of-set kind on update is ErrProductInvalidKind.
	badKind := "gizmo"
	if _, err := gw.UpdateProduct(ctx, "", "room-bar", storage.ProductPatch{Kind: &badKind}); !errors.Is(err, storage.ErrProductInvalidKind) {
		t.Fatalf("update bad kind err = %v, want ErrProductInvalidKind", err)
	}

	// Official rows are read-only.
	if err := gw.UpsertProduct(ctx, storage.Product{Name: "official-prod", Label: "Official", Kind: "device", Official: true}); err != nil {
		t.Fatalf("upsert official: %v", err)
	}
	op, err := gw.GetProduct(ctx, "official-prod")
	if err != nil {
		t.Fatalf("get official: %v", err)
	}
	if !op.Official {
		t.Fatalf("official = %+v, want official=true", op)
	}
	if _, err := gw.UpdateProduct(ctx, "", "official-prod", storage.ProductPatch{Label: &dn}); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("update official err = %v, want ErrTypeOfficial", err)
	}
	if err := gw.DeleteProduct(ctx, "", "official-prod"); !errors.Is(err, storage.ErrTypeOfficial) {
		t.Fatalf("delete official err = %v, want ErrTypeOfficial", err)
	}

	// Unknown id is ErrTypeNotFound.
	if _, err := gw.GetProduct(ctx, "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get unknown err = %v, want ErrTypeNotFound", err)
	}
	if err := gw.DeleteProduct(ctx, "", "nope"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrTypeNotFound", err)
	}

	// Delete the custom row, then confirm it is gone.
	if err := gw.DeleteProduct(ctx, "", "room-bar"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := gw.GetProduct(ctx, "room-bar"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get after delete err = %v, want ErrTypeNotFound", err)
	}
}

// TestProductComponentTypeAndIcon covers the #614 floor: every product carries
// a component_type (explicit or defaulted from kind) and an optional icon
// override. An explicit component_type round-trips, an omitted one defaults to
// the generic instance of the resolved kind (the same fallback the
// product_type_backfill migration applies retroactively), an unknown
// component_type is ErrProductRefNotFound, and update both reclassifies and
// refuses to resolve an empty string (component_type has no clear state).
func TestProductComponentTypeAndIcon(t *testing.T) {
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	icon := "camera"
	m, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "acme-cam", Label: "Acme Cam", Kind: "device",
		ComponentType: "camera", Icon: &icon,
	})
	if err != nil {
		t.Fatalf("create with explicit component_type: %v", err)
	}
	if m.ComponentType != "camera" || m.ComponentTypeID == "" {
		t.Fatalf("create component_type = %+v, want camera with a resolved id", m)
	}
	if m.Icon == nil || *m.Icon != "camera" {
		t.Fatalf("create icon = %v, want camera", m.Icon)
	}

	// No component_type named: defaults to the generic instance of the kind.
	svc, err := gw.CreateProduct(ctx, "", storage.Product{Name: "acme-svc", Label: "Acme Svc", Kind: "service"})
	if err != nil {
		t.Fatalf("create with no component_type: %v", err)
	}
	if svc.ComponentType != "generic-service" {
		t.Fatalf("defaulted component_type = %q, want generic-service", svc.ComponentType)
	}

	// An unknown component_type is ErrProductRefNotFound (422-worthy), the same
	// sentinel an unknown vendor or driver raises.
	if _, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "acme-bad-type", Label: "Bad", Kind: "device", ComponentType: "no-such-type",
	}); !errors.Is(err, storage.ErrProductRefNotFound) {
		t.Fatalf("bad component_type err = %v, want ErrProductRefNotFound", err)
	}

	// Update reclassifies.
	upd, err := gw.UpdateProduct(ctx, "", "acme-cam", storage.ProductPatch{ComponentType: strPtr("mic")})
	if err != nil {
		t.Fatalf("update reclassify: %v", err)
	}
	if upd.ComponentType != "mic" {
		t.Fatalf("update component_type = %q, want mic", upd.ComponentType)
	}

	// component_type is required, so an empty-string patch does not clear it:
	// it fails to resolve, same as any other unknown reference.
	if _, err := gw.UpdateProduct(ctx, "", "acme-cam", storage.ProductPatch{ComponentType: strPtr("")}); !errors.Is(err, storage.ErrProductRefNotFound) {
		t.Fatalf("empty component_type patch err = %v, want ErrProductRefNotFound (no clear state)", err)
	}
	// And the row was left exactly as the reclassify left it.
	reread, err := gw.GetProduct(ctx, "acme-cam")
	if err != nil {
		t.Fatalf("get after refused clear: %v", err)
	}
	if reread.ComponentType != "mic" {
		t.Fatalf("component_type after refused clear = %q, want unchanged mic", reread.ComponentType)
	}
}

func strPtr(s string) *string { return &s }
