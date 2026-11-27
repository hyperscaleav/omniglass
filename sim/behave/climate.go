package behave

import "github.com/hyperscaleav/omniglass/sim/calendar"

// ClimateState is a room's temperature as a controller reads it.
type ClimateState struct {
	// TemperatureC is the reading: ClimateC with noise.
	TemperatureC float64
	// ClimateC is the underlying temperature the reading is taken from.
	ClimateC float64
	// SetpointC is where the room settles with nobody in it; LoadCPerPerson
	// the warmth each attendee adds toward it.
	SetpointC, LoadCPerPerson float64
}

// ClimateParams are the room climate's rules.
type ClimateParams struct {
	// Approach is the share of the way to its target the temperature moves a
	// tick; MaxRiseC the most the people in a room warm its target; NoiseC
	// the noise on the reading.
	Approach, MaxRiseC, NoiseC float64
}

// DefaultClimate is the control processor's room.
var DefaultClimate = ClimateParams{Approach: 0.2, MaxRiseC: 4, NoiseC: 0.1}

// Climate is the room's next temperature: the underlying value moves
// Approach of the way to its target, the setpoint plus the load of the room's
// attendees (to MaxRiseC) while the room is in use and the setpoint otherwise;
// the reading is the value before the move with NoiseC of noise (one draw). A
// room with no calendar sits at its setpoint, with the same noise.
func Climate(prev ClimateState, room calendar.RoomState, p ClimateParams, rng Source) ClimateState {
	next := prev
	next.TemperatureC = Round(1, Step(rng, p.NoiseC, prev.ClimateC))
	if !room.HasCalendar {
		next.ClimateC = prev.SetpointC
		return next
	}
	target := prev.SetpointC
	if room.InUse {
		target = prev.SetpointC + Clamp(0, p.MaxRiseC, prev.LoadCPerPerson*float64(room.Attendees))
	}
	next.ClimateC = Round(3, prev.ClimateC+p.Approach*(target-prev.ClimateC))
	return next
}
