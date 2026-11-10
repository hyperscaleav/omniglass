package closedloop

import (
	"cmp"
	"slices"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// SimHopCount is the sim hop (#1694) on one protocol: In is every request the
// simulated endpoints settled inside the window on traffic the node tallies,
// Lost the ones whose reply never reached the node. Lost splits by why: the
// sim withheld the answer (a wrong community, its own datagram loss), the
// sim could not write it (dropped), or it left the sim and never came back
// (not returned: lost on the way, or late past the node's timeout). The
// counts are requests, not telemetry items: the sim cannot know which kinds
// an answer would have carried.
type SimHopCount struct {
	Protocol    string `json:"protocol"`
	In          int64  `json:"in"`
	Lost        int64  `json:"lost"`
	Withheld    int64  `json:"withheld"`
	Dropped     int64  `json:"dropped"`
	NotReturned int64  `json:"not_returned"`
}

// SimHop reads one node's sim hop, a count per protocol the node tallied that
// the sim settled anything on, in protocol order. It reads the narrowest sim
// span (the start's second read to the end's first), as the
// answered-not-returned gap does: a reply the sim settled there either came
// back, is in flight at the node's end read, belongs to traffic the node does
// not tally, or was lost. A refusal or error that came back is returned: the
// device said no, and the task reports it. A node that restarted reads
// nothing, since no figure for it is meaningful.
func SimHop(b NodeBracket) []SimHopCount {
	if restarted(b) {
		return nil
	}
	s1, s1a, s2b, s2 := b.Start.SimBefore, b.Start.SimAfter, b.End.SimBefore, b.End.SimAfter
	var out []SimHopCount
	for _, n2 := range b.End.Node.Transports {
		p := n2.Transport
		n1 := tallyRow(b.Start.Node, p)
		got := func(c collection.TallyCounters) int64 { return c.Answered + c.Refused + c.Error }
		received := s2.Protocol(p).Received - s1.Protocol(p).Received
		untallied := max(received-(n2.Sent-n1.Sent), 0)
		in := max(s2b.Protocol(p).Settled()-s1a.Protocol(p).Settled()-untallied, 0)
		if in == 0 {
			continue
		}
		lost := min(max(in-(got(n2)-got(n1))-n2.InFlight, 0), in)
		withheld := min(max(s2b.Protocol(p).Withheld-s1a.Protocol(p).Withheld, 0), lost)
		dropped := min(max(s2b.Protocol(p).Dropped-s1a.Protocol(p).Dropped, 0), lost-withheld)
		out = append(out, SimHopCount{Protocol: p, In: in, Lost: lost, Withheld: withheld, Dropped: dropped,
			NotReturned: lost - withheld - dropped})
	}
	slices.SortFunc(out, func(a, b SimHopCount) int { return cmp.Compare(a.Protocol, b.Protocol) })
	return out
}

// SimHops is the window's sim hop over every node, summed per protocol, in
// protocol order; nil when no node settled anything it tallies.
func SimHops(brackets []NodeBracket) []SimHopCount {
	sum := map[string]SimHopCount{}
	for _, b := range brackets {
		for _, c := range SimHop(b) {
			s := sum[c.Protocol]
			s.Protocol = c.Protocol
			s.In += c.In
			s.Lost += c.Lost
			s.Withheld += c.Withheld
			s.Dropped += c.Dropped
			s.NotReturned += c.NotReturned
			sum[c.Protocol] = s
		}
	}
	if len(sum) == 0 {
		return nil
	}
	out := make([]SimHopCount, 0, len(sum))
	for _, c := range sum {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b SimHopCount) int { return cmp.Compare(a.Protocol, b.Protocol) })
	return out
}
