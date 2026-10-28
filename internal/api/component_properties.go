package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The component property surface: the properties read (every property the
// component has, declared, calculated, observed or told, each row carrying its
// want/told/is reconciliation, #1297) and the two writes that set and clear one
// declared override. Gated by component:read / component:update and
// scope-injected on the component arc, so an out-of-scope component is a
// non-disclosing 404 rather than a forbidden.

// componentPropertyInstance is the instance dimension these routes write. The
// component detail sets the un-instanced value; a per-instance property (one per
// endpoint, say) lands with the surface that addresses instances.
const componentPropertyInstance = ""

type effectivePropertyBody struct {
	PropertyTypeName string              `json:"property_type_name" doc:"The catalog property name"`
	PropertyTypeID   string              `json:"property_type_id" doc:"The catalog property's uuid, the stable form of property_type_name"`
	Label            string              `json:"label,omitempty" doc:"The property's human label; omitted when unset"`
	DataType         string              `json:"data_type" doc:"The declared value type, from the property catalog"`
	Required         bool                `json:"required" doc:"Whether the product contract requires a value; always false off-contract"`
	IsSet            bool                `json:"is_set" doc:"True when the component overrides the contract default"`
	FromContract     bool                `json:"from_contract" doc:"True when the component's product declares the property; false for one set directly on the component"`
	DefaultValue     json.RawMessage     `json:"default_value,omitempty" doc:"The contract default; omitted when the contract sets none"`
	SetValue         json.RawMessage     `json:"set_value,omitempty" doc:"The component's override; omitted when the property is unset"`
	Value            json.RawMessage     `json:"value,omitempty" doc:"The effective value: the override, else a calc rule's calculated value, else the contract default; omitted for a property nothing declares or calculates (one only observed or told), whose standing is its reconciliation"`
	ValueID          string              `json:"value_id,omitempty" doc:"The stored value's id when set; omitted when the property is unset"`
	SourceRule       string              `json:"source_rule,omitempty" required:"false" doc:"The calc rule whose calculated sample is the value, by the name it was written under; absent when the value is a declared one or a default"`
	SourceRuleID     string              `json:"source_rule_id,omitempty" required:"false" doc:"That calc rule's uuid, the stable form of source_rule"`
	Reconciliation   *reconciliationBody `json:"reconciliation,omitempty" required:"false" doc:"Where the property stands: what was declared (want), what a command told the owner (told) and what is observed (is), with drift computed on read. Absent on a property only a calc rule writes, since a calculated value is not what anyone asked for"`
}

// reconciliationBody is a property row's want/told/is pivot (#1297): the read
// side of the provenance axis, resolved across every live instance of the key
// (ADR-0456) and computed on read so it cannot go stale.
type reconciliationBody struct {
	Want       json.RawMessage `json:"want" doc:"The declared value, resolved live from the cascade (the owner's set value, else the contract default); null when nothing declares the property"`
	Told       json.RawMessage `json:"told" doc:"The newest intended value a command set, on any instance; null until commanded"`
	Is         json.RawMessage `json:"is" doc:"The observed value, resolved across every live instance of the property (a deleted endpoint's frozen reading left out): an instance that disagrees with want if any does (the newest such), else the newest observed; null when nothing observes it"`
	IsInstance string          `json:"is_instance" doc:"The instance the observed value came from: the endpoint that observed it or the instance its sender named, empty for the owner-level series (and when is is null)"`
	Drift      bool            `json:"drift" doc:"True when any live observed instance differs from want; false when nothing is declared, since there is nothing to drift from"`
}

func toEffectivePropertyBody(e *storage.EffectiveProperty) effectivePropertyBody {
	var rec *reconciliationBody
	if r := e.Reconciliation; r != nil {
		rec = &reconciliationBody{Want: r.Want, Told: r.Told, Is: r.Is, IsInstance: r.IsInstance, Drift: r.Drift}
	}
	return effectivePropertyBody{
		Reconciliation:   rec,
		PropertyTypeName: e.PropertyTypeName,
		PropertyTypeID:   e.PropertyTypeID,
		Label:            e.Label,
		DataType:         e.DataType,
		Required:         e.Required,
		IsSet:            e.IsSet,
		FromContract:     e.FromContract,
		DefaultValue:     json.RawMessage(e.DefaultValue),
		SetValue:         json.RawMessage(e.SetValue),
		Value:            json.RawMessage(e.Value),
		ValueID:          e.ValueID,
		SourceRule:       e.SourceRule,
		SourceRuleID:     e.SourceRuleID,
	}
}

type componentPropertiesOutput struct {
	Body struct {
		Component  string                  `json:"component"`
		Properties []effectivePropertyBody `json:"properties"`
	}
}

type componentPropertyOutput struct {
	Body componentPropertyBody
}

// componentPropertyBody is the write reply: the stored override, echoed back
// so a client that just set a value holds its id without re-reading the list.
type componentPropertyBody struct {
	Component        string          `json:"component"`
	PropertyTypeName string          `json:"property_type_name"`
	PropertyTypeID   string          `json:"property_type_id" doc:"The catalog property's uuid, the stable form of property_type_name"`
	Value            json.RawMessage `json:"value" doc:"The stored value, shape given by the property's data_type"`
	ValueID          string          `json:"value_id" doc:"The stored value's id"`
}

// componentPropertyPathInput addresses one property on one component.
type componentPropertyPathInput struct {
	Name     string `path:"name" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
	Property string `path:"property" doc:"The property name"`
}

type setComponentPropertyInput struct {
	Name     string `path:"name" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
	Property string `path:"property" doc:"The property name"`
	Body     struct {
		Value any `json:"value" doc:"The value to declare, shape given by the property's data_type"`
	}
}

// registerComponentPropertyRoutes wires the per-component property surface: the
// effective read and the set/clear of one declared override. The read is gated by
// component:read and the writes by component:update, each carrying the matching
// scope so the gateway resolves the component within it.
func registerComponentPropertyRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-component-properties",
		Extensions:  mcpExposed(),
		Method:      http.MethodGet,
		Path:        "/components/{name}/properties",
		Summary:     "List a component's properties and where each stands",
		Description: "Every property the component has: each its product declares, resolved to the component's own value or the contract default (is_set marks the override), each set directly on the component (from_contract false), each a calc rule writes (source_rule), and each only observed or told by a command. Every row but a calculated one carries its reconciliation: want (declared), told (intended), is (observed, worst case across every live instance, naming is_instance) and drift, computed on read. Gated by component:read; an out-of-scope component is a non-disclosing 404.",
	}, "component", "read"), func(ctx context.Context, in *componentPathInput) (*componentPropertiesOutput, error) {
		eff, err := gw.EffectiveProperties(ctx, "component", in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentPropertyErr(err)
		}
		out := &componentPropertiesOutput{}
		out.Body.Component = in.Name
		out.Body.Properties = make([]effectivePropertyBody, 0, len(eff))
		for i := range eff {
			out.Body.Properties = append(out.Body.Properties, toEffectivePropertyBody(&eff[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "set-component-property",
		Method:      http.MethodPut,
		Path:        "/components/{name}/properties/{property}",
		Summary:     "Set a property on a component",
		Description: "Declares a value for the property on this component, overriding the product contract's default. Idempotent: the first set stores the value, a later set replaces it. The property need not be on the contract, but it must exist in the catalog (422 otherwise). Gated by component:update; read and update scopes drive the 404 versus 403 split.",
	}, "component", "update"), func(ctx context.Context, in *setComponentPropertyInput) (*componentPropertyOutput, error) {
		raw, err := encodePropertyJSON(in.Body.Value, "value")
		if err != nil {
			return nil, err
		}
		pv, err := gw.SetProperty(ctx, actorID(ctx), "component", in.Name, in.Property,
			componentPropertyInstance, raw,
			a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "component", "update"))
		if err != nil {
			return nil, mapComponentPropertyErr(err)
		}
		return &componentPropertyOutput{Body: componentPropertyBody{
			Component:        in.Name,
			PropertyTypeName: pv.PropertyTypeName,
			PropertyTypeID:   pv.PropertyTypeID,
			Value:            json.RawMessage(pv.Value),
			ValueID:          pv.ID,
		}}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "clear-component-property",
		Method:        http.MethodDelete,
		Path:          "/components/{name}/properties/{property}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Clear a property on a component",
		Description:   "Removes the component's declared value, so the property falls back to the product contract's default (or leaves the effective read entirely when it was off-contract). Clearing a property the component never set is a 404. Gated by component:update; read and update scopes drive the 404 versus 403 split.",
	}, "component", "update"), func(ctx context.Context, in *componentPropertyPathInput) (*struct{}, error) {
		if err := gw.ClearProperty(ctx, actorID(ctx), "component", in.Name, in.Property,
			componentPropertyInstance,
			a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "component", "update")); err != nil {
			return nil, mapComponentPropertyErr(err)
		}
		return nil, nil
	})
}

// mapComponentPropertyErr translates the property-value sentinels into HTTP
// status. A value the component never set is a 404 (the address is empty), a
// write naming a property or owner that does not exist is a request fault (422),
// and the component sentinels keep the component routes' non-disclosing mapping.
func mapComponentPropertyErr(err error) error {
	if refused, ok := mapKeyWriterErr(err); ok {
		return refused
	}
	switch {
	case errors.Is(err, storage.ErrPropertyNotFound):
		return huma.Error404NotFound("property not set on this component")
	case errors.Is(err, storage.ErrPropertyRefNotFound):
		return huma.Error422UnprocessableEntity("unknown property")
	default:
		return mapComponentErr(err)
	}
}
