package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/updatemask"
)

// locationBody is the wire shape of a location: name-addressable, classified by
// location_type, optionally nested under a parent (by id).
type locationBody struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
	// The NAME's pen (#686); see systemBody for why it is read-only. True only
	// where the location's type carries a name rule (#687), which no shipped
	// place type does (ADR-0103): an operator declares a positional type to
	// reach it.
	NameGenerated bool `json:"name_generated" doc:"Whether the platform picked this name (from the location_type's name rule) rather than an operator typing it."`
	// The LABEL's pen (#682); see componentBody for why it is read-only.
	LabelGenerated bool              `json:"label_generated" doc:"Whether the platform rendered this label from a label rule rather than an operator typing it. Read-only: write label to claim it, write an empty label to hand it back."`
	LocationType   string            `json:"location_type" doc:"The location_type name"`
	LocationTypeID string            `json:"location_type_id" doc:"The location_type's uuid, the stable form of location_type"`
	ParentID       *string           `json:"parent_id,omitempty" doc:"The parent location's id, the canonical handle"`
	Parent         *string           `json:"parent,omitempty" doc:"The parent location's name, for display; absent for a site root"`
	Path           string            `json:"path,omitempty" doc:"The dotted address (e.g. boi.17c.415a); no accessor, since a location's own address IS its location-tree ancestor chain. Set on a GET or LIST response; empty on a create/update/move/rename response (refetch the row to see it)."`
	PathSegments   []string          `json:"path_segments,omitempty" doc:"path split on '.'."`
	Renders        *renderBody       `json:"renders,omitempty" doc:"Two display-only compact forms of path, dash and bare. Neither is accepted back by the resolver: stripping/compacting is lossy."`
	Actions        []string          `json:"actions,omitempty" doc:"The scope-aware actions the caller may perform on this row (create a child, update, delete); a UI hint, the server still enforces."`
	EffectiveTags  map[string]string `json:"effective_tags,omitempty" doc:"The resolved effective tags (key -> winning value) that cascade onto this location (platform and its location tree); for the Tags column."`
}

func toLocationBody(l *storage.Location) locationBody {
	return locationBody{
		ID: l.ID, Name: l.Name, Label: l.Label,
		NameGenerated: l.NameGenerated, LabelGenerated: l.LabelGenerated,
		LocationType: l.LocationType, LocationTypeID: l.LocationTypeID, ParentID: l.ParentID, Parent: l.ParentName,
		Path: l.Path, PathSegments: l.PathSegments, Renders: toRenderBody(l.Path, l.Renders),
	}
}

type listLocationsOutput struct {
	Body struct {
		Locations []locationBody `json:"locations"`
	}
}

// locationTypeBody is the wire shape of a location_type registry row: the
// stable id a location is classified by, its label, the icon the
// console renders as each location's leading tree glyph, AllowedParentTypes
// (the placement constraint: a set of location_type ids and/or the reserved
// "root" sentinel; empty means unconstrained), and whether it ships with the
// binary. The registry lists alphabetically by label.
type locationTypeBody struct {
	ID                 string            `json:"id" doc:"The location type's uuid, the stable handle that survives a rename"`
	Name               string            `json:"name" doc:"The name an operator reads and types; renameable"`
	Label              string            `json:"label"`
	Icon               string            `json:"icon"`
	AllowedParentTypes []string          `json:"allowed_parent_types"`
	LabelRule          string            `json:"label_rule,omitempty" doc:"The label template locations of this type get; empty falls back to the global rule for locations"`
	NameRule           *nameRuleReadBody `json:"name_rule,omitempty" doc:"How the platform NAMES locations of this type; absent means an operator names every one of them"`
	Official           bool              `json:"official" doc:"True for a row this release ships. A shipped row is never written by an operator: an edit forks it"`
	Forked             bool              `json:"forked" doc:"True when this shipped row carries changes of yours overriding it. Restore discards them"`
}

// nameRuleBody is the wire shape of a location_type's name rule (#687): a
// DECLARATION, deliberately not a template like label_rule beside it, because a
// name has to satisfy the slug rule and lands in a unique index other rows
// reference, so it is refused at rule-edit time by minting from it (ADR-0102).
//
// An empty stem is a POSITIONAL type, whose ordinal genuinely is its name: a
// parking deck called 1, then 2. bare_first is ignored there, since suppressing
// the ordinal of a name that IS its ordinal would leave nothing.
type nameRuleBody struct {
	Stem      string `json:"stem" maxLength:"90" pattern:"^([a-z0-9][a-z0-9-]*)?$" doc:"The generated name's prefix (wing gives wing, wing-2); empty makes the type positional, so the name is the ordinal alone (1, 2, 3). The 90-character ceiling is the mint's: 90 plus the widest ordinal is exactly the 100-character name cap, so every stem this admits mints legally at both ends of its output space"`
	BareFirst bool   `json:"bare_first,omitempty" doc:"Suppress the ordinal on the first of this stem in a parent, so the only wing there is wing and the second is wing-2. Ignored when stem is empty."`
}

// nameRuleReadBody is a rule as it comes BACK: the declaration an operator
// wrote, plus the names it actually mints. A read shape of its own rather than a
// read-only field on the write shape, so a request body never carries a field
// the server would ignore.
//
// Examples exist because a surface has to say what a rule PRODUCES, and the only
// honest source for that is the mint (#710). A console that re-derived
// "<stem>-<n>" in TypeScript would be the duplication #695 tracked and #702
// closed for the drafted name: two implementations of one shape, whose failure
// mode is an operator promised a name no create makes. It costs nothing to serve
// (a pure function over two fields already in hand) and it is a fact about the
// RULE, not about the fleet, so it needs no read scope and reserves no ordinal.
type nameRuleReadBody struct {
	Stem      string   `json:"stem" doc:"The generated name's prefix; empty makes the type positional"`
	BareFirst bool     `json:"bare_first,omitempty" doc:"True when the first of this stem in a parent carries no ordinal"`
	Examples  []string `json:"examples" doc:"The first two names this rule mints in one parent, produced by the same mint a create allocates from: the first shows whether the first of a stem carries a number, the second shows the shape every later one takes. Read these rather than rebuilding the shape, which is what keeps one definition of a generated name"`
}

// toLocationTypeBody is the one projection of a registry row onto the wire.
// The three routes that return one used to spell it out each time, which is
// fine for five columns and a trap at seven: a rule added to two of the three
// is a field that reads back empty from whichever one was missed.
func toLocationTypeBody(lt *storage.LocationType) locationTypeBody {
	return locationTypeBody{
		ID: lt.ID, Name: lt.Name, Label: lt.Label, Icon: lt.Icon,
		AllowedParentTypes: lt.AllowedParentTypes, LabelRule: derefStr(lt.LabelRule),
		NameRule: toNameRuleBody(lt.NameRule), Official: lt.Official, Forked: lt.Forked,
	}
}

func toNameRuleBody(r *storage.NameRule) *nameRuleReadBody {
	if r == nil {
		return nil
	}
	return &nameRuleReadBody{Stem: r.Stem, BareFirst: r.BareFirst, Examples: r.Examples()}
}

func toStorageNameRule(r *nameRuleBody) *storage.NameRule {
	if r == nil {
		return nil
	}
	return &storage.NameRule{Stem: r.Stem, BareFirst: r.BareFirst}
}

type listLocationTypesOutput struct {
	Body struct {
		LocationTypes []locationTypeBody `json:"location_types"`
	}
}

type locationTypePathInput struct {
	ID string `path:"id" doc:"The location_type id"`
}

type createLocationTypeInput struct {
	Body struct {
		Name               string   `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The globally unique name (e.g. wing); \"root\" is reserved"`
		Label              string   `json:"label" minLength:"1" doc:"What an operator reads in pickers and lists"`
		Icon               string   `json:"icon,omitempty" doc:"A glyph key; the console falls back to map-pin when empty"`
		AllowedParentTypes []string `json:"allowed_parent_types,omitempty" doc:"location_type names and/or the reserved root sentinel this type may be placed under; empty means unconstrained"`
		LabelRule          string   `json:"label_rule,omitempty" doc:"The label template locations of this type get, a Go text/template over the location data map; omit to fall back to the global rule. Refused (422) if it does not compile"`
		// A declaration, not a template: see nameRuleBody. Omit it and an
		// operator names every location of this type, which is the default and
		// the state three of the four shipped types stay in.
		NameRule *nameRuleBody `json:"name_rule,omitempty" doc:"How the platform NAMES locations of this type; omit to have an operator name every one of them. Refused (422) if it cannot mint a legal name"`
	}
}

type updateLocationTypeInput struct {
	ID   string `path:"id"`
	Body struct {
		// The mask is what makes a nullable OBJECT field clearable, and it is
		// the house answer for every one that follows (#692, ADR-0091): an
		// omitted key and an explicit null both decode to the same nil in Go,
		// so the body alone can never spell "clear". The mask carries the
		// intent instead, which is the reason AIP-134 has one.
		UpdateMask         []string      `json:"update_mask,omitempty" doc:"Which fields this write changes (AIP-134). Omit it and the fields present in the body change and nothing else; name a field here and it is written even when the body leaves it empty, which is how a field is CLEARED (name_rule back to operator-named); send [\"*\"] for full replacement. A field this resource does not patch is a 422 naming it"`
		Label              *string       `json:"label,omitempty" doc:"A new operator-facing label"`
		Icon               *string       `json:"icon,omitempty" doc:"A new glyph key; the console falls back to map-pin when empty"`
		AllowedParentTypes *[]string     `json:"allowed_parent_types,omitempty" doc:"Replaces the allowed-parent set; omit to leave unchanged, [] to clear back to unconstrained"`
		LabelRule          *string       `json:"label_rule,omitempty" doc:"A new label template for locations of this type; omit to leave unchanged, \"\" to clear back to the global rule. Refused (422) if it does not compile. Editing it does not restamp anything: apply it with /locations:recomputeLabels, having seen the blast radius with :previewLabels"`
		NameRule           *nameRuleBody `json:"name_rule,omitempty" doc:"A new name rule for locations of this type; omit to leave unchanged, or name name_rule in update_mask with no rule here to CLEAR it back to operator-named. Refused (422) if it cannot mint a legal name. Setting it renames nothing that already exists: it decides how the NEXT nameless create, :resetName, move or reclassify names a row"`
	}
}

type locationTypeOutput struct {
	Body locationTypeBody
}

type locationOutput struct {
	Body locationBody
}

type locationPathInput struct {
	Name string `path:"name" doc:"The location's name, or a dotted address (e.g. boi.17c.415a)"`
}

type createLocationInput struct {
	Body struct {
		// Name is optional (#687), exactly as a component's and a system's are:
		// omit it and the platform mints one from the location_type's name
		// rule, and marks name_generated. Supplied, it is validated exactly as
		// before. Omitting it for a type carrying no rule is a 422: only some
		// kinds of place have a name the platform can know.
		Name         string  `json:"name,omitempty" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"Name, unique within its placement (the address; lowercase letters, digits, hyphens). Omit to have the platform generate one from the location_type's name rule."`
		Label        string  `json:"label,omitempty" doc:"What an operator reads; the name is the address"`
		LocationType string  `json:"location_type" minLength:"1" doc:"The location_type, by name or uuid (campus, building, ...)"`
		Parent       *string `json:"parent,omitempty" doc:"Parent location name; omit for a root location"`
		// The create form's ordinal precondition (#702); see
		// createComponentInput for why it is a number and never a name.
		ExpectedName *string `json:"expected_name,omitempty" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The name a create form previewed (POST /locations:renderLabel returns it). The create is refused with a 409 naming what it would produce instead, rather than silently landing a different name, if the number was taken or the location_type's name rule moved while the form was open. It does not name the row (the platform still does, and the row is still name_generated): it only asserts what that name will be. Applies only when the platform names the row: sending it beside a name is a 422."`
	}
}

// updateLocationInput is the PATCH body. It deliberately carries no name: a rename
// is the :rename custom method, gated by location:rename. It carries no parent
// either (#627 Task 13): a move is the :move custom method, gated by
// location:move, because a placement change is an authorization act, not a
// label edit.
type updateLocationInput struct {
	Name string `path:"name" doc:"The location's name, or a dotted address (e.g. boi.17c.415a)"`
	Body struct {
		Label        *string `json:"label,omitempty" doc:"A new operator-facing label"`
		LocationType *string `json:"location_type,omitempty" doc:"Re-types the location: a location_type, by name or uuid"`
	}
}

// moveLocationInput is the :move body: Parent is required (422 if omitted),
// since it is the only field a location's move carries. Re-parents the
// location (a tree move), cycle-guarded and placement-validated. Moving to
// root is not supported: MoveLocation gains no clear-to-root capability
// locations have never had (see the ADR for why this asymmetry with
// component/system is deliberate).
type moveLocationInput struct {
	Name string `path:"name" doc:"The location's name, or a dotted address (e.g. boi.17c.415a)"`
	Body struct {
		Parent *string `json:"parent,omitempty" doc:"Re-parents the location (a tree move) to this location name; cycle-guarded and placement-validated. Moving to root is not supported."`
	}
}

// renameLocationInput is the :rename body. The name rule lives here, in the
// contract, not only in the prose below it.
type renameLocationInput struct {
	Name string `path:"name" doc:"The location's current name, a dotted address, or its uuid"`
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"100" pattern:"^[a-z0-9][a-z0-9-]*$" doc:"The new name, unique within its placement (lowercase letters, digits, hyphens)"`
	}
}

// registerLocationRoutes wires the location CRUD surface. Each route declares its
// capability with a.require (the fast-reject), and each handler resolves the
// caller's per-action scope and hands it to the gateway, which expands it to the
// row filter and writes audit. The capability is necessary, the scope decides.
func registerLocationRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	registerLabelRecomputeRoutes(api, a, gw, "location", "/locations")
	registerLocationLabelDraft(api, a, gw)
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-locations",
		Method:      http.MethodGet,
		Path:        "/locations",
		Summary:     "List locations in scope",
		Description: "Lists the locations the caller may read, each filtered to its scope subtree. Gated by location:read.",
	}, "location", "read"), func(ctx context.Context, _ *struct{}) (*listLocationsOutput, error) {
		locs, err := gw.ListLocations(ctx, a.scopeFor(ctx, "location", "read"))
		if err != nil {
			return nil, huma.Error500InternalServerError("list locations")
		}
		ids := make([]string, len(locs))
		for i := range locs {
			ids[i] = locs[i].ID
		}
		effTags, err := gw.EffectiveTags(ctx, "location", ids)
		if err != nil {
			return nil, huma.Error500InternalServerError("list locations")
		}
		acts, err := a.rowActions(ctx, gw, "location", ids)
		if err != nil {
			return nil, huma.Error500InternalServerError("list locations")
		}
		out := &listLocationsOutput{}
		out.Body.Locations = make([]locationBody, 0, len(locs))
		for i := range locs {
			b := toLocationBody(&locs[i])
			b.Actions = acts[locs[i].ID]
			b.EffectiveTags = effTags[locs[i].ID]
			out.Body.Locations = append(out.Body.Locations, b)
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "list-location-types",
		Method:      http.MethodGet,
		Path:        "/location-types",
		Summary:     "List location types",
		Description: "Lists the location_type registry (the shape-definers a location is classified by), ordered alphabetically by label. Populates the type picker on the location form. Gated by location_type:read.",
	}, "location_type", "read"), func(ctx context.Context, _ *struct{}) (*listLocationTypesOutput, error) {
		types, err := gw.ListLocationTypes(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list location types")
		}
		out := &listLocationTypesOutput{}
		out.Body.LocationTypes = make([]locationTypeBody, 0, len(types))
		for i := range types {
			out.Body.LocationTypes = append(out.Body.LocationTypes, toLocationTypeBody(&types[i]))
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-location-type",
		Method:        http.MethodPost,
		Path:          "/location-types",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a location type",
		Description:   "Creates a custom (non-official) location_type, optionally with the label_rule locations of that type get. An unparseable rule is a 422. Gated by location_type:create.",
	}, "location_type", "create"), func(ctx context.Context, in *createLocationTypeInput) (*locationTypeOutput, error) {
		lt, err := gw.CreateLocationType(ctx, actorID(ctx), storage.LocationType{
			Name: in.Body.Name, Label: in.Body.Label, Icon: in.Body.Icon,
			AllowedParentTypes: in.Body.AllowedParentTypes, LabelRule: ptrOrNil(in.Body.LabelRule),
			NameRule: toStorageNameRule(in.Body.NameRule),
		})
		if err != nil {
			return nil, mapTypeErr(err, "location_type")
		}
		return &locationTypeOutput{Body: toLocationTypeBody(lt)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-location-type",
		Method:      http.MethodPatch,
		Path:        "/location-types/{id}",
		Summary:     "Update a location type",
		Description: "Patches a location_type's label, icon, allowed parents, label_rule or name_rule. An unparseable label_rule (or a name_rule that cannot mint a legal name) is a 422 at rule-edit time, never a broken row at create time, and setting a label rule restamps nothing on its own: apply it with /locations:recomputeLabels after seeing the blast radius with :previewLabels. A shipped (official) row is never written: the patch FORKS it, storing your version over the shipped one, and the response comes back with forked=true under the same id. `:restore` discards the fork. Gated by location_type:update.",
	}, "location_type", "update"), func(ctx context.Context, in *updateLocationTypeInput) (*locationTypeOutput, error) {
		patch := storage.LocationTypePatch{
			Label: in.Body.Label, Icon: in.Body.Icon,
			AllowedParentTypes: in.Body.AllowedParentTypes, LabelRule: in.Body.LabelRule,
			NameRule: toStorageNameRule(in.Body.NameRule),
		}
		// The implied mask is read off the PATCH the write is about to store,
		// not off the body field by field, so "which fields did the caller
		// send" is defined once for both layers instead of once per layer,
		// where the two definitions would drift.
		write, err := updatemask.Resolve(in.Body.UpdateMask, storage.LocationTypePatchFields, patch.Populated())
		if err != nil {
			return nil, mapTypeErr(err, "location_type")
		}
		patch.Write = write
		lt, err := gw.UpdateLocationType(ctx, actorID(ctx), in.ID, patch)
		if err != nil {
			return nil, mapTypeErr(err, "location_type")
		}
		return &locationTypeOutput{Body: toLocationTypeBody(lt)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "restore-location-type",
		Method:      http.MethodPost,
		Path:        "/location-types/{id}:restore",
		Summary:     "Restore a location type's shipped values",
		Description: "Discards your fork of a shipped location_type, so reads return the values this release ships, including a rule a later release WITHDREW. 409 when the row carries no fork of yours. Gated by location_type:update, the same permission that took the fork: restoring is undoing your own edit, not deleting a row.",
	}, "location_type", "update"), func(ctx context.Context, in *locationTypePathInput) (*locationTypeOutput, error) {
		lt, err := gw.RestoreLocationType(ctx, actorID(ctx), in.ID)
		if err != nil {
			return nil, mapTypeErr(err, "location_type")
		}
		return &locationTypeOutput{Body: toLocationTypeBody(lt)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-location-type",
		Method:        http.MethodDelete,
		Path:          "/location-types/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a location type",
		Description:   "Deletes a custom location_type, refused if official (422) or still referenced by a location (409). Gated by location_type:delete.",
	}, "location_type", "delete"), func(ctx context.Context, in *locationTypePathInput) (*struct{}, error) {
		if err := gw.DeleteLocationType(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapTypeErr(err, "location_type")
		}
		return nil, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-location",
		Method:      http.MethodGet,
		Path:        "/locations/{name}",
		Summary:     "Get a location",
		Description: "Fetches a location by name within the caller's read scope. Out of scope is a non-disclosing 404. Gated by location:read.",
	}, "location", "read"), func(ctx context.Context, in *locationPathInput) (*locationOutput, error) {
		l, err := gw.GetLocation(ctx, in.Name, a.scopeFor(ctx, "location", "read"))
		if err != nil {
			return nil, mapLocationErr(err)
		}
		return &locationOutput{Body: toLocationBody(l)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "create-location",
		Method:        http.MethodPost,
		Path:          "/locations",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a location",
		Description:   "Creates a location, optionally under a parent (a root needs an all-scoped grant). Omit name and the platform generates one from the location_type's name rule, taking the lowest free ordinal among the siblings in that placement; a type carrying no name rule refuses (422), since a building's real name is not something the platform can know. Gated by location:create.",
	}, "location", "create"), func(ctx context.Context, in *createLocationInput) (*locationOutput, error) {
		l, err := gw.CreateLocation(ctx, actorID(ctx), storage.LocationSpec{
			Name:         in.Body.Name,
			Label:        in.Body.Label,
			LocationType: in.Body.LocationType,
			ParentName:   in.Body.Parent,
			ExpectedName: in.Body.ExpectedName,
		}, a.scopeFor(ctx, "location", "create"))
		if err != nil {
			return nil, mapLocationErr(err)
		}
		return &locationOutput{Body: toLocationBody(l)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "update-location",
		Method:      http.MethodPatch,
		Path:        "/locations/{name}",
		Summary:     "Update a location",
		Description: "Patches a location's label or location_type. The name is not patchable: renaming is the :rename custom method. Placement is not patchable either: re-parenting is the :move custom method, gated separately, because a placement change is an authorization act. Changing the location_type of a location the PLATFORM named re-mints the name from the new type's name rule, and is refused (422) when the new type carries none, which is every shipped type: :rename the location first to claim its name, then reclassify it. A location an operator named is never renamed by a reclassify. Gated by location:update; the read and update scopes drive the 404 versus 403 split.",
	}, "location", "update"), func(ctx context.Context, in *updateLocationInput) (*locationOutput, error) {
		l, err := gw.UpdateLocation(ctx, actorID(ctx), in.Name, storage.LocationPatch{
			Label:        in.Body.Label,
			LocationType: in.Body.LocationType,
		}, a.scopeFor(ctx, "location", "read"), a.scopeFor(ctx, "location", "update"))
		if err != nil {
			return nil, mapLocationErr(err)
		}
		return &locationOutput{Body: toLocationBody(l)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "move-location",
		Method:      http.MethodPost,
		Path:        "/locations/{name}:move",
		Summary:     "Move a location",
		Description: "Re-parents a location (a tree move): parent is required (422 if omitted), cycle-guarded, and placement-validated against the resolved location_type. Moving to root is not supported (422): MoveLocation gains no clear-to-root capability locations have never had. A separate act from update, and a separately grantable one (location:move), because a placement change is an authorization act, not a label edit. Recorded under its own audit verb, move, distinct from update. Does not recompute health. A taken name at the destination is a 409. A move can RENAME the location: a platform-generated name is scoped to its parent bucket, so a move that changes the parent re-mints the name and the ordinal under the new parent, from the same location_type name rule. A move that re-states the current parent leaves the name alone, and an operator-typed name is never touched. Gated by location:move; the read and move scopes drive the 404 versus 403 split.",
	}, "location", "move"), func(ctx context.Context, in *moveLocationInput) (*locationOutput, error) {
		if in.Body.Parent == nil {
			return nil, huma.Error422UnprocessableEntity("move requires parent")
		}
		l, err := gw.MoveLocation(ctx, actorID(ctx), in.Name, storage.LocationMove{
			ParentName: in.Body.Parent,
		}, a.scopeFor(ctx, "location", "read"), a.scopeFor(ctx, "location", "move"))
		if err != nil {
			switch {
			case errors.Is(err, storage.ErrLocationExistsUnderParent):
				return nil, huma.Error409Conflict(fmt.Sprintf("a location named %q already exists under %q", locationMoverName(ctx, gw, a, in.Name), derefStr(in.Body.Parent)))
			case errors.Is(err, storage.ErrLocationExistsAtRoot):
				return nil, huma.Error409Conflict(fmt.Sprintf("a root location named %q already exists", locationMoverName(ctx, gw, a, in.Name)))
			}
			return nil, mapLocationErr(err)
		}
		return &locationOutput{Body: toLocationBody(l)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "rename-location",
		Method:      http.MethodPost,
		Path:        "/locations/{name}:rename",
		Summary:     "Rename a location",
		Description: "Moves the location's name, the address an operator types and every external reference stores. A separate act from an update, and a separately grantable one, because it breaks bookmarks, runbooks, and integration config outside this system; inside it nothing breaks, since every reference holds the uuid. A taken name is a 409, an illegal or uuid-shaped one a 422. Gated by location:rename; the read and rename scopes drive the 404 versus 403 split.",
	}, "location", "rename"), func(ctx context.Context, in *renameLocationInput) (*locationOutput, error) {
		l, err := gw.RenameLocation(ctx, actorID(ctx), in.Name, in.Body.Name,
			a.scopeFor(ctx, "location", "read"), a.scopeFor(ctx, "location", "rename"))
		if err != nil {
			return nil, mapLocationErr(err)
		}
		return &locationOutput{Body: toLocationBody(l)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "reset-location-name",
		Method:      http.MethodPost,
		Path:        "/locations/{name}:resetName",
		Summary:     "Regenerate a location's name",
		Description: "Hands the pen back to the platform, the same verb components and systems carry: the name is re-minted from the location_type's name rule and the lowest free ordinal in this placement, and name_generated goes back to true. A location_type carrying no name rule refuses (422), which is every type a shipped fleet has: only a positional kind of place, one whose number is an arbitrary disambiguator rather than a designation read off the signage (a parking deck, a rack row), has a name the platform can generate, and an operator declares such a type themselves. Gated by location:rename, the same token :rename uses.",
	}, "location", "rename"), func(ctx context.Context, in *locationPathInput) (*locationOutput, error) {
		l, err := gw.ResetLocationName(ctx, actorID(ctx), in.Name,
			a.scopeFor(ctx, "location", "read"), a.scopeFor(ctx, "location", "rename"))
		if err != nil {
			return nil, mapLocationErr(err)
		}
		return &locationOutput{Body: toLocationBody(l)}, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID: "check-location-name",
		Method:      http.MethodPost,
		Path:        "/locations:checkName",
		Summary:     "Check a location name",
		Description: "Reports whether a proposed name is a valid slug and currently free within the given placement (under the given parent, or among roots when no parent is given). Advisory (Save is still gated by the unique constraint). Gated by location:update.",
	}, "location", "update"), func(ctx context.Context, in *checkNameInput) (*checkNameOutput, error) {
		out := &checkNameOutput{}
		if err := storage.ValidateName("location", in.Body.Name); err != nil {
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
		taken, err := gw.LocationNameTaken(ctx, in.Body.Name, in.Body.Parent)
		if err != nil {
			return nil, mapLocationErr(err)
		}
		out.Body.Available = !taken
		if taken {
			out.Body.Reason = "That name is already taken."
		}
		return out, nil
	})

	huma.Register(api, a.gated(huma.Operation{
		OperationID:   "delete-location",
		Method:        http.MethodDelete,
		Path:          "/locations/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a location",
		Description:   "Deletes a location, refused (409) while it still has child locations or is still referenced elsewhere. Gated by location:delete; read and delete scopes drive the 404 versus 403 split.",
	}, "location", "delete"), func(ctx context.Context, in *locationPathInput) (*struct{}, error) {
		if err := gw.DeleteLocation(ctx, actorID(ctx), in.Name,
			a.scopeFor(ctx, "location", "read"), a.scopeFor(ctx, "location", "delete")); err != nil {
			return nil, mapLocationErr(err)
		}
		return nil, nil
	})
}

// actorID is the authenticated principal id for the audit row, empty if absent
// (authn middleware guarantees presence on these routes).
func actorID(ctx context.Context) string {
	if pr, ok := principalFrom(ctx); ok {
		return pr.ID
	}
	return ""
}

// locationMoverName resolves ref back to the location's own name for a 409
// collision message; see componentMoverName's doc comment for the reasoning.
func locationMoverName(ctx context.Context, gw storage.Gateway, a *authenticator, ref string) string {
	if l, err := gw.GetLocation(ctx, ref, a.scopeFor(ctx, "location", "read")); err == nil {
		return l.Name
	}
	return ref
}

// mapLocationErr translates the gateway's location sentinels into HTTP status:
// the non-disclosing 404, the readable-not-actionable 403, occupancy and
// name-clash 409, and the request faults 422. A placement violation carries
// the offending child and parent type names in its message.
func mapLocationErr(err error) error {
	if refErr, ok := mapRefErr(err); ok {
		return refErr
	}
	if ordErr, ok := mapDraftedNameErr(err); ok {
		return ordErr
	}
	var placementErr *storage.PlacementError
	if errors.As(err, &placementErr) {
		return huma.Error422UnprocessableEntity(placementErr.Error())
	}
	switch {
	case errors.Is(err, storage.ErrLocationNotFound):
		return huma.Error404NotFound("location not found")
	case errors.Is(err, storage.ErrLocationForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, storage.ErrReferenced):
		return huma.Error409Conflict("location is still referenced by another record")
	case errors.Is(err, storage.ErrLocationOccupied):
		return huma.Error409Conflict("location has child locations")
	case errors.Is(err, storage.ErrLocationExists):
		return huma.Error409Conflict("location name already exists")
	case errors.Is(err, storage.ErrEntityNameIsUUID):
		return huma.Error422UnprocessableEntity("location name may not be a uuid: that form is reserved for an entity's id")
	case errors.Is(err, storage.ErrInvalidEntityName):
		return huma.Error422UnprocessableEntity("invalid name")
	case errors.Is(err, storage.ErrParentNotFound):
		return huma.Error422UnprocessableEntity("parent location not found")
	case errors.Is(err, storage.ErrUnknownType):
		return huma.Error422UnprocessableEntity("unknown location_type")
	case errors.Is(err, storage.ErrLocationCycle):
		return huma.Error422UnprocessableEntity("cannot move a location under itself or a descendant")
	case errors.Is(err, storage.ErrLocationTypeNoNameRule):
		return errNoGeneratedName("this location_type has no name rule, so the platform cannot generate a name for it: supply a name on create, or :rename the location to claim its name before reclassifying it to this type")
	default:
		return huma.Error500InternalServerError("location operation failed")
	}
}
