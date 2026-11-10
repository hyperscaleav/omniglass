package labfootprint

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/obsstat"
)

var (
	simStarted  = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	nodeStarted = simStarted.Add(time.Second)
	windowStart = simStarted.Add(time.Minute)
	windowEnd   = windowStart.Add(2 * time.Minute)
)

// served is one protocol's sim row with every request answered.
func served(p string, accepted, received int64) collection.SimProtocolObservation {
	return collection.SimProtocolObservation{Protocol: p, Accepted: accepted, Received: received, Answered: received, Served: 3}
}

// sent is one transport's node row with every request answered.
func sent(tr string, connections, n int64) collection.TallyCounters {
	return collection.TallyCounters{Transport: tr, Connections: connections, Sent: n, Answered: n}
}

func simRead(at time.Time, rows ...collection.SimProtocolObservation) collection.SimObservation {
	return collection.SimObservation{Node: "edge", At: at, Started: simStarted, Devices: 3, Protocols: rows}
}

// balanced is a two-minute window in which every hop carried everything:
// 60 http requests on 60 connections, 30 snmp datagrams, 10 tcp connects and
// 6 batches acknowledged and landed.
func balanced() NodeBracket {
	start := collection.EndpointObservation{
		SimBefore: simRead(windowStart, served("http", 100, 100), served("snmp", 0, 50), served("tcp", 10, 0)),
		Node: collection.RequestTallySnapshot{Node: "edge", At: windowStart, Started: nodeStarted, BatchesAcknowledged: 10,
			Transports: []collection.TallyCounters{sent("http", 100, 100), sent("snmp", 0, 50), {Transport: "tcp", Connections: 10}}},
		SimAfter: simRead(windowStart, served("http", 100, 100), served("snmp", 0, 50), served("tcp", 10, 0)),
	}
	end := collection.EndpointObservation{
		SimBefore: simRead(windowEnd, served("http", 160, 160), served("snmp", 0, 80), served("tcp", 20, 0)),
		Node: collection.RequestTallySnapshot{Node: "edge", At: windowEnd, Started: nodeStarted, BatchesAcknowledged: 16,
			Transports: []collection.TallyCounters{sent("http", 160, 160), sent("snmp", 0, 80), {Transport: "tcp", Connections: 20}}},
		SimAfter: simRead(windowEnd, served("http", 160, 160), served("snmp", 0, 80), served("tcp", 20, 0)),
	}
	return NodeBracket{
		Node: "edge", Start: start, End: end, Window: 2 * time.Minute,
		Receipts: ReceiptCount{Landed: 6},
		Targets: []PollTarget{
			{ComponentID: "c-http", Transport: "http", Interval: 2 * time.Second},
			{ComponentID: "c-snmp", Transport: "snmp", Interval: 10 * time.Second},
		},
	}
}

// protocol returns a pointer to one protocol's row in a sim read.
func protocol(o *collection.SimObservation, p string) *collection.SimProtocolObservation {
	for i := range o.Protocols {
		if o.Protocols[i].Protocol == p {
			return &o.Protocols[i]
		}
	}
	panic("no row " + p)
}

// transportRow returns a pointer to one transport's row in a node read.
func transportRow(n *collection.RequestTallySnapshot, tr string) *collection.TallyCounters {
	for i := range n.Transports {
		if n.Transports[i].Transport == tr {
			return &n.Transports[i]
		}
	}
	panic("no row " + tr)
}

// endSims applies f to both of the end bracket's sim reads.
func endSims(b *NodeBracket, f func(*collection.SimObservation)) {
	f(&b.End.SimBefore)
	f(&b.End.SimAfter)
}

func describe(gs []ClosedLoopGap) string {
	var s []string
	for _, g := range gs {
		s = append(s, fmt.Sprintf("%+v", g))
	}
	return "[" + strings.Join(s, "; ") + "]"
}

// losses is the gaps that read as loss with a count.
func losses(gs []ClosedLoopGap) []ClosedLoopGap {
	var out []ClosedLoopGap
	for _, g := range gs {
		if g.Reading == ReadingLoss && g.Count > 0 {
			out = append(out, g)
		}
	}
	return out
}

// Each hop's gap is a lower bound: it is positive only where something was
// lost, and the term that can hide a loss prints beside it.
func TestReconcileNamesEachHopsGap(t *testing.T) {
	for _, c := range []struct {
		name  string
		plant func(*NodeBracket)
		want  ClosedLoopGap
	}{
		{"a connection the sim never accepted", func(b *NodeBracket) {
			endSims(b, func(o *collection.SimObservation) { protocol(o, "http").Accepted = 157 })
		}, ClosedLoopGap{Gap: GapConnectNotAccepted, Protocol: "http", Count: 3, MaskedBy: maskedByBacklog}},
		{"a tcp connect the sim never accepted", func(b *NodeBracket) {
			endSims(b, func(o *collection.SimObservation) { protocol(o, "tcp").Accepted = 18 })
		}, ClosedLoopGap{Gap: GapConnectNotAccepted, Protocol: "tcp", Count: 2, MaskedBy: maskedByBacklog}},
		{"a request the sim never received", func(b *NodeBracket) {
			endSims(b, func(o *collection.SimObservation) {
				p := protocol(o, "snmp")
				p.Received, p.Answered = 75, 75
			})
		}, ClosedLoopGap{Gap: GapSentNotReceived, Protocol: "snmp", Count: 5, MaskedBy: maskedByNodeInFlight}},
		{"a request the sim never settled", func(b *NodeBracket) {
			endSims(b, func(o *collection.SimObservation) { protocol(o, "http").Answered = 157 })
		}, ClosedLoopGap{Gap: GapReceivedNotSettled, Protocol: "http", Count: 3}},
		{"an answer the node never got back", func(b *NodeBracket) {
			transportRow(&b.End.Node, "http").Answered = 157
		}, ClosedLoopGap{Gap: GapAnsweredNotReturned, Protocol: "http", Count: 3, MaskedBy: maskedByReturn}},
		{"a batch acknowledged that never landed", func(b *NodeBracket) {
			b.Receipts = ReceiptCount{Landed: 4, Margin: 1, Backlog: 3, AfterEnd: 1}
		}, ClosedLoopGap{Gap: GapPublishedNotLanded, Count: 2, Masked: 5, MaskedBy: maskedByReceiptEdges}},
		{"a batch the spool dropped", func(b *NodeBracket) {
			b.End.Node.BatchesDropped = 2
		}, ClosedLoopGap{Gap: GapSpoolDropped, Count: 2}},
		{"a poll target that drew no request", func(b *NodeBracket) {
			endSims(b, func(o *collection.SimObservation) {
				p := protocol(o, "http")
				p.Unhit, p.UnhitSample = 1, []string{"c-http"}
			})
		}, ClosedLoopGap{Gap: GapUnhitEndpoint, Protocol: "http", Count: 1, ComponentID: "c-http"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := balanced()
			c.plant(&b)
			got := losses(Reconcile(b))
			c.want.Node, c.want.Reading = "edge", ReadingLoss
			if len(got) != 1 || got[0] != c.want {
				t.Fatalf("gaps = %s, want only %+v", describe(got), c.want)
			}
		})
	}
}

// When every hop carried everything there is nothing to report.
func TestReconcileBalancedFiguresYieldNoGap(t *testing.T) {
	if got := Reconcile(balanced()); len(got) != 0 {
		t.Fatalf("balanced figures read %s", describe(got))
	}
}

// A request in flight at either read is in transit, not lost: it is the
// masking term, never the gap.
func TestReconcileRequestsInFlightAtEitherReadAreNoGap(t *testing.T) {
	b := balanced()
	// At the start, 5 http requests are out: the sim holds them too.
	n := transportRow(&b.Start.Node, "http")
	n.Sent, n.InFlight = 105, 5
	for _, o := range []*collection.SimObservation{&b.Start.SimBefore, &b.Start.SimAfter} {
		p := protocol(o, "http")
		p.Received, p.InFlight = 105, 5
	}
	// At the end, 5 more are out: 3 of them have not reached the sim yet,
	// 2 are being served, and one answer is on its way back.
	n = transportRow(&b.End.Node, "http")
	n.Sent, n.Answered, n.InFlight = 165, 160, 5
	for _, o := range []*collection.SimObservation{&b.End.SimBefore, &b.End.SimAfter} {
		p := protocol(o, "http")
		p.Received, p.Answered, p.InFlight = 162, 161, 1
	}
	// One snmp datagram the sim answered came back after the node gave up.
	n = transportRow(&b.End.Node, "snmp")
	n.Answered, n.TimedOut = 79, 1
	gs := Reconcile(b)
	if l := losses(gs); len(l) != 0 {
		t.Fatalf("requests in flight read as loss: %s", describe(l))
	}
	late := find(gs, GapAnsweredNotReturned, "snmp")
	if late == nil || late.AnsweredLate != 1 || late.Masked != 1 {
		t.Fatalf("the late snmp answer = %+v, want it printed beside its gap", late)
	}
	inflight := find(gs, GapSentNotReceived, "http")
	if inflight == nil || inflight.Masked != 5 {
		t.Fatalf("http sent-not-received = %+v, want the node's 5 in flight as its masking term", inflight)
	}
}

func find(gs []ClosedLoopGap, gap, protocol string) *ClosedLoopGap {
	for i := range gs {
		if gs[i].Gap == gap && gs[i].Protocol == protocol {
			return &gs[i]
		}
	}
	return nil
}

// The kernel answers icmp, so the sim never sees it: an icmp target is named
// as not observed, never as unhit.
func TestReconcileIcmpIsNotObservedBySim(t *testing.T) {
	b := balanced()
	b.Targets = append(b.Targets, PollTarget{ComponentID: "c-ping", Transport: "icmp", Interval: 2 * time.Second},
		PollTarget{ComponentID: "c-ping2", Transport: "icmp", Interval: 2 * time.Second})
	gs := Reconcile(b)
	if l := losses(gs); len(l) != 0 {
		t.Fatalf("icmp read as loss: %s", describe(l))
	}
	g := find(gs, GapUnhitEndpoint, "icmp")
	if g == nil || g.Reading != ReadingNotObserved || g.Count != 2 {
		t.Fatalf("icmp = %+v, want its 2 targets read %q", g, ReadingNotObserved)
	}
}

// A target is unhit only when the window should have drawn it twice; an
// unhit device the sample could not name is the masking term; a target the
// sim does not name unhit, or on a transport no node dials, is no gap.
func TestReconcileUnhitTargets(t *testing.T) {
	b := balanced()
	b.Targets = append(b.Targets,
		PollTarget{ComponentID: "c-slow", Transport: "http", Interval: 5 * time.Minute},
		PollTarget{ComponentID: "c-tls", Transport: "https", Interval: time.Second},
		PollTarget{ComponentID: "c-hook", Transport: "webhook", Interval: time.Second})
	endSims(&b, func(o *collection.SimObservation) {
		p := protocol(o, "http")
		p.Unhit, p.UnhitSample = 25, []string{"c-slow", "c-tls"}
		for len(p.UnhitSample) < collection.SimObservationSampleCap {
			p.UnhitSample = append(p.UnhitSample, fmt.Sprintf("other-%d", len(p.UnhitSample)))
		}
	})
	gs := Reconcile(b)
	l := losses(gs)
	if len(l) != 1 || l[0].ComponentID != "c-tls" || l[0].Protocol != "http" {
		t.Fatalf("unhit = %s, want only the https target, as http", describe(l))
	}
	var masked int64
	for _, g := range gs {
		if g.Gap == GapUnhitEndpoint && g.Protocol == "http" {
			masked += g.Masked
		}
	}
	if masked != 5 {
		t.Fatalf("unhit masking = %d, want the 5 unhit devices past the sample", masked)
	}
}

// A kernel impairment loses traffic below the sim, so on a node whose sim
// shows one active, a gap reads impaired with its count, not loss.
func TestReconcileImpairedNodeReadsImpaired(t *testing.T) {
	b := balanced()
	b.End.SimAfter.Impaired = 1
	endSims(&b, func(o *collection.SimObservation) {
		p := protocol(o, "snmp")
		p.Received, p.Answered = 75, 75
	})
	gs := Reconcile(b)
	if l := losses(gs); len(l) != 0 {
		t.Fatalf("an impaired gap read as loss: %s", describe(l))
	}
	g := find(gs, GapSentNotReceived, "snmp")
	if g == nil || g.Reading != ReadingImpaired || g.Count != 5 {
		t.Fatalf("snmp = %+v, want 5 read %q", g, ReadingImpaired)
	}
}

// A counter that went backwards is a restart: the node reads restarted,
// once, and never a negative or a made-up gap.
func TestReconcileCounterResetReadsRestarted(t *testing.T) {
	for _, c := range []struct {
		name  string
		plant func(*NodeBracket)
	}{
		{"the node restarted", func(b *NodeBracket) {
			b.End.Node.Started = windowEnd.Add(-time.Second)
			transportRow(&b.End.Node, "http").Sent = 3
		}},
		{"the sim restarted", func(b *NodeBracket) {
			endSims(b, func(o *collection.SimObservation) { o.Started = windowEnd.Add(-time.Second) })
		}},
		{"a sim counter went backwards", func(b *NodeBracket) {
			endSims(b, func(o *collection.SimObservation) {
				p := protocol(o, "http")
				p.Received, p.Answered = 90, 90
			})
		}},
		{"the batches went backwards", func(b *NodeBracket) { b.End.Node.BatchesAcknowledged = 2 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := balanced()
			c.plant(&b)
			gs := Reconcile(b)
			if len(gs) != 1 || gs[0].Reading != ReadingRestarted || gs[0].Node != "edge" {
				t.Fatalf("gaps = %s, want one %q for the node", describe(gs), ReadingRestarted)
			}
		})
	}
}

// A protocol the node does not tally yet (a session transport) reads its
// sim traffic as untallied, never as answers the node lost.
func TestReconcileUntalliedProtocolIsNoGap(t *testing.T) {
	b := balanced()
	b.Start.SimBefore.Protocols = append(b.Start.SimBefore.Protocols, served("ssh", 4, 10))
	b.Start.SimAfter.Protocols = append(b.Start.SimAfter.Protocols, served("ssh", 4, 10))
	endSims(&b, func(o *collection.SimObservation) { o.Protocols = append(o.Protocols, served("ssh", 6, 40)) })
	// An http session the node does not tally rides the same face.
	endSims(&b, func(o *collection.SimObservation) {
		p := protocol(o, "http")
		p.Accepted, p.Received, p.Answered = 161, 170, 170
	})
	if l := losses(Reconcile(b)); len(l) != 0 {
		t.Fatalf("untallied traffic read as loss: %s", describe(l))
	}
}

// The node's cadence is read off its container's command, and a target's
// interval is the slower of its own and the tick it waits on.
func TestNodeCadenceAndEffectiveInterval(t *testing.T) {
	c := ParseNodeCadence([]string{"node", "run", "--simulate", "--heartbeat", "2s", "--probe-every=10s", "--poll", "x"})
	if c.Heartbeat != 2*time.Second || c.ProbeEvery != 10*time.Second {
		t.Fatalf("cadence = %+v", c)
	}
	if d := ParseNodeCadence([]string{"node", "run"}); d.Heartbeat != 30*time.Second || d.ProbeEvery != 0 {
		t.Fatalf("default cadence = %+v", d)
	}
	for _, x := range []struct {
		every string
		want  time.Duration
	}{{"", 10 * time.Second}, {"1m", time.Minute}, {"500ms", 2 * time.Second}, {"nonsense", 2 * time.Second}} {
		if got := c.Interval(x.every); got != x.want {
			t.Errorf("Interval(%q) = %v, want %v", x.every, got, x.want)
		}
	}
	if got := (NodeCadence{Heartbeat: 2 * time.Second}).Interval(""); got != 2*time.Second {
		t.Errorf("a probe with no probe interval = %v, want the heartbeat", got)
	}
}

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
	h := r.protocol("http")
	if h == nil || h.Received != 120 || h.Accepted != 120 || h.Answered != 120 {
		t.Fatalf("http = %+v, want both nodes' 60 requests", h)
	}
	if h.PollTargets != 3 || r.protocol("snmp").PollTargets != 2 || r.protocol("tcp").PollTargets != 0 {
		t.Fatalf("poll targets: http %d, snmp %d, tcp %d, want 3, 2 and 0 (a target due under twice in the window is not counted)",
			h.PollTargets, r.protocol("snmp").PollTargets, r.protocol("tcp").PollTargets)
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

	tbl := ScaleReport{Endpoints: &r}.Table()
	for _, want := range []string{"endpoints", "http received", "120", "3 poll targets", "spool-dropped", "overlap-ratio", "not wired yet", "since the sim started"} {
		if !strings.Contains(tbl, want) {
			t.Errorf("table lacks %q:\n%s", want, tbl)
		}
	}
	raw, err := json.Marshal(ScaleReport{Endpoints: &r})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"endpoints":`, `"node_dispatch_queue_depth":null`, `"node_busy_workers":null`, `"gap":"spool-dropped"`, `"poll_targets":3`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("json lacks %s: %s", want, raw)
		}
	}
}
