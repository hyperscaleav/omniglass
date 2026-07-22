package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// productBody is the wire shape of a product registry row. A product ties the
// classification catalogs together: a vendor (who makes it), a driver (what
// talks to it), a kind, an optional parent product, and the capabilities it
// provides. The registry lists alphabetically by display_name.
type productBody struct {
	ID              string   `json:"id" doc:"The product's uuid, the stable handle that survives a rename"`
	Name            string   `json:"name" doc:"The kebab handle an operator reads and types; renameable"`
	DisplayName     string   `json:"display_name"`
	Vendor          string   `json:"vendor,omitempty" doc:"The vendor's handle"`
	VendorID        string   `json:"vendor_id,omitempty" doc:"The vendor's uuid; the stable form of vendor"`
	DriverID        string   `json:"driver_id,omitempty"`
	Kind            string   `json:"kind" enum:"device,app,service,vm"`
	ParentProduct   string   `json:"parent_product,omitempty" doc:"The parent product's handle"`
	ParentProductID string   `json:"parent_product_id,omitempty" doc:"The parent product's uuid; the stable form of parent_product"`
	Capabilities    []string `json:"capabilities"`
	Official        bool     `json:"official"`
}

// derefStr reads an optional string pointer as its value, "" when nil, so the
// nullable vendor/driver/parent columns render as an omitted wire field.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ptrOrNil turns an empty wire string into a nil pointer so an omitted optional
// reference stores as SQL NULL rather than the empty string (which no FK
// target matches).
func ptrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// emptyPtrToNil collapses a pointer to an empty string down to nil, so a PATCH
// that carries an optional reference as "" is read as "not provided" (coalesce
// keeps the current value) rather than an attempt to set the empty string, which
// no FK target matches. This keeps update consistent with create's ptrOrNil.
func emptyPtrToNil(p *string) *string {
	if p == nil || *p == "" {
		return nil
	}
	return p
}

func toProductBody(m *storage.Product) productBody {
	caps := m.Capabilities
	if caps == nil {
		caps = []string{}
	}
	return productBody{
		ID: m.ID, Name: m.Name, DisplayName: m.DisplayName,
		Vendor: derefStr(m.VendorName), VendorID: derefStr(m.VendorID),
		DriverID:      derefStr(m.DriverID),
		Kind:          m.Kind,
		ParentProduct: derefStr(m.ParentProductName), ParentProductID: derefStr(m.ParentProductID),
		Capabilities: caps, Official: m.Official,
	}
}

type listProductsOutput struct {
	Body struct {
		Products []productBody `json:"products"`
	}
}

type productPathInput struct {
	ID string `path:"id" doc:"The product id"`
}

type createProductInput struct {
	Body struct {
		Name            string   `json:"name" minLength:"1" doc:"The globally unique kebab handle; renameable"`
		DisplayName     string   `json:"display_name" minLength:"1"`
		VendorID        string   `json:"vendor_id,omitempty" doc:"The vendor, by handle or uuid"`
		DriverID        string   `json:"driver_id,omitempty"`
		Kind            string   `json:"kind,omitempty" enum:"device,app,service,vm" default:"device"`
		ParentProductID string   `json:"parent_product_id,omitempty" doc:"The parent product, by handle or uuid"`
		Capabilities    []string `json:"capabilities,omitempty"`
	}
}

type updateProductInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName     *string   `json:"display_name,omitempty"`
		VendorID        *string   `json:"vendor_id,omitempty"`
		DriverID        *string   `json:"driver_id,omitempty"`
		Kind            *string   `json:"kind,omitempty" enum:"device,app,service,vm"`
		ParentProductID *string   `json:"parent_product_id,omitempty"`
		Capabilities    *[]string `json:"capabilities,omitempty"`
	}
}

type productOutput struct {
	Body productBody
}

// validProductKind reports whether kind is one of the closed product-kind set
// (device/app/service/vm). The DB CHECK constraint enforces it too; this rejects
// a bad value at the edge with a clean 422 instead of a 500.
func validProductKind(kind string) bool {
	switch kind {
	case "device", "app", "service", "vm":
		return true
	}
	return false
}

// mapProductErr translates the product storage sentinels into HTTP status. An
// unknown vendor/driver/parent/capability reference and an out-of-set kind are
// 422s; everything else falls through to the shared type-registry mapping
// (not-found 404, duplicate 409, official read-only 422, in-use 409).
func mapProductErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrProductRefNotFound):
		return huma.Error422UnprocessableEntity("product references an unknown vendor, driver, parent, or capability")
	case errors.Is(err, storage.ErrProductInvalidKind):
		return huma.Error422UnprocessableEntity("kind must be one of device, app, service, vm")
	default:
		return mapTypeErr(err, "product")
	}
}

// registerProductRoutes wires the product registry CRUD surface, on the same
// pattern as the vendor/driver/capability catalogs. Gated by
// product:read|create|update|delete: product:read sits in the viewer read-floor
// (*:read), the mutations at the admin tier, exactly like vendor:*.
func registerProductRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-products",
		Method:      http.MethodGet,
		Path:        "/products",
		Summary:     "List products",
		Description: "Lists the product registry, ordered alphabetically by display name. Each product carries its vendor, driver, kind, and capabilities. Gated by product:read.",
	}, "product", "read"), func(ctx context.Context, _ *struct{}) (*listProductsOutput, error) {
		items, err := gw.ListProducts(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list products")
		}
		out := &listProductsOutput{}
		out.Body.Products = make([]productBody, 0, len(items))
		for i := range items {
			out.Body.Products = append(out.Body.Products, toProductBody(&items[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-product",
		Method:        http.MethodPost,
		Path:          "/products",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a product",
		Description:   "Creates a custom (non-official) product and sets its capabilities. Gated by product:create.",
	}, "product", "create"), func(ctx context.Context, in *createProductInput) (*productOutput, error) {
		if in.Body.Kind == "" {
			in.Body.Kind = string(storage.ProductDevice) // default, matches the column default
		}
		if !validProductKind(in.Body.Kind) {
			return nil, huma.Error422UnprocessableEntity("kind must be one of device, app, service, vm")
		}
		m, err := gw.CreateProduct(ctx, actorID(ctx), storage.Product{
			Name: in.Body.Name, DisplayName: in.Body.DisplayName,
			VendorID: ptrOrNil(in.Body.VendorID), DriverID: ptrOrNil(in.Body.DriverID),
			Kind: in.Body.Kind, ParentProductID: ptrOrNil(in.Body.ParentProductID),
			Capabilities: in.Body.Capabilities,
		})
		if err != nil {
			return nil, mapProductErr(err)
		}
		return &productOutput{Body: toProductBody(m)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-product",
		Method:      http.MethodGet,
		Path:        "/products/{id}",
		Summary:     "Get a product",
		Description: "Fetches a product by id, with its capabilities. Gated by product:read.",
	}, "product", "read"), func(ctx context.Context, in *productPathInput) (*productOutput, error) {
		m, err := gw.GetProduct(ctx, in.ID)
		if err != nil {
			return nil, mapProductErr(err)
		}
		return &productOutput{Body: toProductBody(m)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-product",
		Method:      http.MethodPatch,
		Path:        "/products/{id}",
		Summary:     "Update a product",
		Description: "Patches a custom product's display_name, vendor, driver, kind, or parent, and replaces its capabilities when provided. Official products are read-only (422). Gated by product:update.",
	}, "product", "update"), func(ctx context.Context, in *updateProductInput) (*productOutput, error) {
		if in.Body.Kind != nil && !validProductKind(*in.Body.Kind) {
			return nil, huma.Error422UnprocessableEntity("kind must be one of device, app, service, vm")
		}
		m, err := gw.UpdateProduct(ctx, actorID(ctx), in.ID, storage.ProductPatch{
			DisplayName: in.Body.DisplayName,
			VendorID:    emptyPtrToNil(in.Body.VendorID), DriverID: emptyPtrToNil(in.Body.DriverID),
			Kind: in.Body.Kind, ParentProductID: emptyPtrToNil(in.Body.ParentProductID), Capabilities: in.Body.Capabilities,
		})
		if err != nil {
			return nil, mapProductErr(err)
		}
		return &productOutput{Body: toProductBody(m)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-product",
		Method:        http.MethodDelete,
		Path:          "/products/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a product",
		Description:   "Deletes a custom product, refused if official (422) or still referenced by a component (409). Gated by product:delete.",
	}, "product", "delete"), func(ctx context.Context, in *productPathInput) (*struct{}, error) {
		if err := gw.DeleteProduct(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapProductErr(err)
		}
		return nil, nil
	})
}
