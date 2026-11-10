package closedloop

import (
	"slices"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// GapRatio is one gap on one protocol read as a loss ratio: Count over In,
// the figure the hop could have lost it from. 0 is healthy. Ratio is nil when
// nothing went in (a ratio over nothing is absent, never 0), when the gap is
// not observed by the sim, and on a restarted node.
type GapRatio struct {
	ClosedLoopGap
	In    int64    `json:"in"`
	Ratio *float64 `json:"ratio"`
}

// Ratio is count over in; false when in is 0, so there is no ratio to read.
func Ratio(count, in int64) (float64, bool) {
	if in <= 0 {
		return 0, false
	}
	return float64(count) / float64(in), true
}

// Ratios reads every gap of one node's window as a ratio, the zeroes
// Reconcile leaves out included, in travel order and then by protocol. A
// node whose counters reset reads its one restarted entry.
func Ratios(b NodeBracket) []GapRatio {
	gaps := Reconcile(b)
	if restarted(b) {
		return []GapRatio{{ClosedLoopGap: gaps[0]}}
	}
	reading := ReadingLoss
	if impaired(b) {
		reading = ReadingImpaired
	}
	type key struct{ gap, protocol string }
	rows := map[key]*GapRatio{}
	var order []key
	put := func(gap, protocol string, in int64) {
		k := key{gap, protocol}
		if r, ok := rows[k]; ok {
			r.In += in
			return
		}
		rows[k] = &GapRatio{ClosedLoopGap: ClosedLoopGap{Gap: gap, Node: b.Node, Protocol: protocol, Reading: reading}, In: max(in, 0)}
		order = append(order, k)
	}

	s1a, s2b, s2 := b.Start.SimAfter, b.End.SimBefore, b.End.SimAfter
	put(GapConnectNotAccepted, "", nodeConnections(b))
	replied := func(o collection.SimProtocolObservation) int64 { return o.Answered + o.Refused + o.Error }
	for _, n2 := range b.End.Node.Transports {
		p := n2.Transport
		n1 := tallyRow(b.Start.Node, p)
		put(GapSentNotReceived, p, n2.Sent-n1.Sent)
		// Reconcile reads answers over the narrowest sim span, so that span's
		// answers are what could have been lost on the way back.
		put(GapAnsweredNotReturned, p, replied(s2b.Protocol(p))-replied(s1a.Protocol(p)))
	}
	// Reconcile counts what went unsettled after the start's residual, so
	// what the sim received from the start's second read is what it could
	// have left unsettled.
	for _, p := range s2.Protocols {
		put(GapReceivedNotSettled, p.Protocol, p.Received-s1a.Protocol(p.Protocol).Received)
	}
	acknowledged := b.End.Node.BatchesAcknowledged - b.Start.Node.BatchesAcknowledged
	put(GapPublishedNotLanded, "", acknowledged)
	put(GapSpoolDropped, "", acknowledged+b.End.Node.BatchesDropped-b.Start.Node.BatchesDropped)
	for _, t := range b.Targets {
		if p, ok := dueProtocol(t, b.Window); ok {
			put(GapUnhitEndpoint, p, 1)
		} else if t.Transport == "icmp" && due(t, b.Window) {
			put(GapUnhitEndpoint, "icmp", 1)
			rows[key{GapUnhitEndpoint, "icmp"}].Reading = ReadingNotObserved
		}
	}

	// Fold Reconcile's counts in; unhit arrives one row per component plus
	// its sample's masking term, so counts on one protocol sum.
	for _, g := range gaps {
		k := key{g.Gap, g.Protocol}
		r, ok := rows[k]
		if !ok {
			put(g.Gap, g.Protocol, 0)
			r = rows[k]
		}
		r.Count += g.Count
		r.Masked += g.Masked
		r.AnsweredLate += g.AnsweredLate
		r.Reading = g.Reading
		if r.MaskedBy == "" {
			r.MaskedBy = g.MaskedBy
		}
	}

	travel := GapNames()
	slices.SortStableFunc(order, func(a, c key) int {
		if d := slices.Index(travel, a.gap) - slices.Index(travel, c.gap); d != 0 {
			return d
		}
		return strings.Compare(a.protocol, c.protocol)
	})
	out := make([]GapRatio, 0, len(order))
	for _, k := range order {
		r := *rows[k]
		if v, ok := Ratio(r.Count, r.In); ok && r.Reading != ReadingNotObserved {
			r.Ratio = &v
		}
		out = append(out, r)
	}
	return out
}
