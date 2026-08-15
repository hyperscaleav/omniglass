package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The views tier: reads shaped for one operator surface rather than for one
// entity.
//
// Every other read here answers "what is this row"; a view answers "what does
// this screen need", and it exists precisely when composing the entity reads in
// the browser would be the wrong shape. The fleet canvas is the first case. It
// paints one square per component across the whole fleet, and fetching an
// fleet-sized component list to do that is a page that never finishes loading,
// so the projection carries a dot (an id, a verdict, two flags) and nothing an
// operator can only see after clicking.
//
// A view is a READ and only a read. It composes the Storage Gateway's scoped
// reads and adds no write path, no cache, and no judgement of its own: the
// verdicts it serves are the ones the health rollup already recorded, so the
// canvas and the detail page cannot land on different answers about a room.

type fleetDotBody struct {
	Component string `json:"component" doc:"The component's uuid: what the canvas navigates to when a dot is clicked"`
	Name      string `json:"name" doc:"The component's name, for the dot's hover title"`
	Verdict   string `json:"verdict" doc:"healthy, incomplete, degraded, or outage: the component's own verdict, which is the only thing that colours the dot"`
	Primary   bool   `json:"primary" doc:"True in the component's primary system, the one cluster that draws it solid. A shared component is a ghost outline everywhere else, so the fleet never counts one physical device twice"`
	Shared    bool   `json:"shared" doc:"True when the component belongs to more than one system, which is what earns it a ring"`
}

type fleetSystemBody struct {
	ID       string         `json:"id" doc:"The system's uuid, the address the canvas navigates by"`
	Name     string         `json:"name"`
	Label    string         `json:"label"`
	Location string         `json:"location,omitempty" doc:"The uuid of the location this system is placed at; absent when it is placed nowhere"`
	Verdict  string         `json:"verdict" doc:"The system's recorded verdict, the same one its health read serves"`
	Dots     []fleetDotBody `json:"dots" doc:"One entry per component in this system; empty when the caller may read the system but not its components"`
}

type fleetLocationBody struct {
	ID             string `json:"id" doc:"The location's uuid, the address the canvas navigates by"`
	Name           string `json:"name"`
	Label          string `json:"label"`
	LocationType   string `json:"location_type" doc:"The type's name, which the band renders as its type chip"`
	LocationTypeID string `json:"location_type_id" doc:"The type's uuid, the stable handle beside its renameable name"`
	Parent         string `json:"parent,omitempty" doc:"The uuid of this location's parent; absent on a root. The tree is flat here and assembled by the client"`
	Verdict        string `json:"verdict" doc:"The location's recorded verdict, worst-wins over the systems beneath it"`
}

type fleetViewOutput struct {
	Body struct {
		Locations []fleetLocationBody `json:"locations" doc:"Every location the caller may read, flat: each carries its parent, and the client builds the tree. Flat rather than nested so the fleet can be gathered into bands by something other than place without a second read"`
		Systems   []fleetSystemBody   `json:"systems" doc:"Every system the caller may read, each carrying its dots"`
	}
}

func toFleetViewOutput(v *storage.FleetView) *fleetViewOutput {
	out := &fleetViewOutput{}
	out.Body.Locations = make([]fleetLocationBody, 0, len(v.Locations))
	for i := range v.Locations {
		l := &v.Locations[i]
		body := fleetLocationBody{
			ID: l.ID, Name: l.Name, Label: l.Label,
			LocationType: l.LocationType, LocationTypeID: l.LocationTypeID, Verdict: l.Verdict,
		}
		if l.ParentID != nil {
			body.Parent = *l.ParentID
		}
		out.Body.Locations = append(out.Body.Locations, body)
	}
	out.Body.Systems = make([]fleetSystemBody, 0, len(v.Systems))
	for i := range v.Systems {
		s := &v.Systems[i]
		body := fleetSystemBody{
			ID: s.ID, Name: s.Name, Label: s.Label,
			Verdict: s.Verdict, Dots: make([]fleetDotBody, 0, len(s.Dots)),
		}
		if s.LocationID != nil {
			body.Location = *s.LocationID
		}
		for _, d := range s.Dots {
			body.Dots = append(body.Dots, fleetDotBody{
				Component: d.ComponentID, Name: d.Name, Verdict: d.Verdict,
				Primary: d.Primary, Shared: d.Shared,
			})
		}
		out.Body.Systems = append(out.Body.Systems, body)
	}
	return out
}

// registerViewRoutes wires the surface-shaped reads.
func registerViewRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-fleet-view",
		Method:      http.MethodGet,
		Path:        "/views/fleet",
		Summary:     "Read the whole in-scope fleet in one call",
		Description: "Returns every in-scope location (flat, with parent and verdict), every in-scope system (with location and verdict), and one dot per component in each system. A dot carries the component id, its verdict, and the primary/shared flags, not a full component row. Each tier is scoped on its own read permission: a caller who can read locations but not components gets locations with empty systems, and a caller with no scope gets an empty result, not an error. Gated by location:read.",
	}, "location", "read"), func(ctx context.Context, _ *struct{}) (*fleetViewOutput, error) {
		// Three scopes, three permissions. Resolving them separately is what
		// makes the tiers independent: the canvas is a location-rooted surface,
		// so location:read is the door, but a caller's reach into systems and
		// components is their own and is injected into each query rather than
		// inherited from the door they came through.
		v, err := gw.FleetProjection(ctx,
			a.scopeFor(ctx, "location", "read"),
			a.scopeFor(ctx, "system", "read"),
			a.scopeFor(ctx, "component", "read"),
		)
		if err != nil {
			return nil, huma.Error500InternalServerError("read fleet view")
		}
		return toFleetViewOutput(v), nil
	})
}
