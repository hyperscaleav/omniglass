package labfootprint

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/obsstat"
)

// This file is the closed loop (#1639): what each simulated endpoint saw
// arrive, reconciled hop by hop against what its node sent and what landed.
// Each node is read twice, at the window's start and end, and each read is a
// bracket of three (the simulator, the node, the simulator again), so a
// request in transit between two reads is a masking term printed beside its
// gap, never a gap. Every gap is a lower bound: positive only where something
// was lost. The arithmetic is pure; the harness (scale_run.go) takes the reads.

// The hops of the loop, in the order a request travels them.
const (
	// GapConnectNotAccepted: a connection the node opened that the sim
	// never accepted.
	GapConnectNotAccepted = "connect-not-accepted"
	// GapSentNotReceived: a request whose bytes left the node and never
	// reached the sim.
	GapSentNotReceived = "sent-not-received"
	// GapReceivedNotSettled: a request the sim received and never settled.
	GapReceivedNotSettled = "received-not-settled"
	// GapAnsweredNotReturned: an answer the sim gave that the node never got.
	GapAnsweredNotReturned = "answered-not-returned"
	// GapPublishedNotLanded: a batch the bus acknowledged with no receipt.
	GapPublishedNotLanded = "published-not-landed"
	// GapSpoolDropped: a batch the node's spool gave up on.
	GapSpoolDropped = "spool-dropped"
	// GapUnhitEndpoint: an enabled poll target the sim saw nothing for.
	GapUnhitEndpoint = "unhit-endpoint"
	// GapRestarted is the one entry a node whose counters reset gets.
	GapRestarted = "restarted"
)

// GapNames is every hop's gap, in travel order.
func GapNames() []string {
	return []string{GapConnectNotAccepted, GapSentNotReceived, GapReceivedNotSettled,
		GapAnsweredNotReturned, GapPublishedNotLanded, GapSpoolDropped, GapUnhitEndpoint}
}

// How a gap reads.
const (
	// ReadingLoss: the count was lost.
	ReadingLoss = "loss"
	// ReadingImpaired: the node's sim carried an applied impairment, which
	// loses traffic on purpose below the sim's view.
	ReadingImpaired = "impaired"
	// ReadingRestarted: a counter reset between the reads, so no figure for
	// the node is meaningful.
	ReadingRestarted = "restarted"
	// ReadingNotObserved: the kernel answers it (icmp), so the sim never sees it.
	ReadingNotObserved = "not observed by the sim"
)

// The masking terms: what can hide a loss inside each gap's lower bound.
const (
	maskedByBacklog      = "connections still in the kernel backlog at the end read"
	maskedByNodeInFlight = "requests in flight at the node's end read"
	maskedByReturn       = "answers in flight, answers late past the node's timeout, and untallied traffic on the protocol"
	maskedByReceiptEdges = "receipts landed in the margin before the window or after its end"
	maskedByUnhitSample  = "unhit devices past the sample, not checked against the targets"
)

// ReceiptCount is one node's telemetry receipts around the window [T0, T1].
// Landed counts every receipt landed after T0 minus the acknowledgement wait
// (the generous superset); Margin how many of those landed before T0, and
// AfterEnd how many after T1. Margin and AfterEnd are the masking term.
type ReceiptCount struct {
	Landed   int64 `json:"landed"`
	Margin   int64 `json:"margin"`
	AfterEnd int64 `json:"after_end"`
}

// PollTarget is one enabled poll task's endpoint, at the interval it actually
// runs (NodeCadence.Interval).
type PollTarget struct {
	ComponentID string
	Transport   string
	Interval    time.Duration
}

// NodeBracket is one node's two bracketed reads and what the database and
// the task table say about its window.
type NodeBracket struct {
	Node       string
	Start, End collection.EndpointObservation
	Receipts   ReceiptCount
	Targets    []PollTarget
	Window     time.Duration
}

// ClosedLoopGap is one gap on one node. Count is the lower bound read as
// Reading; Masked is the term printed beside it (MaskedBy names it);
// AnsweredLate is the part of Masked the sim answered after the node gave up.
type ClosedLoopGap struct {
	Gap          string `json:"gap"`
	Node         string `json:"node"`
	Protocol     string `json:"protocol,omitempty"`
	Count        int64  `json:"count"`
	Masked       int64  `json:"masked,omitempty"`
	MaskedBy     string `json:"masked_by,omitempty"`
	AnsweredLate int64  `json:"answered_late,omitempty"`
	ComponentID  string `json:"component_id,omitempty"`
	Reading      string `json:"reading"`
}

// NodeCadence is how often a node ticks and probes, read off its command.
type NodeCadence struct {
	Heartbeat  time.Duration
	ProbeEvery time.Duration
}

// ParseNodeCadence reads --heartbeat and --probe-every off a node's command;
// a flag missing or unreadable keeps the node's default.
func ParseNodeCadence(cmd []string) NodeCadence {
	c := NodeCadence{Heartbeat: node.DefaultHeartbeat}
	flag := func(i int, name string) (string, bool) {
		if v, ok := strings.CutPrefix(cmd[i], name+"="); ok {
			return v, true
		}
		if cmd[i] == name && i+1 < len(cmd) {
			return cmd[i+1], true
		}
		return "", false
	}
	for i := range cmd {
		if v, ok := flag(i, "--heartbeat"); ok {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				c.Heartbeat = d
			}
		}
		if v, ok := flag(i, "--probe-every"); ok {
			if d, err := time.ParseDuration(v); err == nil && d >= 0 {
				c.ProbeEvery = d
			}
		}
	}
	return c
}

// Interval is how often a poll task with schedule every actually runs: a
// driver poll at its own interval, checked each tick, so never faster than
// the heartbeat; a bare reachability probe (every empty) at the probe
// interval, every tick when there is none.
func (c NodeCadence) Interval(every string) time.Duration {
	var d time.Duration
	if every == "" {
		d = c.ProbeEvery
	} else if p, err := time.ParseDuration(every); err == nil && p > 0 {
		d = p
	}
	return max(d, c.Heartbeat)
}

// Reconcile reads one node's gaps. A node whose node or sim restarted, or any
// of whose counters went backwards, gets a single restarted entry. On a node
// whose sim carries an applied impairment, every gap reads impaired.
func Reconcile(b NodeBracket) []ClosedLoopGap {
	if restarted(b) {
		return []ClosedLoopGap{{Gap: GapRestarted, Node: b.Node, Reading: ReadingRestarted}}
	}
	reading := ReadingLoss
	if impaired(b) {
		reading = ReadingImpaired
	}
	var out []ClosedLoopGap
	add := func(g ClosedLoopGap) {
		g.Count, g.Masked, g.AnsweredLate = max(g.Count, 0), max(g.Masked, 0), max(g.AnsweredLate, 0)
		if g.Count == 0 && g.Masked == 0 && g.AnsweredLate == 0 {
			return
		}
		g.Node = b.Node
		if g.Reading == "" {
			g.Reading = reading
		}
		out = append(out, g)
	}
	s1, s1a, s2b, s2 := b.Start.SimBefore, b.Start.SimAfter, b.End.SimBefore, b.End.SimAfter

	// The node hops, per transport the node tallied. A tally transport is
	// named as the sim's protocol, so the rows pair by name.
	for _, n2 := range b.End.Node.Transports {
		p := n2.Transport
		n1 := tallyRow(b.Start.Node, p)
		sent := n2.Sent - n1.Sent
		received := s2.Protocol(p).Received - s1.Protocol(p).Received
		// The widest sim span (first read to last) carries the most the sim
		// could have seen, so the difference is a floor.
		add(ClosedLoopGap{Gap: GapConnectNotAccepted, Protocol: p, MaskedBy: maskedByBacklog,
			Count: (n2.Connections - n1.Connections) - (s2.Protocol(p).Accepted - s1.Protocol(p).Accepted)})
		add(ClosedLoopGap{Gap: GapSentNotReceived, Protocol: p, MaskedBy: maskedByNodeInFlight,
			Count: sent - received - n2.InFlight, Masked: n2.InFlight})
		// Answers: the narrowest sim span (the start's second read to the
		// end's first) holds only answers given after the node's start read
		// and before its end read. Each one came back, is in flight, timed
		// out at the node, or belongs to traffic the node does not tally.
		replied := func(o collection.SimProtocolObservation) int64 { return o.Answered + o.Refused + o.Error }
		got := func(c collection.TallyCounters) int64 { return c.Answered + c.Refused + c.Error }
		untallied := max(received-sent, 0)
		residual := replied(s2b.Protocol(p)) - replied(s1a.Protocol(p)) - (got(n2) - got(n1)) - n2.InFlight - untallied
		late := min(max(residual, 0), n2.TimedOut-n1.TimedOut)
		add(ClosedLoopGap{Gap: GapAnsweredNotReturned, Protocol: p, MaskedBy: maskedByReturn,
			Count: residual - late, Masked: n2.InFlight + late + untallied, AnsweredLate: late})
	}

	// Inside the sim, at its last read: a request received is settled or in
	// flight.
	for _, p := range s2.Protocols {
		add(ClosedLoopGap{Gap: GapReceivedNotSettled, Protocol: p.Protocol, Count: p.Received - p.Settled() - p.InFlight})
	}

	// The telemetry hops.
	add(ClosedLoopGap{Gap: GapPublishedNotLanded, MaskedBy: maskedByReceiptEdges,
		Count:  (b.End.Node.BatchesAcknowledged - b.Start.Node.BatchesAcknowledged) - b.Receipts.Landed,
		Masked: b.Receipts.Margin + b.Receipts.AfterEnd})
	add(ClosedLoopGap{Gap: GapSpoolDropped, Count: b.End.Node.BatchesDropped - b.Start.Node.BatchesDropped})

	out = append(out, unhit(b, reading)...)
	return out
}

// unhit names each poll target the window should have drawn at least twice
// that the sim's last read names as having seen nothing. The sim names a
// capped sample, so devices past it are a masking term. An icmp target is
// never seen by the sim; a push transport is never polled.
func unhit(b NodeBracket, reading string) []ClosedLoopGap {
	var out []ClosedLoopGap
	var icmp int64
	sampled := map[string]bool{}
	var protocols []string
	for _, t := range b.Targets {
		p, ok := dueProtocol(t, b.Window)
		if !ok {
			if t.Transport == "icmp" && due(t, b.Window) {
				icmp++
			}
			continue
		}
		if !slices.Contains(protocols, p) {
			protocols = append(protocols, p)
		}
		if slices.Contains(b.End.SimAfter.Protocol(p).UnhitSample, t.ComponentID) && !sampled[p+" "+t.ComponentID] {
			sampled[p+" "+t.ComponentID] = true
			out = append(out, ClosedLoopGap{Gap: GapUnhitEndpoint, Node: b.Node, Protocol: p, Count: 1, ComponentID: t.ComponentID, Reading: reading})
		}
	}
	for _, p := range protocols {
		o := b.End.SimAfter.Protocol(p)
		if past := int64(o.Unhit - len(o.UnhitSample)); past > 0 {
			out = append(out, ClosedLoopGap{Gap: GapUnhitEndpoint, Node: b.Node, Protocol: p, Masked: past, MaskedBy: maskedByUnhitSample, Reading: reading})
		}
	}
	if icmp > 0 {
		out = append(out, ClosedLoopGap{Gap: GapUnhitEndpoint, Node: b.Node, Protocol: "icmp", Count: icmp, Reading: ReadingNotObserved})
	}
	return out
}

// due is whether the window should draw the target at least twice.
func due(t PollTarget, window time.Duration) bool {
	return t.Interval > 0 && t.Interval < window/2
}

// dueProtocol is the sim protocol a target due in the window lands on; false
// when it is not due or the sim does not serve its transport.
func dueProtocol(t PollTarget, window time.Duration) (string, bool) {
	if !due(t, window) {
		return "", false
	}
	return collection.SimProtocolOf(t.Transport)
}

// tallyRow is a transport's row in a node read, zero when it has none.
func tallyRow(s collection.RequestTallySnapshot, transport string) collection.TallyCounters {
	for _, c := range s.Transports {
		if c.Transport == transport {
			return c
		}
	}
	return collection.TallyCounters{Transport: transport}
}

// impaired is whether any of the node's sim reads carried an applied
// impairment. TODO(#1639): attribute an impairment to the protocol it
// impairs, and count the changes, so an unimpaired protocol on an impaired
// node still reads loss.
func impaired(b NodeBracket) bool {
	for _, o := range []collection.SimObservation{b.Start.SimBefore, b.Start.SimAfter, b.End.SimBefore, b.End.SimAfter} {
		if o.Impaired > 0 {
			return true
		}
	}
	return false
}

// restarted is whether the node or its sim started again between the reads,
// or any counter the reads compare went backwards.
func restarted(b NodeBracket) bool {
	sims := []collection.SimObservation{b.Start.SimBefore, b.Start.SimAfter, b.End.SimBefore, b.End.SimAfter}
	for i := 1; i < len(sims); i++ {
		if !sims[i].Started.Equal(sims[0].Started) {
			return true
		}
		for _, p := range sims[i-1].Protocols {
			q := sims[i].Protocol(p.Protocol)
			if q.Accepted < p.Accepted || q.Received < p.Received || q.Settled() < p.Settled() || q.StreamsOpened < p.StreamsOpened {
				return true
			}
		}
	}
	n1, n2 := b.Start.Node, b.End.Node
	if !n1.Started.Equal(n2.Started) || n2.BatchesAcknowledged < n1.BatchesAcknowledged || n2.BatchesDropped < n1.BatchesDropped {
		return true
	}
	for _, c1 := range n1.Transports {
		c2 := tallyRow(n2, c1.Transport)
		if c2.Connections < c1.Connections || c2.Sent < c1.Sent || c2.Settled() < c1.Settled() {
			return true
		}
	}
	return false
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

func (r *EndpointReport) protocol(name string) *EndpointProtocol {
	for i := range r.Protocols {
		if r.Protocols[i].Protocol == name {
			return &r.Protocols[i]
		}
	}
	return nil
}

// row is the protocol's row, added when it has none.
func (r *EndpointReport) row(name string) *EndpointProtocol {
	if p := r.protocol(name); p != nil {
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

// table renders the closed loop's lines.
func (r EndpointReport) table(b *strings.Builder) {
	line := func(name, value string) { fmt.Fprintf(b, "  %-28s %s\n", name, value) }
	b.WriteString("\nendpoints: what each simulated endpoint observed, reconciled with what its node sent\n")
	for _, p := range r.Protocols {
		line(p.Protocol+" received", fmt.Sprintf("%s   (%s poll targets; accepted %s, answered %s, refused %s, error %s, dropped %s, withheld %s, streams %s)",
			count(float64(p.Received)), count(float64(p.PollTargets)), count(float64(p.Accepted)), count(float64(p.Answered)), count(float64(p.Refused)),
			count(float64(p.Error)), count(float64(p.Dropped)), count(float64(p.Withheld)), count(float64(p.StreamsOpened))))
		if p.Latency.N > 0 {
			line(p.Protocol+" service time", fmt.Sprintf("p50 %.2f ms, p95 %.2f ms, p99 %.2f ms   (%s settled)",
				p.Latency.P50, p.Latency.P95, p.Latency.P99, count(float64(p.Latency.N))))
		}
		line(p.Protocol+" overlap-ratio", fmt.Sprintf("%.3f   (%s requests found their face busy)", p.OverlapRatio, count(float64(p.Overlapped))))
		var highs []string
		for _, n := range sortedKeys(p.InFlightHigh) {
			highs = append(highs, fmt.Sprintf("%s %d", n, p.InFlightHigh[n]))
		}
		line(p.Protocol+" in flight high", strings.Join(highs, ", "))
	}
	gauge := func(v *int64) string {
		if v == nil {
			return "not wired yet"
		}
		return fmt.Sprint(*v)
	}
	for _, n := range r.Nodes {
		var flags string
		if n.Impaired {
			flags += ", impaired"
		}
		if n.Restarted {
			flags += ", restarted"
		}
		line(n.Node+" arrival high", fmt.Sprintf("%d   (dispatch queue depth %s, busy workers %s%s)",
			n.ArrivalHigh, gauge(n.DispatchQueueDepth), gauge(n.BusyWorkers), flags))
	}
	for _, g := range r.GapTotals {
		v := fmt.Sprintf("%s lost", count(float64(g.Loss)))
		if g.Impaired > 0 {
			v += fmt.Sprintf(", %s impaired", count(float64(g.Impaired)))
		}
		if g.AnsweredLate > 0 {
			v += fmt.Sprintf(", %s answered late", count(float64(g.AnsweredLate)))
		}
		if g.Masked > 0 {
			v += fmt.Sprintf("   (masked %s: %s)", count(float64(g.Masked)), g.MaskedBy)
		}
		line(g.Gap, v)
	}
	// Each node's entries past a masking term alone, so a loss names where.
	for _, g := range r.Gaps {
		if g.Count == 0 && g.Reading == ReadingLoss {
			continue
		}
		what := g.Protocol
		if g.ComponentID != "" {
			what += " " + g.ComponentID
		}
		line("  "+g.Node+" "+g.Gap, strings.TrimSpace(fmt.Sprintf("%s %d %s", what, g.Count, g.Reading)))
	}
	if len(r.WorstFaces) > 0 {
		b.WriteString("  worst faces since the sim started (not windowed):\n")
		for _, f := range r.WorstFaces {
			fmt.Fprintf(b, "    %s %s %s %s: %d of %d overlapped\n", f.Node, f.DeviceID, f.Face, f.Protocol, f.Overlapped, f.Received)
		}
	}
}

func sortedKeys(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
