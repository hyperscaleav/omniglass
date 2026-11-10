package delivery

import (
	"slices"
	"strings"
)

// HopInstance is the instance a hop the node measures besides its own spool
// writes under: the hop and the kind, "schedule/metric" (#1693). The bare
// kind stays the spool's (the node hop's), so the two never share a series.
func HopInstance(h Hop, k Kind) string { return string(h) + "/" + string(k) }

// ParseHopInstance reads a HopInstance back, ok false for anything that is not
// a known hop and a known kind.
func ParseHopInstance(s string) (Hop, Kind, bool) {
	h, k, found := strings.Cut(s, "/")
	if !found || !slices.Contains(Hops, Hop(h)) || !slices.Contains(Kinds, Kind(k)) {
		return "", "", false
	}
	return Hop(h), Kind(k), true
}

// Scheduled counts the schedule hop (#1693) for one task: made runs fell due
// and were made, skipped runs fell due and were not (the task was still
// running, or its slot passed). Each run counts once for every kind it
// produces, so a kind's loss is its skipped dues over its dues.
func (c *Counts) Scheduled(kinds []Kind, made, skipped int) {
	for _, k := range kinds {
		c.add(k, made+skipped, made)
	}
}

// HopReadings is Readings under hop h's instances, HopInstance(h, kind), for a
// hop the node measures beside its spool.
func (c Counts) HopReadings(h Hop) []Reading {
	rs := c.Readings()
	for i := range rs {
		rs[i].Instance = HopInstance(h, Kind(rs[i].Instance))
	}
	return rs
}

// The schedule hop's outcomes (#1693): a due was made, or skipped. The
// dispatcher counts them per run, never a batch's tally, so they are no
// member of Outcomes, which is every outcome a receipt can carry.
const (
	// Made is a due the dispatcher ran: out of the schedule hop.
	Made Outcome = "made"
	// Skipped is a due the dispatcher passed over, the task still running
	// or its slot gone by: the schedule hop's loss.
	Skipped Outcome = "skipped"
)
