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
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	DisplayName  string  `json:"display_name,omitempty"`
	LocationType string  `json:"location_type"`
	ParentID     *string `json:"parent_id,omitempty"`
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

// locationTypeBody is the wire shape of a location_type registry row: the stable
// id a location is classified by, its display_name, the rank that orders the
// registry, and whether it ships with the binary.
type locationTypeBody struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Rank        int    `json:"rank"`
	Official    bool   `json:"official"`
}

type listLocationTypesOutput struct {
	Body struct {
		LocationTypes []locationTypeBody `json:"location_types"`
	}
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
		out := &listLocationsOutput{}
		out.Body.Locations = make([]locationBody, 0, len(locs))
		for i := range locs {
			out.Body.Locations = append(out.Body.Locations, toLocationBody(&locs[i]))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-location-types",
		Method:      http.MethodGet,
		Path:        "/location-types",
		Summary:     "List location types",
		Description: "Lists the location_type registry (the shape-definers a location is classified by), ordered by rank. Populates the type picker on the location form. Gated by location:read.",
		Middlewares: huma.Middlewares{a.authn, a.require("location", "read")},
	}, func(ctx context.Context, _ *struct{}) (*listLocationTypesOutput, error) {
		types, err := gw.ListLocationTypes(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("list location types")
		}
		out := &listLocationTypesOutput{}
		out.Body.LocationTypes = make([]locationTypeBody, 0, len(types))
		for i := range types {
			out.Body.LocationTypes = append(out.Body.LocationTypes, locationTypeBody{
				ID: types[i].ID, DisplayName: types[i].DisplayName, Rank: types[i].Rank, Official: types[i].Official,
			})
		}
		return out, nil
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
