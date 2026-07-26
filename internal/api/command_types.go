package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// commandTypeBody is the wire shape of a command type: an entry in the driver-owned
// catalog of what a component can be told (the "do" half). settle_window_seconds is
// the driver's fact about actuation timing; target_property_type names the property a
// settleable command sets (empty for fire-and-forget); params_schema is an optional
// JSON Schema for the invocation params. official marks a seed-owned, read-only type.
type commandTypeBody struct {
	ID                  string          `json:"id" doc:"The command type's uuid, the stable form of name"`
	Name                string          `json:"name"`
	DisplayName         string          `json:"display_name,omitempty"`
	Description         string          `json:"description,omitempty"`
	ParamsSchema        json.RawMessage `json:"params_schema,omitempty" doc:"A JSON Schema fragment for the invocation params"`
	SettleWindowSeconds int             `json:"settle_window_seconds" doc:"How long the device is given to actuate before a mismatch is a failed command"`
	TargetPropertyType  string          `json:"target_property_type,omitempty" doc:"The property this command sets (empty for a fire-and-forget command)"`
	Official            bool            `json:"official"`
}

func toCommandTypeBody(ct *storage.CommandType) commandTypeBody {
	b := commandTypeBody{
		ID: ct.ID, Name: ct.Name, DisplayName: ct.DisplayName, Description: ct.Description,
		SettleWindowSeconds: ct.SettleWindowSeconds, TargetPropertyType: ct.TargetPropertyType, Official: ct.Official,
	}
	if len(ct.ParamsSchema) > 0 {
		b.ParamsSchema = json.RawMessage(ct.ParamsSchema)
	}
	return b
}

type listCommandTypesOutput struct {
	Body struct {
		CommandTypes []commandTypeBody `json:"command_types"`
	}
}

type commandTypeOutput struct{ Body commandTypeBody }

type commandTypeNameInput struct {
	Name string `path:"name" doc:"The command type's name"`
}

type createCommandTypeInput struct {
	Body struct {
		Name                string `json:"name" minLength:"1" doc:"The command type name (lowercase, dot-hierarchied)"`
		DisplayName         string `json:"display_name,omitempty" doc:"A human label"`
		Description         string `json:"description,omitempty" doc:"What the command does"`
		ParamsSchema        any    `json:"params_schema,omitempty" doc:"A JSON Schema fragment for the params"`
		SettleWindowSeconds int    `json:"settle_window_seconds,omitempty" doc:"The actuation window in seconds (0 = fire-and-forget)"`
		TargetPropertyType  string `json:"target_property_type,omitempty" doc:"The property this command sets, for settlement"`
	}
}

type updateCommandTypeInput struct {
	Name string `path:"name" doc:"The command type's name"`
	Body struct {
		DisplayName         *string `json:"display_name,omitempty" doc:"A human label"`
		Description         *string `json:"description,omitempty" doc:"What the command does"`
		ParamsSchema        any     `json:"params_schema,omitempty" doc:"A JSON Schema fragment (replaces wholesale)"`
		SettleWindowSeconds *int    `json:"settle_window_seconds,omitempty" doc:"The actuation window in seconds"`
		TargetPropertyType  *string `json:"target_property_type,omitempty" doc:"The property this command sets (empty clears it)"`
	}
}

// registerCommandTypeRoutes wires the command_type catalog: the estate-wide "do"
// registry (no scope injection, reference data) and its custom-type CRUD, gated by
// command_type:read / :create / :update / :delete. Official types are read-only.
// Mirrors the property_type and event_type catalogs.
func registerCommandTypeRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-command-type",
		Method:      http.MethodGet,
		Path:        "/command-types",
		Summary:     "List command types",
		Description: "Lists every registered command type (official and custom). Estate-wide reference data. Gated by command_type:read.",
	}, "command_type", "read"), func(ctx context.Context, _ *struct{}) (*listCommandTypesOutput, error) {
		cts, err := gw.ListCommandTypes(ctx)
		if err != nil {
			return nil, mapCommandTypeErr(err)
		}
		out := &listCommandTypesOutput{}
		out.Body.CommandTypes = make([]commandTypeBody, 0, len(cts))
		for i := range cts {
			out.Body.CommandTypes = append(out.Body.CommandTypes, toCommandTypeBody(&cts[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-command-type",
		Method:      http.MethodGet,
		Path:        "/command-types/{name}",
		Summary:     "Get a command type",
		Description: "Returns one command type by name. Gated by command_type:read.",
	}, "command_type", "read"), func(ctx context.Context, in *commandTypeNameInput) (*commandTypeOutput, error) {
		ct, err := gw.GetCommandType(ctx, in.Name)
		if err != nil {
			return nil, mapCommandTypeErr(err)
		}
		return &commandTypeOutput{Body: toCommandTypeBody(ct)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-command-type",
		Method:        http.MethodPost,
		Path:          "/command-types",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a command type",
		Description:   "Registers a custom command type (official=false). The name must be a valid key; a target property, when set, must be registered. Gated by command_type:create.",
	}, "command_type", "create"), func(ctx context.Context, in *createCommandTypeInput) (*commandTypeOutput, error) {
		schema, err := marshalValidation(in.Body.ParamsSchema)
		if err != nil {
			return nil, err
		}
		ct, err := gw.CreateCommandType(ctx, actorID(ctx), storage.CommandTypeSpec{
			Name:                in.Body.Name,
			DisplayName:         in.Body.DisplayName,
			Description:         in.Body.Description,
			ParamsSchema:        schema,
			SettleWindowSeconds: in.Body.SettleWindowSeconds,
			TargetPropertyType:  in.Body.TargetPropertyType,
		})
		if err != nil {
			return nil, mapCommandTypeErr(err)
		}
		return &commandTypeOutput{Body: toCommandTypeBody(ct)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-command-type",
		Method:      http.MethodPatch,
		Path:        "/command-types/{name}",
		Summary:     "Update a command type",
		Description: "Patches a custom command type's label, description, params schema, settle window, or target (a nil field is unchanged; an empty target clears it). The name is fixed at creation. Official types are read-only. Gated by command_type:update.",
	}, "command_type", "update"), func(ctx context.Context, in *updateCommandTypeInput) (*commandTypeOutput, error) {
		schema, err := marshalValidation(in.Body.ParamsSchema)
		if err != nil {
			return nil, err
		}
		ct, err := gw.UpdateCommandType(ctx, actorID(ctx), in.Name, storage.CommandTypePatch{
			DisplayName:         in.Body.DisplayName,
			Description:         in.Body.Description,
			ParamsSchema:        schema,
			SettleWindowSeconds: in.Body.SettleWindowSeconds,
			TargetPropertyType:  in.Body.TargetPropertyType,
		})
		if err != nil {
			return nil, mapCommandTypeErr(err)
		}
		return &commandTypeOutput{Body: toCommandTypeBody(ct)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-command-type",
		Method:        http.MethodDelete,
		Path:          "/command-types/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a command type",
		Description:   "Removes a custom command type by name. Official types are read-only. Gated by command_type:delete.",
	}, "command_type", "delete"), func(ctx context.Context, in *commandTypeNameInput) (*struct{}, error) {
		if err := gw.DeleteCommandType(ctx, actorID(ctx), in.Name); err != nil {
			return nil, mapCommandTypeErr(err)
		}
		return nil, nil
	})
}

// mapCommandTypeErr translates the gateway's command-type sentinels into HTTP status.
func mapCommandTypeErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrCommandTypeNotFound):
		return huma.Error404NotFound("command type not found")
	case errors.Is(err, storage.ErrCommandTypeExists):
		return huma.Error409Conflict("a command type with this name already exists")
	case errors.Is(err, storage.ErrCommandTypeOfficial):
		return huma.Error409Conflict("an official command type is read-only")
	case errors.Is(err, storage.ErrCommandTypeInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	default:
		return huma.Error500InternalServerError("command type operation failed")
	}
}
