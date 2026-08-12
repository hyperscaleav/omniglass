package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// systemBody is the wire shape of a system.
type systemBody struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	// The NAME's pen (#686); read-only for the same reason the label's is: an
	// operator claims it with :rename and returns it with :resetName, so there
	// is exactly one way to say who owns the name.
	NameGenerated bool `json:"name_generated" doc:"Whether the platform picked this name (from the system_type's stem) rather than an operator typing it."`
	// The LABEL's pen (#682); see componentBody for why it is read-only.
	DisplayNameGenerated bool              `json:"display_name_generated" doc:"Whether the platform rendered this display name from a label rule rather than an operator typing it. Read-only: write display_name to claim it, write an empty display_name to hand it back."`
	Standard             string            `json:"standard,omitempty" doc:"The standard's handle, for display; omitted for a one-off system"`
	StandardID           string            `json:"standard_id,omitempty" doc:"The standard's uuid; the stable form of standard"`
	SystemType           string            `json:"system_type,omitempty" doc:"The system_type's name, for display: what kind of space this is (board, class, video-wall). Omitted for an unclassified system. Distinct from standard, which is the blueprint it is built to."`
	SystemTypeID         string            `json:"system_type_id,omitempty" doc:"The system_type's uuid; the stable form of system_type"`
	ParentID             *string           `json:"parent_id,omitempty" doc:"The parent system's id, the canonical handle"`
	Parent               *string           `json:"parent,omitempty" doc:"The parent system's name, for display; absent for a root system"`
	LocationID           *string           `json:"location_id,omitempty" doc:"The location's id, the canonical handle"`
	Location             *string           `json:"location,omitempty" doc:"The location's name, for display"`
	MemberCount          int               `json:"member_count" doc:"How many components are bound into this system"`
	Path                 string            `json:"path,omitempty" doc:"The dotted address (e.g. boi.17c.$sys.av). Set on a GET or LIST response; empty on a create/update/move/rename response (refetch the row to see it)."`
	PathSegments         []string          `json:"path_segments,omitempty" doc:"path split on '.', accessors included, so the round trip through the resolver stays lossless."`
	Renders              *renderBody       `json:"renders,omitempty" doc:"Two display-only compact forms of path, dash and bare. Neither is accepted back by the resolver: stripping/compacting is lossy."`
	Actions              []string          `json:"actions,omitempty" doc:"The scope-aware actions the caller may perform on this row (create a child, update, delete); a UI hint, the server still enforces."`
	EffectiveTags        map[string]string `json:"effective_tags,omitempty" doc:"The resolved effective tags (key -> winning value) that cascade onto this system (platform, its location, its system tree); for the Tags column."`
}

func toSystemBody(s *storage.System) systemBody {
	return systemBody{
		ID: s.ID, Name: s.Name, DisplayName: s.DisplayName,
		NameGenerated: s.NameGenerated, DisplayNameGenerated: s.DisplayNameGenerated,
		Standard: derefStr(s.StandardName), StandardID: derefStr(s.StandardID),
		SystemType: derefStr(s.SystemTypeName), SystemTypeID: derefStr(s.SystemTypeID),
		ParentID: s.ParentID, Parent: s.ParentName, LocationID: s.LocationID, Location: s.LocationName,
		MemberCount: s.MemberCount,
		Path:        s.Path, PathSegments: s.PathSegments, Renders: toRenderBody(s.Path, s.Renders),
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
	Name             string `json:"name" doc:"The name an operator reads and types; renameable"`
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
		Name             string `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The globally unique name; renameable"`
		DisplayName      string `json:"display_name" minLength:"1" doc:"What an operator reads in pickers and lists"`
		ParentStandardID string `json:"parent_standard_id,omitempty" doc:"A standard this one is a variant of, by handle or uuid"`
	}
}

type updateStandardInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName      *string `json:"display_name,omitempty" doc:"A new operator-facing label"`
		ParentStandardID *string `json:"parent_standard_id,omitempty" doc:"A new variant parent, by handle or uuid"`
	}
}

type standardOutput struct {
	Body standardBody
}

type systemPathInput struct {
	Name string `path:"name" doc:"The system's name, or a dotted address (e.g. boi.17c.$sys.av)"`
}

type createSystemInput struct {
	Body struct {
		// Name is optional (#686), exactly as a component's is: omit it and the
		// platform mints the system_type chain's stem plus the lowest free
		// ordinal in this placement, suppressing the ordinal for the first of
		// that stem in the bucket, and marks name_generated. Supplied, it is
		// validated exactly as before. Omitting it without a system_type is a
		// 422: the stem lives on that registry row.
		Name        string `json:"name,omitempty" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"Name, unique within its placement (the address; lowercase letters, digits, hyphens). Omit to have the platform generate one from the system_type's stem."`
		DisplayName string `json:"display_name,omitempty" doc:"What an operator reads; the name is the address"`
		StandardID  string `json:"standard_id,omitempty" doc:"The standard it conforms to, by handle or uuid; omit for a one-off system"`
		// Nullable for now: a floor on system_type_id waits until the shipped
		// tree has proven out.
		SystemTypeID string  `json:"system_type_id,omitempty" doc:"The system_type it is classified as (what kind of space it is), by name or uuid; omit to leave it unclassified"`
		Parent       *string `json:"parent,omitempty" doc:"Parent system name; omit for a root system"`
		Location     *string `json:"location,omitempty" doc:"Location name this system is placed at"`
	}
}

// updateSystemInput is the PATCH body. It deliberately carries no name: a rename
// is the :rename custom method, gated by system:rename. It carries no placement
// either (#627 Task 13): a move is the :move custom method, gated by
// system:move, because a placement change is an authorization act, not a label
// edit.
type updateSystemInput struct {
	Name string `path:"name" doc:"The system's name, or a dotted address (e.g. boi.17c.$sys.av)"`
	Body struct {
		DisplayName  *string `json:"display_name,omitempty" doc:"A new operator-facing label"`
		StandardID   *string `json:"standard_id,omitempty" doc:"A new standard, by handle or uuid; \"\" clears it (a one-off system)"`
		SystemTypeID *string `json:"system_type_id,omitempty" doc:"A new system_type, by name or uuid; \"\" clears it (an unclassified system)"`
	}
}

// moveSystemInput is the :move body: at least one of Location or Parent is
// required (422 otherwise). Both follow the house three-state convention:
// omitted unchanged, "" clears, a name sets. Parent is a cycle-guarded,
// scope-injected reparent within the system tree.
type moveSystemInput struct {
	Name string `path:"name" doc:"The system's name, or a dotted address (e.g. boi.17c.$sys.av)"`
	Body struct {
		Location *string `json:"location,omitempty" doc:"Relocates the system to this location name. An empty string clears its placement."`
		Parent   *string `json:"parent,omitempty" doc:"Re-parents the system within the system tree to this system name; cycle-guarded and scope-injected. An empty string makes it a root system (requires an all-scoped move grant)."`
	}
}

// renameSystemInput is the :rename body. The name rule lives here, in the
// contract, not only in the prose below it.
type renameSystemInput struct {
	Name string `path:"name" doc:"The system's current name, a dotted address, or its uuid"`
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The new name, unique within its placement (lowercase letters, digits, hyphens)"`
	}
}

// checkNameInput is the request for the collection-level :checkName advisory.
// Shared across the systems/components/locations name checks; declared once
// here. Parent and Location mirror the create body's own placement fields
// (#627: name uniqueness is scoped to placement, not the whole estate), so the
// availability check runs against the same bucket a create would actually
// land in. A parent wins over a location, matching CreateComponent's and
// CreateSystem's own resolution order; the location route ignores Location
// (it carries no located-at column of its own) and checks only Parent.
type checkNameInput struct {
	Body struct {
		Name     string  `json:"name" doc:"The proposed name to check"`
		Parent   *string `json:"parent,omitempty" doc:"The parent (by name or uuid) the entity would be created under, if any; omit for a root/unplaced check"`
		Location *string `json:"location,omitempty" doc:"The location (by name or uuid) the entity would be placed at, if any and if unparented; ignored by the locations check"`
	}
}

// checkNameOutput is the advisory verdict: whether the proposed name is a valid
// slug and whether it is currently free within the checked placement. Shared
// across the three entity name checks.
type checkNameOutput struct {
	Body struct {
		Valid     bool   `json:"valid" doc:"Whether the name matches the slug rule"`
		Available bool   `json:"available" doc:"Whether the name is free within the checked placement (parent/location); a name taken elsewhere in the estate is still available here"`
		Reason    string `json:"reason,omitempty" doc:"Human explanation when not valid or not available"`
	}
}

// registerSystemRoutes wires the system CRUD surface, mirroring locations: each
// route declares its capability, each handler resolves the caller's per-action
// scope and hands it to the gateway.
func registerSystemRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	registerLabelRecomputeRoutes(api, a, gw, "system", "/systems")
	registerSystemLabelDraft(api, a, gw)
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
		Description:   "Creates a system, optionally under a parent (a root needs an all-scoped grant), at a location, conforming to a standard, and classified as a system_type. Gated by system:create; the location reference resolves within the caller's location:read scope, because the label this stores is rendered from it, and a location outside that scope is refused (422) exactly as :renderLabel refuses to preview it.",
	}, "system", "create"), func(ctx context.Context, in *createSystemInput) (*systemOutput, error) {
		s, err := gw.CreateSystem(ctx, actorID(ctx), storage.SystemSpec{
			Name:         in.Body.Name,
			DisplayName:  in.Body.DisplayName,
			StandardID:   ptrOrNil(in.Body.StandardID),
			SystemTypeID: ptrOrNil(in.Body.SystemTypeID),
			ParentName:   in.Body.Parent,
			LocationName: in.Body.Location,
		}, a.scopeFor(ctx, "system", "create"), a.scopeFor(ctx, "location", "read"))
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
		Description: "Patches a system's display_name, standard, or system_type. The name is not patchable: renaming is the :rename custom method. Placement is not patchable either: relocating or re-parenting is the :move custom method, gated separately, because a placement change is an authorization act. The standard and system_type fields both follow the three-state convention: an omitted field is unchanged, an explicit empty string clears (a one-off system, an unclassified system), a name sets. Gated by system:update; read and update scopes drive the 404 versus 403 split.",
	}, "system", "update"), func(ctx context.Context, in *updateSystemInput) (*systemOutput, error) {
		s, err := gw.UpdateSystem(ctx, actorID(ctx), in.Name, storage.SystemPatch{
			DisplayName: in.Body.DisplayName,
			// Deliberately NOT emptyPtrToNil: that collapses an explicit "" into
			// "omitted", which would make clearing (declassify) impossible. The
			// storage layer reads "" as clear. Same for SystemTypeID.
			StandardID:   in.Body.StandardID,
			SystemTypeID: in.Body.SystemTypeID,
		}, a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		return &systemOutput{Body: toSystemBody(s)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "move-system",
		Method:      http.MethodPost,
		Path:        "/systems/{name}:move",
		Summary:     "Move a system",
		Description: "Relocates and/or re-parents a system: at least one of location or parent is required (422 otherwise). Both follow the three-state convention (an omitted field is unchanged, an explicit empty string clears, a name sets). A reparent is cycle-guarded and scope-injected; clearing parent to root requires an all-scoped move grant, the same authorization a root create already requires. A separate act from update, and a separately grantable one (system:move), because a placement change is an authorization act, not a label edit: it moves a row out from under one grant's subtree and under another's. Recorded under its own audit verb, move, distinct from update. A relocate still recomputes health at both ends (the location it left and the one it arrived at); a reparent does not, since the health rollup runs system -> location, never through the system tree. A taken name at the destination is a 409. A move can RENAME the system: a platform-generated name is scoped to its placement bucket, so a move that changes the bucket re-mints the name and the ordinal in the destination. A move that changes no bucket, including a re-stated placement and a relocate of a parented system (a parent wins over a location), leaves the name alone, and an operator-typed name is never touched. Gated by system:move; read and move scopes drive the 404 versus 403 split, and the destination location resolves within the caller's location:read scope, because the move restamps the label from it: a destination outside that scope is refused (422).",
	}, "system", "move"), func(ctx context.Context, in *moveSystemInput) (*systemOutput, error) {
		if in.Body.Location == nil && in.Body.Parent == nil {
			return nil, huma.Error422UnprocessableEntity("move requires at least one of location or parent")
		}
		s, err := gw.MoveSystem(ctx, actorID(ctx), in.Name, storage.SystemMove{
			LocationName: in.Body.Location,
			ParentName:   in.Body.Parent,
		}, a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "move"), a.scopeFor(ctx, "location", "read"))
		if err != nil {
			switch {
			case errors.Is(err, storage.ErrSystemExistsUnderParent):
				return nil, huma.Error409Conflict(fmt.Sprintf("a system named %q already exists under %q", systemMoverName(ctx, gw, a, in.Name), derefStr(in.Body.Parent)))
			case errors.Is(err, storage.ErrSystemExistsInLocation):
				return nil, huma.Error409Conflict(fmt.Sprintf("a system named %q already exists at %q", systemMoverName(ctx, gw, a, in.Name), derefStr(in.Body.Location)))
			case errors.Is(err, storage.ErrSystemExistsUnplaced):
				return nil, huma.Error409Conflict(fmt.Sprintf("an unplaced system named %q already exists", systemMoverName(ctx, gw, a, in.Name)))
			}
			return nil, mapSystemErr(err)
		}
		return &systemOutput{Body: toSystemBody(s)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "rename-system",
		Method:      http.MethodPost,
		Path:        "/systems/{name}:rename",
		Summary:     "Rename a system",
		Description: "Moves the system's name, the address an operator types and every external reference stores. A separate act from an update, and a separately grantable one, because it breaks bookmarks, runbooks, and integration config outside this system; inside it nothing breaks, since every reference holds the uuid. A taken name is a 409, an illegal or uuid-shaped one a 422. Gated by system:rename; read and rename scopes drive the 404 versus 403 split.",
	}, "system", "rename"), func(ctx context.Context, in *renameSystemInput) (*systemOutput, error) {
		s, err := gw.RenameSystem(ctx, actorID(ctx), in.Name, in.Body.Name,
			a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "rename"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		return &systemOutput{Body: toSystemBody(s)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "reset-system-name",
		Method:      http.MethodPost,
		Path:        "/systems/{name}:resetName",
		Summary:     "Regenerate a system's name",
		Description: "Hands the pen back to the platform: regenerates the name from the system's current system_type and placement (the same rule a nameless create applies, the type's stem plus the lowest free ordinal, bare for the first of that stem in the placement) and marks it name_generated, whether or not it already was. An unclassified system is a 422: the stem lives on the system_type. Gated by system:rename, the same token :rename uses: it changes the name, exactly that permission's blast radius.",
	}, "system", "rename"), func(ctx context.Context, in *systemPathInput) (*systemOutput, error) {
		s, err := gw.ResetSystemName(ctx, actorID(ctx), in.Name,
			a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "rename"))
		if err != nil {
			return nil, mapSystemErr(err)
		}
		return &systemOutput{Body: toSystemBody(s)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "check-system-name",
		Method:      http.MethodPost,
		Path:        "/systems:checkName",
		Summary:     "Check a system name",
		Description: "Reports whether a proposed name is a valid slug and currently free within the given placement (parent wins over location; neither means the root/unplaced bucket). Advisory (Save is still gated by the unique constraint). Gated by system:update.",
	}, "system", "update"), func(ctx context.Context, in *checkNameInput) (*checkNameOutput, error) {
		out := &checkNameOutput{}
		if err := storage.ValidateName("system", in.Body.Name); err != nil {
			out.Body.Valid = false
			// A uuid passes the slug rule, so the generic reason would describe
			// exactly what the operator typed and explain nothing.
			if errors.Is(err, storage.ErrEntityNameIsUUID) {
				out.Body.Reason = "A name cannot be a uuid: that form is reserved for an entity's id."
			} else {
				out.Body.Reason = "Use lowercase letters, digits, and hyphens."
			}
			return out, nil
		}
		out.Body.Valid = true
		taken, err := gw.SystemNameTaken(ctx, in.Body.Name, in.Body.Parent, in.Body.Location)
		if err != nil {
			return nil, mapSystemErr(err)
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

// systemMoverName resolves ref back to the system's own name for a 409
// collision message; see componentMoverName's doc comment for the reasoning.
func systemMoverName(ctx context.Context, gw storage.Gateway, a *authenticator, ref string) string {
	if s, err := gw.GetSystem(ctx, ref, a.scopeFor(ctx, "system", "read")); err == nil {
		return s.Name
	}
	return ref
}

// mapSystemErr translates the gateway's system sentinels into HTTP status,
// mirroring locations.
func mapSystemErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
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
	case errors.Is(err, storage.ErrEntityNameIsUUID):
		return huma.Error422UnprocessableEntity("system name may not be a uuid: that form is reserved for an entity's id")
	case errors.Is(err, storage.ErrInvalidEntityName):
		return huma.Error422UnprocessableEntity("invalid name")
	case errors.Is(err, storage.ErrParentSystemNotFound):
		return huma.Error422UnprocessableEntity("parent system not found")
	case errors.Is(err, storage.ErrSystemCycle):
		return huma.Error422UnprocessableEntity("cannot move a system under itself or a descendant")
	case errors.Is(err, storage.ErrUnknownStandard):
		return huma.Error422UnprocessableEntity("unknown standard")
	case errors.Is(err, storage.ErrUnknownSystemType):
		return huma.Error422UnprocessableEntity("unknown system_type")
	case errors.Is(err, storage.ErrSystemTypeRequiredForName):
		return huma.Error422UnprocessableEntity("a system with no system_type has no stem to generate a name from: supply a name, classify it, or :rename it before un-classifying it")
	case errors.Is(err, storage.ErrSystemTypeNoStem):
		return huma.Error422UnprocessableEntity("this system_type has no stem to generate a name from; supply a name explicitly, or fix the system_type registry")
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
