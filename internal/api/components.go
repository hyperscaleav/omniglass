package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

type componentBody struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	DisplayName   string  `json:"display_name,omitempty"`
	ParentID      *string `json:"parent_id,omitempty" doc:"The parent component's id, the canonical handle"`
	Parent        *string `json:"parent,omitempty" doc:"The parent component's name, for display; absent for a root component"`
	SystemID      *string `json:"system_id,omitempty" doc:"The primary system's id, the canonical handle"`
	System        *string `json:"system,omitempty" doc:"Name of the component's primary system, its default when no system is named. A component may belong to several; read /components/{name}/memberships for all of them."`
	SystemCount   int     `json:"system_count" doc:"How many systems this component belongs to; more than one means it is shared."`
	LocationID    *string `json:"location_id,omitempty" doc:"The location's id, the canonical handle"`
	Location      *string `json:"location,omitempty" doc:"The location's name, for display"`
	ProductID     *string `json:"product_id,omitempty" doc:"The product (catalog SKU) this component is an instance of, if any; the stable handle that survives a rename."`
	Product       *string `json:"product,omitempty" doc:"The product's name, for display; the form a body round-trips."`
	NameGenerated bool    `json:"name_generated" doc:"Whether the platform picked this name (a server-side generator) rather than an operator typing it."`
	// The LABEL's pen (#682), read-only for the same reason name_generated is:
	// an operator claims it by writing display_name and returns it by clearing
	// display_name, so there is exactly one way to say who owns the label.
	DisplayNameGenerated bool              `json:"display_name_generated" doc:"Whether the platform rendered this display name from a label rule rather than an operator typing it. Read-only: write display_name to claim it, write an empty display_name to hand it back."`
	Path                 string            `json:"path,omitempty" doc:"The dotted address (e.g. boi.17c.415a.$comp.display-1): derived from the component's own placement, never from a system it belongs to. Set on a GET or LIST response; empty on a create/update/move/rename/resetName response (refetch the row to see it)."`
	PathSegments         []string          `json:"path_segments,omitempty" doc:"path split on '.', accessors included, so the round trip through the resolver stays lossless."`
	Renders              *renderBody       `json:"renders,omitempty" doc:"Two display-only compact forms of path, dash and bare. Neither is accepted back by the resolver: stripping/compacting is lossy."`
	Actions              []string          `json:"actions,omitempty" doc:"The scope-aware actions the caller may perform on this row (create a child, update, delete); a UI hint, the server still enforces."`
	EffectiveTags        map[string]string `json:"effective_tags,omitempty" doc:"The resolved effective tags (key -> winning value) that cascade onto this component; for the Tags column. Provenance is in the effective-tags detail view."`
}

func toComponentBody(c *storage.Component) componentBody {
	return componentBody{
		ID: c.ID, Name: c.Name, DisplayName: c.DisplayName,
		ParentID: c.ParentID, Parent: c.ParentName, SystemID: c.PrimarySystemID, System: c.PrimarySystem, SystemCount: c.SystemCount, LocationID: c.LocationID, Location: c.LocationName, ProductID: c.ProductID, Product: c.ProductHandle,
		NameGenerated: c.NameGenerated, DisplayNameGenerated: c.DisplayNameGenerated,
		Path: c.Path, PathSegments: c.PathSegments, Renders: toRenderBody(c.Path, c.Renders),
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
	Name string `path:"name" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
}

type createComponentInput struct {
	Body struct {
		// Name is optional (#627 Task 14): omit it and the platform mints
		// "<stem>-<n>" from the classified product's component_type, marking
		// name_generated. Supplied, it is validated exactly as before.
		Name        string  `json:"name,omitempty" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"Name, unique within its placement (the address; lowercase letters, digits, hyphens). Omit to have the platform generate one from the product's type."`
		DisplayName string  `json:"display_name,omitempty" doc:"What an operator reads; the name is the address"`
		Parent      *string `json:"parent,omitempty" doc:"Parent component name; omit for a root component"`
		System      *string `json:"system,omitempty" doc:"Primary system name this component belongs to. Naming one writes that system's membership, so it costs the system:update permission and resolves in that scope; omitted, the create costs component:create alone."`
		Location    *string `json:"location,omitempty" doc:"Location name this component is placed at"`
		// Product is required: every component is an instance of a product (the
		// classification floor). It stays a pointer rather than a plain
		// required string so the handler can report a specific, actionable
		// message (naming the generics) instead of Huma's generic
		// missing-required-field text.
		Product *string `json:"product,omitempty" doc:"Product (catalog SKU) this component is an instance of, by name or uuid. Required: use a generic (generic-device, generic-app, generic-service) until a real product is modeled."`
		// ExpectedName is the create form's precondition (#702, and its
		// review). It carries the drafted NAME rather than the ordinal,
		// because the name is what the form showed and what the operator is
		// owed: it holds the stem, the suppression and the number together, so
		// a type edit under the open form invalidates the claim where a number
		// would have passed. It is still a precondition and NOT the name field:
		// the create leaves name empty, so the row stays name_generated and the
		// pen never moves. A pointer, so "absent" is the only spelling of no
		// expectation.
		ExpectedName *string `json:"expected_name,omitempty" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The name a create form previewed (POST /components:renderLabel returns it). The create is refused with a 409 naming what it would produce instead, rather than silently landing a different name, if the number was taken or the type's stem moved while the form was open. It does not name the row (the platform still does, and the row is still name_generated): it only asserts what that name will be. Applies only when the platform names the row: sending it beside a name is a 422."`
	}
}

// updateComponentInput is the PATCH body. It deliberately carries no name: a
// rename is the :rename custom method, gated by component:rename. It carries no
// placement either (#627 Task 13): a move is the :move custom method, gated by
// component:move, because a placement change is an authorization act, not a
// label edit.
type updateComponentInput struct {
	Name string `path:"name" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
	Body struct {
		DisplayName *string `json:"display_name,omitempty" doc:"A new operator-facing label"`
		// Product does NOT follow the house three-state convention: it is
		// required (the classification floor, every component is an instance
		// of a product), so it has no clear state. An omitted field is
		// unchanged and a name reclassifies, but an explicit empty string is a
		// 422, not a clear.
		Product *string `json:"product,omitempty" doc:"Re-classifies the component to this product (catalog SKU), by name or uuid. Required once set: an empty string is refused (422), not a clear. Explicitly-set property values persist; the new product's contract defaults follow."`
	}
}

// moveComponentInput is the :move body: at least one of Location or Parent is
// required (422 otherwise). Both follow the house three-state convention: an
// omitted field is unchanged, an explicit empty string clears, and a name sets.
// So they are pointers passed straight through (never emptyPtrToNil, which
// would collapse an intended clear).
type moveComponentInput struct {
	Name string `path:"name" doc:"The component's name, or a dotted address (e.g. boi.17c.415a.$comp.display-1)"`
	Body struct {
		Location *string `json:"location,omitempty" doc:"Relocates the component to this location name. An empty string clears its placement."`
		Parent   *string `json:"parent,omitempty" doc:"Re-parents the component within the component tree to this component name; cycle-guarded and scope-injected. An empty string makes it a root component (requires an all-scoped move grant)."`
	}
}

// renameComponentInput is the :rename body. The name rule lives here, in the
// contract, not only in the prose below it: the pattern and the ceiling are what
// the generated client, CLI, and JSONSchema enforce.
type renameComponentInput struct {
	Name string `path:"name" doc:"The component's current name, a dotted address, or its uuid"`
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The new name, unique within its placement (lowercase letters, digits, hyphens)"`
	}
}

// errProductRequired is the 422 a component create returns when no product is
// named: the classification floor (every component is an instance of a
// product) is a hard requirement at the API, even though the storage layer
// falls back to generic-device for a direct-gateway caller (seed, devseed,
// tests) that skips it. Names the three generics so the message is
// immediately actionable.
const errProductRequired = "a component must be an instance of a product; name one, or use a generic (generic-device, generic-app, generic-service) until a real product is modeled"

// ErrSystemBindNeedsUpdate is the 403 a component create returns when it names a
// system and the caller does not hold system:update (#707). The system on a
// create is not a field on the component, it is the component's primary
// MEMBERSHIP, the row PUT /systems/{name}/members/{component} writes under
// system:update, so the create cannot be the cheaper way to write it.
//
// It names the permission rather than answering a bare "forbidden" because this
// message is what the CLI prints and what the console falls back to: a refusal an
// operator cannot act on sends them to an administrator with no ask. The second
// sentence is the recovery that does not need a grant at all, since creating the
// component and binding it later are separately authorized acts.
//
// Exported because the CLI's own test asserts this message reaches an operator's
// stdout. A copy of the string over there would go on passing while the API's
// wording drifted, which is the one thing that test exists to catch.
const ErrSystemBindNeedsUpdate = "creating a component into a system writes that system's membership, which requires system:update (the same permission PUT /systems/{name}/members/{component} requires). Create the component without a system, or ask for system:update."

// ErrSystemBindOutOfScope is the 403 for the OTHER half of the same gate: the
// caller holds system:update, but not over this system (#707 review). The
// permission and the scope are two independent layers and both are enforced, so
// both have to be sayable.
//
// It exists because the refusal it replaces was a lie. The bind used to resolve
// in the update scope alone, so a system outside it came back as the
// non-disclosing "system not found" (422), which a caller that had just read
// that system on the same screen could only read as a broken platform. A
// principal with a location-scoped deploy grant is exactly that caller: the
// cross-tier expansion is unbuilt (#10), so its system:update scope is empty
// while its viewer floor reads every system in the estate.
//
// Naming the scope discloses nothing new, because this branch is reached only
// after the reference resolved inside the caller's own system:read scope: a
// caller that cannot see the row still gets the not-found.
const ErrSystemBindOutOfScope = "creating a component into a system writes that system's membership, and this system is outside the system:update scope your grants resolve to (the same scope PUT /systems/{name}/members/{component} resolves in). It is not missing: you can read it, but not write its membership. Create the component without a system, or ask for a system:update grant covering it."

// registerComponentRoutes wires the component CRUD surface, on the same pattern
// as locations and systems.
func registerComponentRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	registerLabelRecomputeRoutes(api, a, gw, "component", "/components")
	registerComponentLabelDraft(api, a, gw)
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

	huma.Register(api, a.conditionalGated(a.gated(huma.Operation{
		OperationID:   "create-component",
		Method:        http.MethodPost,
		Path:          "/components",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a component",
		Description:   "Creates a component, optionally under a parent (a root needs an all-scoped grant), bound to a system and a location, and classified by a product (required; naming a generic is fine until a real product is modeled). Gated by component:create. The location reference resolves within the caller's location:read scope, because the label this stores is rendered from it, and one outside that scope is refused (422) exactly as :renderLabel refuses to preview it. Naming a system additionally requires system:update, and resolves within that scope, because the component's primary membership is inserted from it: it is the same row the membership route writes, so the two paths cost the same permission. A system outside that scope is refused with a 403 naming it when the caller may read the system (denying its existence to someone who can GET it would be a lie) and with the non-disclosing 422 when the caller may not.",
	}, "component", "create"), "system", "update"), func(ctx context.Context, in *createComponentInput) (*componentOutput, error) {
		if in.Body.Product == nil || *in.Body.Product == "" {
			return nil, huma.Error422UnprocessableEntity(errProductRequired)
		}
		// The membership gate (#707), checked here and not in middleware because
		// middleware cannot see the body: a create that names no system writes no
		// membership and costs component:create alone.
		if in.Body.System != nil && *in.Body.System != "" && !a.allows(ctx, "system", "update") {
			return nil, huma.Error403Forbidden(ErrSystemBindNeedsUpdate)
		}
		c, err := gw.CreateComponent(ctx, actorID(ctx), storage.ComponentSpec{
			Name:         in.Body.Name,
			DisplayName:  in.Body.DisplayName,
			ParentName:   in.Body.Parent,
			SystemName:   in.Body.System,
			LocationName: in.Body.Location,
			ProductName:  in.Body.Product,
			ExpectedName: in.Body.ExpectedName,
		}, a.scopeFor(ctx, "component", "create"), a.scopeFor(ctx, "location", "read"),
			a.scopeFor(ctx, "system", "read"), a.scopeFor(ctx, "system", "update"))
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
		Description: "Patches a component's display_name or product. The name is not patchable: renaming is the :rename custom method. Placement is not patchable either: relocating or re-parenting is the :move custom method, gated separately, because a placement change is an authorization act. Product is required, so it is unchanged when omitted and reclassified when named, but an explicit empty string is refused (422), not a clear. Gated by component:update; read and update scopes drive the 404 versus 403 split.",
	}, "component", "update"), func(ctx context.Context, in *updateComponentInput) (*componentOutput, error) {
		if in.Body.Product != nil && *in.Body.Product == "" {
			return nil, huma.Error422UnprocessableEntity("product cannot be cleared; a component must always be an instance of a product")
		}
		c, err := gw.UpdateComponent(ctx, actorID(ctx), in.Name, storage.ComponentPatch{
			DisplayName: in.Body.DisplayName,
			ProductName: in.Body.Product,
		}, a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "component", "update"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		return &componentOutput{Body: toComponentBody(c)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "move-component",
		Method:      http.MethodPost,
		Path:        "/components/{name}:move",
		Summary:     "Move a component",
		Description: "Relocates and/or re-parents a component: at least one of location or parent is required (422 otherwise). Both follow the three-state convention (an omitted field is unchanged, an explicit empty string clears, a name sets). A reparent is cycle-guarded and scope-injected; clearing parent to root requires an all-scoped move grant, the same authorization a root create already requires. A separate act from update, and a separately grantable one (component:move), because a placement change is an authorization act, not a label edit: it moves a row out from under one grant's subtree and under another's. Recorded under its own audit verb, move, distinct from update. Does not recompute health: a component's own verdict is purely its active alarms, unaffected by where it sits. A taken name at the destination is a 409. Gated by component:move; read and move scopes drive the 404 versus 403 split, and the destination location resolves within the caller's location:read scope, because the move restamps the label from it: a destination outside that scope is refused (422).",
	}, "component", "move"), func(ctx context.Context, in *moveComponentInput) (*componentOutput, error) {
		if in.Body.Location == nil && in.Body.Parent == nil {
			return nil, huma.Error422UnprocessableEntity("move requires at least one of location or parent")
		}
		c, err := gw.MoveComponent(ctx, actorID(ctx), in.Name, storage.ComponentMove{
			LocationName: in.Body.Location,
			ParentName:   in.Body.Parent,
		}, a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "component", "move"), a.scopeFor(ctx, "location", "read"))
		if err != nil {
			switch {
			case errors.Is(err, storage.ErrComponentExistsUnderParent):
				return nil, huma.Error409Conflict(fmt.Sprintf("a component named %q already exists under %q", componentMoverName(ctx, gw, a, in.Name), derefStr(in.Body.Parent)))
			case errors.Is(err, storage.ErrComponentExistsInLocation):
				return nil, huma.Error409Conflict(fmt.Sprintf("a component named %q already exists at %q", componentMoverName(ctx, gw, a, in.Name), derefStr(in.Body.Location)))
			case errors.Is(err, storage.ErrComponentExistsUnplaced):
				return nil, huma.Error409Conflict(fmt.Sprintf("an unplaced component named %q already exists", componentMoverName(ctx, gw, a, in.Name)))
			}
			return nil, mapComponentErr(err)
		}
		return &componentOutput{Body: toComponentBody(c)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "rename-component",
		Method:      http.MethodPost,
		Path:        "/components/{name}:rename",
		Summary:     "Rename a component",
		Description: "Moves the component's name, the address an operator types and every external reference stores. A separate act from an update, and a separately grantable one, because it breaks bookmarks, runbooks, and integration config outside this system; inside it nothing breaks, since every reference holds the uuid. A taken name is a 409, an illegal or uuid-shaped one a 422. Gated by component:rename; read and rename scopes drive the 404 versus 403 split.",
	}, "component", "rename"), func(ctx context.Context, in *renameComponentInput) (*componentOutput, error) {
		c, err := gw.RenameComponent(ctx, actorID(ctx), in.Name, in.Body.Name,
			a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "component", "rename"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		return &componentOutput{Body: toComponentBody(c)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "reset-component-name",
		Method:      http.MethodPost,
		Path:        "/components/{name}:resetName",
		Summary:     "Regenerate a component's name",
		Description: "Hands the pen back to the platform: regenerates the name from the component's current type and placement (the same \"<stem>-<n>\" rule a nameless create applies) and marks it name_generated, whether or not it already was. Gated by component:rename, the same token :rename uses: it changes the name, exactly that permission's blast radius.",
	}, "component", "rename"), func(ctx context.Context, in *componentPathInput) (*componentOutput, error) {
		c, err := gw.ResetComponentName(ctx, actorID(ctx), in.Name,
			a.scopeFor(ctx, "component", "read"), a.scopeFor(ctx, "component", "rename"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		return &componentOutput{Body: toComponentBody(c)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "check-component-name",
		Method:      http.MethodPost,
		Path:        "/components:checkName",
		Summary:     "Check a component name",
		Description: "Reports whether a proposed name is a valid slug and currently free within the given placement (parent wins over location; neither means the unplaced/root bucket). Advisory (Save is still gated by the unique constraint). Gated by component:update.",
	}, "component", "update"), func(ctx context.Context, in *checkNameInput) (*checkNameOutput, error) {
		out := &checkNameOutput{}
		if err := storage.ValidateName("component", in.Body.Name); err != nil {
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
		taken, err := gw.ComponentNameTaken(ctx, in.Body.Name, in.Body.Parent, in.Body.Location)
		if err != nil {
			return nil, mapComponentErr(err)
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

// componentMoverName resolves ref (whatever form the caller addressed the
// move by: a bare name, a dotted address, or a uuid) back to the component's
// own name, for a 409 collision message that names the mover by what an
// operator actually reads rather than echoing the raw path reference. The
// move already failed and rolled back, so the row is unchanged; a fresh read
// within the caller's own read scope is safe (it discloses nothing the
// caller could not already read a moment ago) and falls back to ref itself if
// the lookup somehow misses, so the 409 still names something rather than
// producing an empty quote.
func componentMoverName(ctx context.Context, gw storage.Gateway, a *authenticator, ref string) string {
	if c, err := gw.GetComponent(ctx, ref, a.scopeFor(ctx, "component", "read")); err == nil {
		return c.Name
	}
	return ref
}

func mapComponentErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	if ordErr, ok := mapDraftedNameErr(err); ok {
		return ordErr
	}
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
	case errors.Is(err, storage.ErrEntityNameIsUUID):
		return huma.Error422UnprocessableEntity("component name may not be a uuid: that form is reserved for an entity's id")
	case errors.Is(err, storage.ErrInvalidEntityName):
		return huma.Error422UnprocessableEntity("invalid name")
	case errors.Is(err, storage.ErrParentComponentNotFound):
		return huma.Error422UnprocessableEntity("parent component not found")
	case errors.Is(err, storage.ErrComponentCycle):
		return huma.Error422UnprocessableEntity("cannot move a component under itself or a descendant")
	// Before ErrSystemNotFound, and a 403 rather than a 422: the reference is
	// well formed and names a row the caller can read, so what is wrong is the
	// caller's authority over it, not the request body.
	case errors.Is(err, storage.ErrSystemBindForbidden):
		return huma.Error403Forbidden(ErrSystemBindOutOfScope)
	case errors.Is(err, storage.ErrSystemNotFound):
		return huma.Error422UnprocessableEntity("system not found")
	case errors.Is(err, storage.ErrLocationNotFound):
		return huma.Error422UnprocessableEntity("location not found")
	case errors.Is(err, storage.ErrProductNotFound):
		return huma.Error422UnprocessableEntity("product not found")
	case errors.Is(err, storage.ErrComponentTypeNoStem):
		return errNoGeneratedName("this product's component_type has no stem to generate a name from; supply a name explicitly, or fix the component_type registry")
	default:
		return huma.Error500InternalServerError("component operation failed")
	}
}
