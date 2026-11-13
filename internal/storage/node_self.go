package storage

import (
	"time"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// NodeSelfReport is a node's report of itself (#1625): the node-self block of
// its tick, landed on the node's own platform component under the platform
// writer. Node is the publishing node, taken from its subject, never from the
// batch: the report names no owner, and the gateway resolves the component
// from the node row (node.platform_component_id), so a node speaks only for
// its own component.
type NodeSelfReport struct {
	Node    string
	Metrics []NodeSelfSample
}

// NodeSelfSample is one node-self observation: a declared node-self platform
// key (writerclass.NodeSelf), its instance (empty but where the key
// discriminates, as session-held-count carries the session state), its value
// and its time.
type NodeSelfSample struct {
	Key      string
	Instance string
	Value    float64
	TS       time.Time
}

// NodeSelfIndex is the index a refused node-self report carries in
// LandResult.Refused: it is no one of the batch's Groups, so it never names
// one, and its refusal drops the report alone while the tick's task groups
// land.
const NodeSelfIndex = -2

// nodeSelfArc is the arc key preloadArcs resolves a node's platform component
// under: the node's name, in a kind no row carries.
func nodeSelfArc(node string) string { return "platform\x00" + node }

// nodeSelfSource is the source a node-self row records: the node that
// reported it.
func nodeSelfSource(node string) string { return "node/" + node }

// nodeSelfWriter is the writer a node's self-report lands under: the
// process (the platform class admits it alone), held to the node-self keys
// and to the one component, the node's platform component. component is ""
// when the node has none (registered before the boot seed created the
// platform), which refuses every row.
func nodeSelfWriter(component string) KeyWriter {
	return KeyWriter{Kind: writerclass.Process, Component: component, self: true}
}

// nodeSelfGroup is the batch's node-self report as the group it lands, once
// preloadArcs has resolved the node's platform component: its rows on that
// component, under the node-self writer. ok is false for a batch with no
// report. A node with no platform component (registered while the database
// had no platform system, before the next boot adopts it) has nowhere to
// land its report: refused is its refusal, so the report drops alone, read
// as refused and never redelivered, while the tick's task groups land.
func (b IngestBatch) nodeSelfGroup(bt *batchTx) (g landGroup, refused error, ok bool) {
	r := b.NodeSelf
	if r == nil || len(r.Metrics) == 0 {
		return landGroup{}, nil, false
	}
	component := bt.arcs[nodeSelfArc(r.Node)]
	if component == "" {
		return landGroup{}, &KeyWriterError{Key: r.Metrics[0].Key, Class: writerclass.Platform, Writer: writerclass.Process, self: true}, true
	}
	// The node row's foreign key holds it: the component exists, so its arc
	// needs no read of its own.
	bt.arcs["component\x00"+component] = component
	return landGroup{IngestGroup: IngestGroup{Writer: nodeSelfWriter(component), Metrics: r.writes(component)}, index: NodeSelfIndex}, nil, true
}

// writes is the report's samples as metric rows on component.
func (r *NodeSelfReport) writes(component string) []MetricSampleWrite {
	out := make([]MetricSampleWrite, 0, len(r.Metrics))
	for _, s := range r.Metrics {
		out = append(out, MetricSampleWrite{OwnerKind: "component", OwnerID: component, Key: s.Key,
			Instance: s.Instance, Value: s.Value, Source: nodeSelfSource(r.Node), TS: s.TS})
	}
	return out
}

// admitNodeSelf holds one row of a node's self-report to its writer: a
// declared node-self key (a node never speaks for another platform writer's
// key, health or checkin-age), on the node's own platform component. Pure.
func admitNodeSelf(w KeyWriter, r keyedWrite) bool {
	return writerclass.NodeSelf(r.key) && w.Component != "" && r.ownerKind == "component" && r.owner == w.Component
}
