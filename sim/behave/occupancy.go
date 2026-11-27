package behave

import "github.com/hyperscaleav/omniglass/sim/calendar"

// OccupancyGate is what makes a counter read the calendar rather than fall
// back.
type OccupancyGate int

const (
	// GateScheduled reads the calendar while a meeting runs or one lies
	// ahead (a calendar always has a next one, so neither means no calendar).
	GateScheduled OccupancyGate = iota
	// GateCalendar reads it whenever the room has meeting knobs.
	GateCalendar
)

// WalkParams is the random walk of a counter with no calendar.
type WalkParams struct{ Step, Max float64 }

// OccupancyParams are a people counter's rules.
type OccupancyParams struct {
	// MissChance is the chance a tick misses one person in a meeting of two
	// or more, as someone steps out.
	MissChance float64
	Gate       OccupancyGate
	// Walk is the walk a counter with no calendar takes; nil counts the
	// device's own call instead.
	Walk *WalkParams
}

// DefaultOccupancy is the Tessellate census counter's rules (a walk when it
// has no calendar). The Kestrel bar's camera counts the call instead:
// CallCount is the same with a nil Walk and GateCalendar.
var DefaultOccupancy = OccupancyParams{MissChance: 0.15, Gate: GateScheduled, Walk: &WalkParams{Step: 2, Max: 14}}

// CallCount is the people counter of a device that counts the call it is in
// when its room has no calendar.
var CallCount = OccupancyParams{MissChance: 0.15, Gate: GateCalendar}

// OccupancyState is a counter's reading and the signals derived from it.
type OccupancyState struct {
	Count, Prev int
	// LastEvent is increased, decreased or steady against the tick before.
	LastEvent string
	Vacant    bool
}

// PeopleCount is the next reading of a people counter: the calendar's actual
// attendees (zero in a no-show and in an empty room), less the one who stepped
// out on MissChance of ticks, and with no calendar the walk or the device's
// own call (callPeers). It draws once at most, and only in a meeting of two or
// more.
func PeopleCount(prev OccupancyState, room calendar.RoomState, callPeers int, p OccupancyParams, rng Source) OccupancyState {
	next := OccupancyState{Prev: prev.Count}
	gated := room.Scheduled
	if p.Gate == GateCalendar {
		gated = room.HasCalendar
	}
	switch {
	case gated:
		next.Count = room.Attendees
		if next.Count > 1 && rng.Chance(p.MissChance) {
			next.Count--
		}
	case p.Walk != nil:
		next.Count = int(Round(0, Clamp(0, p.Walk.Max, Step(rng, p.Walk.Step, float64(prev.Count)))))
	default:
		next.Count = callPeers
	}
	switch delta := next.Count - next.Prev; {
	case delta > 0:
		next.LastEvent = "increased"
	case delta < 0:
		next.LastEvent = "decreased"
	default:
		next.LastEvent = "steady"
	}
	next.Vacant = next.Count == 0
	return next
}
