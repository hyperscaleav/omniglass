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
// talks to it), a kind, the component_type it is classified under (the
// taxonomy above product), an optional icon override, an optional parent
// product, and the capabilities it provides. The registry lists alphabetically
// by display_name.
type productBody struct {
	ID              string   `json:"id" doc:"The product's uuid, the stable handle that survives a rename"`
	Name            string   `json:"name" doc:"The name an operator reads and types; renameable"`
	DisplayName     string   `json:"display_name"`
	Vendor          string   `json:"vendor,omitempty" doc:"The vendor's handle"`
	VendorID        string   `json:"vendor_id,omitempty" doc:"The vendor's uuid; the stable form of vendor"`
	Driver          string   `json:"driver,omitempty" doc:"The driver's handle"`
	DriverID        string   `json:"driver_id,omitempty" doc:"The driver's uuid; the stable form of driver"`
	Kind            string   `json:"kind" enum:"device,app,service"`
	ComponentType   string   `json:"component_type" doc:"The component_type this product is classified under (mic, camera, ...); the taxonomy above product"`
	ComponentTypeID string   `json:"component_type_id" doc:"The component_type's uuid; the stable form of component_type"`
	Icon            string   `json:"icon,omitempty" doc:"A product-level icon override; unset inherits the component_type's icon"`
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
		Driver: derefStr(m.DriverName), DriverID: derefStr(m.DriverID),
		Kind:            m.Kind,
		ComponentType:   m.ComponentType,
		ComponentTypeID: m.ComponentTypeID,
		Icon:            derefStr(m.Icon),
		ParentProduct:   derefStr(m.ParentProductName), ParentProductID: derefStr(m.ParentProductID),
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
		Name            string   `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The globally unique name; renameable"`
		DisplayName     string   `json:"display_name" minLength:"1" doc:"What an operator reads in pickers and lists"`
		VendorID        string   `json:"vendor_id,omitempty" doc:"The vendor, by handle or uuid"`
		DriverID        string   `json:"driver_id,omitempty" doc:"The driver that talks to it, by handle or uuid"`
		Kind            string   `json:"kind" enum:"device,app,service" doc:"What class of thing the product is. vm is retired (folded into app); required, no default, so every product states its class explicitly."`
		ComponentType   string   `json:"component_type" minLength:"1" doc:"The component_type this product is classified under (mic, camera, ...), by name or uuid; every product must belong to one of the tree's nodes. The generics (generic-device, generic-app, generic-service) fit anything not yet modeled more specifically."`
		Icon            string   `json:"icon,omitempty" doc:"A product-level icon override; unset inherits the component_type's icon"`
		ParentProductID string   `json:"parent_product_id,omitempty" doc:"The parent product, by handle or uuid"`
		Capabilities    []string `json:"capabilities,omitempty" doc:"Capability names the product provides (the default set its components inherit)"`
	}
}

type updateProductInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName     *string   `json:"display_name,omitempty" doc:"A new operator-facing label"`
		VendorID        *string   `json:"vendor_id,omitempty" doc:"A new vendor, by handle or uuid"`
		DriverID        *string   `json:"driver_id,omitempty" doc:"A new driver, by handle or uuid"`
		Kind            *string   `json:"kind,omitempty" enum:"device,app,service" doc:"A new product class"`
		ComponentType   *string   `json:"component_type,omitempty" doc:"Reclassifies the product to this component_type, by name or uuid; component_type is required, so this only reclassifies, it never clears"`
		Icon            *string   `json:"icon,omitempty" doc:"A new icon override"`
		ParentProductID *string   `json:"parent_product_id,omitempty" doc:"A new parent product, by handle or uuid"`
		Capabilities    *[]string `json:"capabilities,omitempty" doc:"Replaces the capability-name set; omit to leave unchanged"`
	}
}

type productOutput struct {
	Body productBody
}

// validProductKind reports whether kind is one of the closed product-kind set
// (device/app/service; vm retired). The DB CHECK constraint enforces it too;
// this rejects a bad value at the edge with a clean 422 instead of a 500.
func validProductKind(kind string) bool {
	switch kind {
	case "device", "app", "service":
		return true
	}
	return false
}

// mapProductErr translates the product storage sentinels into HTTP status. An
// unknown vendor/driver/parent/capability/component_type reference and an
// out-of-set kind are 422s; everything else falls through to the shared
// type-registry mapping (not-found 404, duplicate 409, official read-only
// 422, in-use 409).
func mapProductErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrProductRefNotFound):
		return huma.Error422UnprocessableEntity("product references an unknown vendor, driver, parent, capability, or component_type")
	case errors.Is(err, storage.ErrProductInvalidKind):
		return huma.Error422UnprocessableEntity("kind must be one of device, app, service")
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
		Description: "Lists the product registry, ordered alphabetically by display name. Each product carries its vendor, driver, kind, component_type, and capabilities. Gated by product:read.",
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
		Description:   "Creates a custom (non-official) product, classified under a component_type, and sets its capabilities. kind and component_type are both required; kind refuses vm (retired, folded into app). Gated by product:create.",
	}, "product", "create"), func(ctx context.Context, in *createProductInput) (*productOutput, error) {
		if !validProductKind(in.Body.Kind) {
			return nil, huma.Error422UnprocessableEntity("kind must be one of device, app, service")
		}
		m, err := gw.CreateProduct(ctx, actorID(ctx), storage.Product{
			Name: in.Body.Name, DisplayName: in.Body.DisplayName,
			VendorID: ptrOrNil(in.Body.VendorID), DriverID: ptrOrNil(in.Body.DriverID),
			Kind: in.Body.Kind, ComponentType: in.Body.ComponentType, Icon: ptrOrNil(in.Body.Icon),
			ParentProductID: ptrOrNil(in.Body.ParentProductID),
			Capabilities:    in.Body.Capabilities,
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
		Description: "Fetches a product by its name or its uuid, with its capabilities. Either form resolves, so `omniglass product get acme-soundbar` and the uuid are interchangeable. Gated by product:read.",
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
		Description: "Patches a custom product's display_name, vendor, driver, kind, component_type, icon, or parent, and replaces its capabilities when provided. component_type is required, so an empty string on it is a 422 (a reclassify names a real type; it never clears). Official products are read-only (422). Gated by product:update.",
	}, "product", "update"), func(ctx context.Context, in *updateProductInput) (*productOutput, error) {
		if in.Body.Kind != nil && !validProductKind(*in.Body.Kind) {
			return nil, huma.Error422UnprocessableEntity("kind must be one of device, app, service")
		}
		m, err := gw.UpdateProduct(ctx, actorID(ctx), in.ID, storage.ProductPatch{
			DisplayName: in.Body.DisplayName,
			VendorID:    emptyPtrToNil(in.Body.VendorID), DriverID: emptyPtrToNil(in.Body.DriverID),
			Kind: in.Body.Kind, ComponentType: in.Body.ComponentType, Icon: in.Body.Icon,
			ParentProductID: emptyPtrToNil(in.Body.ParentProductID), Capabilities: in.Body.Capabilities,
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
