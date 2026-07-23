package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The location property surface: the effective read (every property the
// location's type declares, resolved against the location's own value, plus any
// off-contract property set directly on it) and the two writes that set and clear
// one declared override. The component surface's twin on the location arc, over
// the same owner-generic gateway primitive. Gated by location:read /
// location:update and scope-injected on the location arc, so an out-of-scope
// location is a non-disclosing 404 rather than a forbidden.

// locationPropertyInstance is the instance dimension these routes write. The
// location detail sets the un-instanced value; a per-instance property lands with
// the surface that addresses instances.
const locationPropertyInstance = ""

type locationPropertiesOutput struct {
	Body struct {
		Location   string                  `json:"location"`
		Properties []effectivePropertyBody `json:"properties"`
	}
}

type locationPropertyOutput struct {
	Body locationPropertyBody
}

// locationPropertyValueBody is the write reply: the stored override, echoed back
// so a client that just set a value holds its id without re-reading the list.
type locationPropertyBody struct {
	Location       string          `json:"location"`
	PropertyName   string          `json:"property_name"`
	PropertyTypeID string          `json:"property_type_id" doc:"The catalog property's uuid, the stable form of property_name"`
	Value          json.RawMessage `json:"value" doc:"The stored value, shape given by the property's data_type"`
	ValueID        string          `json:"value_id" doc:"The stored value's id"`
}

// locationPropertyPathInput addresses one property on one location.
type locationPropertyPathInput struct {
	Name     string `path:"name" doc:"The location's unique name"`
	Property string `path:"property" doc:"The property name"`
}

type setLocationPropertyInput struct {
	Name     string `path:"name" doc:"The location's unique name"`
	Property string `path:"property" doc:"The property name"`
	Body     struct {
		Value any `json:"value" doc:"The value to declare, shape given by the property's data_type"`
	}
}

// registerLocationPropertyRoutes wires the per-location property surface: the
// effective read and the set/clear of one declared override. The read is gated by
// location:read and the writes by location:update, each carrying the matching
// scope so the gateway resolves the location within it.
func registerLocationPropertyRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-location-properties",
		Method:      http.MethodGet,
		Path:        "/locations/{name}/properties",
		Summary:     "List a location's effective properties",
		Description: "Every property the location's type declares, resolved to the location's own value or the contract default (is_set marks the override), plus any property set directly on the location (from_contract false). Gated by location:read; an out-of-scope location is a non-disclosing 404.",
	}, "location", "read"), func(ctx context.Context, in *locationPathInput) (*locationPropertiesOutput, error) {
		eff, err := gw.EffectiveProperties(ctx, "location", in.Name, a.scopeFor(ctx, "location", "read"))
		if err != nil {
			return nil, mapLocationPropertyErr(err)
		}
		out := &locationPropertiesOutput{}
		out.Body.Location = in.Name
		out.Body.Properties = make([]effectivePropertyBody, 0, len(eff))
		for i := range eff {
			out.Body.Properties = append(out.Body.Properties, toEffectivePropertyBody(&eff[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "set-location-property",
		Method:      http.MethodPut,
		Path:        "/locations/{name}/properties/{property}",
		Summary:     "Set a property on a location",
		Description: "Declares a value for the property on this location, overriding the location type contract's default. Idempotent: the first set stores the value, a later set replaces it. The property need not be on the contract, but it must exist in the catalog (422 otherwise). Gated by location:update; an out-of-scope location is a non-disclosing 404.",
	}, "location", "update"), func(ctx context.Context, in *setLocationPropertyInput) (*locationPropertyOutput, error) {
		raw, err := encodePropertyJSON(in.Body.Value, "value")
		if err != nil {
			return nil, err
		}
		pv, err := gw.SetProperty(ctx, actorID(ctx), "location", in.Name, in.Property,
			locationPropertyInstance, raw, a.scopeFor(ctx, "location", "update"))
		if err != nil {
			return nil, mapLocationPropertyErr(err)
		}
		return &locationPropertyOutput{Body: locationPropertyBody{
			Location:       in.Name,
			PropertyName:   pv.PropertyName,
			PropertyTypeID: pv.PropertyTypeID,
			Value:          json.RawMessage(pv.Value),
			ValueID:        pv.ID,
		}}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "clear-location-property",
		Method:        http.MethodDelete,
		Path:          "/locations/{name}/properties/{property}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Clear a property on a location",
		Description:   "Removes the location's declared value, so the property falls back to the location type contract's default (or leaves the effective read entirely when it was off-contract). Clearing a property the location never set is a 404. Gated by location:update; an out-of-scope location is a non-disclosing 404.",
	}, "location", "update"), func(ctx context.Context, in *locationPropertyPathInput) (*struct{}, error) {
		if err := gw.ClearProperty(ctx, actorID(ctx), "location", in.Name, in.Property,
			locationPropertyInstance, a.scopeFor(ctx, "location", "update")); err != nil {
			return nil, mapLocationPropertyErr(err)
		}
		return nil, nil
	})
}

// mapLocationPropertyErr translates the property-value sentinels into HTTP status.
// A value the location never set is a 404 (the address is empty), a write naming a
// property or owner that does not exist is a request fault (422), and the location
// sentinels keep the location routes' non-disclosing mapping.
func mapLocationPropertyErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrPropertyNotFound):
		return huma.Error404NotFound("property not set on this location")
	case errors.Is(err, storage.ErrPropertyRefNotFound):
		return huma.Error422UnprocessableEntity("unknown property")
	default:
		return mapLocationErr(err)
	}
}
