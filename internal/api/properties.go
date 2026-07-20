package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// propertyBody is the wire shape of a property: the typed signal-catalog entry a
// datapoint observes and a field declares. Kind (metric/state/log) is present only
// for an observed property; validation is a JSON Schema fragment; official marks a
// seed-owned, read-only property.
type propertyBody struct {
	Name        string          `json:"name"`
	DataType    string          `json:"data_type"`
	DisplayName string          `json:"display_name,omitempty"`
	Description string          `json:"description,omitempty"`
	Unit        *string         `json:"unit,omitempty"`
	Kind        *string         `json:"kind,omitempty"`
	Validation  json.RawMessage `json:"validation,omitempty" doc:"A JSON Schema fragment constraining the value"`
	Official    bool            `json:"official"`
}

func toPropertyBody(p *storage.Property) propertyBody {
	b := propertyBody{
		Name: p.Name, DataType: p.DataType, DisplayName: p.DisplayName,
		Description: p.Description, Unit: p.Unit, Kind: p.Kind, Official: p.Official,
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
		Name        string  `json:"name" minLength:"1" doc:"The property name (lowercase, dot-hierarchied)"`
		DataType    string  `json:"data_type" enum:"string,int,float,bool,json" doc:"The value type"`
		DisplayName string  `json:"display_name,omitempty" doc:"A human label"`
		Description string  `json:"description,omitempty" doc:"What the property means"`
		Unit        *string `json:"unit,omitempty" doc:"A display unit (observed properties)"`
		Kind        *string `json:"kind,omitempty" enum:"metric,state,log" doc:"The observed kind; omit for a declared-only property"`
		Validation  any     `json:"validation,omitempty" doc:"A JSON Schema fragment constraining the value"`
	}
}

type propertyNameInput struct {
	Name string `path:"name" doc:"The property's name"`
}

type updatePropertyInput struct {
	Name string `path:"name" doc:"The property's name"`
	Body struct {
		DisplayName *string `json:"display_name,omitempty" doc:"A human label"`
		Description *string `json:"description,omitempty" doc:"What the property means"`
		Unit        *string `json:"unit,omitempty" doc:"A display unit"`
		Validation  any     `json:"validation,omitempty" doc:"A JSON Schema fragment (replaces wholesale)"`
	}
}

// registerPropertyRoutes wires the property catalog: the estate-wide signal
// directory (no scope injection, it is reference data) and its custom-property CRUD.
// Read rides the viewer floor; create/update/delete are gated by property:create /
// property:update / property:delete. Official (seed-owned) properties are read-only.
func registerPropertyRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-property",
		Method:      http.MethodGet,
		Path:        "/properties",
		Summary:     "List properties",
		Description: "Lists every registered property (official and custom). The catalog is estate-wide reference data. Gated by property:read.",
	}, "property", "read"), func(ctx context.Context, _ *struct{}) (*listPropertiesOutput, error) {
		properties, err := gw.ListProperties(ctx)
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
		OperationID: "get-property",
		Method:      http.MethodGet,
		Path:        "/properties/{name}",
		Summary:     "Get a property",
		Description: "Returns one property by name. Gated by property:read.",
	}, "property", "read"), func(ctx context.Context, in *propertyNameInput) (*propertyOutput, error) {
		p, err := gw.GetProperty(ctx, in.Name)
		if err != nil {
			return nil, mapPropertyErr(err)
		}
		return &propertyOutput{Body: toPropertyBody(p)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-property",
		Method:        http.MethodPost,
		Path:          "/properties",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a property",
		Description:   "Registers a custom property (official=false). The name must be a valid property key. Gated by property:create.",
	}, "property", "create"), func(ctx context.Context, in *createPropertyInput) (*propertyOutput, error) {
		validation, err := marshalValidation(in.Body.Validation)
		if err != nil {
			return nil, err
		}
		p, err := gw.CreateProperty(ctx, actorID(ctx), storage.PropertySpec{
			Name:        in.Body.Name,
			DataType:    in.Body.DataType,
			DisplayName: in.Body.DisplayName,
			Description: in.Body.Description,
			Unit:        in.Body.Unit,
			Kind:        in.Body.Kind,
			Validation:  validation,
		})
		if err != nil {
			return nil, mapPropertyErr(err)
		}
		return &propertyOutput{Body: toPropertyBody(p)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-property",
		Method:      http.MethodPatch,
		Path:        "/properties/{name}",
		Summary:     "Update a property",
		Description: "Patches a custom property's label, description, unit, or validation (a nil field is unchanged). Data type and kind are fixed at creation. Official properties are read-only. Gated by property:update.",
	}, "property", "update"), func(ctx context.Context, in *updatePropertyInput) (*propertyOutput, error) {
		validation, err := marshalValidation(in.Body.Validation)
		if err != nil {
			return nil, err
		}
		p, err := gw.UpdateProperty(ctx, actorID(ctx), in.Name, storage.PropertyPatch{
			DisplayName: in.Body.DisplayName,
			Description: in.Body.Description,
			Unit:        in.Body.Unit,
			Validation:  validation,
		})
		if err != nil {
			return nil, mapPropertyErr(err)
		}
		return &propertyOutput{Body: toPropertyBody(p)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-property",
		Method:        http.MethodDelete,
		Path:          "/properties/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a property",
		Description:   "Removes a custom property by name. Official properties are read-only. Gated by property:delete.",
	}, "property", "delete"), func(ctx context.Context, in *propertyNameInput) (*struct{}, error) {
		if err := gw.DeleteProperty(ctx, actorID(ctx), in.Name); err != nil {
			return nil, mapPropertyErr(err)
		}
		return nil, nil
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

// mapPropertyErr translates the gateway's property sentinels into HTTP status.
func mapPropertyErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrPropertyNotFound):
		return huma.Error404NotFound("property not found")
	case errors.Is(err, storage.ErrPropertyExists):
		return huma.Error409Conflict("a property with this name already exists")
	case errors.Is(err, storage.ErrPropertyOfficial):
		return huma.Error409Conflict("an official property is read-only")
	case errors.Is(err, storage.ErrPropertyInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	default:
		return huma.Error500InternalServerError("property operation failed")
	}
}
