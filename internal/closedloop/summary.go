package closedloop

import (
	"sort"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/obsstat"
)

// Quantiles is a latency distribution's p50, p95 and p99, in milliseconds,
// over N samples.
type Quantiles struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	N   int     `json:"n"`
}

// EndpointReport is the closed loop over every node.
type EndpointReport struct {
	Protocols []EndpointProtocol `json:"protocols"`
	Nodes     []EndpointNode     `json:"nodes"`
	GapTotals []GapTotal         `json:"gap_totals"`
	Gaps      []ClosedLoopGap    `json:"gaps"`
	// WorstFaces are the faces that most often saw two requests at once,
	// counted since each sim started, not over the window.
	WorstFaces []EndpointFace `json:"worst_faces_since_start"`
}

// EndpointProtocol is one protocol's window, summed over the nodes: the sim's
// counters from the first read to the last, the service time merged across
// nodes, and each node's in-flight high water.
type EndpointProtocol struct {
	Protocol      string `json:"protocol"`
	Accepted      int64  `json:"accepted"`
	Received      int64  `json:"received"`
	Answered      int64  `json:"answered"`
	Refused       int64  `json:"refused"`
	Error         int64  `json:"error"`
	Dropped       int64  `json:"dropped"`
	Withheld      int64  `json:"withheld"`
	StreamsOpened int64  `json:"streams_opened"`
	// PollTargets is how many enabled poll targets the window should have
	// drawn on this protocol at least twice, over every node.
	PollTargets  int64   `json:"poll_targets"`
	Overlapped   int64   `json:"overlapped"`
	OverlapRatio float64 `json:"overlap_ratio"`
	// Latency is the sim's service time, in milliseconds.
	Latency      Quantiles        `json:"latency_ms"`
	InFlightHigh map[string]int64 `json:"in_flight_high"`
}

// EndpointNode is one node's concurrency: the most requests its sim held at
// once over every protocol, beside the node's own dispatch gauges (nil when
// no pool reports one yet).
type EndpointNode struct {
	Node               string `json:"node"`
	ArrivalHigh        int64  `json:"arrival_high"`
	DispatchQueueDepth *int64 `json:"node_dispatch_queue_depth"`
	BusyWorkers        *int64 `json:"node_busy_workers"`
	Impaired           bool   `json:"impaired"`
	Restarted          bool   `json:"restarted"`
}

// GapTotal is one gap summed over the nodes.
type GapTotal struct {
	Gap          string `json:"gap"`
	Loss         int64  `json:"loss"`
	Impaired     int64  `json:"impaired"`
	Masked       int64  `json:"masked"`
	MaskedBy     string `json:"masked_by,omitempty"`
	AnsweredLate int64  `json:"answered_late"`
}

// worstFaceCount is how many overlapping faces the report names.
const worstFaceCount = 5

// EndpointFace is one overlapping face on a node.
type EndpointFace struct {
	Node string `json:"node"`
	collection.SimFaceOverlap
}

// Loss is every gap's loss.
func (r EndpointReport) Loss() int64 {
	var n int64
	for _, g := range r.GapTotals {
		n += g.Loss
	}
	return n
}

func (r *EndpointReport) Protocol(name string) *EndpointProtocol {
	for i := range r.Protocols {
		if r.Protocols[i].Protocol == name {
			return &r.Protocols[i]
		}
	}
	return nil
}

// row is the protocol's row, added when it has none.
func (r *EndpointReport) row(name string) *EndpointProtocol {
	if p := r.Protocol(name); p != nil {
		return p
	}
	r.Protocols = append(r.Protocols, EndpointProtocol{Protocol: name, InFlightHigh: map[string]int64{}})
	return &r.Protocols[len(r.Protocols)-1]
}

// SummarizeEndpoints reconciles every node and sums the window.
func SummarizeEndpoints(brackets []NodeBracket) EndpointReport {
	bs := append([]NodeBracket(nil), brackets...)
	sort.Slice(bs, func(i, j int) bool { return bs[i].Node < bs[j].Node })
	var r EndpointReport
	latency := map[string][]uint64{}
	for _, b := range bs {
		gaps := Reconcile(b)
		r.Gaps = append(r.Gaps, gaps...)
		s1, s2 := b.Start.SimBefore, b.End.SimAfter
		from, to := s1.At, s2.At
		n := EndpointNode{Node: b.Node, Impaired: impaired(b), Restarted: len(gaps) == 1 && gaps[0].Reading == ReadingRestarted}
		arrivals := map[int64]int64{}
		for _, p2 := range s2.Protocols {
			p1 := s1.Protocol(p2.Protocol)
			ep := r.row(p2.Protocol)
			if !n.Restarted {
				ep.Accepted += p2.Accepted - p1.Accepted
				ep.Received += p2.Received - p1.Received
				ep.Answered += p2.Answered - p1.Answered
				ep.Refused += p2.Refused - p1.Refused
				ep.Error += p2.Error - p1.Error
				ep.Dropped += p2.Dropped - p1.Dropped
				ep.Withheld += p2.Withheld - p1.Withheld
				ep.StreamsOpened += p2.StreamsOpened - p1.StreamsOpened
				ep.Overlapped += p2.Overlapped - p1.Overlapped
				latency[p2.Protocol] = obsstat.MergeLatency(latency[p2.Protocol], obsstat.SubtractLatency(p2.Latency, p1.Latency))
			}
			ep.InFlightHigh[b.Node] = obsstat.HighWater(p2.InFlightHigh, from, to)
			for _, e := range p2.InFlightHigh {
				if e.Epoch >= obsstat.EpochOf(from) && e.Epoch <= obsstat.EpochOf(to) {
					arrivals[e.Epoch] += e.Max
				}
			}
		}
		for _, v := range arrivals {
			n.ArrivalHigh = max(n.ArrivalHigh, v)
		}
		for _, t := range b.Targets {
			if p, ok := dueProtocol(t, b.Window); ok {
				r.row(p).PollTargets++
			}
		}
		if g := b.End.Node.QueueDepth; g != nil {
			v := g.High(from, to)
			n.DispatchQueueDepth = &v
		}
		if g := b.End.Node.BusyWorkers; g != nil {
			v := g.High(from, to)
			n.BusyWorkers = &v
		}
		r.Nodes = append(r.Nodes, n)
		for _, f := range s2.Faces {
			r.WorstFaces = append(r.WorstFaces, EndpointFace{Node: b.Node, SimFaceOverlap: f})
		}
	}
	sort.Slice(r.Protocols, func(i, j int) bool { return r.Protocols[i].Protocol < r.Protocols[j].Protocol })
	for i := range r.Protocols {
		p := &r.Protocols[i]
		if p.Received > 0 {
			p.OverlapRatio = float64(p.Overlapped) / float64(p.Received)
		}
		h := latency[p.Protocol]
		var total uint64
		for _, c := range h {
			total += c
		}
		p.Latency = Quantiles{
			P50: obsstat.LatencyQuantile(h, 0.50) / 1000,
			P95: obsstat.LatencyQuantile(h, 0.95) / 1000,
			P99: obsstat.LatencyQuantile(h, 0.99) / 1000,
			N:   int(total),
		}
	}
	sort.SliceStable(r.WorstFaces, func(i, j int) bool { return r.WorstFaces[i].Overlapped > r.WorstFaces[j].Overlapped })
	if len(r.WorstFaces) > worstFaceCount {
		r.WorstFaces = r.WorstFaces[:worstFaceCount]
	}
	for _, name := range GapNames() {
		t := GapTotal{Gap: name}
		for _, g := range r.Gaps {
			if g.Gap != name {
				continue
			}
			switch g.Reading {
			case ReadingLoss:
				t.Loss += g.Count
			case ReadingImpaired:
				t.Impaired += g.Count
			}
			t.Masked += g.Masked
			t.AnsweredLate += g.AnsweredLate
			if g.MaskedBy != "" && g.MaskedBy != maskedByUnhitSample {
				t.MaskedBy = g.MaskedBy
			}
		}
		if t.MaskedBy == "" && t.Masked > 0 {
			t.MaskedBy = maskedByUnhitSample
		}
		r.GapTotals = append(r.GapTotals, t)
	}
	return r
}
