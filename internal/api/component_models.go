package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/jackc/pgx/v5/pgconn"
)

// componentModelBody is the wire shape of a component_model registry row. The
// registry lists alphabetically by display_name, like component_make.
type componentModelBody struct {
	ID           string     `json:"id"`
	DisplayName  string     `json:"display_name"`
	MakeID       string     `json:"make_id"`
	ModelNumber  string     `json:"model_number"`
	Family       string     `json:"family,omitempty"`
	ReleasedAt   *time.Time `json:"released_at,omitempty"`
	EosAt        *time.Time `json:"eos_at,omitempty"`
	EolAt        *time.Time `json:"eol_at,omitempty"`
	FrontImageID *string    `json:"front_image_id,omitempty"`
	BackImageID  *string    `json:"back_image_id,omitempty"`
	Official     bool       `json:"official"`
}

func toComponentModelBody(m *storage.ComponentModel) componentModelBody {
	return componentModelBody{
		ID: m.ID, DisplayName: m.DisplayName, MakeID: m.MakeID, ModelNumber: m.ModelNumber,
		Family: m.Family, ReleasedAt: m.ReleasedAt, EosAt: m.EosAt, EolAt: m.EolAt,
		FrontImageID: m.FrontImageID, BackImageID: m.BackImageID, Official: m.Official,
	}
}

type listComponentModelsOutput struct {
	Body struct {
		Models []componentModelBody `json:"models"`
	}
}

type componentModelPathInput struct {
	ID string `path:"id" doc:"The component_model id"`
}

type createComponentModelInput struct {
	Body struct {
		ID           string     `json:"id" minLength:"1" doc:"Globally unique model id"`
		DisplayName  string     `json:"display_name" minLength:"1"`
		MakeID       string     `json:"make_id" minLength:"1" doc:"The owning component_make id"`
		ModelNumber  string     `json:"model_number" minLength:"1"`
		Family       string     `json:"family,omitempty"`
		ReleasedAt   *time.Time `json:"released_at,omitempty"`
		EosAt        *time.Time `json:"eos_at,omitempty"`
		EolAt        *time.Time `json:"eol_at,omitempty"`
		FrontImageID *string    `json:"front_image_id,omitempty"`
		BackImageID  *string    `json:"back_image_id,omitempty"`
	}
}

type updateComponentModelInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName  *string    `json:"display_name,omitempty"`
		ModelNumber  *string    `json:"model_number,omitempty"`
		Family       *string    `json:"family,omitempty"`
		ReleasedAt   *time.Time `json:"released_at,omitempty"`
		EosAt        *time.Time `json:"eos_at,omitempty"`
		EolAt        *time.Time `json:"eol_at,omitempty"`
		FrontImageID *string    `json:"front_image_id,omitempty"`
		BackImageID  *string    `json:"back_image_id,omitempty"`
	}
}

type componentModelOutput struct {
	Body componentModelBody
}

// isForeignKeyViolation reports whether err is a Postgres foreign_key_violation
// (23503), used to turn an unknown make_id into a clean 422 rather than a raw
// 500. Mirrors storage.isUniqueViolation's use of pgconn.PgError.Code, but the
// component_model insert doesn't pre-check the make_id (the FK is the check),
// so the API layer detects it directly off the wrapped storage error.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// registerComponentModelRoutes wires the component_model registry CRUD
// surface, on the same pattern as component_make. Gated by
// model:read|create|update|delete: model:read sits in the viewer read-floor
// (*:read), the mutations at the admin tier, exactly like make:*.
func registerComponentModelRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, huma.Operation{
		OperationID: "list-component-models",
		Method:      http.MethodGet,
		Path:        "/component-models",
		Summary:     "List component models",
		Description: "Lists the component_model registry, ordered alphabetically by display name. Gated by model:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("model", "read")},
	}, func(ctx context.Context, _ *struct{}) (*listComponentModelsOutput, error) {
		models, err := gw.ListComponentModels(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list component models")
		}
		out := &listComponentModelsOutput{}
		out.Body.Models = make([]componentModelBody, 0, len(models))
		for i := range models {
			out.Body.Models = append(out.Body.Models, toComponentModelBody(&models[i]))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-component-model",
		Method:        http.MethodPost,
		Path:          "/component-models",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a component model",
		Description:   "Creates a custom (non-official) component_model referencing an existing component_make. An unknown make_id is a 422. Gated by model:create.",
		Middlewares:   huma.Middlewares{a.authn, a.require("model", "create")},
	}, func(ctx context.Context, in *createComponentModelInput) (*componentModelOutput, error) {
		m, err := gw.CreateComponentModel(ctx, actorID(ctx), storage.ComponentModel{
			ID: in.Body.ID, DisplayName: in.Body.DisplayName, MakeID: in.Body.MakeID,
			ModelNumber: in.Body.ModelNumber, Family: in.Body.Family,
			ReleasedAt: in.Body.ReleasedAt, EosAt: in.Body.EosAt, EolAt: in.Body.EolAt,
			FrontImageID: in.Body.FrontImageID, BackImageID: in.Body.BackImageID,
		})
		if err != nil {
			if isForeignKeyViolation(err) {
				return nil, huma.Error422UnprocessableEntity("make_id does not name an existing component_make")
			}
			return nil, mapTypeErr(err, "component_model")
		}
		return &componentModelOutput{Body: toComponentModelBody(m)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-component-model",
		Method:      http.MethodGet,
		Path:        "/component-models/{id}",
		Summary:     "Get a component model",
		Description: "Fetches a component_model by id. Gated by model:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("model", "read")},
	}, func(ctx context.Context, in *componentModelPathInput) (*componentModelOutput, error) {
		m, err := gw.GetComponentModel(ctx, in.ID)
		if err != nil {
			return nil, mapTypeErr(err, "component_model")
		}
		return &componentModelOutput{Body: toComponentModelBody(m)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-component-model",
		Method:      http.MethodPatch,
		Path:        "/component-models/{id}",
		Summary:     "Update a component model",
		Description: "Patches a custom component_model's display_name, model_number, family, lifecycle timestamps, or image pointers. make_id is not patchable. Official models are read-only (422). Gated by model:update.",
		Middlewares: huma.Middlewares{a.authn, a.require("model", "update")},
	}, func(ctx context.Context, in *updateComponentModelInput) (*componentModelOutput, error) {
		m, err := gw.UpdateComponentModel(ctx, actorID(ctx), in.ID, storage.ComponentModelPatch{
			DisplayName: in.Body.DisplayName, ModelNumber: in.Body.ModelNumber, Family: in.Body.Family,
			ReleasedAt: in.Body.ReleasedAt, EosAt: in.Body.EosAt, EolAt: in.Body.EolAt,
			FrontImageID: in.Body.FrontImageID, BackImageID: in.Body.BackImageID,
		})
		if err != nil {
			return nil, mapTypeErr(err, "component_model")
		}
		return &componentModelOutput{Body: toComponentModelBody(m)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-component-model",
		Method:        http.MethodDelete,
		Path:          "/component-models/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a component model",
		Description:   "Deletes a custom component_model, refused if official (422). Gated by model:delete.",
		Middlewares:   huma.Middlewares{a.authn, a.require("model", "delete")},
	}, func(ctx context.Context, in *componentModelPathInput) (*struct{}, error) {
		if err := gw.DeleteComponentModel(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapTypeErr(err, "component_model")
		}
		return nil, nil
	})
}
