package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// propertyBody is the wire shape of a property: the non-numeric signal-catalog
// entry a sample observes and a field declares (#587 split the numeric lane into
// /metric-types, which carries unit and precision). validation is a JSON Schema
// fragment; official marks a seed-owned, read-only property.
type propertyBody struct {
	ID            string          `json:"id" doc:"The property's uuid, the stable form the contract and telemetry keys store"`
	Name          string          `json:"name"`
	DataType      string          `json:"data_type"`
	Label         string          `json:"label,omitempty"`
	Description   string          `json:"description,omitempty"`
	Validation    json.RawMessage `json:"validation,omitempty" doc:"A JSON Schema fragment constraining the value"`
	Official      bool            `json:"official"`
	RetentionDays *int            `json:"retention_days,omitempty" required:"false" doc:"The type's own retention window in days, overriding its lane's; absent when the lane's applies (#417)"`
	WrittenBy     *calcWriterBody `json:"written_by,omitempty" required:"false" doc:"The calc rule that writes this property, absent when none does (one enabled rule writes a key; a disabled writer is named when no rule is enabled)"`
	WriterClass   string          `json:"writer_class" enum:"node-probe,platform,calc,collected" doc:"Who may write this key (ADR-0161): node-probe (a node's own probe or session machine, under the endpoint it probed), platform (the Omniglass process itself: the health rollup), calc (the calc rule that outputs it), or collected (a driver's emits or a push, and a command's or an operator's value). The Storage Gateway refuses any write whose writer the class does not admit"`
}

func toPropertyBody(p *storage.PropertyType) propertyBody {
	b := propertyBody{
		ID: p.ID, Name: p.Name, DataType: p.DataType, Label: p.Label,
		RetentionDays: p.RetentionDays,
		Description:   p.Description, Official: p.Official, WrittenBy: toCalcWriterBody(p.WrittenBy),
		WriterClass: string(p.WriterClass),
	}
	if len(p.Validation) > 0 {
		b.Validation = json.RawMessage(p.Validation)
	}
	return b
}

type listPropertiesOutput struct {
	Body struct {
		Properties []propertyBody `json:"properties"`
	}
}

type propertyOutput struct {
	Body propertyBody
}

type createPropertyInput struct {
	Body struct {
		Name        string `json:"name" minLength:"1" doc:"The property name (lowercase kebab)"`
		DataType    string `json:"data_type" enum:"string,bool,json" doc:"The value type; a numeric signal is a metric type"`
		Label       string `json:"label,omitempty" doc:"A human label"`
		Description string `json:"description,omitempty" doc:"What the property means"`
		Validation  any    `json:"validation,omitempty" doc:"A JSON Schema fragment constraining the value"`
	}
}

type setPropertyTypeRetentionInput struct {
	Name string `path:"name" doc:"The property type's name"`
	Body setTypeRetentionBody
}

type propertyNameInput struct {
	Name string `path:"name" doc:"The property's name"`
}

type updatePropertyInput struct {
	Name string `path:"name" doc:"The property's name"`
	Body struct {
		Label       *string `json:"label,omitempty" doc:"A human label"`
		Description *string `json:"description,omitempty" doc:"What the property means"`
		Validation  any     `json:"validation,omitempty" doc:"A JSON Schema fragment (replaces wholesale)"`
	}
}

// registerPropertyRoutes wires the property catalog: the fleet-wide signal
// directory (no scope injection, it is reference data) and its custom-property CRUD.
// Read rides the viewer floor; create/update/delete are gated by property_type:create /
// property_type:update / property_type:delete. Official (seed-owned) properties are read-only.
func registerPropertyRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-property-type",
		Method:      http.MethodGet,
		Path:        "/property-types",
		Summary:     "List properties",
		Description: "Lists every registered property (official and custom). The catalog is fleet-wide reference data. Gated by property_type:read.",
	}, "property_type", "read"), func(ctx context.Context, _ *struct{}) (*listPropertiesOutput, error) {
		properties, err := gw.ListPropertyTypes(ctx)
		if err != nil {
			return nil, mapPropertyErr(err)
		}
		out := &listPropertiesOutput{}
		out.Body.Properties = make([]propertyBody, 0, len(properties))
		for i := range properties {
			out.Body.Properties = append(out.Body.Properties, toPropertyBody(&properties[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-property-type",
		Method:      http.MethodGet,
		Path:        "/property-types/{name}",
		Summary:     "Get a property",
		Description: "Returns one property by name. Gated by property_type:read.",
	}, "property_type", "read"), func(ctx context.Context, in *propertyNameInput) (*propertyOutput, error) {
		p, err := gw.GetPropertyType(ctx, in.Name)
		if err != nil {
			return nil, mapPropertyErr(err)
		}
		return &propertyOutput{Body: toPropertyBody(p)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-property-type",
		Method:        http.MethodPost,
		Path:          "/property-types",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a property",
		Description:   "Registers a custom property (official=false). The name must be a valid property key. Gated by property_type:create.",
	}, "property_type", "create"), func(ctx context.Context, in *createPropertyInput) (*propertyOutput, error) {
		validation, err := marshalValidation(in.Body.Validation)
		if err != nil {
			return nil, err
		}
		p, err := gw.CreatePropertyType(ctx, actorID(ctx), storage.PropertyTypeSpec{
			Name:        in.Body.Name,
			DataType:    in.Body.DataType,
			Label:       in.Body.Label,
			Description: in.Body.Description,
			Validation:  validation,
		})
		if err != nil {
			return nil, mapPropertyErr(err)
		}
		return &propertyOutput{Body: toPropertyBody(p)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-property-type",
		Method:      http.MethodPatch,
		Path:        "/property-types/{name}",
		Summary:     "Update a property",
		Description: "Patches a custom property's label, description, or validation (a nil field is unchanged). Data type is fixed at creation. Official properties are read-only. Gated by property_type:update.",
	}, "property_type", "update"), func(ctx context.Context, in *updatePropertyInput) (*propertyOutput, error) {
		validation, err := marshalValidation(in.Body.Validation)
		if err != nil {
			return nil, err
		}
		p, err := gw.UpdatePropertyType(ctx, actorID(ctx), in.Name, storage.PropertyTypePatch{
			Label:       in.Body.Label,
			Description: in.Body.Description,
			Validation:  validation,
		})
		if err != nil {
			return nil, mapPropertyErr(err)
		}
		return &propertyOutput{Body: toPropertyBody(p)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-property-type",
		Method:        http.MethodDelete,
		Path:          "/property-types/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a property",
		Description:   "Removes a custom property by name. Official properties are read-only. A property a calc rule writes is a 409 naming the rule, and one identity bindings use as their match key is a 409 counting them. Gated by property_type:delete.",
	}, "property_type", "delete"), func(ctx context.Context, in *propertyNameInput) (*struct{}, error) {
		if err := gw.DeletePropertyType(ctx, actorID(ctx), in.Name); err != nil {
			return nil, mapPropertyErr(err)
		}
		return nil, nil
	})

	huma.Register(api, a.platformGated(a.gated(huma.Operation{
		OperationID: "set-property-type-retention",
		Method:      http.MethodPost,
		Path:        "/property-types/{name}:setRetention",
		Summary:     "Set a property type's retention window",
		Description: "Sets how many days this type's transitions are kept, overriding the property lane's window (retention.property_days), or with null returns it to the lane's. Works on an official type: a shipped type's definition is read-only, but how long an install keeps its samples is the install's, and the boot seed never resets it. The retention floor still holds: a declared value and a series' latest value are never removed. 1 to 36,500 days. Audited as set_retention. Gated by property_type:update and platform:update.",
	}, "property_type", "update"), "update"), func(ctx context.Context, in *setPropertyTypeRetentionInput) (*propertyOutput, error) {
		// platformGated only publishes the second permission; the handler
		// enforces it, as every platform-tier write does.
		if err := a.requirePlatform(ctx, "update"); err != nil {
			return nil, err
		}
		pt, err := gw.SetPropertyTypeRetention(ctx, actorID(ctx), in.Name, in.Body.Days)
		if err != nil {
			return nil, mapPropertyErr(err)
		}
		return &propertyOutput{Body: toPropertyBody(pt)}, nil
	})
}

// marshalValidation encodes an optional JSON Schema fragment to raw bytes; a nil
// fragment stays nil (unchanged / unset).
func marshalValidation(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("validation is not encodable")
	}
	return raw, nil
}

// mapPropertyErr translates the gateway's property sentinels into HTTP
// status. mapRefErr runs first: a property type's name stays a single global
// token (#627 Task 12), so a dotted ref is storage.ErrAddressNotAccepted, a
// 422 naming the kind, rather than falling through to the plain not-found
// below.
func mapPropertyErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	var keyInUse *storage.IdentityKeyInUseError
	if errors.As(err, &keyInUse) {
		return huma.Error409Conflict(keyInUse.Error())
	}
	var inUse *storage.CalcOutputInUseError
	if errors.As(err, &inUse) {
		return huma.Error409Conflict(inUse.Error())
	}
	switch {
	case errors.Is(err, storage.ErrPropertyTypeNotFound):
		return huma.Error404NotFound("property not found")
	case errors.Is(err, storage.ErrPropertyTypeExists):
		return huma.Error409Conflict("a property with this name already exists")
	case errors.Is(err, storage.ErrPropertyTypeOfficial):
		return huma.Error409Conflict("an official property is read-only")
	case errors.Is(err, storage.ErrPropertyTypeInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	default:
		return huma.Error500InternalServerError("property operation failed")
	}
}
