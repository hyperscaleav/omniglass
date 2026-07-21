package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The system property surface: the effective read (every property the system's
// standard declares, resolved against the system's own value, plus any
// off-contract property set directly on it) and the two writes that set and clear
// one declared override. The component surface's twin on the system arc, over the
// same owner-generic gateway primitive. Gated by system:read / system:update and
// scope-injected on the system arc, so an out-of-scope system is a non-disclosing
// 404 rather than a forbidden.

// systemPropertyInstance is the instance dimension these routes write. The system
// detail sets the un-instanced value; a per-instance property lands with the
// surface that addresses instances.
const systemPropertyInstance = ""

type systemPropertiesOutput struct {
	Body struct {
		System     string                  `json:"system"`
		Properties []effectivePropertyBody `json:"properties"`
	}
}

type systemPropertyOutput struct {
	Body systemPropertyValueBody
}

// systemPropertyValueBody is the write reply: the stored override, echoed back so
// a client that just set a value holds its id without re-reading the list.
type systemPropertyValueBody struct {
	System       string          `json:"system"`
	PropertyName string          `json:"property_name"`
	Value        json.RawMessage `json:"value" doc:"The stored value, shape given by the property's data_type"`
	ValueID      string          `json:"value_id" doc:"The stored value's id"`
}

// systemPropertyPathInput addresses one property on one system.
type systemPropertyPathInput struct {
	Name     string `path:"name" doc:"The system's unique name"`
	Property string `path:"property" doc:"The property name"`
}

type setSystemPropertyInput struct {
	Name     string `path:"name" doc:"The system's unique name"`
	Property string `path:"property" doc:"The property name"`
	Body     struct {
		Value any `json:"value" doc:"The value to declare, shape given by the property's data_type"`
	}
}

// registerSystemPropertyRoutes wires the per-system property surface: the
// effective read and the set/clear of one declared override. The read is gated by
// system:read and the writes by system:update, each carrying the matching scope so
// the gateway resolves the system within it.
func registerSystemPropertyRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-system-properties",
		Method:      http.MethodGet,
		Path:        "/systems/{name}/properties",
		Summary:     "List a system's effective properties",
		Description: "Every property the system's standard declares, resolved to the system's own value or the contract default (is_set marks the override), plus any property set directly on the system (from_contract false). Gated by system:read; an out-of-scope system is a non-disclosing 404.",
	}, "system", "read"), func(ctx context.Context, in *systemPathInput) (*systemPropertiesOutput, error) {
		eff, err := gw.EffectiveProperties(ctx, "system", in.Name, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapSystemPropertyErr(err)
		}
		out := &systemPropertiesOutput{}
		out.Body.System = in.Name
		out.Body.Properties = make([]effectivePropertyBody, 0, len(eff))
		for i := range eff {
			out.Body.Properties = append(out.Body.Properties, toEffectivePropertyBody(&eff[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "set-system-property",
		Method:      http.MethodPut,
		Path:        "/systems/{name}/properties/{property}",
		Summary:     "Set a property on a system",
		Description: "Declares a value for the property on this system, overriding the standard contract's default. Idempotent: the first set stores the value, a later set replaces it. The property need not be on the contract, but it must exist in the catalog (422 otherwise). Gated by system:update; an out-of-scope system is a non-disclosing 404.",
	}, "system", "update"), func(ctx context.Context, in *setSystemPropertyInput) (*systemPropertyOutput, error) {
		raw, err := encodePropertyJSON(in.Body.Value, "value")
		if err != nil {
			return nil, err
		}
		pv, err := gw.SetPropertyValue(ctx, actorID(ctx), "system", in.Name, in.Property,
			systemPropertyInstance, raw, a.scopeFor(ctx, "system", "update"))
		if err != nil {
			return nil, mapSystemPropertyErr(err)
		}
		return &systemPropertyOutput{Body: systemPropertyValueBody{
			System:       in.Name,
			PropertyName: pv.PropertyName,
			Value:        json.RawMessage(pv.Value),
			ValueID:      pv.ID,
		}}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "clear-system-property",
		Method:        http.MethodDelete,
		Path:          "/systems/{name}/properties/{property}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Clear a property on a system",
		Description:   "Removes the system's declared value, so the property falls back to the standard contract's default (or leaves the effective read entirely when it was off-contract). Clearing a property the system never set is a 404. Gated by system:update; an out-of-scope system is a non-disclosing 404.",
	}, "system", "update"), func(ctx context.Context, in *systemPropertyPathInput) (*struct{}, error) {
		if err := gw.ClearPropertyValue(ctx, actorID(ctx), "system", in.Name, in.Property,
			systemPropertyInstance, a.scopeFor(ctx, "system", "update")); err != nil {
			return nil, mapSystemPropertyErr(err)
		}
		return nil, nil
	})
}

// mapSystemPropertyErr translates the property-value sentinels into HTTP status. A
// value the system never set is a 404 (the address is empty), a write naming a
// property or owner that does not exist is a request fault (422), and the system
// sentinels keep the system routes' non-disclosing mapping.
func mapSystemPropertyErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrPropertyValueNotFound):
		return huma.Error404NotFound("property not set on this system")
	case errors.Is(err, storage.ErrPropertyRefNotFound):
		return huma.Error422UnprocessableEntity("unknown property")
	default:
		return mapSystemErr(err)
	}
}
