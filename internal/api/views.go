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
// the browser would be the wrong shape. The estate canvas is the first case. It
// paints one square per component across the whole estate, and fetching an
// estate-sized component list to do that is a page that never finishes loading,
// so the projection carries a dot (an id, a verdict, two flags) and nothing an
// operator can only see after clicking.
//
// A view is a READ and only a read. It composes the Storage Gateway's scoped
// reads and adds no write path, no cache, and no judgement of its own: the
// verdicts it serves are the ones the health rollup already recorded, so the
// canvas and the detail page cannot land on different answers about a room.

type estateDotBody struct {
	Component string `json:"component" doc:"The component's uuid: what the canvas navigates to when a dot is clicked"`
	Name      string `json:"name" doc:"The component's name, for the dot's hover title"`
	Verdict   string `json:"verdict" doc:"healthy, incomplete, degraded, or outage: the component's own verdict, which is the only thing that colours the dot"`
	Primary   bool   `json:"primary" doc:"True in the component's primary system, the one cluster that draws it solid. A shared component is a ghost outline everywhere else, so the estate never counts one physical device twice"`
	Shared    bool   `json:"shared" doc:"True when the component belongs to more than one system, which is what earns it a ring"`
}

type estateSystemBody struct {
	ID       string          `json:"id" doc:"The system's uuid, the address the canvas navigates by"`
	Name     string          `json:"name"`
	Label    string          `json:"label"`
	Location string          `json:"location,omitempty" doc:"The uuid of the location this system is placed at; absent when it is placed nowhere"`
	Verdict  string          `json:"verdict" doc:"The system's recorded verdict, the same one its health read serves"`
	Dots     []estateDotBody `json:"dots" doc:"One entry per component in this system; empty when the caller may read the system but not its components"`
}

type estateLocationBody struct {
	ID             string `json:"id" doc:"The location's uuid, the address the canvas navigates by"`
	Name           string `json:"name"`
	Label          string `json:"label"`
	LocationType   string `json:"location_type" doc:"The type's name, which the band renders as its type chip"`
	LocationTypeID string `json:"location_type_id" doc:"The type's uuid, the stable handle beside its renameable name"`
	Parent         string `json:"parent,omitempty" doc:"The uuid of this location's parent; absent on a root. The tree is flat here and assembled by the client"`
	Verdict        string `json:"verdict" doc:"The location's recorded verdict, worst-wins over the systems beneath it"`
}

type estateViewOutput struct {
	Body struct {
		Locations []estateLocationBody `json:"locations" doc:"Every location the caller may read, flat: each carries its parent, and the client builds the tree. Flat rather than nested so the estate can be gathered into bands by something other than place without a second read"`
		Systems   []estateSystemBody   `json:"systems" doc:"Every system the caller may read, each carrying its dots"`
	}
}

func toEstateViewOutput(v *storage.EstateView) *estateViewOutput {
	out := &estateViewOutput{}
	out.Body.Locations = make([]estateLocationBody, 0, len(v.Locations))
	for i := range v.Locations {
		l := &v.Locations[i]
		body := estateLocationBody{
			ID: l.ID, Name: l.Name, Label: l.Label,
			LocationType: l.LocationType, LocationTypeID: l.LocationTypeID, Verdict: l.Verdict,
		}
		if l.ParentID != nil {
			body.Parent = *l.ParentID
		}
		out.Body.Locations = append(out.Body.Locations, body)
	}
	out.Body.Systems = make([]estateSystemBody, 0, len(v.Systems))
	for i := range v.Systems {
		s := &v.Systems[i]
		body := estateSystemBody{
			ID: s.ID, Name: s.Name, Label: s.Label,
			Verdict: s.Verdict, Dots: make([]estateDotBody, 0, len(s.Dots)),
		}
		if s.LocationID != nil {
			body.Location = *s.LocationID
		}
		for _, d := range s.Dots {
			body.Dots = append(body.Dots, estateDotBody{
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
		OperationID: "get-estate-view",
		Method:      http.MethodGet,
		Path:        "/views/estate",
		Summary:     "Read the estate as the canvas draws it",
		Description: "The whole in-scope estate in one read: every location flat with its parent and verdict, every system with its location and verdict, and one dot per component in each system. A dot carries an id, a verdict, and the primary/shared flags, never a component row: the canvas paints a square per component across the estate, and an estate-sized component list to draw squares is the cost this read exists to avoid. Each tier is scoped on its own read permission, so a principal who may read the place tree but not its components gets the shape of their estate with no contents, and one with no estate scope at all gets an empty canvas rather than a refusal. Gated by location:read.",
	}, "location", "read"), func(ctx context.Context, _ *struct{}) (*estateViewOutput, error) {
		// Three scopes, three permissions. Resolving them separately is what
		// makes the tiers independent: the canvas is a location-rooted surface,
		// so location:read is the door, but a caller's reach into systems and
		// components is their own and is injected into each query rather than
		// inherited from the door they came through.
		v, err := gw.EstateProjection(ctx,
			a.scopeFor(ctx, "location", "read"),
			a.scopeFor(ctx, "system", "read"),
			a.scopeFor(ctx, "component", "read"),
		)
		if err != nil {
			return nil, huma.Error500InternalServerError("read estate view")
		}
		return toEstateViewOutput(v), nil
	})
}
