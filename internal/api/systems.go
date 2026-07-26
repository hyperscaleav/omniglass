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
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	DisplayName   string            `json:"display_name,omitempty"`
	Standard      string            `json:"standard,omitempty" doc:"The standard's handle, for display; omitted for a one-off system"`
	StandardID    string            `json:"standard_id,omitempty" doc:"The standard's uuid; the stable form of standard"`
	ParentID      *string           `json:"parent_id,omitempty" doc:"The parent system's id, the canonical handle"`
	Parent        *string           `json:"parent,omitempty" doc:"The parent system's name, for display; absent for a root system"`
	LocationID    *string           `json:"location_id,omitempty" doc:"The location's id, the canonical handle"`
	Location      *string           `json:"location,omitempty" doc:"The location's name, for display"`
	MemberCount   int               `json:"member_count" doc:"How many components are bound into this system"`
	Actions       []string          `json:"actions,omitempty" doc:"The scope-aware actions the caller may perform on this row (create a child, update, delete); a UI hint, the server still enforces."`
	EffectiveTags map[string]string `json:"effective_tags,omitempty" doc:"The resolved effective tags (key -> winning value) that cascade onto this system (platform, its location, its system tree); for the Tags column."`
}

func toSystemBody(s *storage.System) systemBody {
	return systemBody{
		ID: s.ID, Name: s.Name, DisplayName: s.DisplayName,
		Standard: derefStr(s.StandardName), StandardID: derefStr(s.StandardID), ParentID: s.ParentID, Parent: s.ParentName, LocationID: s.LocationID, Location: s.LocationName,
		MemberCount: s.MemberCount,
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

// standardBody is the wire shape of a standard: the blueprint a system conforms
// to, the system-side counterpart of a product. The catalog lists alphabetically
// by display_name.
type standardBody struct {
	ID               string `json:"id" doc:"The standard's uuid, the stable handle that survives a rename"`
	Name             string `json:"name" doc:"The kebab handle an operator reads and types; renameable"`
	DisplayName      string `json:"display_name"`
	ParentStandard   string `json:"parent_standard,omitempty" doc:"The parent standard's handle"`
	ParentStandardID string `json:"parent_standard_id,omitempty" doc:"The parent standard's uuid; the stable form of parent_standard"`
	Official         bool   `json:"official"`
}

func toStandardBody(st *storage.Standard) standardBody {
	return standardBody{
		ID: st.ID, DisplayName: st.DisplayName,
		Name: st.Name, ParentStandard: derefStr(st.ParentStandardName), ParentStandardID: derefStr(st.ParentStandardID), Official: st.Official,
	}
}

type listStandardsOutput struct {
	Body struct {
		Standards []standardBody `json:"standards"`
	}
}

type standardPathInput struct {
	ID string `path:"id" doc:"The standard id"`
}

type createStandardInput struct {
	Body struct {
		Name             string `json:"name" minLength:"1" doc:"The globally unique kebab handle; renameable"`
		DisplayName      string `json:"display_name" minLength:"1"`
		ParentStandardID string `json:"parent_standard_id,omitempty" doc:"A standard this one is a variant of, by handle or uuid"`
	}
}

type updateStandardInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName      *string `json:"display_name,omitempty"`
		ParentStandardID *string `json:"parent_standard_id,omitempty"`
	}
}

type standardOutput struct {
	Body standardBody
}

type systemPathInput struct {
	Name string `path:"name" doc:"The system's unique name"`
}

type createSystemInput struct {
	Body struct {
		Name        string  `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"Globally unique name (the address; lowercase letters, digits, hyphens)"`
		DisplayName string  `json:"display_name,omitempty"`
		StandardID  string  `json:"standard_id,omitempty" doc:"A standard id; omit for a one-off system that conforms to none"`
		Parent      *string `json:"parent,omitempty" doc:"Parent system name; omit for a root system"`
		Location    *string `json:"location,omitempty" doc:"Location name this system is placed at"`
	}
}

type updateSystemInput struct {
	Name string `path:"name"`
	Body struct {
		Name        *string `json:"name,omitempty" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"A new globally unique technical name (rename)"`
		DisplayName *string `json:"display_name,omitempty"`
		StandardID  *string `json:"standard_id,omitempty"`
		// Placement fields, house three-state (omitted unchanged, "" clears, name
		// sets), passed straight through. Parent is a cycle-guarded, scope-injected
		// reparent within the system tree.
		Location *string `json:"location,omitempty" doc:"Relocates the system to this location name. An empty string clears its placement."`
		Parent   *string `json:"parent,omitempty" doc:"Re-parents the system within the system tree to this system name; cycle-guarded and scope-injected. An empty string makes it a root system."`
	}
}

// checkNameInput is the request for the collection-level :checkName advisory.
// Shared across the systems/components/locations name checks; declared once here.
type checkNameInput struct {
	Body struct {
		Name string `json:"name" doc:"The proposed technical name to check"`
	}
}

// checkNameOutput is the advisory verdict: whether the proposed name is a valid
// slug and whether it is currently free. Availability is scope-blind to match
// the global unique constraint. Shared across the three entity name checks.
type checkNameOutput struct {
	Body struct {
		Valid     bool   `json:"valid" doc:"Whether the name matches the slug rule"`
		Available bool   `json:"available" doc:"Whether the name is free (scope-blind, matches the global unique constraint)"`
		Reason    string `json:"reason,omitempty" doc:"Human explanation when not valid or not available"`
	}
}

// registerSystemRoutes wires the system CRUD surface, mirroring locations: each
// route declares its capability, each handler resolves the caller's per-action
// scope and hands it to the gateway.
func registerSystemRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-systems",
		Method:      http.MethodGet,
		Path:        "/systems",
		Summary:     "List systems in scope",
		Description: "Lists the systems the caller may read, each filtered to its scope subtree. Gated by system:read.",
	}, "system", "read"), func(ctx context.Context, _ *struct{}) (*listSystemsOutput, error) {
		systems, err := gw.ListSystems(ctx, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, huma.Error500InternalServerError("list systems")
		}
		ids := make([]string, len(systems))
		for i := range systems {
			ids[i] = systems[i].ID
		}
		effTags, err := gw.EffectiveTags(ctx, "system", ids)
		if err != nil {
			return nil, huma.Error500InternalServerError("list systems")
		}
		acts, err := a.rowActions(ctx, gw, "system", ids)
		if err != nil {
			return nil, huma.Error500InternalServerError("list systems")
		}
		out := &listSystemsOutput{}
		out.Body.Systems = make([]systemBody, 0, len(systems))
		for i := range systems {
			b := toSystemBody(&systems[i])
			b.Actions = acts[systems[i].ID]
			b.EffectiveTags = effTags[systems[i].ID]
			out.Body.Systems = append(out.Body.Systems, b)
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-system",
		Method:      http.MethodGet,
		Path:        "/systems/{name}",
		Summary:     "Get a system",
		Description: "Fetches a system by name within the caller's read scope. Out of scope is a non-disclosing 404. Gated by system:read.",
	}, "system", "read"), func(ctx context.Context, in *systemPathInput) (*systemOutput, error) {
		s, err := gw.GetSystem(ctx, in.Name, a.scopeFor(ctx, "system", "read"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		return &systemOutput{Body: toSystemBody(s)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-system",
		Method:        http.MethodPost,
		Path:          "/systems",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a system",
		Description:   "Creates a system, optionally under a parent (a root needs an all-scoped grant) and at a location. Gated by system:create.",
	}, "system", "create"), func(ctx context.Context, in *createSystemInput) (*systemOutput, error) {
		s, err := gw.CreateSystem(ctx, actorID(ctx), storage.SystemSpec{
			Name:         in.Body.Name,
			DisplayName:  in.Body.DisplayName,
			StandardID:   ptrOrNil(in.Body.StandardID),
			ParentName:   in.Body.Parent,
			LocationName: in.Body.Location,
		}, a.scopeFor(ctx, "system", "create"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		return &systemOutput{Body: toSystemBody(s)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-system",
		Method:      http.MethodPatch,
		Path:        "/systems/{name}",
		Summary:     "Update a system",
		Description: "Patches a system's display_name, standard, location, or parent. The classification and placement fields follow the three-state convention: an omitted field is unchanged, an explicit empty string clears (a one-off, an unplaced system, a root system), a name sets. A reparent is cycle-guarded and scope-injected. Gated by system:update; read and update scopes drive the 404 versus 403 split.",
	}, "system", "update"), func(ctx context.Context, in *updateSystemInput) (*systemOutput, error) {
		s, err := gw.UpdateSystem(ctx, actorID(ctx), in.Name, storage.SystemPatch{
			Name:        in.Body.Name,
			DisplayName: in.Body.DisplayName,
			// Deliberately NOT emptyPtrToNil: that collapses an explicit "" into
			// "omitted", which would make clearing (declassify, unplace, lift-to-root)
			// impossible. The storage layer reads "" as clear.
			StandardID:   in.Body.StandardID,
			LocationName: in.Body.Location,
			ParentName:   in.Body.Parent,
		}, a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		return &systemOutput{Body: toSystemBody(s)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "check-system-name",
		Method:      http.MethodPost,
		Path:        "/systems:checkName",
		Summary:     "Check a system technical name",
		Description: "Reports whether a proposed technical name is a valid slug and currently free. Advisory (Save is still gated by the unique constraint). Availability is scope-blind to match the global unique constraint. Gated by system:update.",
	}, "system", "update"), func(ctx context.Context, in *checkNameInput) (*checkNameOutput, error) {
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
		taken, err := gw.SystemNameTaken(ctx, in.Body.Name)
		if err != nil {
			return nil, huma.Error500InternalServerError("check system name")
		}
		out.Body.Available = !taken
		if taken {
			out.Body.Reason = "That name is already taken."
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-system",
		Method:        http.MethodDelete,
		Path:          "/systems/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a system",
		Description:   "Deletes a system, refused (409) while it still has child systems or is still referenced elsewhere. Gated by system:delete; read and delete scopes drive the 404 versus 403 split.",
	}, "system", "delete"), func(ctx context.Context, in *systemPathInput) (*struct{}, error) {
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
	case errors.Is(err, storage.ErrReferenced):
		return huma.Error409Conflict("system is still referenced by another record")
	case errors.Is(err, storage.ErrSystemOccupied):
		return huma.Error409Conflict("system has child systems")
	case errors.Is(err, storage.ErrSystemExists):
		return huma.Error409Conflict("system name already exists")
	case errors.Is(err, storage.ErrInvalidName):
		return huma.Error422UnprocessableEntity("invalid name")
	case errors.Is(err, storage.ErrParentSystemNotFound):
		return huma.Error422UnprocessableEntity("parent system not found")
	case errors.Is(err, storage.ErrSystemCycle):
		return huma.Error422UnprocessableEntity("cannot move a system under itself or a descendant")
	case errors.Is(err, storage.ErrUnknownStandard):
		return huma.Error422UnprocessableEntity("unknown standard")
	case errors.Is(err, storage.ErrLocationNotFound):
		return huma.Error422UnprocessableEntity("location not found")
	default:
		return huma.Error500InternalServerError("system operation failed")
	}
}

// mapStandardErr translates the standard storage sentinels into HTTP status. An
// unknown parent is a 422; everything else falls through to the shared
// type-registry mapping (not-found 404, duplicate 409, official read-only 422,
// in-use 409).
func mapStandardErr(err error) error {
	if errors.Is(err, storage.ErrParentStandardNotFound) {
		return huma.Error422UnprocessableEntity("standard references an unknown parent standard")
	}
	return mapTypeErr(err, "standard")
}

// registerStandardRoutes wires the standard catalog CRUD surface, on the same
// pattern as products. A standard is not a bare type registry: it carries a
// declared property contract (and later a role set), so it is a Catalog entity
// gated by its own standard:read|create|update|delete rather than the inherited
// type:*. standard:read sits in the viewer read-floor (*:read), the mutations at
// the admin tier, exactly like product:*.
func registerStandardRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-standards",
		Method:      http.MethodGet,
		Path:        "/standards",
		Summary:     "List standards",
		Description: "Lists the standard catalog, ordered alphabetically by display name. A standard is the blueprint a system conforms to. Gated by standard:read.",
	}, "standard", "read"), func(ctx context.Context, _ *struct{}) (*listStandardsOutput, error) {
		items, err := gw.ListStandards(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list standards")
		}
		out := &listStandardsOutput{}
		out.Body.Standards = make([]standardBody, 0, len(items))
		for i := range items {
			out.Body.Standards = append(out.Body.Standards, toStandardBody(&items[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-standard",
		Method:        http.MethodPost,
		Path:          "/standards",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a standard",
		Description:   "Creates a custom (non-official) standard, optionally as a variant of another. Gated by standard:create.",
	}, "standard", "create"), func(ctx context.Context, in *createStandardInput) (*standardOutput, error) {
		st, err := gw.CreateStandard(ctx, actorID(ctx), storage.Standard{
			Name: in.Body.Name, DisplayName: in.Body.DisplayName,
			ParentStandardID: ptrOrNil(in.Body.ParentStandardID),
		})
		if err != nil {
			return nil, mapStandardErr(err)
		}
		return &standardOutput{Body: toStandardBody(st)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-standard",
		Method:      http.MethodGet,
		Path:        "/standards/{id}",
		Summary:     "Get a standard",
		Description: "Fetches a standard by id. Gated by standard:read.",
	}, "standard", "read"), func(ctx context.Context, in *standardPathInput) (*standardOutput, error) {
		st, err := gw.GetStandard(ctx, in.ID)
		if err != nil {
			return nil, mapStandardErr(err)
		}
		return &standardOutput{Body: toStandardBody(st)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-standard",
		Method:      http.MethodPatch,
		Path:        "/standards/{id}",
		Summary:     "Update a standard",
		Description: "Patches a custom standard's display_name or parent. Official standards are read-only (422). Gated by standard:update.",
	}, "standard", "update"), func(ctx context.Context, in *updateStandardInput) (*standardOutput, error) {
		st, err := gw.UpdateStandard(ctx, actorID(ctx), in.ID, storage.StandardPatch{
			DisplayName:      in.Body.DisplayName,
			ParentStandardID: emptyPtrToNil(in.Body.ParentStandardID),
		})
		if err != nil {
			return nil, mapStandardErr(err)
		}
		return &standardOutput{Body: toStandardBody(st)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-standard",
		Method:        http.MethodDelete,
		Path:          "/standards/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a standard",
		Description:   "Deletes a custom standard, refused if official (422) or still referenced by a system (409). Gated by standard:delete.",
	}, "standard", "delete"), func(ctx context.Context, in *standardPathInput) (*struct{}, error) {
		if err := gw.DeleteStandard(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapStandardErr(err)
		}
		return nil, nil
	})
}
