package api

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/simhub"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// SimStatusFunc reports the simulator beside a node (bus.Server.SimStatus).
type SimStatusFunc func(node string) (simhub.Status, bool)

// WithSimStatus supplies the server's view of the simulators beside its
// nodes. Without it every node reads as having no simulator.
func WithSimStatus(f SimStatusFunc) Option {
	return func(o *options) { o.simStatus = f }
}

// simConnectedWindow is how recently a simulator must have pulled to read as
// connected: comfortably more than its pull interval.
const simConnectedWindow = 90 * time.Second

type simDeviceBody struct {
	ComponentID    string            `json:"component_id"`
	Component      string            `json:"component" doc:"The component's name."`
	Product        string            `json:"product" doc:"The component's product; its name selects the simulator profile."`
	Address        string            `json:"address" doc:"The host the component's endpoints target."`
	Serving        bool              `json:"serving" doc:"Whether the simulator reported serving this component in its latest batch."`
	Status         string            `json:"status" enum:"running,starting,failed,offline" doc:"What the simulator is doing with the component, most urgent first: offline (its simulator stopped reporting), starting (added since the simulator's last pull, or handed over moments ago), failed (handed to the simulator but not started), running."`
	Hits           int               `json:"hits" doc:"Connections and requests the simulated device has answered since the server started."`
	HitsByProtocol map[string]int    `json:"hits_by_protocol"`
	LastHitAt      *time.Time        `json:"last_hit_at,omitempty"`
	LastProtocol   string            `json:"last_protocol,omitempty"`
	Impairment     *impairmentFields `json:"impairment,omitempty" doc:"What an operator has broken on the device; absent when healthy."`
	Impaired       bool              `json:"impaired" doc:"Whether the simulator reported applying an impairment to the device in its latest batch."`
}

type simulationOutput struct {
	Body struct {
		Connected    bool                           `json:"connected" doc:"A simulator beside this node pulled its device list recently."`
		LastSeen     *time.Time                     `json:"last_seen,omitempty"`
		Capabilities collection.SimCapabilities     `json:"capabilities"`
		Impairments  collection.SimImpairmentStatus `json:"impairments" doc:"Whether the simulator can apply impairments (and why not), its last apply error, and the devices it impairs."`
		Devices      []simDeviceBody                `json:"devices"`
	}
}

func registerSimulationRoutes(api huma.API, a *authenticator, gw storage.Gateway, status SimStatusFunc) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-node-simulation",
		Method:      http.MethodGet,
		Path:        "/nodes/{name}/simulation",
		Summary:     "Get a node's simulator",
		Description: "The devices a simulator beside the node serves (each component with an endpoint placed on the node, within the caller's component read scope), joined to the hits the server has seen and the simulator's capabilities. Hits are live and per server process, not stored. Requires an all-scope node read. Gated by node:read.",
	}, "node", "read"), func(ctx context.Context, in *nodePathInput) (*simulationOutput, error) {
		if _, err := gw.GetNode(ctx, in.Name, a.scopeFor(ctx, "node", "read")); err != nil {
			return nil, mapNodeErr(err)
		}
		devices, err := gw.NodeSimDevices(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, huma.Error500InternalServerError("get node simulation")
		}
		var st simhub.Status
		var seen bool
		if status != nil {
			st, seen = status(in.Name)
		}
		return simulationBody(devices, st, seen, time.Now()), nil
	})
}

// simulationBody joins the node's candidate devices to its simulator's
// status. Pure, so the join is unit-testable.
func simulationBody(devices []storage.SimDevice, st simhub.Status, seen bool, now time.Time) *simulationOutput {
	out := &simulationOutput{}
	if seen {
		ls := st.LastSeen
		out.Body.LastSeen = &ls
		out.Body.Connected = now.Sub(st.LastSeen) <= simConnectedWindow
		out.Body.Capabilities = st.Capabilities
		out.Body.Impairments = st.Impairments
	}
	// The simulator's impaired list covers its whole node: keep only the
	// devices in this caller's view.
	visible := make(map[string]bool, len(devices))
	for _, d := range devices {
		visible[d.ComponentID] = true
	}
	impaired := map[string]bool{}
	out.Body.Impairments.Impaired = nil
	for _, id := range st.Impairments.Impaired {
		if visible[id] {
			impaired[id] = true
			out.Body.Impairments.Impaired = append(out.Body.Impairments.Impaired, id)
		}
	}
	out.Body.Devices = make([]simDeviceBody, 0, len(devices))
	for _, d := range devices {
		b := simDeviceBody{ComponentID: d.ComponentID, Component: d.Name, Product: d.Product, Address: d.Address,
			Serving: st.Serving[d.ComponentID], HitsByProtocol: map[string]int{}, Impaired: impaired[d.ComponentID],
			Status: simState(out.Body.Connected, st.Listed[d.ComponentID], st.Serving[d.ComponentID], now.Sub(st.ListedSince[d.ComponentID]))}
		if d.Impairment != nil {
			f := impairmentFields(*d.Impairment)
			b.Impairment = &f
		}
		if h, ok := st.Devices[d.ComponentID]; ok {
			b.Hits = h.Count
			for k, v := range h.ByProtocol {
				b.HitsByProtocol[k] = v
			}
			last := h.LastAt
			b.LastHitAt, b.LastProtocol = &last, h.LastProtocol
		}
		out.Body.Devices = append(out.Body.Devices, b)
	}
	sort.SliceStable(out.Body.Devices, func(i, j int) bool { return out.Body.Devices[i].Component < out.Body.Devices[j].Component })
	return out
}

// simStartGrace is how long a component handed to the simulator may go
// unconfirmed before it reads as failed: the simulator starts it and reports
// it within about a second of its pull.
const simStartGrace = 5 * time.Second

// simState is a simulated component's status, most urgent first.
func simState(connected, listed, serving bool, handedFor time.Duration) string {
	switch {
	case !connected:
		return "offline"
	case serving:
		return "running"
	case !listed || handedFor < simStartGrace:
		return "starting"
	}
	return "failed"
}
