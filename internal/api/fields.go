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
	DisplayName   string `json:"display_name,omitempty" doc:"Optional human label; the raw name is the key. Omitted when unset"`
	DataType      string `json:"data_type"`
	DefaultValue  any    `json:"default_value,omitempty" doc:"The type-level default, shape given by data_type; omitted when unset"`
}

func toFieldDefinitionBody(fd *storage.FieldDefinition) fieldDefinitionBody {
	b := fieldDefinitionBody{ID: fd.ID, ComponentType: fd.ComponentType, Name: fd.Name, DisplayName: fd.DisplayName, DataType: fd.DataType}
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

// effectiveFieldBody is one field resolved for a component: the effective value
// (the set literal or the type default), plus the override if the component set
// one. Value and SetValue are polymorphic, shaped by data_type.
type effectiveFieldBody struct {
	FieldID     string `json:"field_id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty" doc:"Optional human label; omitted when unset"`
	DataType    string `json:"data_type"`
	Value       any    `json:"value" doc:"The effective value: the set literal, or the type default when unset"`
	SetValue    any    `json:"set_value,omitempty" doc:"The component's override; omitted when the field is unset"`
	IsSet       bool   `json:"is_set" doc:"True when the component overrides the type default"`
	ValueID     string `json:"value_id,omitempty" doc:"The field_value id when set; the id to DELETE to clear the override. Omitted when the field is unset"`
}

type effectiveFieldsOutput struct {
	Body struct {
		Fields []effectiveFieldBody `json:"fields"`
	}
}

// fieldValueBody is the wire shape of a single stored field value: a literal a
// component sets for a field defined on its type.
type fieldValueBody struct {
	ID          string `json:"id"`
	FieldID     string `json:"field_id"`
	ComponentID string `json:"component_id"`
	Value       any    `json:"value" doc:"The literal, shape given by the field's data_type"`
}

type fieldValueOutput struct {
	Body fieldValueBody
}

func toEffectiveFieldBody(ef *storage.EffectiveField) effectiveFieldBody {
	b := effectiveFieldBody{FieldID: ef.FieldID, Name: ef.Name, DisplayName: ef.DisplayName, DataType: ef.DataType, IsSet: ef.IsSet, ValueID: ef.ValueID}
	if len(ef.Value) > 0 {
		_ = json.Unmarshal(ef.Value, &b.Value)
	}
	if len(ef.SetValue) > 0 {
		_ = json.Unmarshal(ef.SetValue, &b.SetValue)
	}
	return b
}

func toFieldValueBody(fv *storage.FieldValue) fieldValueBody {
	b := fieldValueBody{ID: fv.ID, FieldID: fv.FieldID, ComponentID: fv.ComponentID}
	if len(fv.Value) > 0 {
		_ = json.Unmarshal(fv.Value, &b.Value)
	}
	return b
}

type createFieldDefinitionInput struct {
	Body struct {
		ComponentType string `json:"component_type" minLength:"1" doc:"The component_type this field is defined on"`
		Name          string `json:"name" minLength:"1" doc:"The field name; unique per component_type"`
		DisplayName   string `json:"display_name,omitempty" doc:"Optional human label; falls back to name when unset"`
		DataType      string `json:"data_type" enum:"string,int,float,bool,json" doc:"The declared value type"`
		DefaultValue  any    `json:"default_value,omitempty" doc:"Optional type-level default, validated against data_type"`
	}
}

type updateFieldDefinitionInput struct {
	ID   string `path:"id" doc:"The field definition's id"`
	Body struct {
		DataType     string `json:"data_type" enum:"string,int,float,bool,json" doc:"The declared value type"`
		DisplayName  string `json:"display_name,omitempty" doc:"Optional human label; falls back to name when unset"`
		DefaultValue any    `json:"default_value,omitempty" doc:"Optional type-level default, validated against data_type"`
	}
}

type fieldDefinitionIDInput struct {
	ID string `path:"id" doc:"The field definition's id"`
}

type effectiveFieldsInput struct {
	Name string `path:"name" doc:"The component's name"`
}

type setFieldValueInput struct {
	Name string `path:"name" doc:"The component's name"`
	Body struct {
		Field string `json:"field" minLength:"1" doc:"The field name, defined on the component's type"`
		Value any    `json:"value" doc:"The literal, validated against the field's data_type"`
	}
}

type fieldValueIDInput struct {
	ID string `path:"id" doc:"The field value's id"`
}

type updateFieldValueInput struct {
	ID   string `path:"id" doc:"The field value's id"`
	Body struct {
		Value any `json:"value" doc:"The new literal, validated against the field's fixed data_type"`
	}
}

// registerFieldRoutes wires the field-definition catalog: a flat, unscoped
// directory of typed fields declared on component types. Every route is gated by
// a field:<action> permission; there is no ABAC scope (the catalog is estate-wide
// like the type registries).
func registerFieldRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-field-definitions",
		Method:      http.MethodGet,
		Path:        "/field-definitions",
		Summary:     "List field definitions",
		Description: "Lists every field defined on any component_type (the catalog directory). Gated by field:read.",
	}, "field", "read"), func(ctx context.Context, _ *struct{}) (*listFieldDefinitionsOutput, error) {
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

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-field-definition",
		Method:        http.MethodPost,
		Path:          "/field-definitions",
		DefaultStatus: http.StatusCreated,
		Summary:       "Define a field",
		Description:   "Declares a typed field on a component_type. The default, if given, is validated against data_type. Gated by field:create.",
	}, "field", "create"), func(ctx context.Context, in *createFieldDefinitionInput) (*fieldDefinitionOutput, error) {
		def, err := encodeFieldDefault(in.Body.DefaultValue)
		if err != nil {
			return nil, err
		}
		fd, err := gw.CreateFieldDefinition(ctx, actorID(ctx), storage.FieldDefinitionSpec{
			ComponentType: in.Body.ComponentType,
			Name:          in.Body.Name,
			DisplayName:   in.Body.DisplayName,
			DataType:      in.Body.DataType,
			DefaultValue:  def,
		})
		if err != nil {
			return nil, mapFieldErr(err)
		}
		return &fieldDefinitionOutput{Body: toFieldDefinitionBody(fd)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-field-definition",
		Method:      http.MethodPatch,
		Path:        "/field-definitions/{id}",
		Summary:     "Update a field definition",
		Description: "Replaces a field's data_type and default value, revalidating the default. component_type and name are fixed at creation. Gated by field:update.",
	}, "field", "update"), func(ctx context.Context, in *updateFieldDefinitionInput) (*fieldDefinitionOutput, error) {
		def, err := encodeFieldDefault(in.Body.DefaultValue)
		if err != nil {
			return nil, err
		}
		fd, err := gw.UpdateFieldDefinition(ctx, actorID(ctx), in.ID, in.Body.DataType, in.Body.DisplayName, def)
		if err != nil {
			return nil, mapFieldErr(err)
		}
		return &fieldDefinitionOutput{Body: toFieldDefinitionBody(fd)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-field-definition",
		Method:        http.MethodDelete,
		Path:          "/field-definitions/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a field definition",
		Description:   "Removes a field definition by id. Gated by field:delete.",
	}, "field", "delete"), func(ctx context.Context, in *fieldDefinitionIDInput) (*struct{}, error) {
		if err := gw.DeleteFieldDefinition(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapFieldErr(err)
		}
		return nil, nil
	})

	// Value routes. Unlike the definition catalog these are ABAC-scoped to the
	// component: the field:<action> scope contains the owning component (the arc),
	// mirroring the variable value routes.
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-effective-fields",
		Method:      http.MethodGet,
		Path:        "/components/{name}/fields",
		Summary:     "List a component's effective fields",
		Description: "Each field defined on the component's type, resolved to the set literal or the type default (is_set marks the override). Gated by field:read; the component must be in the caller's field read scope.",
	}, "field", "read"), func(ctx context.Context, in *effectiveFieldsInput) (*effectiveFieldsOutput, error) {
		eff, err := gw.EffectiveFields(ctx, in.Name, a.scopeFor(ctx, "field", "read"))
		if err != nil {
			return nil, mapFieldErr(err)
		}
		out := &effectiveFieldsOutput{}
		out.Body.Fields = make([]effectiveFieldBody, 0, len(eff))
		for i := range eff {
			out.Body.Fields = append(out.Body.Fields, toEffectiveFieldBody(&eff[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "set-field-value",
		Method:        http.MethodPost,
		Path:          "/components/{name}/fields",
		DefaultStatus: http.StatusCreated,
		Summary:       "Set a field value on a component",
		Description:   "Sets a literal for a field defined on the component's type, validated against its data_type. Gated by field:create; the component must be in the caller's field create scope.",
	}, "field", "create"), func(ctx context.Context, in *setFieldValueInput) (*fieldValueOutput, error) {
		raw, err := json.Marshal(in.Body.Value)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("value is not encodable")
		}
		fv, err := gw.CreateFieldValue(ctx, actorID(ctx), in.Name, in.Body.Field, raw, a.scopeFor(ctx, "field", "create"))
		if err != nil {
			return nil, mapFieldErr(err)
		}
		return &fieldValueOutput{Body: toFieldValueBody(fv)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-field-value",
		Method:      http.MethodPatch,
		Path:        "/field-values/{id}",
		Summary:     "Update a field value",
		Description: "Replaces a field value's literal, revalidated against the field's fixed data_type. Gated by field:update; read and update scopes on the owning component drive the 404 versus 403 split.",
	}, "field", "update"), func(ctx context.Context, in *updateFieldValueInput) (*fieldValueOutput, error) {
		raw, err := json.Marshal(in.Body.Value)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("value is not encodable")
		}
		fv, err := gw.UpdateFieldValue(ctx, actorID(ctx), in.ID, raw,
			a.scopeFor(ctx, "field", "read"), a.scopeFor(ctx, "field", "update"))
		if err != nil {
			return nil, mapFieldErr(err)
		}
		return &fieldValueOutput{Body: toFieldValueBody(fv)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-field-value",
		Method:        http.MethodDelete,
		Path:          "/field-values/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a field value",
		Description:   "Clears a component's override for a field, reverting it to the type default. Gated by field:delete; read and delete scopes on the owning component drive the 404 versus 403 split.",
	}, "field", "delete"), func(ctx context.Context, in *fieldValueIDInput) (*struct{}, error) {
		if err := gw.DeleteFieldValue(ctx, actorID(ctx), in.ID,
			a.scopeFor(ctx, "field", "read"), a.scopeFor(ctx, "field", "delete")); err != nil {
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
// tiers) into HTTP status. Shared by the definition routes and the value routes.
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
