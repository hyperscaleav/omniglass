package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type componentBody struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	DisplayName   string   `json:"display_name,omitempty"`
	ComponentType string   `json:"component_type"`
	ParentID      *string  `json:"parent_id,omitempty"`
	SystemID      *string  `json:"system_id,omitempty"`
	LocationID    *string  `json:"location_id,omitempty"`
	Actions       []string `json:"actions,omitempty" doc:"The scope-aware actions the caller may perform on this row (create a child, update, delete); a UI hint, the server still enforces."`
}

func toComponentBody(c *storage.Component) componentBody {
	return componentBody{
		ID: c.ID, Name: c.Name, DisplayName: c.DisplayName, ComponentType: c.ComponentType,
		ParentID: c.ParentID, SystemID: c.SystemID, LocationID: c.LocationID,
	}
}

type listComponentsOutput struct {
	Body struct {
		Components []componentBody `json:"components"`
	}
}

type componentOutput struct {
	Body componentBody
}

type componentPathInput struct {
	Name string `path:"name" doc:"The component's unique name"`
}

type createComponentInput struct {
	Body struct {
		Name          string  `json:"name" minLength:"1" doc:"Globally unique name (the address)"`
		DisplayName   string  `json:"display_name,omitempty"`
		ComponentType string  `json:"component_type" minLength:"1" doc:"A component_type id"`
		Parent        *string `json:"parent,omitempty" doc:"Parent component name; omit for a root component"`
		System        *string `json:"system,omitempty" doc:"Primary system name this component belongs to"`
		Location      *string `json:"location,omitempty" doc:"Location name this component is placed at"`
	}
}

type updateComponentInput struct {
	Name string `path:"name"`
	Body struct {
		DisplayName   *string `json:"display_name,omitempty"`
		ComponentType *string `json:"component_type,omitempty"`
	}
}

// registerComponentRoutes wires the component CRUD surface, on the same pattern
// as locations and systems.
func registerComponentRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, huma.Operation{
		OperationID: "list-components",
		Method:      http.MethodGet,
		Path:        "/components",
		Summary:     "List components in scope",
		Description: "Lists the components the caller may read, each filtered to its scope subtree. Gated by component:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("component", "read")},
	}, func(ctx context.Context, _ *struct{}) (*listComponentsOutput, error) {
		comps, err := gw.ListComponents(ctx, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, huma.Error500InternalServerError("list components")
		}
		ids := make([]string, len(comps))
		for i := range comps {
			ids[i] = comps[i].ID
		}
		acts, err := a.rowActions(ctx, gw, "component", ids)
		if err != nil {
			return nil, huma.Error500InternalServerError("list components")
		}
		out := &listComponentsOutput{}
		out.Body.Components = make([]componentBody, 0, len(comps))
		for i := range comps {
			b := toComponentBody(&comps[i])
			b.Actions = acts[comps[i].ID]
			out.Body.Components = append(out.Body.Components, b)
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-component",
		Method:      http.MethodGet,
		Path:        "/components/{name}",
		Summary:     "Get a component",
		Description: "Fetches a component by name within the caller's read scope. Out of scope is a non-disclosing 404. Gated by component:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("component", "read")},
	}, func(ctx context.Context, in *componentPathInput) (*componentOutput, error) {
		c, err := gw.GetComponent(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		return &componentOutput{Body: toComponentBody(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-component",
		Method:        http.MethodPost,
		Path:          "/components",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a component",
		Description:   "Creates a component, optionally under a parent (a root needs an all-scoped grant), bound to a system and a location. Gated by component:create.",
		Middlewares:   huma.Middlewares{a.authn, a.require("component", "create")},
	}, func(ctx context.Context, in *createComponentInput) (*componentOutput, error) {
		c, err := gw.CreateComponent(ctx, actorID(ctx), storage.ComponentSpec{
			Name:          in.Body.Name,
			DisplayName:   in.Body.DisplayName,
			ComponentType: in.Body.ComponentType,
			ParentName:    in.Body.Parent,
			SystemName:    in.Body.System,
			LocationName:  in.Body.Location,
		}, a.scopeFor(ctx, "component", "create"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		return &componentOutput{Body: toComponentBody(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-component",
		Method:      http.MethodPatch,
		Path:        "/components/{name}",
		Summary:     "Update a component",
		Description: "Patches a component's display_name or component_type. Gated by component:update; read and update scopes drive the 404 versus 403 split.",
		Middlewares: huma.Middlewares{a.authn, a.require("component", "update")},
	}, func(ctx context.Context, in *updateComponentInput) (*componentOutput, error) {
		c, err := gw.UpdateComponent(ctx, actorID(ctx), in.Name, storage.ComponentPatch{
			DisplayName:   in.Body.DisplayName,
			ComponentType: in.Body.ComponentType,
		}, a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "component", "update"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		return &componentOutput{Body: toComponentBody(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-component",
		Method:        http.MethodDelete,
		Path:          "/components/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a component",
		Description:   "Deletes a component, refused while it still has child components. Gated by component:delete; read and delete scopes drive the 404 versus 403 split.",
		Middlewares:   huma.Middlewares{a.authn, a.require("component", "delete")},
	}, func(ctx context.Context, in *componentPathInput) (*struct{}, error) {
		if err := gw.DeleteComponent(ctx, actorID(ctx), in.Name,
			a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "component", "delete")); err != nil {
			return nil, mapComponentErr(err)
		}
		return nil, nil
	})
}

func mapComponentErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrComponentNotFound):
		return huma.Error404NotFound("component not found")
	case errors.Is(err, storage.ErrComponentForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, storage.ErrComponentOccupied):
		return huma.Error409Conflict("component has child components")
	case errors.Is(err, storage.ErrComponentExists):
		return huma.Error409Conflict("component name already exists")
	case errors.Is(err, storage.ErrParentComponentNotFound):
		return huma.Error422UnprocessableEntity("parent component not found")
	case errors.Is(err, storage.ErrUnknownComponentType):
		return huma.Error422UnprocessableEntity("unknown component_type")
	case errors.Is(err, storage.ErrSystemNotFound):
		return huma.Error422UnprocessableEntity("system not found")
	case errors.Is(err, storage.ErrLocationNotFound):
		return huma.Error422UnprocessableEntity("location not found")
	default:
		return huma.Error500InternalServerError("component operation failed")
	}
}
