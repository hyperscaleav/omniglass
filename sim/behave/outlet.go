package behave

import "github.com/hyperscaleav/omniglass/sim/calendar"

// OutletParams are what one PDU outlet draws, in milliamps.
type OutletParams struct {
	// Idle is the draw with the room idle, InUse while it is in use and InCall
	// while a call has someone in it (zero InCall: no call level).
	Idle, InUse, InCall int
	// CalendarOff is an outlet that draws nothing in a room with a calendar:
	// nothing is plugged into it, and only a room without one walks.
	CalendarOff bool
	// Walk is the drift of an outlet in a room with no calendar.
	Walk Band
}

// DefaultOutletWalk is the drift of a PDU outlet with no calendar, in
// milliamps.
var DefaultOutletWalk = Band{Lo: 100, Hi: 1200, Step: 40}

// OutletCurrent is an outlet's next draw in milliamps: nothing when it is off,
// the level its gear is at for the room (idle, in use, in a call with someone
// in it) when the room has a calendar, and the walk when it has none. At most
// one draw, only on the walk.
func OutletCurrent(prev int, on bool, room calendar.RoomState, p OutletParams, rng Source) int {
	if !on {
		return 0
	}
	if !room.HasCalendar {
		return int(Round(0, Clamp(p.Walk.Lo, p.Walk.Hi, Step(rng, p.Walk.Step, float64(prev)))))
	}
	if p.CalendarOff {
		return 0
	}
	v := p.Idle
	if room.InUse {
		v = p.InUse
	}
	if p.InCall > 0 && room.CallActive && room.Attendees > 0 {
		v = p.InCall
	}
	return v
}
