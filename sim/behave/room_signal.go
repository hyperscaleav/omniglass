package behave

import "github.com/hyperscaleav/omniglass/sim/calendar"

// PanelState is a touch panel that follows its room: whether it is awake, and
// its active page.
type PanelState struct {
	Awake bool
	Page  string
}

// PanelParams names the pages a panel shows.
type PanelParams struct{ Idle, InCall, Sharing string }

// DefaultPanel is the Newtron touch panel's.
var DefaultPanel = PanelParams{Idle: "idle", InCall: "in-call", Sharing: "sharing"}

// PanelFollowsRoom is the next state of a panel that wakes for its room's
// meetings and sleeps after them: it is awake exactly while the room is in use
// (the window the displays wake in), and its page is the call while a meeting
// someone attends is held, sharing through the stretch content is shared, and
// idle otherwise, a no-show included. A panel in a room with no calendar keeps
// its state.
func PanelFollowsRoom(prev PanelState, room calendar.RoomState, p PanelParams) PanelState {
	if !room.HasCalendar {
		return prev
	}
	next := PanelState{Awake: room.InUse, Page: p.Idle}
	if room.CallActive {
		next.Page = p.InCall
		if room.Sharing {
			next.Page = p.Sharing
		}
	}
	return next
}

// RouteState is a presentation switcher that follows its room: the input it
// routes, the signal on it, and the input the meeting last asked for and the
// one before it (the route is an edge, not a level).
type RouteState struct {
	Input, Signal string
	Want, Prev    string
}

// RouteParams names the inputs a meeting asks for.
type RouteParams struct {
	// Conferencing is the codec's feed, wanted while a call is held; Content
	// the lectern laptop, wanted through the stretch content is shared; Idle
	// the input nothing is plugged into, wanted between meetings.
	Conferencing, Content, Idle string
}

// DefaultRoute is the Meridian MX 84's.
var DefaultRoute = RouteParams{Conferencing: "hdmi2", Content: "hdmi1", Idle: "dp1"}

// RouteFollowsRoom is the next state of a switcher that routes what the room's
// meeting wants: the wanted input is written when it changes, so an operator's
// route inside a meeting holds until the meeting asks for another, and the
// signal read is the signal on the input now routed (signals, by input). A
// switcher in a room with no calendar keeps its route and signal and, with
// them, the wanted input it had.
func RouteFollowsRoom(prev RouteState, room calendar.RoomState, signals map[string]string, p RouteParams) RouteState {
	next := prev
	next.Prev = prev.Want
	if !room.HasCalendar {
		return next
	}
	switch {
	case !room.CallActive:
		next.Want = p.Idle
	case room.Sharing:
		next.Want = p.Content
	default:
		next.Want = p.Conferencing
	}
	if next.Want != next.Prev {
		next.Input = next.Want
	}
	next.Signal = SignalOnInput(next.Signal, next.Input, signals)
	return next
}

// SignalOnInput is the signal a switcher reports for its routed input: what
// that input's own signal reads, so a source pulled from the routed input drops
// it on the next tick. An input the switcher does not have leaves the reading
// as it was.
func SignalOnInput(prev, input string, signals map[string]string) string {
	if s, ok := signals[input]; ok {
		return s
	}
	return prev
}

// ExtenderParams names the signal an extender reports.
type ExtenderParams struct{ Present, Absent string }

// DefaultExtender is the Meridian XL-1's.
var DefaultExtender = ExtenderParams{Present: "present", Absent: "absent"}

// ExtenderSignal is the signal on an extender's input: present while a meeting
// someone attends is held, the switcher routing a source onto the cable, and
// absent between meetings and through a no-show. An extender in a room with no
// calendar keeps its signal.
func ExtenderSignal(prev string, room calendar.RoomState, p ExtenderParams) string {
	if !room.HasCalendar {
		return prev
	}
	if room.CallActive {
		return p.Present
	}
	return p.Absent
}
