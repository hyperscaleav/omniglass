package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// fieldDefinitionBody is the wire shape of a field definition: a typed field
// declared on a component_type. The catalog is flat and unscoped like the type
// registries (no owner arc, no ABAC scope), so a duplicate name is a request
// fault rather than a scope fault.
type fieldDefinitionBody struct {
	ID            string `json:"id"`
	ComponentType string `json:"component_type"`
	Name          string `json:"name"`
	DataType      string `json:"data_type"`
	DefaultValue  any    `json:"default_value,omitempty" doc:"The type-level default, shape given by data_type; omitted when unset"`
}

func toFieldDefinitionBody(fd *storage.FieldDefinition) fieldDefinitionBody {
	b := fieldDefinitionBody{ID: fd.ID, ComponentType: fd.ComponentType, Name: fd.Name, DataType: fd.DataType}
	if len(fd.DefaultValue) > 0 {
		_ = json.Unmarshal(fd.DefaultValue, &b.DefaultValue)
	}
	return b
}

type listFieldDefinitionsOutput struct {
	Body struct {
		FieldDefinitions []fieldDefinitionBody `json:"field_definitions"`
	}
}

type fieldDefinitionOutput struct {
	Body fieldDefinitionBody
}

type createFieldDefinitionInput struct {
	Body struct {
		ComponentType string `json:"component_type" minLength:"1" doc:"The component_type this field is defined on"`
		Name          string `json:"name" minLength:"1" doc:"The field name; unique per component_type"`
		DataType      string `json:"data_type" enum:"string,int,float,bool,json" doc:"The declared value type"`
		DefaultValue  any    `json:"default_value,omitempty" doc:"Optional type-level default, validated against data_type"`
	}
}

type updateFieldDefinitionInput struct {
	ID   string `path:"id" doc:"The field definition's id"`
	Body struct {
		DataType     string `json:"data_type" enum:"string,int,float,bool,json" doc:"The declared value type"`
		DefaultValue any    `json:"default_value,omitempty" doc:"Optional type-level default, validated against data_type"`
	}
}

type fieldDefinitionIDInput struct {
	ID string `path:"id" doc:"The field definition's id"`
}

// registerFieldRoutes wires the field-definition catalog: a flat, unscoped
// directory of typed fields declared on component types. Every route is gated by
// a field:<action> permission; there is no ABAC scope (the catalog is estate-wide
// like the type registries).
func registerFieldRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, huma.Operation{
		OperationID: "list-field-definitions",
		Method:      http.MethodGet,
		Path:        "/field-definitions",
		Summary:     "List field definitions",
		Description: "Lists every field defined on any component_type (the catalog directory). Gated by field:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("field", "read")},
	}, func(ctx context.Context, _ *struct{}) (*listFieldDefinitionsOutput, error) {
		defs, err := gw.ListFieldDefinitions(ctx)
		if err != nil {
			return nil, mapFieldErr(err)
		}
		out := &listFieldDefinitionsOutput{}
		out.Body.FieldDefinitions = make([]fieldDefinitionBody, 0, len(defs))
		for i := range defs {
			out.Body.FieldDefinitions = append(out.Body.FieldDefinitions, toFieldDefinitionBody(&defs[i]))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-field-definition",
		Method:        http.MethodPost,
		Path:          "/field-definitions",
		DefaultStatus: http.StatusCreated,
		Summary:       "Define a field",
		Description:   "Declares a typed field on a component_type. The default, if given, is validated against data_type. Gated by field:create.",
		Middlewares:   huma.Middlewares{a.authn, a.require("field", "create")},
	}, func(ctx context.Context, in *createFieldDefinitionInput) (*fieldDefinitionOutput, error) {
		def, err := encodeFieldDefault(in.Body.DefaultValue)
		if err != nil {
			return nil, err
		}
		fd, err := gw.CreateFieldDefinition(ctx, actorID(ctx), storage.FieldDefinitionSpec{
			ComponentType: in.Body.ComponentType,
			Name:          in.Body.Name,
			DataType:      in.Body.DataType,
			DefaultValue:  def,
		})
		if err != nil {
			return nil, mapFieldErr(err)
		}
		return &fieldDefinitionOutput{Body: toFieldDefinitionBody(fd)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-field-definition",
		Method:      http.MethodPatch,
		Path:        "/field-definitions/{id}",
		Summary:     "Update a field definition",
		Description: "Replaces a field's data_type and default value, revalidating the default. component_type and name are fixed at creation. Gated by field:update.",
		Middlewares: huma.Middlewares{a.authn, a.require("field", "update")},
	}, func(ctx context.Context, in *updateFieldDefinitionInput) (*fieldDefinitionOutput, error) {
		def, err := encodeFieldDefault(in.Body.DefaultValue)
		if err != nil {
			return nil, err
		}
		fd, err := gw.UpdateFieldDefinition(ctx, actorID(ctx), in.ID, in.Body.DataType, def)
		if err != nil {
			return nil, mapFieldErr(err)
		}
		return &fieldDefinitionOutput{Body: toFieldDefinitionBody(fd)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-field-definition",
		Method:        http.MethodDelete,
		Path:          "/field-definitions/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a field definition",
		Description:   "Removes a field definition by id. Gated by field:delete.",
		Middlewares:   huma.Middlewares{a.authn, a.require("field", "delete")},
	}, func(ctx context.Context, in *fieldDefinitionIDInput) (*struct{}, error) {
		if err := gw.DeleteFieldDefinition(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapFieldErr(err)
		}
		return nil, nil
	})
}

// encodeFieldDefault marshals the polymorphic default value into the jsonb bytes
// the gateway stores. A nil value (the field has no default) encodes to nil, not
// a JSON null.
func encodeFieldDefault(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, huma.Error422UnprocessableEntity("default_value is not encodable")
	}
	return raw, nil
}

// mapFieldErr translates the gateway's field sentinels (definition and value
// tiers) into HTTP status. Shared by the definition routes here and the value
// routes in a later slice.
func mapFieldErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrFieldDefinitionNotFound),
		errors.Is(err, storage.ErrFieldValueNotFound):
		return huma.Error404NotFound("field not found")
	case errors.Is(err, storage.ErrComponentNotFound):
		return huma.Error404NotFound("component not found")
	case errors.Is(err, storage.ErrComponentForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, storage.ErrFieldDefinitionConflict),
		errors.Is(err, storage.ErrFieldValueConflict):
		return huma.Error409Conflict("field already exists")
	case errors.Is(err, storage.ErrUnknownComponentType):
		return huma.Error422UnprocessableEntity("unknown component_type")
	case errors.Is(err, storage.ErrFieldNotApplicable):
		return huma.Error422UnprocessableEntity("field is not defined for this component's type")
	case errors.Is(err, storage.ErrInvalidValue):
		return huma.Error422UnprocessableEntity(err.Error())
	default:
		return huma.Error500InternalServerError("field operation failed")
	}
}
