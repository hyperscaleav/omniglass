package behave

import "github.com/hyperscaleav/omniglass/sim/calendar"

// Strip is one input of a mixer: the user's mute, whether its gate is open and
// its peak level.
type Strip struct {
	Muted, Gate bool
	PeakDB      float64
}

// GateState is a mixer's inputs and how many gates are open.
type GateState struct {
	Active int
	Strips []Strip
}

// GateParams are a mixer's rules.
type GateParams struct {
	// Opens is, per strip, the head count the room must exceed for a talker
	// to be at it.
	Opens []int
	// Base and PerHead set an open gate's peak, Base plus PerHead for each
	// attendee, with Jitter of noise, within Peak.
	Base, PerHead, Jitter float64
	Peak                  Band
	// Floor is a shut gate's noise: Center with Noise of jitter, within Shut.
	Center, Noise float64
	Shut          Band
}

// DefaultGates is the Polaris DSP's four table and ceiling strips, one opening
// for every two attendees.
var DefaultGates = GateParams{
	Opens: []int{0, 2, 4, 6},
	Base:  -52, PerHead: 2, Jitter: 1.5, Peak: Band{Lo: -60, Hi: -12},
	Center: -86, Noise: 2, Shut: Band{Lo: -90, Hi: -82},
}

// Gates is the mixer's talkers for the room: each strip opens a gate for every
// two attendees actually in the room (a no-show opens none), each open gate's
// peak rises with the head count, and a shut gate sits at the noise floor. A
// strip an operator muted stays shut. One draw per strip, in order. A mixer in
// a room with no calendar keeps its strips as declared.
func Gates(prev GateState, room calendar.RoomState, p GateParams, rng Source) GateState {
	if !room.HasCalendar {
		return prev
	}
	next := GateState{Strips: make([]Strip, len(prev.Strips))}
	for i, s := range prev.Strips {
		on := i < len(p.Opens) && room.Attendees > p.Opens[i]
		open := on && !s.Muted
		next.Strips[i] = Strip{Muted: s.Muted, Gate: open}
		if open {
			next.Active++
			next.Strips[i].PeakDB = Round(1, Clamp(p.Peak.Lo, p.Peak.Hi, p.Base+p.PerHead*float64(room.Attendees)+Step(rng, p.Jitter, 0)))
		} else {
			next.Strips[i].PeakDB = Round(1, Clamp(p.Shut.Lo, p.Shut.Hi, Step(rng, p.Noise, p.Center)))
		}
	}
	return next
}
