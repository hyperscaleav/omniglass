package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// productBodyWire is the decoded product wire shape for the e2e assertions.
type productBodyWire struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Vendor       string   `json:"vendor"`
	VendorID     string   `json:"vendor_id"`
	Driver       string   `json:"driver"`
	DriverID     string   `json:"driver_id"`
	Kind         string   `json:"kind"`
	Official     bool     `json:"official"`
	Capabilities []string `json:"capabilities"`
}

// TestProductsAPI drives the product registry over HTTP: a viewer reads the
// seeded official rows under the product:read floor but cannot create, an admin
// (owner) creates a custom row with a vendor/driver/kind and capabilities, bad
// references and a bad kind are 422s, an official row is read-only (422 on patch
// and delete), capabilities are replaced on patch, and the admin deletes the
// custom row. Mirrors TestVendorsAPI; product is a flat registry like vendor, so
// product:* is wired exactly like vendor:*.
func TestProductsAPI(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ownerTok := bootstrapOwnerTok(t, ctx, gw)

	srv := httptest.NewServer(api.NewHandler(gw))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	// A plain viewer (read everywhere, write nothing) reads the seeded official
	// products via the product:read floor (*:read).
	viewerTok := principalWithGrants(t, ctx, dsn, "product-viewer", []grant{{role: "viewer", scopeKind: "all"}})
	out := c.do(viewerTok, http.MethodGet, "/products", nil, http.StatusOK)
	var listed struct {
		Products []struct {
			ID       string `json:"id"`
			Official bool   `json:"official"`
		} `json:"products"`
	}
	if err := json.Unmarshal(out, &listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed.Products) == 0 {
		t.Fatalf("products empty, want seeded rows")
	}

	// The viewer cannot create (403, capability fast-reject).
	c.do(viewerTok, http.MethodPost, "/products",
		map[string]any{"name": "nope", "display_name": "Nope"}, http.StatusForbidden)

	// Admin (owner) creates a custom product with a vendor, driver, kind, and
	// capabilities.
	var created productBodyWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodPost, "/products", map[string]any{
		"name": "acme-bar", "display_name": "Acme Bar",
		"vendor_id": "cisco", "driver_id": "cisco-xapi", "kind": "device",
		"capabilities": []string{"speaker", "microphone"},
	}, http.StatusCreated), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	// Both forms come back: the uuid that survives a rename and the handle typed.
	if created.Name != "acme-bar" || created.Official {
		t.Fatalf("created = %+v, want name=acme-bar official=false", created)
	}
	// A reference carries both too, and the vendor was named by its handle.
	if created.Vendor != "cisco" {
		t.Fatalf("created vendor = %q, want the handle cisco", created.Vendor)
	}
	if created.Vendor != "cisco" || created.Driver != "cisco-xapi" || created.Kind != "device" {
		t.Fatalf("created refs = %+v, want cisco/cisco-xapi/device", created)
	}
	if strings.Join(created.Capabilities, ",") != "microphone,speaker" {
		t.Fatalf("created capabilities = %v, want [microphone speaker]", created.Capabilities)
	}

	// Duplicate id is a 409 (shared mapTypeErr ErrTypeExists branch).
	c.do(ownerTok, http.MethodPost, "/products",
		map[string]any{"name": "acme-bar", "display_name": "Dup"}, http.StatusConflict)

	// An unknown reference is a 422.
	c.do(ownerTok, http.MethodPost, "/products",
		map[string]any{"id": "bad-ref", "display_name": "Bad", "vendor_id": "no-such-vendor"}, http.StatusUnprocessableEntity)

	// An out-of-set kind is a 422.
	c.do(ownerTok, http.MethodPost, "/products",
		map[string]any{"id": "bad-kind", "display_name": "Bad", "kind": "gizmo"}, http.StatusUnprocessableEntity)

	// The custom row is mutable, and a patch replaces its capabilities.
	c.do(ownerTok, http.MethodPatch, "/products/acme-bar",
		map[string]any{"display_name": "Acme Bar Pro", "kind": "app", "capabilities": []string{"camera", "codec"}}, http.StatusOK)
	var reread productBodyWire
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/products/acme-bar", nil, http.StatusOK), &reread); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if reread.Kind != "app" {
		t.Fatalf("patched kind = %q, want app", reread.Kind)
	}
	if strings.Join(reread.Capabilities, ",") != "camera,codec" {
		t.Fatalf("patched capabilities = %v, want [camera codec]", reread.Capabilities)
	}

	// A patch that carries an optional reference as "" reads as "not provided"
	// (consistent with create's empty-string handling), not an attempt to set the
	// empty string against a foreign key: 200, and the current vendor is kept.
	c.do(ownerTok, http.MethodPatch, "/products/acme-bar",
		map[string]any{"vendor_id": ""}, http.StatusOK)
	if err := json.Unmarshal(c.do(ownerTok, http.MethodGet, "/products/acme-bar", nil, http.StatusOK), &reread); err != nil {
		t.Fatalf("decode get after empty vendor patch: %v", err)
	}
	// Asserted on the handle: vendor_id is the uuid now, and the point of the
	// assertion is that the vendor was KEPT, which the handle says legibly.
	if reread.Vendor != "cisco" {
		t.Fatalf("vendor after empty-string patch = %q, want cisco (kept)", reread.Vendor)
	}

	// The seeded official row (cisco-room-bar) is read-only: 422 on patch and delete.
	c.do(ownerTok, http.MethodPatch, "/products/cisco-room-bar",
		map[string]any{"display_name": "X"}, http.StatusUnprocessableEntity)
	c.do(ownerTok, http.MethodDelete, "/products/cisco-room-bar", nil, http.StatusUnprocessableEntity)

	// Admin deletes the custom row.
	c.do(ownerTok, http.MethodDelete, "/products/acme-bar", nil, http.StatusNoContent)
	c.do(ownerTok, http.MethodGet, "/products/acme-bar", nil, http.StatusNotFound)

	// Unknown id is a 404.
	c.do(ownerTok, http.MethodDelete, "/products/nope", nil, http.StatusNotFound)
}
