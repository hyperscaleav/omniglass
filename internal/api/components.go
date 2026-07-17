package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type componentBody struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	DisplayName   string            `json:"display_name,omitempty"`
	ComponentType string            `json:"component_type"`
	ParentID      *string           `json:"parent_id,omitempty"`
	SystemID      *string           `json:"system_id,omitempty"`
	LocationID    *string           `json:"location_id,omitempty"`
	Actions       []string          `json:"actions,omitempty" doc:"The scope-aware actions the caller may perform on this row (create a child, update, delete); a UI hint, the server still enforces."`
	EffectiveTags map[string]string `json:"effective_tags,omitempty" doc:"The resolved effective tags (key -> winning value) that cascade onto this component; for the Tags column. Provenance is in the effective-tags detail view."`
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
		Name          string  `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"Globally unique name (the address; lowercase letters, digits, hyphens)"`
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
		Name          *string `json:"name,omitempty" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"A new globally unique technical name (rename)"`
		DisplayName   *string `json:"display_name,omitempty"`
		ComponentType *string `json:"component_type,omitempty"`
	}
}

// componentTypeBody is the wire shape of a component_type registry row. The
// registry lists alphabetically by display_name.
type componentTypeBody struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Official    bool   `json:"official"`
}

type listComponentTypesOutput struct {
	Body struct {
		ComponentTypes []componentTypeBody `json:"component_types"`
	}
}

type componentTypePathInput struct {
	ID string `path:"id" doc:"The component_type id"`
}

type createComponentTypeInput struct {
	Body struct {
		ID          string `json:"id" minLength:"1" doc:"Globally unique type id"`
		DisplayName string `json:"display_name" minLength:"1"`
	}
}

type updateComponentTypeInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName *string `json:"display_name,omitempty"`
	}
}

type componentTypeOutput struct {
	Body componentTypeBody
}

// registerComponentRoutes wires the component CRUD surface, on the same pattern
// as locations and systems.
func registerComponentRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-components",
		Method:      http.MethodGet,
		Path:        "/components",
		Summary:     "List components in scope",
		Description: "Lists the components the caller may read, each filtered to its scope subtree. Gated by component:read.",
	}, "component", "read"), func(ctx context.Context, _ *struct{}) (*listComponentsOutput, error) {
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
		effTags, err := gw.EffectiveTags(ctx, "component", ids)
		if err != nil {
			return nil, huma.Error500InternalServerError("list components")
		}
		out := &listComponentsOutput{}
		out.Body.Components = make([]componentBody, 0, len(comps))
		for i := range comps {
			b := toComponentBody(&comps[i])
			b.Actions = acts[comps[i].ID]
			b.EffectiveTags = effTags[comps[i].ID]
			out.Body.Components = append(out.Body.Components, b)
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-component-types",
		Method:      http.MethodGet,
		Path:        "/types/component",
		Summary:     "List component types",
		Description: "Lists the component_type registry, ordered alphabetically by display name. Populates the type picker on the component form. Gated by type:read.",
	}, "type", "read"), func(ctx context.Context, _ *struct{}) (*listComponentTypesOutput, error) {
		types, err := gw.ListComponentTypes(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list component types")
		}
		out := &listComponentTypesOutput{}
		out.Body.ComponentTypes = make([]componentTypeBody, 0, len(types))
		for i := range types {
			out.Body.ComponentTypes = append(out.Body.ComponentTypes, componentTypeBody{
				ID: types[i].ID, DisplayName: types[i].DisplayName, Official: types[i].Official,
			})
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-component-type",
		Method:        http.MethodPost,
		Path:          "/types/component",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a component type",
		Description:   "Creates a custom (non-official) component_type. Gated by type:create.",
	}, "type", "create"), func(ctx context.Context, in *createComponentTypeInput) (*componentTypeOutput, error) {
		ct, err := gw.CreateComponentType(ctx, actorID(ctx), storage.ComponentType{
			ID: in.Body.ID, DisplayName: in.Body.DisplayName,
		})
		if err != nil {
			return nil, mapTypeErr(err, "component_type")
		}
		return &componentTypeOutput{Body: componentTypeBody{ID: ct.ID, DisplayName: ct.DisplayName, Official: ct.Official}}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-component-type",
		Method:      http.MethodPatch,
		Path:        "/types/component/{id}",
		Summary:     "Update a component type",
		Description: "Patches a custom component_type's display_name. Official types are read-only (422). Gated by type:update.",
	}, "type", "update"), func(ctx context.Context, in *updateComponentTypeInput) (*componentTypeOutput, error) {
		ct, err := gw.UpdateComponentType(ctx, actorID(ctx), in.ID, storage.ComponentTypePatch{
			DisplayName: in.Body.DisplayName,
		})
		if err != nil {
			return nil, mapTypeErr(err, "component_type")
		}
		return &componentTypeOutput{Body: componentTypeBody{ID: ct.ID, DisplayName: ct.DisplayName, Official: ct.Official}}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-component-type",
		Method:        http.MethodDelete,
		Path:          "/types/component/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a component type",
		Description:   "Deletes a custom component_type, refused if official (422) or referenced by a component (409). Gated by type:delete.",
	}, "type", "delete"), func(ctx context.Context, in *componentTypePathInput) (*struct{}, error) {
		if err := gw.DeleteComponentType(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapTypeErr(err, "component_type")
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-component",
		Method:      http.MethodGet,
		Path:        "/components/{name}",
		Summary:     "Get a component",
		Description: "Fetches a component by name within the caller's read scope. Out of scope is a non-disclosing 404. Gated by component:read.",
	}, "component", "read"), func(ctx context.Context, in *componentPathInput) (*componentOutput, error) {
		c, err := gw.GetComponent(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		return &componentOutput{Body: toComponentBody(c)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-component",
		Method:        http.MethodPost,
		Path:          "/components",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a component",
		Description:   "Creates a component, optionally under a parent (a root needs an all-scoped grant), bound to a system and a location. Gated by component:create.",
	}, "component", "create"), func(ctx context.Context, in *createComponentInput) (*componentOutput, error) {
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

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-component",
		Method:      http.MethodPatch,
		Path:        "/components/{name}",
		Summary:     "Update a component",
		Description: "Patches a component's display_name or component_type. Gated by component:update; read and update scopes drive the 404 versus 403 split.",
	}, "component", "update"), func(ctx context.Context, in *updateComponentInput) (*componentOutput, error) {
		c, err := gw.UpdateComponent(ctx, actorID(ctx), in.Name, storage.ComponentPatch{
			Name:          in.Body.Name,
			DisplayName:   in.Body.DisplayName,
			ComponentType: in.Body.ComponentType,
		}, a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "component", "update"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		return &componentOutput{Body: toComponentBody(c)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "check-component-name",
		Method:      http.MethodPost,
		Path:        "/components:checkName",
		Summary:     "Check a component technical name",
		Description: "Reports whether a proposed technical name is a valid slug and currently free. Advisory (Save is still gated by the unique constraint). Availability is scope-blind to match the global unique constraint. Gated by component:update.",
	}, "component", "update"), func(ctx context.Context, in *checkNameInput) (*checkNameOutput, error) {
		out := &checkNameOutput{}
		if err := storage.ValidateEntityName(in.Body.Name); err != nil {
			out.Body.Valid = false
			out.Body.Reason = "Use lowercase letters, digits, and hyphens."
			return out, nil
		}
		out.Body.Valid = true
		taken, err := gw.ComponentNameTaken(ctx, in.Body.Name)
		if err != nil {
			return nil, huma.Error500InternalServerError("check component name")
		}
		out.Body.Available = !taken
		if taken {
			out.Body.Reason = "That name is already taken."
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-component",
		Method:        http.MethodDelete,
		Path:          "/components/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a component",
		Description:   "Deletes a component, refused while it still has child components. Gated by component:delete; read and delete scopes drive the 404 versus 403 split.",
	}, "component", "delete"), func(ctx context.Context, in *componentPathInput) (*struct{}, error) {
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
	case errors.Is(err, storage.ErrInvalidName):
		return huma.Error422UnprocessableEntity("invalid name")
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
