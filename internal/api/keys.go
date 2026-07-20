package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// keyBody is the wire shape of a canonical key: the typed keyspace entry a
// datapoint observes and a field declares. Kind (metric/state/log) is present only
// for an observed key; validation is a JSON Schema fragment; official marks a
// seed-owned, read-only key.
type keyBody struct {
	Name        string          `json:"name"`
	DataType    string          `json:"data_type"`
	DisplayName string          `json:"display_name,omitempty"`
	Description string          `json:"description,omitempty"`
	Unit        *string         `json:"unit,omitempty"`
	Kind        *string         `json:"kind,omitempty"`
	Validation  json.RawMessage `json:"validation,omitempty" doc:"A JSON Schema fragment constraining the value"`
	Official    bool            `json:"official"`
}

func toKeyBody(k *storage.Key) keyBody {
	b := keyBody{
		Name: k.Name, DataType: k.DataType, DisplayName: k.DisplayName,
		Description: k.Description, Unit: k.Unit, Kind: k.Kind, Official: k.Official,
	}
	if len(k.Validation) > 0 {
		b.Validation = json.RawMessage(k.Validation)
	}
	return b
}

type listKeysOutput struct {
	Body struct {
		Keys []keyBody `json:"keys"`
	}
}

type keyOutput struct {
	Body keyBody
}

type createKeyInput struct {
	Body struct {
		Name        string  `json:"name" minLength:"1" doc:"The canonical key name (lowercase, dot-hierarchied)"`
		DataType    string  `json:"data_type" enum:"string,int,float,bool,json" doc:"The value type"`
		DisplayName string  `json:"display_name,omitempty" doc:"A human label"`
		Description string  `json:"description,omitempty" doc:"What the key means"`
		Unit        *string `json:"unit,omitempty" doc:"A display unit (observed keys)"`
		Kind        *string `json:"kind,omitempty" enum:"metric,state,log" doc:"The observed kind; omit for a declared-only key"`
		Validation  any     `json:"validation,omitempty" doc:"A JSON Schema fragment constraining the value"`
	}
}

type keyNameInput struct {
	Name string `path:"name" doc:"The key's name"`
}

type updateKeyInput struct {
	Name string `path:"name" doc:"The key's name"`
	Body struct {
		DisplayName *string `json:"display_name,omitempty" doc:"A human label"`
		Description *string `json:"description,omitempty" doc:"What the key means"`
		Unit        *string `json:"unit,omitempty" doc:"A display unit"`
		Validation  any     `json:"validation,omitempty" doc:"A JSON Schema fragment (replaces wholesale)"`
	}
}

// registerKeyRoutes wires the canonical-key catalog: the estate-wide keyspace
// directory (no scope injection, it is reference data) and its custom-key CRUD.
// Read rides the viewer floor; create/update/delete are gated by key:create /
// key:update / key:delete. Official (seed-owned) keys are read-only.
func registerKeyRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-keys",
		Method:      http.MethodGet,
		Path:        "/keys",
		Summary:     "List canonical keys",
		Description: "Lists every registered key (official and custom). The keyspace is estate-wide reference data. Gated by key:read.",
	}, "key", "read"), func(ctx context.Context, _ *struct{}) (*listKeysOutput, error) {
		keys, err := gw.ListKeys(ctx)
		if err != nil {
			return nil, mapKeyErr(err)
		}
		out := &listKeysOutput{}
		out.Body.Keys = make([]keyBody, 0, len(keys))
		for i := range keys {
			out.Body.Keys = append(out.Body.Keys, toKeyBody(&keys[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-key",
		Method:      http.MethodGet,
		Path:        "/keys/{name}",
		Summary:     "Get a canonical key",
		Description: "Returns one key by name. Gated by key:read.",
	}, "key", "read"), func(ctx context.Context, in *keyNameInput) (*keyOutput, error) {
		k, err := gw.GetKey(ctx, in.Name)
		if err != nil {
			return nil, mapKeyErr(err)
		}
		return &keyOutput{Body: toKeyBody(k)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-key",
		Method:        http.MethodPost,
		Path:          "/keys",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a canonical key",
		Description:   "Registers a custom key (official=false). The name must be a valid canonical key. Gated by key:create.",
	}, "key", "create"), func(ctx context.Context, in *createKeyInput) (*keyOutput, error) {
		validation, err := marshalValidation(in.Body.Validation)
		if err != nil {
			return nil, err
		}
		k, err := gw.CreateKey(ctx, actorID(ctx), storage.KeySpec{
			Name:        in.Body.Name,
			DataType:    in.Body.DataType,
			DisplayName: in.Body.DisplayName,
			Description: in.Body.Description,
			Unit:        in.Body.Unit,
			Kind:        in.Body.Kind,
			Validation:  validation,
		})
		if err != nil {
			return nil, mapKeyErr(err)
		}
		return &keyOutput{Body: toKeyBody(k)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-key",
		Method:      http.MethodPatch,
		Path:        "/keys/{name}",
		Summary:     "Update a canonical key",
		Description: "Patches a custom key's label, description, unit, or validation (a nil field is unchanged). Data type and kind are fixed at creation. Official keys are read-only. Gated by key:update.",
	}, "key", "update"), func(ctx context.Context, in *updateKeyInput) (*keyOutput, error) {
		validation, err := marshalValidation(in.Body.Validation)
		if err != nil {
			return nil, err
		}
		k, err := gw.UpdateKey(ctx, actorID(ctx), in.Name, storage.KeyPatch{
			DisplayName: in.Body.DisplayName,
			Description: in.Body.Description,
			Unit:        in.Body.Unit,
			Validation:  validation,
		})
		if err != nil {
			return nil, mapKeyErr(err)
		}
		return &keyOutput{Body: toKeyBody(k)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-key",
		Method:        http.MethodDelete,
		Path:          "/keys/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a canonical key",
		Description:   "Removes a custom key by name. Official keys are read-only. Gated by key:delete.",
	}, "key", "delete"), func(ctx context.Context, in *keyNameInput) (*struct{}, error) {
		if err := gw.DeleteKey(ctx, actorID(ctx), in.Name); err != nil {
			return nil, mapKeyErr(err)
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

// mapKeyErr translates the gateway's key sentinels into HTTP status.
func mapKeyErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrKeyNotFound):
		return huma.Error404NotFound("key not found")
	case errors.Is(err, storage.ErrKeyExists):
		return huma.Error409Conflict("a key with this name already exists")
	case errors.Is(err, storage.ErrKeyOfficial):
		return huma.Error409Conflict("an official key is read-only")
	case errors.Is(err, storage.ErrKeyInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	default:
		return huma.Error500InternalServerError("key operation failed")
	}
}
