package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// variableBody is the wire shape of a stored variable. Value is the polymorphic
// JSON value (a string, number, bool, or object), typed by ValueType; it is shown
// in the clear (a variable is plaintext, unlike a secret).
type variableBody struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	ValueType string  `json:"value_type"`
	OwnerKind string  `json:"owner_kind"`
	OwnerID   *string `json:"owner_id,omitempty" doc:"The owning entity's id, the canonical handle; absent for a global owner"`
	OwnerName string  `json:"owner_name,omitempty"`
	Value     any     `json:"value" doc:"The value, shape given by value_type"`
}

func toVariableBody(v *storage.Variable) variableBody {
	return variableBody{
		ID: v.ID, Name: v.Name, ValueType: v.ValueType,
		OwnerKind: v.OwnerKind, OwnerID: v.OwnerID, OwnerName: v.OwnerName,
		Value: decodeVariableValue(v.Value),
	}
}

// decodeVariableValue turns the stored jsonb bytes into the polymorphic value the
// wire body carries. A malformed value decodes to null rather than failing the
// whole response (the stored value is always valid by the write gate).
func decodeVariableValue(raw json.RawMessage) any {
	var v any
	_ = json.Unmarshal(raw, &v)
	return v
}

type listVariablesOutput struct {
	Body struct {
		Variables []variableBody `json:"variables"`
	}
}

type variableOutput struct {
	Body variableBody
}

type createVariableInput struct {
	Body struct {
		Name      string  `json:"name" minLength:"1" doc:"The cascade key; unique per owner"`
		ValueType string  `json:"value_type" enum:"string,int,float,bool,json" doc:"The declared value type"`
		OwnerKind string  `json:"owner_kind" enum:"global,location,system,component" doc:"Which tier owns this variable"`
		Owner     *string `json:"owner,omitempty" doc:"The owning entity's name; omit for a global variable"`
		Value     any     `json:"value" doc:"The value, validated against value_type"`
	}
}

type variableIDInput struct {
	ID string `path:"id" doc:"The variable's id"`
}

type updateVariableInput struct {
	ID   string `path:"id" doc:"The variable's id"`
	Body struct {
		Value any `json:"value" doc:"The new value, validated against the fixed value_type"`
	}
}

// registerVariableRoutes wires the variable surface: the all-scope admin
// directory and scoped create/update/delete. Read rides the viewer floor; create
// and update are gated by variable:create / variable:update (granted to
// operators), delete by variable:delete (admin, owner).
func registerVariableRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-variables",
		Method:      http.MethodGet,
		Path:        "/variables",
		Summary:     "List variables (admin directory)",
		Description: "Lists every variable. Requires an all-scope read. Gated by variable:read.",
	}, "variable", "read"), func(ctx context.Context, _ *struct{}) (*listVariablesOutput, error) {
		vars, err := gw.ListVariables(ctx, a.scopeFor(ctx, "variable", "read"))
		if err != nil {
			return nil, mapVariableErr(err)
		}
		out := &listVariablesOutput{}
		out.Body.Variables = make([]variableBody, 0, len(vars))
		for i := range vars {
			out.Body.Variables = append(out.Body.Variables, toVariableBody(&vars[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-variable",
		Method:        http.MethodPost,
		Path:          "/variables",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a variable",
		Description:   "Sets a variable at an owner scope (a global variable needs an all-scoped grant). The value is validated against value_type. Gated by variable:create.",
	}, "variable", "create"), func(ctx context.Context, in *createVariableInput) (*variableOutput, error) {
		raw, err := json.Marshal(in.Body.Value)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("value is not encodable")
		}
		v, err := gw.CreateVariable(ctx, actorID(ctx), storage.VariableSpec{
			Name:      in.Body.Name,
			ValueType: in.Body.ValueType,
			OwnerKind: in.Body.OwnerKind,
			OwnerName: in.Body.Owner,
			Value:     raw,
		}, a.scopeFor(ctx, "variable", "create"))
		if err != nil {
			return nil, mapVariableErr(err)
		}
		return &variableOutput{Body: toVariableBody(v)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-variable",
		Method:      http.MethodPatch,
		Path:        "/variables/{id}",
		Summary:     "Update a variable's value",
		Description: "Replaces a variable's value, validated against its fixed value_type. Only the value changes; name, type, and owner are fixed at creation. Gated by variable:update.",
	}, "variable", "update"), func(ctx context.Context, in *updateVariableInput) (*variableOutput, error) {
		raw, err := json.Marshal(in.Body.Value)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("value is not encodable")
		}
		v, err := gw.UpdateVariable(ctx, actorID(ctx), in.ID, raw,
			a.scopeFor(ctx, "variable", "read"), a.scopeFor(ctx, "variable", "update"))
		if err != nil {
			return nil, mapVariableErr(err)
		}
		return &variableOutput{Body: toVariableBody(v)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-variable",
		Method:        http.MethodDelete,
		Path:          "/variables/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a variable",
		Description:   "Removes a variable by id. Gated by variable:delete; read and delete scopes on the owner drive the 404 versus 403 split.",
	}, "variable", "delete"), func(ctx context.Context, in *variableIDInput) (*struct{}, error) {
		if err := gw.DeleteVariable(ctx, actorID(ctx), in.ID,
			a.scopeFor(ctx, "variable", "read"), a.scopeFor(ctx, "variable", "delete")); err != nil {
			return nil, mapVariableErr(err)
		}
		return nil, nil
	})
}

// mapVariableErr translates the gateway's variable sentinels into HTTP status.
func mapVariableErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrVariableNotFound):
		return huma.Error404NotFound("variable not found")
	case errors.Is(err, storage.ErrComponentNotFound):
		return huma.Error404NotFound("component not found")
	case errors.Is(err, storage.ErrVariableForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, storage.ErrVariableExists):
		return huma.Error409Conflict("a variable with this name already exists at this scope")
	case errors.Is(err, storage.ErrUnknownValueType):
		return huma.Error422UnprocessableEntity("unknown value_type")
	case errors.Is(err, storage.ErrVariableOwnerNotFound):
		return huma.Error422UnprocessableEntity("variable owner not found")
	case errors.Is(err, storage.ErrVariableValueInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	default:
		return huma.Error500InternalServerError("variable operation failed")
	}
}
