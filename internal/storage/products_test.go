package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// caps joins a capability slice for order-sensitive comparison (the gateway
// returns them sorted by capability_id).
func caps(s []string) string { return strings.Join(s, ",") }

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

	vendor, driver := "cisco", "cisco-xapi"

	// Create a custom product with a vendor, driver, kind, and capabilities. It
	// is official=false; capabilities come back sorted by id.
	m, err := gw.CreateProduct(ctx, "", storage.Product{
		Name: "room-bar", DisplayName: "Room Bar",
		VendorID: &vendor, DriverID: &driver, Kind: "device",
		Capabilities: []string{"speaker", "microphone", "camera"},
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
	if m.VendorName == nil || *m.VendorName != "cisco" || m.DriverName == nil || *m.DriverName != "cisco-xapi" {
		t.Fatalf("create refs = vendor:%v driver:%v, want cisco/cisco-xapi", m.VendorName, m.DriverName)
	}
	if got := caps(m.Capabilities); got != "camera,microphone,speaker" {
		t.Fatalf("create capabilities = %q, want camera,microphone,speaker", got)
	}

	// Get loads the capabilities.
	got, err := gw.GetProduct(ctx, "room-bar")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if caps(got.Capabilities) != "camera,microphone,speaker" {
		t.Fatalf("get capabilities = %q, want camera,microphone,speaker", caps(got.Capabilities))
	}

	// List loads the capabilities too.
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
	if caps(found.Capabilities) != "camera,microphone,speaker" {
		t.Fatalf("list room-bar capabilities = %q, want camera,microphone,speaker", caps(found.Capabilities))
	}

	// Duplicate id is ErrTypeExists.
	if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: "room-bar", DisplayName: "Dup", Kind: "device"}); !errors.Is(err, storage.ErrTypeExists) {
		t.Fatalf("dup create err = %v, want ErrTypeExists", err)
	}

	// An unknown vendor reference is ErrProductRefNotFound (422-worthy).
	badVendor := "nonexistent-vendor"
	if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: "bad-ref", DisplayName: "Bad", VendorID: &badVendor, Kind: "device"}); !errors.Is(err, storage.ErrProductRefNotFound) {
		t.Fatalf("bad vendor err = %v, want ErrProductRefNotFound", err)
	}

	// An unknown capability is ErrProductRefNotFound too.
	if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: "bad-cap", DisplayName: "Bad", Kind: "device", Capabilities: []string{"nonexistent-cap"}}); !errors.Is(err, storage.ErrProductRefNotFound) {
		t.Fatalf("bad capability err = %v, want ErrProductRefNotFound", err)
	}

	// An out-of-set kind is ErrProductInvalidKind (422-worthy), rejected before
	// the DB CHECK.
	if _, err := gw.CreateProduct(ctx, "", storage.Product{Name: "bad-kind", DisplayName: "Bad", Kind: "gizmo"}); !errors.Is(err, storage.ErrProductInvalidKind) {
		t.Fatalf("bad kind err = %v, want ErrProductInvalidKind", err)
	}

	// Update changes display_name, kind, and the capability set; vendor is left
	// untouched (nil patch field).
	dn, kd := "Room Bar Pro", "app"
	upd, err := gw.UpdateProduct(ctx, "", "room-bar", storage.ProductPatch{
		DisplayName:  &dn,
		Kind:         &kd,
		Capabilities: &[]string{"codec"},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.DisplayName != "Room Bar Pro" || upd.Kind != "app" {
		t.Fatalf("update = %+v, want display_name=Room Bar Pro kind=app", upd)
	}
	if caps(upd.Capabilities) != "codec" {
		t.Fatalf("update capabilities = %q, want codec", caps(upd.Capabilities))
	}
	if upd.VendorName == nil || *upd.VendorName != "cisco" {
		t.Fatalf("update vendor = %v, want unchanged cisco", upd.VendorName)
	}

	// An out-of-set kind on update is ErrProductInvalidKind.
	badKind := "gizmo"
	if _, err := gw.UpdateProduct(ctx, "", "room-bar", storage.ProductPatch{Kind: &badKind}); !errors.Is(err, storage.ErrProductInvalidKind) {
		t.Fatalf("update bad kind err = %v, want ErrProductInvalidKind", err)
	}

	// Official rows are read-only, and an upsert sets their capabilities.
	if err := gw.UpsertProduct(ctx, storage.Product{Name: "official-prod", DisplayName: "Official", Kind: "device", Official: true, Capabilities: []string{"speaker"}}); err != nil {
		t.Fatalf("upsert official: %v", err)
	}
	op, err := gw.GetProduct(ctx, "official-prod")
	if err != nil {
		t.Fatalf("get official: %v", err)
	}
	if !op.Official || caps(op.Capabilities) != "speaker" {
		t.Fatalf("official = %+v, want official=true capabilities=speaker", op)
	}
	if _, err := gw.UpdateProduct(ctx, "", "official-prod", storage.ProductPatch{DisplayName: &dn}); !errors.Is(err, storage.ErrTypeOfficial) {
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

	// Delete the custom row (its capability rows cascade), then confirm it is gone.
	if err := gw.DeleteProduct(ctx, "", "room-bar"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := gw.GetProduct(ctx, "room-bar"); !errors.Is(err, storage.ErrTypeNotFound) {
		t.Fatalf("get after delete err = %v, want ErrTypeNotFound", err)
	}
}
