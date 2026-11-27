package behave

import "github.com/hyperscaleav/omniglass/sim/calendar"

// ShareState is a device's content sharing and the calendar reading behind it.
type ShareState struct {
	Sharing bool
	// Prev is the sharing the room last read. The calendar's sharing is an
	// edge: an operator's share or stop holds until it next changes.
	Prev bool
}

// ShareParams are a sharing device's rules.
type ShareParams struct {
	// ToggleChance is the chance a tick flips sharing in a call the calendar
	// does not speak for (no calendar, or a dialed call).
	ToggleChance float64
	// HoldIdle keeps sharing as it was when there is no call, ending it only
	// on the tick the call ends; else sharing is off outside a call.
	HoldIdle bool
}

// DefaultShare is the sharing rules of the Kestrel conferencing devices.
var DefaultShare = ShareParams{ToggleChance: 0.05}

// CallSignals are the facts of the device's call a sharing reading needs,
// read after the call's own tick.
type CallSignals struct {
	Active, Dialed bool
	// Ended says the call ended this tick.
	Ended bool
}

// ContentSharing is the next sharing of a device in a call. In a calendar
// room's own call it shares exactly through the stretch the room phase says
// (the middle third of a long enough meeting), changing only when that
// changes; in a dialed call or a room with no calendar it flips now and then
// on its own draw (one, only when it reaches it).
func ContentSharing(prev ShareState, room calendar.RoomState, call CallSignals, p ShareParams, rng Source) ShareState {
	next := prev
	switch {
	case call.Active && room.HasCalendar && !call.Dialed:
		if room.Sharing != prev.Prev {
			next.Sharing = room.Sharing
		}
	case call.Active:
		if rng.Chance(p.ToggleChance) {
			next.Sharing = !prev.Sharing
		}
	case call.Ended || !p.HoldIdle:
		next.Sharing = false
	}
	if room.HasCalendar {
		next.Prev = room.Sharing
	}
	return next
}

// ContentSource is the source of what a device shares: none while it is not
// sharing, the source it had while it is, and a draw from sources when
// sharing starts. It draws once, only then.
func ContentSource(prev string, sharing bool, sources []string, rng Source) string {
	switch {
	case !sharing:
		return ""
	case prev != "":
		return prev
	}
	return sources[rng.IntN(1, int64(len(sources)))-1]
}
