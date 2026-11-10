package api

import (
	"context"
	"slices"
	"time"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/simhub"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// WithClosedLoopRing supplies the server's closed-loop ring: the sliding
// window over each node with a simulator that the Platform health page reads.
// Without it the page's simulation section reads no node.
func WithClosedLoopRing(r *closedloop.Ring) Option {
	return func(o *options) { o.ring = r }
}

// serverCadence is the cadence the server assumes for every node: it is not
// told a node's flags, so a node run faster than its defaults has targets due
// more often than the server counts (#1647's thin cut).
var serverCadence = closedloop.NodeCadence{Heartbeat: node.DefaultHeartbeat}

// simLoopSource is the server's closedloop.Source: the bracketed reads and
// device hits through its line to the simulators, the poll targets and
// receipts from the Gateway.
type simLoopSource struct {
	hub SimHub
	gw  storage.Gateway
}

// NewSimLoopSource is the ring's source on a server.
func NewSimLoopSource(hub SimHub, gw storage.Gateway) closedloop.Source {
	return simLoopSource{hub: hub, gw: gw}
}

// Observe brackets the node and reads its simulator's device hits: every
// device it was handed or has hits for, impaired when its simulator says so.
func (s simLoopSource) Observe(ctx context.Context, node string) (collection.EndpointObservation, map[string]closedloop.DeviceRead, error) {
	obs, err := s.hub.EndpointObservation(ctx, node)
	if err != nil {
		return obs, nil, err
	}
	st, _ := s.hub.SimStatus(node)
	return obs, simDeviceReads(st.Listed, st.Devices, st.Impairments.Impaired), nil
}

// Targets is every enabled poll target by node, at the interval it runs
// under the server's assumed cadence.
func (s simLoopSource) Targets(ctx context.Context) (map[string][]closedloop.PollTarget, error) {
	rows, err := s.gw.ListClosedLoopPollTargets(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]closedloop.PollTarget{}
	for _, r := range rows {
		out[r.Node] = append(out[r.Node], closedloop.PollTarget{
			ComponentID: r.ComponentID, Transport: r.Transport, Interval: serverCadence.Interval(r.Every)})
	}
	return out, nil
}

// Receipts counts the node's telemetry receipts by landed_at. The server
// reads no ingest lane's drain, so Backlog is 0 and a batch acknowledged
// before the start read that landed after it counts in the window (ADR-1647).
func (s simLoopSource) Receipts(ctx context.Context, node string, from, start, end, next time.Time) (closedloop.ReceiptCount, error) {
	w, err := s.gw.CountTelemetryReceipts(ctx, collection.TelemetrySubject(node)+" ", from, start, end, next)
	if err != nil {
		return closedloop.ReceiptCount{}, err
	}
	return closedloop.ReceiptCount{Landed: w.Landed, Margin: w.Margin, AfterEnd: w.AfterEnd}, nil
}

// simDeviceReads is each device a simulator was handed or has hits for, its
// hits by protocol, and whether the simulator impairs it.
func simDeviceReads(listed map[string]bool, devices map[string]simhub.DeviceHits, impaired []string) map[string]closedloop.DeviceRead {
	out := make(map[string]closedloop.DeviceRead, len(listed))
	read := func(id string) closedloop.DeviceRead {
		d := closedloop.DeviceRead{ByProtocol: map[string]int64{}, Impaired: slices.Contains(impaired, id)}
		if h, ok := devices[id]; ok {
			for p, n := range h.ByProtocol {
				d.ByProtocol[p] = int64(n)
			}
		}
		return d
	}
	for id := range listed {
		out[id] = read(id)
	}
	for id := range devices {
		out[id] = read(id)
	}
	return out
}

// SimLoopNodes lists the nodes whose simulator pulled within the connected
// window, the nodes a ring brackets.
func SimLoopNodes(hub SimHub, now func() time.Time) func() []string {
	return func() []string {
		var out []string
		for _, n := range hub.Nodes() {
			if st, ok := hub.SimStatus(n); ok && now().Sub(st.LastSeen) <= simConnectedWindow {
				out = append(out, n)
			}
		}
		return out
	}
}
