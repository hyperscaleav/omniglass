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
	Component string     `json:"component" doc:"The server's platform component uuid"`
	Name      string     `json:"name" doc:"The server's platform component name"`
	Version   string     `json:"version,omitempty" doc:"The build the server runs, as its boot wrote it (server-version); absent before a boot has written it"`
	StartedAt *time.Time `json:"started_at,omitempty" doc:"When the server's current boot started (server-started-at); uptime is read_at minus this"`
	Verdict   string     `json:"verdict" doc:"The server component's recorded verdict"`
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
	Loss            *float64                      `json:"loss,omitempty" doc:"The share of what the node's own hops (loss_hops) took in over the last hour that they lost, every kind summed per hop and composed across them as the pipeline's end to end is; 0 is healthy. Absent when nothing went in on any of them, or without a fleet-wide component read"`
}

type platformPipelineFigureBody struct {
	In   uint64   `json:"in" doc:"Items the hop took in over the span (delivery-in-count summed)"`
	Out  uint64   `json:"out" doc:"Items the hop handed on over the span (delivery-out-count summed)"`
	Loss *float64 `json:"loss,omitempty" doc:"(in - out) / in over the span's summed counts, never an average of ratios; 0 is healthy. Absent when nothing went in"`
}

type platformPipelineSpanBody struct {
	Newest   platformPipelineFigureBody `json:"newest" doc:"Each component's newest report window while it is current (within two report windows), summed; a component that stopped reporting adds nothing here, only to the last hour"`
	NewestAt *time.Time                 `json:"newest_at,omitempty" doc:"When the newest window was written, current or not"`
	LastHour platformPipelineFigureBody `json:"last_hour" doc:"Every window over the last hour, summed"`
}

type platformPipelineKindBody struct {
	Kind     string                     `json:"kind" enum:"metric,property,event,log" doc:"The kind of item, the series instance"`
	Newest   platformPipelineFigureBody `json:"newest" doc:"Each component's newest report window while it is current (within two report windows), summed; a component that stopped reporting adds nothing here, only to the last hour"`
	NewestAt *time.Time                 `json:"newest_at,omitempty" doc:"When the newest window was written, current or not"`
	LastHour platformPipelineFigureBody `json:"last_hour" doc:"Every window over the last hour, summed"`
}

type platformPipelineComponentBody struct {
	Component string                     `json:"component" doc:"The platform component that recorded this share of the hop, where its series live"`
	Name      string                     `json:"name" doc:"The component's name"`
	Kinds     []platformPipelineKindBody `json:"kinds" doc:"The component's figures per kind it carried"`
	Total     platformPipelineSpanBody   `json:"total" doc:"The component's figures, every kind summed"`
}

type platformPipelineHopBody struct {
	ID             string                          `json:"id" enum:"schedule,sim,node,ingest" doc:"The hop's name, as the delivery model names it"`
	Label          string                          `json:"label" doc:"The hop's title"`
	Description    string                          `json:"description" doc:"What the hop does and what counts as out and as lost there"`
	ComponentClass string                          `json:"component_class" enum:"node,server" doc:"Which platform components record the hop"`
	Keys           []string                        `json:"keys" doc:"The keys the hop's figures are read from, the same three on every hop"`
	Kinds          []platformPipelineKindBody      `json:"kinds" doc:"The hop's figures per kind it carried, across its components; a kind it carried nothing of is absent"`
	Total          platformPipelineSpanBody        `json:"total" doc:"The hop's figures, every kind summed"`
	ByComponent    []platformPipelineComponentBody `json:"by_component" doc:"Each component's share of the hop, in name order; the shares sum to the hop's figures"`
}

type platformPipelineEndBody struct {
	Newest   *float64 `json:"newest,omitempty" doc:"The current newest windows' end to end loss"`
	LastHour *float64 `json:"last_hour,omitempty" doc:"The last hour's end to end loss"`
}

type platformPipelineEndKindBody struct {
	Kind     string   `json:"kind" enum:"metric,property,event,log" doc:"The kind of item"`
	Newest   *float64 `json:"newest,omitempty" doc:"The current newest windows' end to end loss"`
	LastHour *float64 `json:"last_hour,omitempty" doc:"The last hour's end to end loss"`
}

type platformPipelineBody struct {
	Keys          []platformKeyBody             `json:"keys" doc:"The delivery keys' catalog facts, the explainer the page shows"`
	Hops          []platformPipelineHopBody     `json:"hops" doc:"The hops recorded today, in the order an item crosses them: the page draws these and has no hop list of its own"`
	EndToEnd      []platformPipelineEndKindBody `json:"end_to_end" doc:"Per kind any hop carried, the share lost anywhere on the path: 1 - the product of every hop's survival (1 - loss), over the hops with a loss. Absent where no hop had one"`
	EndToEndTotal platformPipelineEndBody       `json:"end_to_end_total" doc:"The same over every kind summed per hop"`
}

type platformViewOutput struct {
	Body struct {
		ReadAt      time.Time             `json:"read_at" doc:"The server's clock when the view was read: uptime and the liveness pill are both judged against it, so they read one clock"`
		Lab         bool                  `json:"lab" doc:"True when this server runs as an omniglass lab"`
		FiguresRead bool                  `json:"figures_read" doc:"True when the caller's component read reached the platform components. False means the figures, verdicts and servers are left out because the caller may not read them, not because nothing was reported"`
		System      *platformSystemBody   `json:"system,omitempty" doc:"The platform system and its verdict; absent without a fleet-wide system read"`
		Servers     []platformServerBody  `json:"servers" doc:"Every server's platform component and the identity its boot wrote; empty without a fleet-wide component read"`
		Groups      []platformGroupBody   `json:"groups" doc:"The node table's column groups, in order: the page renders these and has no key list of its own"`
		Nodes       []platformNodeBody    `json:"nodes" doc:"One row per node, in name order"`
		LossHops    []string              `json:"loss_hops" doc:"The hops a node's loss composes: the ones its own platform component records"`
		Pipeline    *platformPipelineBody `json:"pipeline,omitempty" doc:"Where work is lost: each hop's in, out and loss per kind, newest window and last hour, and the end to end. Absent without a fleet-wide component read"`
	}
}

func toPlatformPipelineFigure(f storage.PlatformPipelineFigure) platformPipelineFigureBody {
	return platformPipelineFigureBody{In: f.In, Out: f.Out, Loss: f.Loss}
}

func toPlatformPipelineSpan(c storage.PlatformPipelineCell) platformPipelineSpanBody {
	return platformPipelineSpanBody{Newest: toPlatformPipelineFigure(c.Newest), NewestAt: c.NewestAt, LastHour: toPlatformPipelineFigure(c.LastHour)}
}

func toPlatformPipelineKinds(cells []storage.PlatformPipelineCell) []platformPipelineKindBody {
	out := make([]platformPipelineKindBody, 0, len(cells))
	for _, c := range cells {
		out = append(out, platformPipelineKindBody{
			Kind: string(c.Kind), Newest: toPlatformPipelineFigure(c.Newest), NewestAt: c.NewestAt, LastHour: toPlatformPipelineFigure(c.LastHour),
		})
	}
	return out
}

func toPlatformPipeline(p *storage.PlatformPipeline) *platformPipelineBody {
	out := &platformPipelineBody{
		Keys:          make([]platformKeyBody, 0, len(p.Keys)),
		Hops:          make([]platformPipelineHopBody, 0, len(p.Hops)),
		EndToEnd:      make([]platformPipelineEndKindBody, 0, len(p.EndToEnd)),
		EndToEndTotal: platformPipelineEndBody{Newest: p.EndToEndTotal.Newest, LastHour: p.EndToEndTotal.LastHour},
	}
	for _, k := range p.Keys {
		out.Keys = append(out.Keys, platformKeyBody{Name: k.Name, Label: k.Label, Unit: k.Unit, Description: k.Description})
	}
	for _, h := range p.Hops {
		body := platformPipelineHopBody{
			ID: string(h.Hop.ID), Label: h.Hop.Label, Description: h.Hop.Description, ComponentClass: h.Hop.ComponentClass,
			Keys: h.Hop.Keys(), Kinds: toPlatformPipelineKinds(h.Kinds), Total: toPlatformPipelineSpan(h.Total),
			ByComponent: make([]platformPipelineComponentBody, 0, len(h.ByComponent)),
		}
		for _, c := range h.ByComponent {
			body.ByComponent = append(body.ByComponent, platformPipelineComponentBody{
				Component: c.ComponentID, Name: c.Name, Kinds: toPlatformPipelineKinds(c.Kinds), Total: toPlatformPipelineSpan(c.Total),
			})
		}
		out.Hops = append(out.Hops, body)
	}
	for _, e := range p.EndToEnd {
		out.EndToEnd = append(out.EndToEnd, platformPipelineEndKindBody{Kind: string(e.Kind), Newest: e.Newest, LastHour: e.LastHour})
	}
	return out
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
	out.Body.LossHops = make([]string, 0, len(v.LossHops))
	for _, h := range v.LossHops {
		out.Body.LossHops = append(out.Body.LossHops, string(h))
	}
	if v.Pipeline != nil {
		out.Body.Pipeline = toPlatformPipeline(v.Pipeline)
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
		body.Loss = n.Loss
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
		Description: "Returns the read behind the Platform health page: the server clock (read_at), whether this is a lab, the platform system and its verdict, each server's running build and start time, the node table's column groups with each key's catalog facts, and one row per node with its last heartbeat, its platform component's current figure per key (a gauge's latest sample, a window count only while its window is the newest) and its loss over its own hops, and the pipeline: each hop an item crosses, in order, with its in, out and loss per kind and per component, newest window and last hour, and the end to end. The rows need a fleet-wide node:read, so a location-scoped holder is refused rather than shown an empty table. The platform components are unplaced, so the figures, verdicts and servers need a fleet-wide component:read and the system a fleet-wide system:read; short of that each (the pipeline and the node loss with them) is left out, never zeroed. Gated by node:read.",
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
