package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// platformSimulationGap is one gap on one protocol over the window, read as
// a loss ratio: count over in, 0 healthy, null when nothing went in.
type platformSimulationGap struct {
	Gap          string   `json:"gap" doc:"The hop a request or a batch was lost at, in travel order."`
	Protocol     string   `json:"protocol" doc:"The protocol the gap is on; empty for a batch gap, which is per node."`
	Count        int64    `json:"count" doc:"How many were lost at the hop over the window, past its masking term."`
	In           int64    `json:"in" doc:"How many went into the hop over the window: the ratio's denominator."`
	Ratio        *float64 `json:"ratio" required:"true" nullable:"true" doc:"count over in, 0 healthy; null when nothing went in, when the sim does not observe the hop, or on a restarted node."`
	Masked       int64    `json:"masked" doc:"How many the masking term accounts for, not counted as lost."`
	MaskedBy     string   `json:"masked_by" doc:"What the masking term is, empty when the gap has none."`
	AnsweredLate int64    `json:"answered_late" doc:"Answers the node gave up on before they arrived."`
	Reading      string   `json:"reading" enum:"loss,impaired,restarted,not observed by the sim" doc:"How to read the gap: loss is a fault, impaired is expected under an applied impairment, restarted means no figure is meaningful."`
}

// platformSimulationReceipts is the node's receipts counted for the window.
type platformSimulationReceipts struct {
	Landed   int64 `json:"landed" doc:"Batches landed from the margin before the window's start to the bracket after its end."`
	Margin   int64 `json:"margin" doc:"Of those, the ones landed before the start: a batch acknowledged before the start read."`
	Backlog  int64 `json:"backlog" doc:"Batches the ingest lane held at the end. The server does not read it, so it is 0."`
	AfterEnd int64 `json:"after_end" doc:"Of those, the ones landed after the end, acknowledged before the end read."`
}

// platformSimulationProtocol is one protocol at the node's simulator over
// the window.
type platformSimulationProtocol struct {
	Protocol     string               `json:"protocol"`
	Accepted     int64                `json:"accepted" doc:"Connections the simulator's listeners on the protocol accepted over the window; 0 for a connectionless protocol. A probe that only connects, or logs in and asks nothing, draws a connection and no request."`
	Received     int64                `json:"received" doc:"Requests the simulator received on the protocol over the window."`
	Answered     int64                `json:"answered" doc:"Requests it answered."`
	Latency      closedloop.Quantiles `json:"latency_ms" doc:"The simulator's service time in milliseconds; n is 0 when it timed nothing."`
	PollTargets  int64                `json:"poll_targets" doc:"Enabled poll targets the window should have drawn on the protocol at least twice."`
	InFlightHigh int64                `json:"in_flight_high" doc:"The most requests on the protocol the simulator held at once."`
}

// platformSimulationDevice is one simulated device over the window.
type platformSimulationDevice struct {
	ComponentID string           `json:"component_id"`
	Name        string           `json:"name" doc:"The component's name; empty when it is no longer one of the node's simulated devices."`
	Received    map[string]int64 `json:"received" doc:"Requests the device saw over the window, by protocol."`
	Impaired    bool             `json:"impaired" doc:"Whether the simulator impairs the device at the window's end."`
	Unhit       bool             `json:"unhit" doc:"Whether it is a poll target due over the window that the simulator never saw."`
}

// platformSimulationNode is one node with a simulator, read over the window.
type platformSimulationNode struct {
	Node      string                       `json:"node"`
	State     string                       `json:"state" enum:"warming up,measured,restarted" doc:"warming up until the server holds a window and the bracket after it; restarted while the window holds a restart."`
	Start     *time.Time                   `json:"start" required:"true" nullable:"true" doc:"The window's first bracket; null while warming up."`
	End       *time.Time                   `json:"end" required:"true" nullable:"true" doc:"The window's last bracket; null while warming up."`
	Brackets  int                          `json:"brackets" doc:"How many brackets the server holds for the node."`
	Error     string                       `json:"error" doc:"Why the last bracket or its reads failed; empty when they did not."`
	Gaps      []platformSimulationGap      `json:"gaps" required:"true" nullable:"true" doc:"Every gap over the window in travel order; null while warming up."`
	Receipts  platformSimulationReceipts   `json:"receipts"`
	Protocols []platformSimulationProtocol `json:"protocols"`
	// InFlightHigh is the most requests the node's sim held at once over
	// every protocol.
	InFlightHigh int64                      `json:"in_flight_high" doc:"The most requests the simulator held at once over every protocol."`
	QueueDepth   *int64                     `json:"queue_depth" required:"true" nullable:"true" doc:"The node's dispatch queue depth; null until a node reports it."`
	BusyWorkers  *int64                     `json:"busy_workers" required:"true" nullable:"true" doc:"The node's busy-worker high water; null until a node reports it."`
	Devices      []platformSimulationDevice `json:"devices"`
}

type platformSimulationViewOutput struct {
	Body struct {
		ReadAt         time.Time                `json:"read_at" doc:"The server's clock at the read."`
		WindowSeconds  int                      `json:"window_seconds" doc:"A full window's length."`
		BracketSeconds int                      `json:"bracket_seconds" doc:"How often the server brackets each node."`
		Nodes          []platformSimulationNode `json:"nodes" doc:"Each node with a connected simulator; empty when none is."`
	}
}

// toPlatformSimulationNode is a ring's read of one node as the wire carries
// it, its devices named by name.
func toPlatformSimulationNode(w closedloop.NodeWindow, names map[string]string) platformSimulationNode {
	n := platformSimulationNode{Node: w.Node, State: w.State, Brackets: w.Brackets, Error: w.Err,
		Protocols: []platformSimulationProtocol{}, Devices: []platformSimulationDevice{}}
	if w.State == closedloop.WindowWarmingUp {
		return n
	}
	start, end := w.Start, w.End
	n.Start, n.End = &start, &end
	n.Gaps = make([]platformSimulationGap, 0, len(w.Gaps))
	for _, g := range w.Gaps {
		n.Gaps = append(n.Gaps, platformSimulationGap{Gap: g.Gap, Protocol: g.Protocol, Count: g.Count, In: g.In,
			Ratio: g.Ratio, Masked: g.Masked, MaskedBy: g.MaskedBy, AnsweredLate: g.AnsweredLate, Reading: g.Reading})
	}
	rc := w.Bracket.Receipts
	n.Receipts = platformSimulationReceipts{Landed: rc.Landed, Margin: rc.Margin, Backlog: rc.Backlog, AfterEnd: rc.AfterEnd}
	sum := closedloop.SummarizeEndpoints([]closedloop.NodeBracket{w.Bracket})
	for _, p := range sum.Protocols {
		n.Protocols = append(n.Protocols, platformSimulationProtocol{Protocol: p.Protocol, Accepted: p.Accepted, Received: p.Received,
			Answered: p.Answered, Latency: p.Latency, PollTargets: p.PollTargets, InFlightHigh: p.InFlightHigh[w.Node]})
	}
	for _, sn := range sum.Nodes {
		if sn.Node == w.Node {
			n.InFlightHigh, n.QueueDepth, n.BusyWorkers = sn.ArrivalHigh, sn.DispatchQueueDepth, sn.BusyWorkers
		}
	}
	for _, d := range w.Devices {
		n.Devices = append(n.Devices, platformSimulationDevice{ComponentID: d.ComponentID, Name: names[d.ComponentID],
			Received: d.Received, Impaired: d.Impaired, Unhit: d.Unhit})
	}
	return n
}

// registerPlatformSimulationView wires the closed-loop read of the Platform
// health page's simulation section.
func registerPlatformSimulationView(api huma.API, a *authenticator, gw storage.Gateway, ring *closedloop.Ring, log *slog.Logger) {
	huma.Register(api, a.gated(huma.Operation{
		OperationID: "get-platform-simulation-view",
		Method:      http.MethodGet,
		Path:        "/views/platform/simulation",
		Summary:     "Read what each simulator saw against what the platform collected",
		Description: "Returns the read behind the Platform health page's simulation section: one row per node with a connected simulator, read over a sliding window the server brackets each minute by default (bracket_seconds). Each gap is a loss ratio (count over what went into the hop, 0 healthy, null when nothing did), its masking term and how to read it: impaired under an applied impairment, restarted while the window holds a restart, otherwise loss. Beside them, the receipts counted for the window, each protocol's received and answered counts, service-time quantiles and in-flight high water, and each simulated device's requests. A node reads warming up until the server holds a full window. The devices are unplaced simulator state, so the read needs a fleet-wide component:read; short of that it answers not found. Gated by simulation:read.",
	}, "simulation", "read"), func(ctx context.Context, _ *struct{}) (*platformSimulationViewOutput, error) {
		if !a.scopeFor(ctx, "component", "read").All {
			return nil, huma.Error404NotFound("not found")
		}
		out := &platformSimulationViewOutput{}
		out.Body.ReadAt = time.Now().UTC()
		out.Body.BracketSeconds = int(closedloop.RingEvery / time.Second)
		out.Body.WindowSeconds = closedloop.RingSpan * out.Body.BracketSeconds
		out.Body.Nodes = []platformSimulationNode{}
		if ring == nil {
			return out, nil
		}
		out.Body.BracketSeconds = int(ring.Every() / time.Second)
		out.Body.WindowSeconds = int(ring.Length() / time.Second)
		for _, w := range ring.Windows() {
			names := map[string]string{}
			devs, err := gw.NodeSimDevices(ctx, w.Node, scope.Set{All: true})
			if err != nil {
				log.ErrorContext(ctx, "read platform simulation view", "node", w.Node, "error", err)
				return nil, huma.Error500InternalServerError("read platform simulation view")
			}
			for _, d := range devs {
				names[d.ComponentID] = d.Name
			}
			out.Body.Nodes = append(out.Body.Nodes, toPlatformSimulationNode(w, names))
		}
		return out, nil
	})
}
