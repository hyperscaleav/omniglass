package behave

import "github.com/hyperscaleav/omniglass/sim/calendar"

// MicState is a microphone's activity, mute and input level.
type MicState struct {
	// Active is the mic hearing a talker (a transmitting pack, an array in a
	// call); Mute the user's mute.
	Active, Mute bool
	LevelDB      float64
}

// Band is a level's limits and the step it walks within them.
type Band struct{ Lo, Hi, Step float64 }

// MicParams are a microphone's rules.
type MicParams struct {
	// MuteFlipChance is the chance a tick flips the mute while the room has
	// someone in it.
	MuteFlipChance float64
	// Speech is the band the level walks while the room is in use and the mic
	// open; a level below Quiet when it opens rises to Opening first.
	Speech         Band
	Quiet, Opening float64
	// Floor is the noise floor the level sits at otherwise: it is drawn as
	// Center with FloorBand's step, within FloorBand's limits.
	FloorBand Band
	Center    float64
	// Unplaced is the band a mic in a room with no calendar walks, a behavior
	// a scenario can still drive.
	Unplaced Band
}

// DefaultMic is the Lyra microphones' rules.
var DefaultMic = MicParams{
	MuteFlipChance: 0.06,
	Speech:         Band{Lo: -55, Hi: -20, Step: 4}, Quiet: -55, Opening: -40,
	FloorBand: Band{Lo: -72, Hi: -66, Step: 1.5}, Center: -69,
	Unplaced: Band{Lo: -70, Hi: -20, Step: 4},
}

// MicActivity is the next activity and mute of a microphone that hears its
// room: active while anyone attends, and the mute flipping now and then while
// someone does (one draw, only then). A mic in a room with no calendar keeps
// its activity and mute.
func MicActivity(prev MicState, room calendar.RoomState, p MicParams, rng Source) MicState {
	next := prev
	if !room.HasCalendar {
		return next
	}
	next.Active = room.Attendees > 0
	if room.Attendees > 0 && rng.Chance(p.MuteFlipChance) {
		next.Mute = !prev.Mute
	}
	return next
}

// MicLevel is the next input level, read after MicActivity: it rides speech
// within a talker's range while someone is in the room and the mic open, and
// sits at its noise floor otherwise. One draw.
func MicLevel(prev MicState, room calendar.RoomState, p MicParams, rng Source) float64 {
	if !room.HasCalendar {
		return Round(1, Clamp(p.Unplaced.Lo, p.Unplaced.Hi, Step(rng, p.Unplaced.Step, prev.LevelDB)))
	}
	if room.Attendees > 0 && !prev.Mute {
		v := Round(3, prev.LevelDB)
		if v < p.Quiet {
			v = p.Opening
		}
		return Round(1, Clamp(p.Speech.Lo, p.Speech.Hi, Step(rng, p.Speech.Step, v)))
	}
	return Round(1, Clamp(p.FloorBand.Lo, p.FloorBand.Hi, Step(rng, p.FloorBand.Step, p.Center)))
}
