package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// locationBody is the wire shape of a location: name-addressable, classified by
// location_type, optionally nested under a parent (by id).
type locationBody struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	DisplayName   string            `json:"display_name,omitempty"`
	LocationType  string            `json:"location_type"`
	ParentID      *string           `json:"parent_id,omitempty"`
	Actions       []string          `json:"actions,omitempty" doc:"The scope-aware actions the caller may perform on this row (create a child, update, delete); a UI hint, the server still enforces."`
	EffectiveTags map[string]string `json:"effective_tags,omitempty" doc:"The resolved effective tags (key -> winning value) that cascade onto this location (global and its location tree); for the Tags column."`
}

func toLocationBody(l *storage.Location) locationBody {
	return locationBody{
		ID: l.ID, Name: l.Name, DisplayName: l.DisplayName,
		LocationType: l.LocationType, ParentID: l.ParentID,
	}
}

type listLocationsOutput struct {
	Body struct {
		Locations []locationBody `json:"locations"`
	}
}

// locationTypeBody is the wire shape of a location_type registry row: the
// stable id a location is classified by, its display_name, the icon the
// console renders as each location's leading tree glyph, AllowedParentTypes
// (the placement constraint: a set of location_type ids and/or the reserved
// "root" sentinel; empty means unconstrained), and whether it ships with the
// binary. The registry lists alphabetically by display_name.
type locationTypeBody struct {
	ID                 string   `json:"id"`
	DisplayName        string   `json:"display_name"`
	Icon               string   `json:"icon"`
	AllowedParentTypes []string `json:"allowed_parent_types"`
	Official           bool     `json:"official"`
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
		ID                 string   `json:"id" minLength:"1" doc:"Globally unique type id (kebab, e.g. wing); \"root\" is reserved"`
		DisplayName        string   `json:"display_name" minLength:"1"`
		Icon               string   `json:"icon,omitempty" doc:"A glyph key; the console falls back to map-pin when empty"`
		AllowedParentTypes []string `json:"allowed_parent_types,omitempty" doc:"location_type ids and/or the reserved root sentinel this type may be placed under; empty means unconstrained"`
	}
}

type updateLocationTypeInput struct {
	ID   string `path:"id"`
	Body struct {
		DisplayName        *string   `json:"display_name,omitempty"`
		Icon                *string   `json:"icon,omitempty"`
		AllowedParentTypes *[]string `json:"allowed_parent_types,omitempty" doc:"Replaces the allowed-parent set; omit to leave unchanged, [] to clear back to unconstrained"`
	}
}

type locationTypeOutput struct {
	Body locationTypeBody
}

type locationOutput struct {
	Body locationBody
}

type locationPathInput struct {
	Name string `path:"name" doc:"The location's unique name"`
}

type createLocationInput struct {
	Body struct {
		Name         string  `json:"name" minLength:"1" doc:"Globally unique name (the address)"`
		DisplayName  string  `json:"display_name,omitempty"`
		LocationType string  `json:"location_type" minLength:"1" doc:"A location_type id (campus, building, ...)"`
		Parent       *string `json:"parent,omitempty" doc:"Parent location name; omit for a root location"`
	}
}

type updateLocationInput struct {
	Name string `path:"name"`
	Body struct {
		DisplayName  *string `json:"display_name,omitempty"`
		LocationType *string `json:"location_type,omitempty"`
	}
}

// registerLocationRoutes wires the location CRUD surface. Each route declares its
// capability with a.require (the fast-reject), and each handler resolves the
// caller's per-action scope and hands it to the gateway, which expands it to the
// row filter and writes audit. The capability is necessary, the scope decides.
func registerLocationRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, huma.Operation{
		OperationID: "list-locations",
		Method:      http.MethodGet,
		Path:        "/locations",
		Summary:     "List locations in scope",
		Description: "Lists the locations the caller may read, each filtered to its scope subtree. Gated by location:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("location", "read")},
	}, func(ctx context.Context, _ *struct{}) (*listLocationsOutput, error) {
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

	huma.Register(api, huma.Operation{
		OperationID: "list-location-types",
		Method:      http.MethodGet,
		Path:        "/types/location",
		Summary:     "List location types",
		Description: "Lists the location_type registry (the shape-definers a location is classified by), ordered alphabetically by display name. Populates the type picker on the location form. Gated by type:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("type", "read")},
	}, func(ctx context.Context, _ *struct{}) (*listLocationTypesOutput, error) {
		types, err := gw.ListLocationTypes(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list location types")
		}
		out := &listLocationTypesOutput{}
		out.Body.LocationTypes = make([]locationTypeBody, 0, len(types))
		for i := range types {
			out.Body.LocationTypes = append(out.Body.LocationTypes, locationTypeBody{
				ID: types[i].ID, DisplayName: types[i].DisplayName, Icon: types[i].Icon,
				AllowedParentTypes: types[i].AllowedParentTypes, Official: types[i].Official,
			})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-location-type",
		Method:        http.MethodPost,
		Path:          "/types/location",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a location type",
		Description:   "Creates a custom (non-official) location_type. Gated by type:create.",
		Middlewares:   huma.Middlewares{a.authn, a.require("type", "create")},
	}, func(ctx context.Context, in *createLocationTypeInput) (*locationTypeOutput, error) {
		lt, err := gw.CreateLocationType(ctx, actorID(ctx), storage.LocationType{
			ID: in.Body.ID, DisplayName: in.Body.DisplayName, Icon: in.Body.Icon,
			AllowedParentTypes: in.Body.AllowedParentTypes,
		})
		if err != nil {
			return nil, mapTypeErr(err, "location_type")
		}
		return &locationTypeOutput{Body: locationTypeBody{
			ID: lt.ID, DisplayName: lt.DisplayName, Icon: lt.Icon,
			AllowedParentTypes: lt.AllowedParentTypes, Official: lt.Official,
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-location-type",
		Method:      http.MethodPatch,
		Path:        "/types/location/{id}",
		Summary:     "Update a location type",
		Description: "Patches a custom location_type's display_name or icon. Official types are read-only (422). Gated by type:update.",
		Middlewares: huma.Middlewares{a.authn, a.require("type", "update")},
	}, func(ctx context.Context, in *updateLocationTypeInput) (*locationTypeOutput, error) {
		lt, err := gw.UpdateLocationType(ctx, actorID(ctx), in.ID, storage.LocationTypePatch{
			DisplayName: in.Body.DisplayName, Icon: in.Body.Icon,
			AllowedParentTypes: in.Body.AllowedParentTypes,
		})
		if err != nil {
			return nil, mapTypeErr(err, "location_type")
		}
		return &locationTypeOutput{Body: locationTypeBody{
			ID: lt.ID, DisplayName: lt.DisplayName, Icon: lt.Icon,
			AllowedParentTypes: lt.AllowedParentTypes, Official: lt.Official,
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-location-type",
		Method:        http.MethodDelete,
		Path:          "/types/location/{id}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a location type",
		Description:   "Deletes a custom location_type, refused if official (422) or still referenced by a location (409). Gated by type:delete.",
		Middlewares:   huma.Middlewares{a.authn, a.require("type", "delete")},
	}, func(ctx context.Context, in *locationTypePathInput) (*struct{}, error) {
		if err := gw.DeleteLocationType(ctx, actorID(ctx), in.ID); err != nil {
			return nil, mapTypeErr(err, "location_type")
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-location",
		Method:      http.MethodGet,
		Path:        "/locations/{name}",
		Summary:     "Get a location",
		Description: "Fetches a location by name within the caller's read scope. Out of scope is a non-disclosing 404. Gated by location:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("location", "read")},
	}, func(ctx context.Context, in *locationPathInput) (*locationOutput, error) {
		l, err := gw.GetLocation(ctx, in.Name, a.scopeFor(ctx, "location", "read"))
		if err != nil {
			return nil, mapLocationErr(err)
		}
		return &locationOutput{Body: toLocationBody(l)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-location",
		Method:        http.MethodPost,
		Path:          "/locations",
		DefaultStatus: http.StatusCreated,
		Summary:       "Create a location",
		Description:   "Creates a location, optionally under a parent (a root needs an all-scoped grant). Gated by location:create.",
		Middlewares:   huma.Middlewares{a.authn, a.require("location", "create")},
	}, func(ctx context.Context, in *createLocationInput) (*locationOutput, error) {
		l, err := gw.CreateLocation(ctx, actorID(ctx), storage.LocationSpec{
			Name:         in.Body.Name,
			DisplayName:  in.Body.DisplayName,
			LocationType: in.Body.LocationType,
			ParentName:   in.Body.Parent,
		}, a.scopeFor(ctx, "location", "create"))
		if err != nil {
			return nil, mapLocationErr(err)
		}
		return &locationOutput{Body: toLocationBody(l)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-location",
		Method:      http.MethodPatch,
		Path:        "/locations/{name}",
		Summary:     "Update a location",
		Description: "Patches a location's display_name or location_type. Gated by location:update; the read and update scopes drive the 404 versus 403 split.",
		Middlewares: huma.Middlewares{a.authn, a.require("location", "update")},
	}, func(ctx context.Context, in *updateLocationInput) (*locationOutput, error) {
		l, err := gw.UpdateLocation(ctx, actorID(ctx), in.Name, storage.LocationPatch{
			DisplayName:  in.Body.DisplayName,
			LocationType: in.Body.LocationType,
		}, a.scopeFor(ctx, "location", "read"), a.scopeFor(ctx, "location", "update"))
		if err != nil {
			return nil, mapLocationErr(err)
		}
		return &locationOutput{Body: toLocationBody(l)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-location",
		Method:        http.MethodDelete,
		Path:          "/locations/{name}",
		DefaultStatus: http.StatusNoContent,
		Summary:       "Delete a location",
		Description:   "Deletes a location, refused while it still has child locations. Gated by location:delete; read and delete scopes drive the 404 versus 403 split.",
		Middlewares:   huma.Middlewares{a.authn, a.require("location", "delete")},
	}, func(ctx context.Context, in *locationPathInput) (*struct{}, error) {
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

// mapLocationErr translates the gateway's location sentinels into HTTP status:
// the non-disclosing 404, the readable-not-actionable 403, occupancy and
// name-clash 409, and the request faults 422.
func mapLocationErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrLocationNotFound):
		return huma.Error404NotFound("location not found")
	case errors.Is(err, storage.ErrLocationForbidden):
		return huma.Error403Forbidden("forbidden")
	case errors.Is(err, storage.ErrLocationOccupied):
		return huma.Error409Conflict("location has child locations")
	case errors.Is(err, storage.ErrLocationExists):
		return huma.Error409Conflict("location name already exists")
	case errors.Is(err, storage.ErrParentNotFound):
		return huma.Error422UnprocessableEntity("parent location not found")
	case errors.Is(err, storage.ErrUnknownType):
		return huma.Error422UnprocessableEntity("unknown location_type")
	default:
		return huma.Error500InternalServerError("location operation failed")
	}
}
