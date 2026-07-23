package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The location type declared-property contract: what a location type exposes, so
// every location classified by it inherits the same set. The place-side mirror of
// the product and standard contracts, and the same shape: a contract line only
// names a catalog property and says how the type presents it (an optional
// default, whether an instance must set it); data_type and validation stay in the
// property catalog. The line is addressed by name, which makes the write a PUT
// upsert rather than a create/update pair.

type locationTypePropertyBody struct {
	PropertyName   string          `json:"property_name" doc:"The catalog property this location type declares"`
	PropertyTypeID string          `json:"property_type_id" doc:"The catalog property's uuid, the stable form of property_name"`
	DefaultValue   json.RawMessage `json:"default_value,omitempty" doc:"The contract default, shape given by the property's data_type; omitted when the contract sets none"`
	Required       bool            `json:"required" doc:"Whether every location of this type must set the property"`
}

func toLocationTypePropertyBody(lp *storage.LocationTypeProperty) locationTypePropertyBody {
	return locationTypePropertyBody{
		PropertyName:   lp.PropertyName,
		PropertyTypeID: lp.PropertyTypeID,
		DefaultValue:   json.RawMessage(lp.DefaultValue),
		Required:       lp.Required,
	}
}

type listLocationTypePropertiesOutput struct {
	Body struct {
		Properties []locationTypePropertyBody `json:"properties"`
	}
}

type locationTypePropertyOutput struct {
	Body locationTypePropertyBody
}

// locationTypePropertyPathInput addresses one contract line by the location type
// and the property name (the pair the contract is unique on).
type locationTypePropertyPathInput struct {
	ID       string `path:"id" doc:"The location type id"`
	Property string `path:"property" doc:"The property name"`
}

type setLocationTypePropertyInput struct {
	ID       string `path:"id" doc:"The location type id"`
	Property string `path:"property" doc:"The property name"`
	Body     struct {
		DefaultValue any  `json:"default_value,omitempty" doc:"The contract default, validated against the property's data_type; omit for no default"`
		Required     bool `json:"required,omitempty" doc:"Whether every location of this type must set the property; defaults to false"`
	}
}

// registerLocationTypePropertyRoutes wires the location type's declared-property
// contract. The type registry it hangs off is gated by type:*, so the contract
// keeps that permission story: the read rides the type:read viewer floor, the
// writes sit at type:update / type:delete. Official location types ship their
// contract with the release, so an operator write against one is refused (422)
// the same way its registry row is.
func registerLocationTypePropertyRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-location-type-properties",
		Method:      http.MethodGet,
		Path:        "/location-types/{id}/properties",
		Summary:     "List a location type's declared properties",
		Description: "Lists the location type's declared-property contract (what every location of the type exposes), ordered by property name, each with its optional default and required flag. Gated by type:read.",
	}, "type", "read"), func(ctx context.Context, in *locationTypePathInput) (*listLocationTypePropertiesOutput, error) {
		items, err := gw.ListLocationTypeProperties(ctx, in.ID)
		if err != nil {
			return nil, mapLocationTypePropertyErr(err)
		}
		out := &listLocationTypePropertiesOutput{}
		out.Body.Properties = make([]locationTypePropertyBody, 0, len(items))
		for i := range items {
			out.Body.Properties = append(out.Body.Properties, toLocationTypePropertyBody(&items[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "set-location-type-property",
		Method:      http.MethodPut,
		Path:        "/location-types/{id}/properties/{property}",
		Summary:     "Declare a property on a location type",
		Description: "Declares a catalog property on a custom location type, or revises the declaration in place (the line is addressed by name, so the write is idempotent). Official location types are read-only (422); an unknown type is a 404 and a property the catalog does not know is a 422. Gated by type:update.",
	}, "type", "update"), func(ctx context.Context, in *setLocationTypePropertyInput) (*locationTypePropertyOutput, error) {
		def, err := encodePropertyJSON(in.Body.DefaultValue, "default_value")
		if err != nil {
			return nil, err
		}
		lp, err := gw.SetLocationTypeProperty(ctx, actorID(ctx), in.ID, storage.LocationTypePropertySpec{
			PropertyName: in.Property,
			DefaultValue: def,
			Required:     in.Body.Required,
		})
		if err != nil {
			return nil, mapLocationTypePropertyErr(err)
		}
		return &locationTypePropertyOutput{Body: toLocationTypePropertyBody(lp)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-location-type-property",
		Method:        http.MethodDelete,
		Path:          "/location-types/{id}/properties/{property}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Withdraw a property from a location type",
		Description:   "Removes one line from a custom location type's contract; locations of the type keep any value they set for it, now off-contract. A property the type does not declare is a 404, and an official type is read-only (422). Gated by type:delete.",
	}, "type", "delete"), func(ctx context.Context, in *locationTypePropertyPathInput) (*struct{}, error) {
		if err := gw.DeleteLocationTypeProperty(ctx, actorID(ctx), in.ID, in.Property); err != nil {
			return nil, mapLocationTypePropertyErr(err)
		}
		return nil, nil
	})
}

// mapLocationTypePropertyErr translates the contract storage sentinels into HTTP
// status, mirroring the product contract: a property the catalog does not know is
// a 422 (the request names a property that does not exist), everything else falls
// through to the shared type-registry mapping: not-found 404, official read-only
// 422.
func mapLocationTypePropertyErr(err error) error {
	if errors.Is(err, storage.ErrPropertyTypeNotFound) {
		return huma.Error422UnprocessableEntity("unknown property")
	}
	return mapTypeErr(err, "location_type property")
}
