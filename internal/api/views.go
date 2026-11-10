package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The views tier: reads shaped for one operator surface rather than for one
// entity.
//
// Every other read here answers "what is this row"; a view answers "what does
// this screen need", and it exists precisely when composing the entity reads in
// the browser would be the wrong shape. The fleet renderers are the first case. It
// paints one square per component across the whole fleet, and fetching an
// fleet-sized component list to do that is a page that never finishes loading,
// so the projection carries a dot (an id, a verdict, two flags) and nothing an
// operator can only see after clicking.
//
// A view is a READ and only a read. It composes the Storage Gateway's scoped
// reads and adds no write path, no cache, and no judgement of its own: the
// verdicts it serves are the ones the health rollup already recorded, so the
// a renderer and the detail page cannot land on different answers about a room.

type fleetDotBody struct {
	Component string `json:"component" doc:"The component's uuid: what a renderer navigates to when a dot is clicked"`
	Name      string `json:"name" doc:"The component's name, for the dot's hover title"`
	Verdict   string `json:"verdict" doc:"healthy, incomplete, degraded, or outage: the component's own verdict, which is the only thing that colours the dot"`
	Primary   bool   `json:"primary" doc:"True in the component's primary system, the one cluster that draws it solid. A shared component is a ghost outline everywhere else, so the fleet never counts one physical device twice"`
	Shared    bool   `json:"shared" doc:"True when the component belongs to more than one system, which is what earns it a ring"`
}

type fleetSystemBody struct {
	ID       string         `json:"id" doc:"The system's uuid, the address a renderer navigates by"`
	Name     string         `json:"name"`
	Label    string         `json:"label"`
	Location string         `json:"location,omitempty" doc:"The uuid of the location this system is placed at; absent when it is placed nowhere"`
	Verdict  string         `json:"verdict" doc:"The system's recorded verdict, the same one its health read serves"`
	Dots     []fleetDotBody `json:"dots" doc:"One entry per component in this system; empty when the caller may read the system but not its components"`
}

type fleetLocationBody struct {
	ID             string `json:"id" doc:"The location's uuid, the address a renderer navigates by"`
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

type platformSystemBody struct {
	ID      string `json:"id" doc:"The platform system's uuid: the page's verdict links to its system detail, where the verdict is explained"`
	Name    string `json:"name"`
	Verdict string `json:"verdict" doc:"The platform system's recorded verdict, the same one its health read serves"`
}

type platformServerBody struct {
	Component string              `json:"component" doc:"The server's platform component uuid"`
	Name      string              `json:"name" doc:"The server's platform component name"`
	Version   string              `json:"version,omitempty" doc:"The build the server runs, as its boot wrote it (server-version); absent before a boot has written it"`
	StartedAt *time.Time          `json:"started_at,omitempty" doc:"When the server's current boot started (server-started-at); uptime is read_at minus this"`
	Verdict   string              `json:"verdict" doc:"The server component's recorded verdict"`
	Groups    []platformPanelBody `json:"groups" doc:"The server's panels (Requests, Ingest, Queues, Retention), in order: the page renders these and has no key list of its own"`
	Alarms    []platformAlarmBody `json:"alarms" doc:"The open alarms on the server's platform component, newest first; a panel is flagged when one names a key it carries"`
}

type platformPanelKeyBody struct {
	Name        string `json:"name" doc:"The metric type's name: the key the panel's figures carry"`
	Label       string `json:"label" doc:"The metric type's label, the row's title"`
	Unit        string `json:"unit,omitempty" doc:"The metric type's unit, when it has one"`
	Description string `json:"description,omitempty" doc:"The metric type's catalog description, the row's tooltip"`
	Of          string `json:"of,omitempty" doc:"The key this one is read against, printed as a pair (connections in use of the pool's size); absent for a key read alone"`
	Rank        int    `json:"rank,omitempty" doc:"Above zero, the key is a ranked list: only its rank highest instances of the newest window are returned, highest first"`
}

type platformInstanceFigureBody struct {
	Key        string    `json:"key" doc:"The key the figure is a sample of"`
	Instance   string    `json:"instance" doc:"The series instance (a route and status class, a lane, a table, a reason); empty for an unsplit key"`
	Value      float64   `json:"value"`
	ObservedAt time.Time `json:"observed_at" doc:"When the sample was taken"`
}

type platformPanelBody struct {
	Name    string                       `json:"name" doc:"The panel's stable handle"`
	Label   string                       `json:"label" doc:"The panel's title"`
	Keys    []platformPanelKeyBody       `json:"keys" doc:"The panel's keys, in order, each with its catalog facts"`
	Figures []platformInstanceFigureBody `json:"figures" doc:"The latest sample per key and instance, in key order: a ranked key's newest window highest first, cut to its rank; any other key by instance. A key never sampled has no figure"`
}

type platformAlarmBody struct {
	Rule     string    `json:"rule,omitempty" doc:"The name of the rule that raised the alarm; absent for a hand-raised alarm"`
	Key      string    `json:"key,omitempty" doc:"The key the raising rule samples, which names the panel the alarm flags; absent for a hand-raised alarm"`
	Severity string    `json:"severity"`
	Message  string    `json:"message"`
	RaisedAt time.Time `json:"raised_at"`
}

type platformKeyBody struct {
	Name        string `json:"name" doc:"The metric type's name: the key a node's figures are indexed by"`
	Label       string `json:"label" doc:"The metric type's label, the column header"`
	Unit        string `json:"unit,omitempty" doc:"The metric type's unit, when it has one"`
	Description string `json:"description,omitempty" doc:"The metric type's catalog description, the column's tooltip"`
}

type platformGroupBody struct {
	Name  string            `json:"name" doc:"The group's stable handle"`
	Label string            `json:"label" doc:"The group's title over its columns"`
	Keys  []platformKeyBody `json:"keys" doc:"The group's columns, in order, each with its catalog facts"`
}

type platformFigureBody struct {
	Value      float64   `json:"value"`
	ObservedAt time.Time `json:"observed_at" doc:"When the sample was taken"`
}

type platformNodeBody struct {
	Name            string                        `json:"name"`
	Label           string                        `json:"label"`
	Enrolled        bool                          `json:"enrolled"`
	LastHeartbeatAt *time.Time                    `json:"last_heartbeat_at,omitempty" doc:"The node's last heartbeat; absent when it has never sent one"`
	Component       string                        `json:"component,omitempty" doc:"The node's platform component uuid, where every figure has its history; absent for a node the boot has not yet adopted"`
	Verdict         string                        `json:"verdict,omitempty" doc:"The node's platform component verdict, which tones the row; absent without a fleet-wide component read"`
	Figures         map[string]platformFigureBody `json:"figures,omitempty" doc:"The current figure per key, by key name: a gauge's latest sample, a window count only while its window is the newest. A key the node has never reported, or whose last window has passed, is absent, never zero; the whole map is absent without a fleet-wide component read"`
}

type platformViewOutput struct {
	Body struct {
		ReadAt      time.Time            `json:"read_at" doc:"The server's clock when the view was read: uptime and the liveness pill are both judged against it, so they read one clock"`
		Lab         bool                 `json:"lab" doc:"True when this server runs as an omniglass lab"`
		FiguresRead bool                 `json:"figures_read" doc:"True when the caller's component read reached the platform components. False means the figures, verdicts and servers are left out because the caller may not read them, not because nothing was reported"`
		System      *platformSystemBody  `json:"system,omitempty" doc:"The platform system and its verdict; absent without a fleet-wide system read"`
		Servers     []platformServerBody `json:"servers" doc:"Every server's platform component, the identity its boot wrote and its panels; empty without a fleet-wide component read"`
		Groups      []platformGroupBody  `json:"groups" doc:"The node table's column groups, in order: the page renders these and has no key list of its own"`
		Nodes       []platformNodeBody   `json:"nodes" doc:"One row per node, in name order"`
	}
}

func toPlatformViewOutput(v *storage.PlatformView, readAt time.Time, lab bool) *platformViewOutput {
	out := &platformViewOutput{}
	out.Body.ReadAt = readAt
	out.Body.Lab = lab
	out.Body.FiguresRead = v.FiguresRead
	if v.System != nil {
		out.Body.System = &platformSystemBody{ID: v.System.ID, Name: v.System.Name, Verdict: v.System.Verdict}
	}
	out.Body.Servers = make([]platformServerBody, 0, len(v.Servers))
	for _, s := range v.Servers {
		body := platformServerBody{Component: s.ComponentID, Name: s.Name, StartedAt: s.StartedAt, Verdict: s.Verdict}
		if s.Version != nil {
			body.Version = *s.Version
		}
		body.Groups = make([]platformPanelBody, 0, len(s.Groups))
		for _, g := range s.Groups {
			panel := platformPanelBody{Name: g.Name, Label: g.Label,
				Keys:    make([]platformPanelKeyBody, 0, len(g.Keys)),
				Figures: make([]platformInstanceFigureBody, 0, len(g.Figures))}
			for _, k := range g.Keys {
				panel.Keys = append(panel.Keys, platformPanelKeyBody{Name: k.Name, Label: k.Label, Unit: k.Unit,
					Description: k.Description, Of: k.Of, Rank: k.Rank})
			}
			for _, f := range g.Figures {
				panel.Figures = append(panel.Figures, platformInstanceFigureBody{Key: f.Key, Instance: f.Instance, Value: f.Value, ObservedAt: f.ObservedAt})
			}
			body.Groups = append(body.Groups, panel)
		}
		body.Alarms = make([]platformAlarmBody, 0, len(s.Alarms))
		for _, a := range s.Alarms {
			ab := platformAlarmBody{Rule: a.Rule, Severity: a.Severity, Message: a.Message, RaisedAt: a.RaisedAt}
			if a.Key != nil {
				ab.Key = *a.Key
			}
			body.Alarms = append(body.Alarms, ab)
		}
		out.Body.Servers = append(out.Body.Servers, body)
	}
	keys := map[string]platformKeyBody{}
	for _, k := range v.Keys {
		keys[k.Name] = platformKeyBody{Name: k.Name, Label: k.Label, Unit: k.Unit, Description: k.Description}
	}
	out.Body.Groups = make([]platformGroupBody, 0, len(v.Groups))
	for _, g := range v.Groups {
		body := platformGroupBody{Name: g.Name, Label: g.Label, Keys: make([]platformKeyBody, 0, len(g.Keys))}
		for _, k := range g.Keys {
			if kb, ok := keys[k]; ok {
				body.Keys = append(body.Keys, kb)
			}
		}
		out.Body.Groups = append(out.Body.Groups, body)
	}
	out.Body.Nodes = make([]platformNodeBody, 0, len(v.Nodes))
	for _, n := range v.Nodes {
		body := platformNodeBody{Name: n.Name, Label: n.Label, Enrolled: n.Enrolled, LastHeartbeatAt: n.LastHeartbeatAt}
		if n.ComponentID != nil {
			body.Component = *n.ComponentID
		}
		if n.Verdict != nil {
			body.Verdict = *n.Verdict
		}
		if n.Figures != nil {
			body.Figures = make(map[string]platformFigureBody, len(n.Figures))
			for k, f := range n.Figures {
				body.Figures[k] = platformFigureBody{Value: f.Value, ObservedAt: f.ObservedAt}
			}
		}
		out.Body.Nodes = append(out.Body.Nodes, body)
	}
	return out
}

// registerViewRoutes wires the surface-shaped reads.
func registerViewRoutes(api huma.API, a *authenticator, gw storage.Gateway, log *slog.Logger) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-fleet-view",
		Method:      http.MethodGet,
		Path:        "/views/fleet",
		Summary:     "Read the whole in-scope fleet in one call",
		Description: "Returns every in-scope location (flat, with parent and verdict), every in-scope system (with location and verdict), and one dot per component in each system. A dot carries the component id, its verdict, and the primary/shared flags, not a full component row. Each tier is scoped on its own read permission: a caller who can read locations but not components gets locations with empty systems, and a caller with no scope gets an empty result, not an error. Gated by location:read.",
	}, "location", "read"), func(ctx context.Context, _ *struct{}) (*fleetViewOutput, error) {
		// Three scopes, three permissions. Resolving them separately is what
		// makes the tiers independent: Explore is a location-rooted surface,
		// so location:read is the door, but a caller's reach into systems and
		// components is their own and is injected into each query rather than
		// inherited from the door they came through.
		v, err := gw.FleetProjection(ctx,
			a.scopeFor(ctx, "location", "read"),
			a.scopeFor(ctx, "system", "read"),
			a.scopeFor(ctx, "component", "read"),
		)
		if err != nil {
			// The answer stays opaque to the caller; the cause goes to the
			// server's log, where an operator can find it (#1627).
			log.ErrorContext(ctx, "read fleet view", "error", err)
			return nil, huma.Error500InternalServerError("read fleet view")
		}
		return toFleetViewOutput(v), nil
	})
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-platform-view",
		Method:      http.MethodGet,
		Path:        "/views/platform",
		Summary:     "Read Omniglass's own health in one call",
		Description: "Returns the read behind the Platform health page: the server clock (read_at), whether this is a lab, the platform system and its verdict, each server's running build, start time and panels (Requests, Ingest, Queues and Retention: each key's catalog facts and its latest sample per instance, a ranked key cut to the ten highest of its newest window in the database, and the open alarms on the server component with the key each one's rule samples), the node table's column groups with each key's catalog facts, and one row per node with its last heartbeat and its platform component's current figure per key (a gauge's latest sample, a window count only while its window is the newest). The rows need a fleet-wide node:read, so a location-scoped holder is refused rather than shown an empty table. The platform components are unplaced, so the figures, verdicts and servers (with their panels) need a fleet-wide component:read and the system a fleet-wide system:read; short of that each is left out, never zeroed. Gated by node:read.",
	}, "node", "read"), func(ctx context.Context, _ *struct{}) (*platformViewOutput, error) {
		v, err := gw.PlatformView(ctx,
			a.scopeFor(ctx, "node", "read"),
			a.scopeFor(ctx, "system", "read"),
			a.scopeFor(ctx, "component", "read"),
		)
		if errors.Is(err, storage.ErrNodeForbidden) {
			return nil, huma.Error403Forbidden("forbidden")
		}
		if err != nil {
			log.ErrorContext(ctx, "read platform view", "error", err)
			return nil, huma.Error500InternalServerError("read platform view")
		}
		return toPlatformViewOutput(v, time.Now().UTC(), a.lab), nil
	})
}
