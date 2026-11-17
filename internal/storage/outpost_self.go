package storage

import (
	"time"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// OutpostSelfReport is an outpost's report of itself (#1625): the outpost-self block of
// its tick, landed on the outpost's own platform component under the platform
// writer. Outpost is the publishing outpost, taken from its subject, never from the
// batch: the report names no owner, and the gateway resolves the component
// from the outpost row (outpost.platform_component_id), so an outpost speaks only for
// its own component.
type OutpostSelfReport struct {
	Outpost string
	Metrics []OutpostSelfSample
}

// OutpostSelfSample is one outpost-self observation: a declared outpost-self platform
// key (writerclass.OutpostSelf), its instance (empty but where the key
// discriminates, as session-held-count carries the session state), its value
// and its time.
type OutpostSelfSample struct {
	Key      string
	Instance string
	Value    float64
	TS       time.Time
}

// OutpostSelfIndex is the index a refused outpost-self report carries in
// LandResult.Refused: it is no one of the batch's Groups, so it never names
// one, and its refusal drops the report alone while the tick's task groups
// land.
const OutpostSelfIndex = -2

// outpostSelfArc is the arc key preloadArcs resolves an outpost's platform component
// under: the outpost's name, in a kind no row carries.
func outpostSelfArc(outpost string) string { return "platform\x00" + outpost }

// outpostSelfSource is the source an outpost-self row records: the outpost that
// reported it.
func outpostSelfSource(outpost string) string { return "outpost/" + outpost }

// outpostSelfWriter is the writer an outpost's self-report lands under: the
// process (the platform class admits it alone), held to the outpost-self keys
// and to the one component, the outpost's platform component. component is ""
// when the outpost has none (registered before the boot seed created the
// platform), which refuses every row.
func outpostSelfWriter(component string) KeyWriter {
	return KeyWriter{Kind: writerclass.Process, Component: component, self: true}
}

// outpostSelfGroup is the batch's outpost-self report as the group it lands, once
// preloadArcs has resolved the outpost's platform component: its rows on that
// component, under the outpost-self writer. ok is false for a batch with no
// report. An outpost with no platform component (registered while the database
// had no platform system, before the next boot adopts it) has nowhere to
// land its report: refused is its refusal, so the report drops alone, read
// as refused and never redelivered, while the tick's task groups land.
func (b IngestBatch) outpostSelfGroup(bt *batchTx) (g landGroup, refused error, ok bool) {
	r := b.OutpostSelf
	if r == nil || len(r.Metrics) == 0 {
		return landGroup{}, nil, false
	}
	component := bt.arcs[outpostSelfArc(r.Outpost)]
	if component == "" {
		return landGroup{}, &KeyWriterError{Key: r.Metrics[0].Key, Class: writerclass.Platform, Writer: writerclass.Process, self: true}, true
	}
	// The outpost row's foreign key holds it: the component exists, so its arc
	// needs no read of its own.
	bt.arcs["component\x00"+component] = component
	return landGroup{IngestGroup: IngestGroup{Writer: outpostSelfWriter(component), Metrics: r.writes(component)}, index: OutpostSelfIndex}, nil, true
}

// writes is the report's samples as metric rows on component.
func (r *OutpostSelfReport) writes(component string) []MetricSampleWrite {
	out := make([]MetricSampleWrite, 0, len(r.Metrics))
	for _, s := range r.Metrics {
		out = append(out, MetricSampleWrite{OwnerKind: "component", OwnerID: component, Key: s.Key,
			Instance: s.Instance, Value: s.Value, Source: outpostSelfSource(r.Outpost), TS: s.TS})
	}
	return out
}

// admitOutpostSelf holds one row of an outpost's self-report to its writer: a
// declared outpost-self key (an outpost never speaks for another platform writer's
// key, health or checkin-age), on the outpost's own platform component. Pure.
func admitOutpostSelf(w KeyWriter, r keyedWrite) bool {
	return writerclass.OutpostSelf(r.key) && w.Component != "" && r.ownerKind == "component" && r.owner == w.Component
}
