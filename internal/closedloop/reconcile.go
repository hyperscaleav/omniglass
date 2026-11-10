// Package closedloop is the closed loop (#1639): what each simulated
// endpoint saw arrive, reconciled hop by hop against what its node sent and
// what landed. It is pure arithmetic over reads someone else takes: the lab
// harness (internal/labfootprint) brackets a measured window, and the server
// (Ring) brackets a sliding one, and both read the same gaps.
package closedloop

import (
	"slices"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// This file is the closed loop (#1639): what each simulated endpoint saw
// arrive, reconciled hop by hop against what its node sent and what landed.
// Each node is read twice, at the window's start and end, and each read is a
// bracket of three (the simulator, the node, the simulator again), so a
// request in transit between two reads is a masking term printed beside its
// gap, never a gap. Every gap is a lower bound: positive only where something
// was lost. The arithmetic is pure; the reads are the caller's.

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
	maskedByReceiptEdges = "receipts landed in the margin before the window, in the lane's backlog at its start, or after its end"
	maskedByUnhitSample  = "unhit devices past the sample, not checked against the targets"
)

// ReceiptCount is one node's telemetry receipts around the window [T0, T1].
// Landed counts every receipt landed after T0 minus the acknowledgement wait
// (the generous superset); Margin how many of those landed before T0,
// Backlog how many from T0 until the ingest lane drained past the start read
// (batches the bus acknowledged before the window can land there), and
// AfterEnd how many after T1. Margin, Backlog and AfterEnd are the masking
// term.
type ReceiptCount struct {
	Landed   int64 `json:"landed"`
	Margin   int64 `json:"margin"`
	Backlog  int64 `json:"backlog"`
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
	// Between is the sim reads taken between the window's ends (the ring's
	// brackets inside it), read only for an impairment applied and lifted
	// inside the window. The lab harness takes none.
	Between []collection.SimObservation
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
		Masked: b.Receipts.Margin + b.Receipts.Backlog + b.Receipts.AfterEnd})
	add(ClosedLoopGap{Gap: GapSpoolDropped, Count: b.End.Node.BatchesDropped - b.Start.Node.BatchesDropped})

	out = append(out, unhit(b, reading)...)
	return out
}

// unhit names each poll target the window should have drawn at least twice
// that the sim's last read names as having seen nothing. The sim names a
// capped sample, so devices past it are a masking term. An icmp target is
// never seen by the sim; a push transport is never polled. TODO(#1663): check
// every target against what the sim saw inside the window, not since start.
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

// impaired is whether any of the node's sim reads, those between the ends
// included, carried an applied impairment. TODO(#1662): attribute an impairment to the protocol it
// impairs, and count the changes, so an unimpaired protocol on an impaired
// node still reads loss.
func impaired(b NodeBracket) bool {
	for _, o := range append([]collection.SimObservation{b.Start.SimBefore, b.Start.SimAfter, b.End.SimBefore, b.End.SimAfter}, b.Between...) {
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
