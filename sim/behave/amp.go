package behave

import "github.com/hyperscaleav/omniglass/sim/calendar"

// AmpParams are an amplifier's rules.
type AmpParams struct {
	// Temperature: the band it stays in, the temperature it rests at and the
	// one a live amp pulls toward (by Pull of the gap a tick, plus Noise), and
	// the walk of an amp with no calendar.
	Temp        Band
	Rest, Live  float64
	Pull, Noise float64
	// Out is a live channel's level band; Revive the level below which a live
	// channel jumps to its wake level; Floor the silent level (Center, with
	// the band's step, within its limits).
	Out    Band
	Revive float64
	Silent Band
	Center float64
}

// DefaultAmp is the Polaris amplifier's rules.
var DefaultAmp = AmpParams{
	Temp: Band{Lo: 34, Hi: 52, Step: 1}, Rest: 36, Live: 46, Pull: 0.15, Noise: 0.3,
	Out: Band{Lo: -40, Hi: -6, Step: 2}, Revive: -45,
	Silent: Band{Lo: -92, Hi: -88, Step: 1}, Center: -90,
}

// live says the amplifier is driving a room someone is in.
func live(room calendar.RoomState, powerOn bool) bool { return room.Attendees > 0 && powerOn }

// AmpSignalPresent says the amplifier carries a signal: while a room someone
// is in is driven and the amp on. With no calendar it keeps what it had.
func AmpSignalPresent(prev bool, room calendar.RoomState, powerOn bool) bool {
	if !room.HasCalendar {
		return prev
	}
	return live(room, powerOn)
}

// AmpTemperature is the next temperature: noise on a walk toward Live while
// the amp drives a room someone is in and toward Rest otherwise. With no
// calendar it is the plain walk. One draw.
func AmpTemperature(prev float64, room calendar.RoomState, powerOn bool, p AmpParams, rng Source) float64 {
	if !room.HasCalendar {
		return SigFig(3, Clamp(p.Temp.Lo, p.Temp.Hi, Step(rng, p.Temp.Step, prev)))
	}
	t := Round(3, prev)
	to := p.Rest
	if live(room, powerOn) {
		to = p.Live
	}
	return SigFig(3, Clamp(p.Temp.Lo, p.Temp.Hi, Step(rng, p.Noise, t)+p.Pull*(to-t)))
}

// AmpOutputLevel is the next output level of a channel: it rides program audio
// within the live band while a room someone is in is driven (waking at wake
// when it was at the silent floor) and sits at the silent floor otherwise. With
// no calendar it walks in the live band. One draw.
func AmpOutputLevel(prev float64, room calendar.RoomState, powerOn bool, wake float64, p AmpParams, rng Source) float64 {
	if !room.HasCalendar {
		return Round(1, Clamp(p.Out.Lo, p.Out.Hi, Step(rng, p.Out.Step, prev)))
	}
	if live(room, powerOn) {
		v := Round(3, prev)
		if v < p.Revive {
			v = wake
		}
		return Round(1, Clamp(p.Out.Lo, p.Out.Hi, Step(rng, p.Out.Step, v)))
	}
	return Round(1, Clamp(p.Silent.Lo, p.Silent.Hi, Step(rng, p.Silent.Step, p.Center)))
}
