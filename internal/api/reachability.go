package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The reachability read BFF: a per-component, per-interface composition of the
// verdict state (interface.reachable), the layer signals (the raw icmp/tcp probe
// metrics), and the recent transition history the availability strip reads. It is
// a plain typed Huma GET (no ViewResult framework exists), gated by component:read
// and scope-injected through GetComponent: an out-of-scope component is a
// non-disclosing 404, so the datapoint reads below only ever run on a verified,
// in-scope component. Read-only: no engine, bus, or write-path is touched.

// reachHistoryWindow bounds the transition history returned for the strip.
const reachHistoryWindow = 24 * time.Hour

// verdictKey is the state datapoint that carries the per-interface verdict.
const verdictKey = "interface.reachable"

// reachLayer describes one probe layer the panel gates on: its primary signal
// (0/1) and the optional companion metric that carries a timing detail.
type reachLayer struct {
	Layer     string // the layer word: "ping" (L3) or "port" (L4)
	SignalKey string // the primary metric key (icmp.reachable / tcp.open)
	TimingKey string // the companion timing metric (icmp.rtt_avg / tcp.connect_time)
}

// reachLayers is the fixed probe-layer order, L3 then L4.
var reachLayers = []reachLayer{
	{Layer: "ping", SignalKey: "icmp.reachable", TimingKey: "icmp.rtt_avg"},
	{Layer: "port", SignalKey: "tcp.open", TimingKey: "tcp.connect_time"},
}

type reachVerdictBody struct {
	Value string    `json:"value" doc:"The latest stored verdict value (up/down)"`
	TS    time.Time `json:"ts" doc:"When the verdict was observed"`
}

type reachLayerBody struct {
	Layer  string    `json:"layer" doc:"The probe layer word (ping, port)"`
	Check  string    `json:"check" doc:"The datapoint_type key of the primary signal"`
	Value  float64   `json:"value" doc:"The latest signal value (1 = reachable/open, 0 = not)"`
	Detail string    `json:"detail,omitempty" doc:"A human timing detail (rtt / connect time), when present"`
	TS     time.Time `json:"ts" doc:"When the signal was observed"`
}

type reachHistoryBody struct {
	TS    time.Time `json:"ts"`
	Value string    `json:"value"`
}

type reachInterfaceBody struct {
	Interface string             `json:"interface" doc:"The interface name"`
	Type      string             `json:"type" doc:"The interface type (icmp, tcp, ...)"`
	Endpoint  string             `json:"endpoint,omitempty" doc:"The probed endpoint (target[:port]) from the interface params"`
	Node      string             `json:"node,omitempty" doc:"The node that probes this interface"`
	Verdict   *reachVerdictBody  `json:"verdict" doc:"The latest reachability verdict, or null if none yet"`
	Layers    []reachLayerBody   `json:"layers" doc:"The per-layer probe signals that compose the verdict"`
	History   []reachHistoryBody `json:"history" doc:"The recent verdict transitions, oldest first, for the availability strip"`
}

type reachabilityOutput struct {
	Body struct {
		Component  string               `json:"component"`
		Interfaces []reachInterfaceBody `json:"interfaces"`
	}
}

// registerReachabilityRoutes wires the per-component reachability read, the
// operator-facing surface over the collection verdict data.
func registerReachabilityRoutes(api huma.API, a *authenticator, gw storage.Gateway) {
	huma.Register(api, huma.Operation{
		OperationID: "get-component-reachability",
		Method:      http.MethodGet,
		Path:        "/components/{name}/reachability",
		Summary:     "Read a component's per-interface reachability",
		Description: "Composes, per interface, the latest reachability verdict, the probe-layer signals that compose it, and the recent verdict transitions for the availability strip. Gated by component:read; an out-of-scope component is a non-disclosing 404.",
		Middlewares: huma.Middlewares{a.authn, a.require("component", "read")},
	}, func(ctx context.Context, in *componentPathInput) (*reachabilityOutput, error) {
		comp, err := gw.GetComponent(ctx, in.Name, a.scopeFor(ctx, "component", "read"))
		if err != nil {
			return nil, mapComponentErr(err)
		}
		ifaces, err := gw.ListComponentInterfaces(ctx, comp.Name)
		if err != nil {
			return nil, huma.Error500InternalServerError("read reachability")
		}
		since := time.Now().UTC().Add(-reachHistoryWindow)
		out := &reachabilityOutput{}
		out.Body.Component = comp.Name
		out.Body.Interfaces = make([]reachInterfaceBody, 0, len(ifaces))
		for i := range ifaces {
			row, err := composeInterface(ctx, gw, comp.Name, ifaces[i], since)
			if err != nil {
				return nil, huma.Error500InternalServerError("read reachability")
			}
			out.Body.Interfaces = append(out.Body.Interfaces, row)
		}
		return out, nil
	})
}

// composeInterface assembles one interface's reachability row from the state and
// metric sinks. All reads are keyed by the verified component name and the
// interface name (the datapoint instance).
func composeInterface(ctx context.Context, gw storage.Gateway, comp string, it storage.ComponentInterface, since time.Time) (reachInterfaceBody, error) {
	row := reachInterfaceBody{
		Interface: it.Name,
		Type:      it.Type,
		Endpoint:  endpointFromParams(it.Params),
		Node:      it.NodeName,
		Layers:    []reachLayerBody{},
		History:   []reachHistoryBody{},
	}

	verdict, err := gw.LatestState(ctx, comp, verdictKey, it.Name)
	if err != nil {
		return row, err
	}
	if verdict != nil {
		row.Verdict = &reachVerdictBody{Value: verdict.Value, TS: verdict.TS}
	}

	for _, l := range reachLayers {
		signal, err := gw.LatestMetricInstance(ctx, comp, l.SignalKey, it.Name)
		if err != nil {
			return row, err
		}
		if signal == nil {
			continue
		}
		lb := reachLayerBody{Layer: l.Layer, Check: l.SignalKey, Value: signal.Value, TS: signal.TS}
		timing, err := gw.LatestMetricInstance(ctx, comp, l.TimingKey, it.Name)
		if err != nil {
			return row, err
		}
		if timing != nil {
			lb.Detail = fmt.Sprintf("%.1f ms", timing.Value)
		}
		row.Layers = append(row.Layers, lb)
	}

	transitions, err := gw.StateTransitions(ctx, comp, verdictKey, it.Name, since)
	if err != nil {
		return row, err
	}
	for _, tr := range transitions {
		row.History = append(row.History, reachHistoryBody{TS: tr.TS, Value: tr.Value})
	}
	return row, nil
}

// endpointFromParams renders the probed endpoint from an interface's params
// jsonb: target, with :port appended when the params carry one. An empty or
// unparseable params yields an empty endpoint (real field only, never invented).
func endpointFromParams(params []byte) string {
	if len(params) == 0 {
		return ""
	}
	var p struct {
		Target string          `json:"target"`
		Port   json.RawMessage `json:"port"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Target == "" {
		return ""
	}
	if port := portString(p.Port); port != "" {
		return p.Target + ":" + port
	}
	return p.Target
}

// portString normalizes a port that may be a JSON number or string.
func portString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil && n.String() != "" {
		return n.String()
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}
