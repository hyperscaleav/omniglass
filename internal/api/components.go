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
	ParentID      *string           `json:"parent_id,omitempty" doc:"The parent component's id, the canonical handle"`
	Parent        *string           `json:"parent,omitempty" doc:"The parent component's name, for display; absent for a root component"`
	SystemID      *string           `json:"system_id,omitempty" doc:"The primary system's id, the canonical handle"`
	System        *string           `json:"system,omitempty" doc:"Name of the component's primary system, its default when no system is named. A component may belong to several; read /components/{name}/memberships for all of them."`
	SystemCount   int               `json:"system_count" doc:"How many systems this component belongs to; more than one means it is shared."`
	LocationID    *string           `json:"location_id,omitempty" doc:"The location's id, the canonical handle"`
	Location      *string           `json:"location,omitempty" doc:"The location's name, for display"`
	ProductID     *string           `json:"product_id,omitempty" doc:"The product (catalog SKU) this component is an instance of, if any; the stable handle that survives a rename."`
	Product       *string           `json:"product,omitempty" doc:"The product's name, for display; the form a body round-trips."`
	Actions       []string          `json:"actions,omitempty" doc:"The scope-aware actions the caller may perform on this row (create a child, update, delete); a UI hint, the server still enforces."`
	EffectiveTags map[string]string `json:"effective_tags,omitempty" doc:"The resolved effective tags (key -> winning value) that cascade onto this component; for the Tags column. Provenance is in the effective-tags detail view."`
}

func toComponentBody(c *storage.Component) componentBody {
	return componentBody{
		ID: c.ID, Name: c.Name, DisplayName: c.DisplayName,
		ParentID: c.ParentID, Parent: c.ParentName, SystemID: c.PrimarySystemID, System: c.PrimarySystem, SystemCount: c.SystemCount, LocationID: c.LocationID, Location: c.LocationName, ProductID: c.ProductID, Product: c.ProductHandle,
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
		Name        string  `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"Globally unique name (the address; lowercase letters, digits, hyphens)"`
		DisplayName string  `json:"display_name,omitempty"`
		Parent      *string `json:"parent,omitempty" doc:"Parent component name; omit for a root component"`
		System      *string `json:"system,omitempty" doc:"Primary system name this component belongs to"`
		Location    *string `json:"location,omitempty" doc:"Location name this component is placed at"`
		Product     *string `json:"product,omitempty" doc:"Product id (catalog SKU) this component is an instance of"`
	}
}

type updateComponentInput struct {
	Name string `path:"name"`
	Body struct {
		Name        *string `json:"name,omitempty" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"A new globally unique technical name (rename)"`
		DisplayName *string `json:"display_name,omitempty"`
		// The placement and classification fields take the house three-state
		// convention: an omitted field is unchanged, an explicit empty string
		// clears, and a name sets. So they are pointers passed straight through
		// (never emptyPtrToNil, which would collapse an intended clear).
		Parent   *string `json:"parent,omitempty" doc:"Re-parents the component within the component tree to this component name; cycle-guarded and scope-injected. An empty string makes it a root component."`
		Location *string `json:"location,omitempty" doc:"Relocates the component to this location name. An empty string clears its placement."`
		Product  *string `json:"product,omitempty" doc:"Re-classifies the component to this product (catalog SKU). An empty string clears it. Explicitly-set property values persist; the new product's contract defaults follow."`
	}
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
			Name:         in.Body.Name,
			DisplayName:  in.Body.DisplayName,
			ParentName:   in.Body.Parent,
			SystemName:   in.Body.System,
			LocationName: in.Body.Location,
			ProductName:  in.Body.Product,
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
		Description: "Patches a component's technical name, display_name, product, location, or parent. Placement and classification fields follow the three-state convention: an omitted field is unchanged, an explicit empty string clears, a name sets. A reparent is cycle-guarded and scope-injected. Gated by component:update; read and update scopes drive the 404 versus 403 split.",
	}, "component", "update"), func(ctx context.Context, in *updateComponentInput) (*componentOutput, error) {
		c, err := gw.UpdateComponent(ctx, actorID(ctx), in.Name, storage.ComponentPatch{
			Name:        in.Body.Name,
			DisplayName: in.Body.DisplayName,
			// Passed straight through, never emptyPtrToNil: the storage layer reads
			// "" as clear, and collapsing it here would make a relocate-to-none,
			// declassify, or lift-to-root impossible.
			ParentName:   in.Body.Parent,
			LocationName: in.Body.Location,
			ProductName:  in.Body.Product,
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
			// A uuid passes the slug rule, so the generic reason would describe
			// exactly what the operator typed and explain nothing.
			if errors.Is(err, storage.ErrNameIsUUID) {
				out.Body.Reason = "A name cannot be a uuid: that form is reserved for an entity's id."
			} else {
				out.Body.Reason = "Use lowercase letters, digits, and hyphens."
			}
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
		Description:   "Deletes a component, refused (409) while it still has child components or is still referenced elsewhere, such as by a system role it staffs. Gated by component:delete; read and delete scopes drive the 404 versus 403 split.",
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
	case errors.Is(err, storage.ErrReferenced):
		return huma.Error409Conflict("component is still referenced by another record, for example a system role it staffs")
	case errors.Is(err, storage.ErrComponentExists):
		return huma.Error409Conflict("component name already exists")
	case errors.Is(err, storage.ErrInvalidName):
		return huma.Error422UnprocessableEntity("invalid name")
	case errors.Is(err, storage.ErrParentComponentNotFound):
		return huma.Error422UnprocessableEntity("parent component not found")
	case errors.Is(err, storage.ErrComponentCycle):
		return huma.Error422UnprocessableEntity("cannot move a component under itself or a descendant")
	case errors.Is(err, storage.ErrSystemNotFound):
		return huma.Error422UnprocessableEntity("system not found")
	case errors.Is(err, storage.ErrLocationNotFound):
		return huma.Error422UnprocessableEntity("location not found")
	case errors.Is(err, storage.ErrProductNotFound):
		return huma.Error422UnprocessableEntity("product not found")
	default:
		return huma.Error500InternalServerError("component operation failed")
	}
}
