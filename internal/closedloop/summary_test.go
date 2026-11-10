package closedloop

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/obsstat"
)

// The report sums the nodes: per protocol the window's traffic and its
// merged latency, per node the concurrency the endpoints saw beside the
// node's own gauges, and each gap's loss with its masking total.
func TestSummarizeEndpoints(t *testing.T) {
	a, b := balanced(), balanced()
	b.Node = "core"
	for _, o := range []*collection.SimObservation{&b.Start.SimBefore, &b.Start.SimAfter, &b.End.SimBefore, &b.End.SimAfter} {
		o.Node = "core"
	}
	b.End.Node.Node = "core"
	// Latency: 10 http answers at 100 us on edge, 10 at 10 ms on core.
	fast := make([]uint64, obsstat.LatencyBucket(100)+1)
	fast[obsstat.LatencyBucket(100)] = 10
	slow := make([]uint64, obsstat.LatencyBucket(10_000)+1)
	slow[obsstat.LatencyBucket(10_000)] = 10
	endSims(&a, func(o *collection.SimObservation) {
		p := protocol(o, "http")
		p.Latency, p.Overlapped = fast, 6
		p.InFlightHigh = []obsstat.EpochHigh{{Epoch: obsstat.EpochOf(windowStart.Add(time.Minute)), Max: 4}, {Epoch: obsstat.EpochOf(windowStart.Add(-time.Hour)), Max: 99}}
		protocol(o, "snmp").InFlightHigh = []obsstat.EpochHigh{{Epoch: obsstat.EpochOf(windowStart.Add(time.Minute)), Max: 3}}
		o.Faces = []collection.SimFaceOverlap{{ComponentID: "d1", Component: "dsp", Face: "api", Protocol: "http", Received: 9, Overlapped: 6}}
	})
	endSims(&b, func(o *collection.SimObservation) { protocol(o, "http").Latency = slow })
	a.End.Node.QueueDepth = &collection.GaugeReading{HighWater: []obsstat.EpochHigh{{Epoch: obsstat.EpochOf(windowStart.Add(time.Minute)), Max: 7}}}
	b.End.Node.BatchesDropped = 2
	// core polls one more http target, one too slow for the window, and an
	// icmp target the sim never sees.
	b.Targets = append(b.Targets, PollTarget{ComponentID: "c-http-2", Transport: "https", Interval: 5 * time.Second},
		PollTarget{ComponentID: "c-slow", Transport: "http", Interval: time.Minute},
		PollTarget{ComponentID: "c-ping", Transport: "icmp", Interval: 5 * time.Second})

	r := SummarizeEndpoints([]NodeBracket{a, b})
	h := r.Protocol("http")
	if h == nil || h.Received != 120 || h.Accepted != 120 || h.Answered != 120 {
		t.Fatalf("http = %+v, want both nodes' 60 requests", h)
	}
	if h.PollTargets != 3 || r.Protocol("snmp").PollTargets != 2 || r.Protocol("tcp").PollTargets != 0 {
		t.Fatalf("poll targets: http %d, snmp %d, tcp %d, want 3, 2 and 0 (a target due under twice in the window is not counted)",
			h.PollTargets, r.Protocol("snmp").PollTargets, r.Protocol("tcp").PollTargets)
	}
	if h.Latency.N != 20 || h.Latency.P50 >= 1 || h.Latency.P99 < 9 || h.Latency.P99 > 11 {
		t.Fatalf("http latency = %+v, want p50 near 0.1 ms and p99 near 10 ms over 20 answers", h.Latency)
	}
	if h.Overlapped != 6 || h.OverlapRatio != 0.05 {
		t.Fatalf("http overlap = %d, ratio %v, want 6 of 120", h.Overlapped, h.OverlapRatio)
	}
	if h.InFlightHigh["edge"] != 4 {
		t.Fatalf("http in-flight high = %v, want edge's 4 in the window, not the 99 before it", h.InFlightHigh)
	}
	if len(r.Nodes) != 2 || r.Nodes[1].Node != "edge" || r.Nodes[1].ArrivalHigh != 7 {
		t.Fatalf("nodes = %+v, want edge's arrival high 4 + 3", r.Nodes)
	}
	if q := r.Nodes[1].DispatchQueueDepth; q == nil || *q != 7 || r.Nodes[1].BusyWorkers != nil || r.Nodes[0].DispatchQueueDepth != nil {
		t.Fatalf("gauges = %+v", r.Nodes)
	}
	if len(r.WorstFaces) != 1 || r.WorstFaces[0].ComponentID != "d1" {
		t.Fatalf("worst faces = %+v", r.WorstFaces)
	}
	var dropped *GapTotal
	for i := range r.GapTotals {
		if r.GapTotals[i].Gap == GapSpoolDropped {
			dropped = &r.GapTotals[i]
		}
	}
	if len(r.GapTotals) != len(GapNames()) || dropped == nil || dropped.Loss != 2 {
		t.Fatalf("gap totals = %+v, want every gap named, spool-dropped at 2", r.GapTotals)
	}
	if r.Loss() != 2 {
		t.Fatalf("loss = %d, want 2", r.Loss())
	}

}
