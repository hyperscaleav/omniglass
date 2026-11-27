package behave

import (
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// PowerParams names what a device that follows the room reads as on, asleep
// and as the input a meeting wants.
type PowerParams struct {
	On, Standby string
	// Conferencing is the input a wake selects.
	Conferencing string
}

// DefaultPower is what the displays and projectors report.
var DefaultPower = PowerParams{On: "on", Standby: "standby", Conferencing: "hdmi1"}

// PowerState is a device that follows the room: its power, and the
// bookkeeping that makes a wake an edge, not a level held every tick.
type PowerState struct {
	Power string
	// Busy is a meeting someone attends running, or about to (waking).
	Busy bool
	// Known is a meeting running now or one ahead: the room has a calendar
	// that says when to sleep.
	Known bool
	// Awake is busy, or inside the cooldown tail that follows a call.
	Awake bool
	// Until is when the last call's cooldown ends (unix seconds).
	Until int64
}

// PowerFollowsRoom is the next power of a device that wakes for the room's
// meetings and sleeps after them. It wakes on the edge of the room going busy
// (the wake lead before a booked meeting someone attends, the start of a
// walk-in) and sleeps once the cooldown has run, but only if the calendar
// knows the room: an operator's change inside a meeting holds, a no-show wakes
// nothing, and a room with no calendar leaves the power as it was. woke is the
// edge, which InputFollowsMeeting reads.
func PowerFollowsRoom(prev PowerState, room calendar.RoomState, now time.Time, p PowerParams) (next PowerState, woke bool) {
	next = prev
	next.Busy = room.CallActive || room.Occupancy == calendar.PhaseWaking
	next.Known = room.Scheduled
	if room.CallActive {
		next.Until = room.UseUntil
	}
	switch {
	case next.Busy:
		if !prev.Awake {
			next.Power, woke = p.On, true
		}
	case next.Known && now.Unix() >= next.Until:
		next.Power = p.Standby
	}
	next.Awake = next.Busy || now.Unix() < next.Until
	return next, woke
}

// InputFollowsMeeting is the input after the room woke the device: the
// conferencing input on the wake edge, whatever an operator chose otherwise.
func InputFollowsMeeting(prev string, woke bool, p PowerParams) string {
	if woke {
		return p.Conferencing
	}
	return prev
}

// ControlState is a room controller's system power and the last macro its
// program ran.
type ControlState struct {
	RoomPower, Macro string
	// MacroAt is the unix second the macro ran.
	MacroAt int64
}

// MacroParams names a controller's room power and macros.
type MacroParams struct {
	On, Standby          string
	StartMacro, EndMacro string
}

// DefaultMacros are the control processor's.
var DefaultMacros = MacroParams{On: "on", Standby: "standby", StartMacro: "start-meeting", EndMacro: "end-meeting"}

// RoomPower is the next state of a room controller that starts the room for a
// meeting and shuts it down after: its power is on exactly while the room is
// in use (the window the displays wake in), and it runs the start macro as the
// room goes on and the end macro as it goes to standby, the macro chosen
// against the power the room had, so it fires on the edge only and the last
// one and the second it ran are held beside it. A controller in a room with no
// calendar keeps its state.
func RoomPower(prev ControlState, room calendar.RoomState, now time.Time, p MacroParams) ControlState {
	if !room.HasCalendar {
		return prev
	}
	next := prev
	switch {
	case room.InUse && prev.RoomPower == p.Standby:
		next.Macro, next.MacroAt = p.StartMacro, now.Unix()
	case !room.InUse && prev.RoomPower == p.On:
		next.Macro, next.MacroAt = p.EndMacro, now.Unix()
	}
	if room.InUse {
		next.RoomPower = p.On
	} else {
		next.RoomPower = p.Standby
	}
	return next
}
