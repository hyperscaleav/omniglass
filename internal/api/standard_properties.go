package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The standard declared-property contract: what a standard exposes, so every
// system that conforms to it inherits the same set. The system-side mirror of the
// product contract, and the same shape: a contract line only names a catalog
// property and says how the standard presents it (an optional default, whether an
// instance must set it); data_type and validation stay in the property catalog.
// The line is addressed by name, which makes the write a PUT upsert rather than a
// create/update pair.

type standardPropertyBody struct {
	PropertyName string          `json:"property_name" doc:"The catalog property this standard declares"`
	PropertyID   string          `json:"property_id" doc:"The catalog property's uuid, the stable form of property_name"`
	DefaultValue json.RawMessage `json:"default_value,omitempty" doc:"The contract default, shape given by the property's data_type; omitted when the contract sets none"`
	Required     bool            `json:"required" doc:"Whether every system conforming to this standard must set the property"`
}

func toStandardPropertyBody(sp *storage.StandardProperty) standardPropertyBody {
	return standardPropertyBody{
		PropertyName: sp.PropertyName,
		PropertyID:   sp.PropertyID,
		DefaultValue: json.RawMessage(sp.DefaultValue),
		Required:     sp.Required,
	}
}

type listStandardPropertiesOutput struct {
	Body struct {
		Properties []standardPropertyBody `json:"properties"`
	}
}

type standardPropertyOutput struct {
	Body standardPropertyBody
}

// standardPropertyPathInput addresses one contract line by the standard and the
// property name (the pair the contract is unique on).
type standardPropertyPathInput struct {
	ID       string `path:"id" doc:"The standard id"`
	Property string `path:"property" doc:"The property name"`
}

type setStandardPropertyInput struct {
	ID       string `path:"id" doc:"The standard id"`
	Property string `path:"property" doc:"The property name"`
	Body     struct {
		DefaultValue any  `json:"default_value,omitempty" doc:"The contract default, validated against the property's data_type; omit for no default"`
		Required     bool `json:"required,omitempty" doc:"Whether every system conforming to this standard must set the property; defaults to false"`
	}
}

// registerStandardPropertyRoutes wires the standard's declared-property contract,
// a sub-collection of the standard catalog and gated with it: the read rides the
// standard:read viewer floor, the writes sit at standard:update / standard:delete.
// Official standards ship their contract with the release, so an operator write
// against one is refused (422) the same way its catalog row is.
func registerStandardPropertyRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-standard-properties",
		Method:      http.MethodGet,
		Path:        "/standards/{id}/properties",
		Summary:     "List a standard's declared properties",
		Description: "Lists the standard's declared-property contract (what every system conforming to it exposes), ordered by property name, each with its optional default and required flag. Gated by standard:read.",
	}, "standard", "read"), func(ctx context.Context, in *standardPathInput) (*listStandardPropertiesOutput, error) {
		items, err := gw.ListStandardProperties(ctx, in.ID)
		if err != nil {
			return nil, mapStandardPropertyErr(err)
		}
		out := &listStandardPropertiesOutput{}
		out.Body.Properties = make([]standardPropertyBody, 0, len(items))
		for i := range items {
			out.Body.Properties = append(out.Body.Properties, toStandardPropertyBody(&items[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "set-standard-property",
		Method:      http.MethodPut,
		Path:        "/standards/{id}/properties/{property}",
		Summary:     "Declare a property on a standard",
		Description: "Declares a catalog property on a custom standard, or revises the declaration in place (the line is addressed by name, so the write is idempotent). Official standards are read-only (422); an unknown standard is a 404 and a property the catalog does not know is a 422. Gated by standard:update.",
	}, "standard", "update"), func(ctx context.Context, in *setStandardPropertyInput) (*standardPropertyOutput, error) {
		def, err := encodePropertyJSON(in.Body.DefaultValue, "default_value")
		if err != nil {
			return nil, err
		}
		sp, err := gw.SetStandardProperty(ctx, actorID(ctx), in.ID, storage.StandardPropertySpec{
			PropertyName: in.Property,
			DefaultValue: def,
			Required:     in.Body.Required,
		})
		if err != nil {
			return nil, mapStandardPropertyErr(err)
		}
		return &standardPropertyOutput{Body: toStandardPropertyBody(sp)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-standard-property",
		Method:        http.MethodDelete,
		Path:          "/standards/{id}/properties/{property}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Withdraw a property from a standard",
		Description:   "Removes one line from a custom standard's contract; conforming systems keep any value they set for it, now off-contract. A property the standard does not declare is a 404, and an official standard is read-only (422). Gated by standard:delete.",
	}, "standard", "delete"), func(ctx context.Context, in *standardPropertyPathInput) (*struct{}, error) {
		if err := gw.DeleteStandardProperty(ctx, actorID(ctx), in.ID, in.Property); err != nil {
			return nil, mapStandardPropertyErr(err)
		}
		return nil, nil
	})
}

// mapStandardPropertyErr translates the contract storage sentinels into HTTP
// status, mirroring the product contract: a property the catalog does not know is
// a 422 (the request names a property that does not exist), everything else falls
// through to the shared type-registry mapping, where the standard is the type:
// not-found 404, official read-only 422.
func mapStandardPropertyErr(err error) error {
	if errors.Is(err, storage.ErrPropertyNotFound) {
		return huma.Error422UnprocessableEntity("unknown property")
	}
	return mapTypeErr(err, "standard property")
}
