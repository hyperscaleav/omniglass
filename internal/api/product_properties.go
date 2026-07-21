package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The product declared-property contract: what a product exposes, so every
// component that is an instance of it inherits the same set. A contract line only
// names a catalog property and says how the product presents it (an optional
// default, whether an instance must set it); data_type and validation stay in the
// property catalog, so they are never restated per product. The line is addressed
// by name, which makes the write a PUT upsert rather than a create/update pair.

type productPropertyBody struct {
	PropertyName string          `json:"property_name" doc:"The catalog property this product declares"`
	DefaultValue json.RawMessage `json:"default_value,omitempty" doc:"The contract default, shape given by the property's data_type; omitted when the contract sets none"`
	Required     bool            `json:"required" doc:"Whether every instance of this product must set the property"`
}

func toProductPropertyBody(pp *storage.ProductProperty) productPropertyBody {
	return productPropertyBody{
		PropertyName: pp.PropertyName,
		DefaultValue: json.RawMessage(pp.DefaultValue),
		Required:     pp.Required,
	}
}

type listProductPropertiesOutput struct {
	Body struct {
		Properties []productPropertyBody `json:"properties"`
	}
}

type productPropertyOutput struct {
	Body productPropertyBody
}

// productPropertyPathInput addresses one contract line by the product and the
// property name (the pair the contract is unique on).
type productPropertyPathInput struct {
	ID       string `path:"id" doc:"The product id"`
	Property string `path:"property" doc:"The property name"`
}

type setProductPropertyInput struct {
	ID       string `path:"id" doc:"The product id"`
	Property string `path:"property" doc:"The property name"`
	Body     struct {
		DefaultValue any  `json:"default_value,omitempty" doc:"The contract default, validated against the property's data_type; omit for no default"`
		Required     bool `json:"required,omitempty" doc:"Whether every instance of this product must set the property; defaults to false"`
	}
}

// registerProductPropertyRoutes wires the product's declared-property contract,
// a sub-collection of the product registry and gated with it: the read rides the
// product:read viewer floor, the writes sit at product:update / product:delete.
// Official products ship their contract with the release, so an operator write
// against one is refused (422) the same way its registry row is.
func registerProductPropertyRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-product-properties",
		Method:      http.MethodGet,
		Path:        "/products/{id}/properties",
		Summary:     "List a product's declared properties",
		Description: "Lists the product's declared-property contract (what every instance of the product exposes), ordered by property name, each with its optional default and required flag. Gated by product:read.",
	}, "product", "read"), func(ctx context.Context, in *productPathInput) (*listProductPropertiesOutput, error) {
		items, err := gw.ListProductProperties(ctx, in.ID)
		if err != nil {
			return nil, mapProductPropertyErr(err)
		}
		out := &listProductPropertiesOutput{}
		out.Body.Properties = make([]productPropertyBody, 0, len(items))
		for i := range items {
			out.Body.Properties = append(out.Body.Properties, toProductPropertyBody(&items[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "set-product-property",
		Method:      http.MethodPut,
		Path:        "/products/{id}/properties/{property}",
		Summary:     "Declare a property on a product",
		Description: "Declares a catalog property on a custom product, or revises the declaration in place (the line is addressed by name, so the write is idempotent). Official products are read-only (422), and an unknown product or property is a 422. Gated by product:update.",
	}, "product", "update"), func(ctx context.Context, in *setProductPropertyInput) (*productPropertyOutput, error) {
		def, err := encodePropertyJSON(in.Body.DefaultValue, "default_value")
		if err != nil {
			return nil, err
		}
		pp, err := gw.SetProductProperty(ctx, actorID(ctx), in.ID, storage.ProductPropertySpec{
			PropertyName: in.Property,
			DefaultValue: def,
			Required:     in.Body.Required,
		})
		if err != nil {
			return nil, mapProductPropertyErr(err)
		}
		return &productPropertyOutput{Body: toProductPropertyBody(pp)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-product-property",
		Method:        http.MethodDelete,
		Path:          "/products/{id}/properties/{property}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Withdraw a property from a product",
		Description:   "Removes one line from a custom product's contract; instances keep any value they set for it, now off-contract. A property the product does not declare is a 404, and an official product is read-only (422). Gated by product:delete.",
	}, "product", "delete"), func(ctx context.Context, in *productPropertyPathInput) (*struct{}, error) {
		if err := gw.DeleteProductProperty(ctx, actorID(ctx), in.ID, in.Property); err != nil {
			return nil, mapProductPropertyErr(err)
		}
		return nil, nil
	})
}

// encodePropertyJSON marshals a polymorphic operator value into the jsonb bytes
// the gateway stores. A nil value (no default given) encodes to nil rather than a
// JSON null, so "no default" and "the default is null" stay distinct. Shared with
// the component value write.
func encodePropertyJSON(v any, field string) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity(field + " is not encodable")
	}
	return raw, nil
}

// mapProductPropertyErr translates the contract storage sentinels into HTTP
// status. A property the catalog does not know is a 422 (the request names a
// property that does not exist), everything else falls through to the shared
// type-registry mapping, where the product is the type: not-found 404, official
// read-only 422.
func mapProductPropertyErr(err error) error {
	if errors.Is(err, storage.ErrPropertyNotFound) {
		return huma.Error422UnprocessableEntity("unknown property")
	}
	return mapTypeErr(err, "product property")
}
