package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// systemBody is the wire shape of a system.
type systemBody struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	DisplayName string  `json:"display_name,omitempty"`
	SystemType  string  `json:"system_type"`
	ParentID    *string `json:"parent_id,omitempty"`
	LocationID  *string `json:"location_id,omitempty"`
}

func toSystemBody(s *storage.System) systemBody {
	return systemBody{
		ID: s.ID, Name: s.Name, DisplayName: s.DisplayName,
		SystemType: s.SystemType, ParentID: s.ParentID, LocationID: s.LocationID,
	}
}

type listSystemsOutput struct {
	Body struct {
		Systems []systemBody `json:"systems"`
	}
}

type systemOutput struct {
	Body systemBody
}

type systemPathInput struct {
	Name string `path:"name" doc:"The system's unique name"`
}

type createSystemInput struct {
	Body struct {
		Name        string  `json:"name" minLength:"1" doc:"Globally unique name (the address)"`
		DisplayName string  `json:"display_name,omitempty"`
		SystemType  string  `json:"system_type" minLength:"1" doc:"A system_type id"`
		Parent      *string `json:"parent,omitempty" doc:"Parent system name; omit for a root system"`
		Location    *string `json:"location,omitempty" doc:"Location name this system is placed at"`
	}
}

type updateSystemInput struct {
	Name string `path:"name"`
	Body struct {
		DisplayName *string `json:"display_name,omitempty"`
		SystemType  *string `json:"system_type,omitempty"`
	}
}

// registerSystemRoutes wires the system CRUD surface, mirroring locations: each
// route declares its capability, each handler resolves the caller's per-action
// scope and hands it to the gateway.
func registerSystemRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, huma.Operation{
		OperationID: "list-systems",
		Method:      http.MethodGet,
		Path:        "/systems",
		Summary:     "List systems in scope",
		Description: "Lists the systems the caller may read, each filtered to its scope subtree. Gated by system:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("system", "read")},
	}, func(ctx context.Context, _ *struct{}) (*listSystemsOutput, error) {
		systems, err := gw.ListSystems(ctx, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, huma.Error500InternalServerError("list systems")
		}
		out := &listSystemsOutput{}
		out.Body.Systems = make([]systemBody, 0, len(systems))
		for i := range systems {
			out.Body.Systems = append(out.Body.Systems, toSystemBody(&systems[i]))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-system",
		Method:      http.MethodGet,
		Path:        "/systems/{name}",
		Summary:     "Get a system",
		Description: "Fetches a system by name within the caller's read scope. Out of scope is a non-disclosing 404. Gated by system:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("system", "read")},
	}, func(ctx context.Context, in *systemPathInput) (*systemOutput, error) {
		s, err := gw.GetSystem(ctx, in.Name, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		return &systemOutput{Body: toSystemBody(s)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-system",
		Method:        http.MethodPost,
		Path:          "/systems",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a system",
		Description:   "Creates a system, optionally under a parent (a root needs an all-scoped grant) and at a location. Gated by system:create.",
		Middlewares:   huma.Middlewares{a.authn, a.require("system", "create")},
	}, func(ctx context.Context, in *createSystemInput) (*systemOutput, error) {
		s, err := gw.CreateSystem(ctx, actorID(ctx), storage.SystemSpec{
			Name:         in.Body.Name,
			DisplayName:  in.Body.DisplayName,
			SystemType:   in.Body.SystemType,
			ParentName:   in.Body.Parent,
			LocationName: in.Body.Location,
		}, a.scopeFor(ctx, "system", "create"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		return &systemOutput{Body: toSystemBody(s)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-system",
		Method:      http.MethodPatch,
		Path:        "/systems/{name}",
		Summary:     "Update a system",
		Description: "Patches a system's display_name or system_type. Gated by system:update; read and update scopes drive the 404 versus 403 split.",
		Middlewares: huma.Middlewares{a.authn, a.require("system", "update")},
	}, func(ctx context.Context, in *updateSystemInput) (*systemOutput, error) {
		s, err := gw.UpdateSystem(ctx, actorID(ctx), in.Name, storage.SystemPatch{
			DisplayName: in.Body.DisplayName,
			SystemType:  in.Body.SystemType,
		}, a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		return &systemOutput{Body: toSystemBody(s)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-system",
		Method:        http.MethodDelete,
		Path:          "/systems/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a system",
		Description:   "Deletes a system, refused while it still has child systems. Gated by system:delete; read and delete scopes drive the 404 versus 403 split.",
		Middlewares:   huma.Middlewares{a.authn, a.require("system", "delete")},
	}, func(ctx context.Context, in *systemPathInput) (*struct{}, error) {
		if err := gw.DeleteSystem(ctx, actorID(ctx), in.Name,
			a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "delete")); err != nil {
			return nil, mapSystemErr(err)
		}
		return nil, nil
	})
}

// mapSystemErr translates the gateway's system sentinels into HTTP status,
// mirroring locations.
func mapSystemErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrSystemNotFound):
		return huma.Error404NotFound("system not found")
	case errors.Is(err, storage.ErrSystemForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, storage.ErrSystemOccupied):
		return huma.Error409Conflict("system has child systems")
	case errors.Is(err, storage.ErrSystemExists):
		return huma.Error409Conflict("system name already exists")
	case errors.Is(err, storage.ErrParentSystemNotFound):
		return huma.Error422UnprocessableEntity("parent system not found")
	case errors.Is(err, storage.ErrUnknownSystemType):
		return huma.Error422UnprocessableEntity("unknown system_type")
	case errors.Is(err, storage.ErrLocationNotFound):
		return huma.Error422UnprocessableEntity("location not found")
	default:
		return huma.Error500InternalServerError("system operation failed")
	}
}
