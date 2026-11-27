package behave

import (
	"fmt"
	"math"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// CallEvent is what one tick of a call did.
type CallEvent string

// The events a tick can produce; the empty event is a tick that changed
// nothing about who is in the call.
const (
	EventNone  CallEvent = ""
	EventStart CallEvent = "start"
	EventEnd   CallEvent = "end"
	// EventTurn is the room's meeting changing under a running call: the old
	// call ends and the new one starts in one tick.
	EventTurn  CallEvent = "turn"
	EventJoin  CallEvent = "join"
	EventLeave CallEvent = "leave"
)

// CallState is a conferencing device's call and the bookkeeping behind it.
type CallState struct {
	Active       bool
	ID           string
	Participants int

	// Event is what the last tick did; IDPrev and ParticipantsPrev are the
	// call as it stood before it, for the events that close a call.
	Event            CallEvent
	IDPrev           string
	ParticipantsPrev int

	// Cal latches once the room has shown a calendar: a bar that has followed
	// one keeps following it.
	Cal bool
	// Skip is the start of a meeting a dial overtook or a hang-up ended: it
	// is not joined.
	Skip int64
	// Want is the call the room should be in ("c<start>", empty for none) and
	// WantN is who came.
	Want  string
	WantN int
	// Hungup and Dialed are the operator's: a hang-up and a dialed call.
	// DialTicks counts a dialed call's ticks toward DialLength.
	Hungup, Dialed        bool
	DialTicks, DialLength int
	CallSeq               int

	// The device's own schedule, for a room with no calendar.
	Plan                     bool
	Slot                     int
	Boundary, StartAt, EndAt int64
	PlanAt                   int64
	Booked                   bool
}

// CallParams are a conferencing device's call rules.
type CallParams struct {
	// Churn is the chance a tick moves someone in or out of a call; both
	// directions are possible between 1 and the head count wanted, and
	// SplitChance picks join or leave in between.
	Churn, SplitChance float64
	// DialedPeers is the head count a dialed call is open to.
	DialedPeers int
	// DialEnds says a dialed call ends after DialLength ticks of its own.
	DialEnds bool
	// ClearIDOnEnd empties the call's identity when it ends (else it is held).
	ClearIDOnEnd bool
	// Standalone, when set, is the device's own schedule for a room with no
	// calendar, and makes the calendar a latch (Cal). Nil is a device that
	// reads the calendar alone.
	Standalone *StandaloneParams
}

// StandaloneParams are the own-schedule rules of a bar in a room with no
// calendar: when to plan its day and what a call looks like in it.
type StandaloneParams struct {
	// IDBase numbers its calls ("c9000", "c9001", ...).
	IDBase int
	// JoinBelow is the head count at or under which a churn tick only joins;
	// MaxPeers the count a call never grows past.
	JoinBelow, MaxPeers int
	// OpenMin and OpenMax bound who is in a call when it opens.
	OpenMin, OpenMax int64
	// Slot is the draw of a booked slot's length: a draw in 1..SlotDraws at or
	// under ShortUpTo is a ShortSlot-minute slot, at or under MidUpTo a
	// MidSlot, else LongSlot.
	SlotDraws, ShortUpTo, MidUpTo int64
	ShortSlot, MidSlot, LongSlot  int
	// BoundaryShort is the slot grid, in minutes, a ShortSlot slot lands on;
	// every other slot lands on BoundaryOther.
	BoundaryShort, BoundaryOther int
	// BookChance is the chance a free room books its next slot.
	BookChance float64
	// StartMin and StartMax bound the minutes a booked call starts from its
	// boundary; EndEarlyMax the most minutes it ends before the slot does.
	StartMin, StartMax, EndEarlyMax int64
	// ReplanLead is how long before a free slot ends the device plans again.
	ReplanLead time.Duration
}

// DefaultCall is the call rules of the Kestrel conferencing devices.
var DefaultCall = CallParams{Churn: 0.08, SplitChance: 0.5, DialedPeers: 8}

// DefaultStandalone is the Kestrel bar's own day.
var DefaultStandalone = StandaloneParams{
	IDBase: 9000, JoinBelow: 2, MaxPeers: 8, OpenMin: 2, OpenMax: 6,
	SlotDraws: 20, ShortUpTo: 3, MidUpTo: 12, ShortSlot: 15, MidSlot: 30, LongSlot: 60,
	BoundaryShort: 15, BoundaryOther: 30, BookChance: 0.5,
	StartMin: -2, StartMax: 5, EndEarlyMax: 5, ReplanLead: 120 * time.Second,
}

// FollowsCall is the next call of a conferencing device whose room has a
// meeting running: it starts the call the meeting wants, turns it when the
// meeting changes, ends it when the meeting does, opens it with half to all of
// who came, and lets people join and leave inside it, never past who came. A
// no-show is no call, and a meeting a dial overtook or a hang-up ended is not
// joined. Without a calendar (and with Standalone set) it follows its own
// schedule instead (PlanOwnSchedule lays that out). The draws are the churn
// chance, then the join or leave split, then the head count of a call that
// opens.
func FollowsCall(prev CallState, room calendar.RoomState, now time.Time, p CallParams, rng Source) CallState {
	s := prev
	s.ParticipantsPrev, s.IDPrev = prev.Participants, prev.ID
	if p.Standalone != nil {
		s.Cal = prev.Cal || room.Scheduled
	}
	cal := p.Standalone == nil || s.Cal

	var start int64
	var m calendar.Meeting
	if room.HasMeeting {
		start = room.Meeting.Start.Unix()
		m, _ = room.Meeting.AsMeeting()
	}
	if (prev.Dialed || prev.Hungup) && room.HasMeeting {
		s.Skip = start
	}
	s.Hungup = false
	s.Want, s.WantN = "", 0
	if room.HasMeeting && m.Outcome != calendar.OutcomeNoShow && start != s.Skip {
		s.Want, s.WantN = fmt.Sprintf("c%d", start), m.Actual
	}

	s.Event = callEvent(prev, s, cal, now, p, rng)

	if prev.Active && prev.Dialed && s.Event != EventEnd {
		s.DialTicks = prev.DialTicks + 1
	} else {
		s.DialTicks = 0
	}
	if s.Event == EventEnd {
		s.Dialed = false
	}
	switch s.Event {
	case EventStart, EventTurn:
		s.Active = true
	case EventEnd:
		s.Active = false
	}
	if s.Event == EventStart {
		s.CallSeq = prev.CallSeq + 1
	}
	switch {
	case s.Event == EventTurn || (cal && s.Event == EventStart):
		s.ID = s.Want
	case s.Event == EventStart && p.Standalone != nil:
		s.ID = fmt.Sprintf("c%d", p.Standalone.IDBase+s.CallSeq)
	case s.Event == EventEnd && p.ClearIDOnEnd:
		s.ID = ""
	}
	s.Participants = callParticipants(prev.Participants, s, cal, p, rng)
	return s
}

// callEvent is what the tick does to the call.
func callEvent(prev, s CallState, cal bool, now time.Time, p CallParams, rng Source) CallEvent {
	n := prev.Participants
	move := EventNone
	if cal {
		want := s.WantN
		if prev.Dialed {
			want = p.DialedPeers
		}
		if rng.Chance(p.Churn) {
			switch {
			case n < want && n > 1:
				move = EventLeave
				if rng.Chance(p.SplitChance) {
					move = EventJoin
				}
			case n < want:
				move = EventJoin
			case n > 1:
				move = EventLeave
			}
		}
		switch {
		case prev.Active && prev.Dialed:
			if p.DialEnds && prev.DialTicks+1 >= prev.DialLength {
				return EventEnd
			}
			return move
		case prev.Active && s.Want == "":
			return EventEnd
		case prev.Active && s.Want != prev.ID:
			return EventTurn
		case prev.Active:
			return move
		case s.Want != "" && !prev.Dialed:
			return EventStart
		}
		return EventNone
	}

	sa := p.Standalone
	if rng.Chance(p.Churn) {
		if n <= sa.JoinBelow || (n < sa.MaxPeers && rng.Chance(p.SplitChance)) {
			move = EventJoin
		} else {
			move = EventLeave
		}
	}
	unix := now.Unix()
	switch {
	case prev.Active && prev.Dialed:
		if prev.DialTicks+1 >= prev.DialLength {
			return EventEnd
		}
		return move
	case prev.Active && unix >= prev.EndAt:
		return EventEnd
	case prev.Active:
		return move
	case prev.StartAt > 0 && unix >= prev.StartAt && unix < prev.EndAt:
		return EventStart
	}
	return EventNone
}

// callParticipants is who is in the call after the tick's event.
func callParticipants(n int, s CallState, cal bool, p CallParams, rng Source) int {
	switch {
	case s.Event == EventTurn || (cal && s.Event == EventStart):
		lo := int64(1)
		if s.WantN > 2 {
			lo = int64(math.Round(float64(s.WantN) / 2))
		}
		return int(rng.IntN(lo, int64(s.WantN)))
	case s.Event == EventStart:
		return int(rng.IntN(p.Standalone.OpenMin, p.Standalone.OpenMax))
	case s.Event == EventJoin:
		return n + 1
	case s.Event == EventLeave:
		return n - 1
	case s.Event == EventEnd:
		return 0
	}
	return n
}

// NextSlot is the next boundary of a slot of the given minutes strictly after
// unix t, boundaries counted from the epoch.
func NextSlot(t int64, minutes int) int64 {
	size := int64(minutes) * 60
	return (t/size + 1) * size
}

// PlanOwnSchedule is the own schedule of a conferencing device with no
// calendar, run after the call has moved (call is that tick's FollowsCall
// result): a meeting falling due under a dialed call is skipped, and a free
// device with nothing planned books its next slot or leaves it free. It does
// nothing for a device that has latched a calendar. The draws, in order, are
// the slot, whether it books, the start's jitter and the early end.
func PlanOwnSchedule(call CallState, now time.Time, p StandaloneParams, rng Source) CallState {
	s := call
	unix := now.Unix()
	if s.Active && s.Dialed && s.StartAt > 0 && unix >= s.StartAt {
		s.StartAt = 0
	}
	s.Plan = !s.Cal && !s.Active && unix >= s.PlanAt && (s.StartAt == 0 || unix >= s.EndAt)
	if !s.Plan {
		return s
	}
	switch r := rng.IntN(1, p.SlotDraws); {
	case r <= p.ShortUpTo:
		s.Slot = p.ShortSlot
	case r <= p.MidUpTo:
		s.Slot = p.MidSlot
	default:
		s.Slot = p.LongSlot
	}
	grid := p.BoundaryOther
	if s.Slot == p.ShortSlot {
		grid = p.BoundaryShort
	}
	s.Boundary = NextSlot(unix, grid)
	s.Booked = rng.Chance(p.BookChance)
	if s.Booked {
		s.StartAt = s.Boundary + 60*rng.IntN(p.StartMin, p.StartMax)
		s.EndAt = s.Boundary + 60*int64(s.Slot) - 60*rng.IntN(0, p.EndEarlyMax)
		s.PlanAt = 0
	} else {
		s.StartAt, s.EndAt = 0, 0
		s.PlanAt = s.Boundary + 60*int64(s.Slot) - int64(p.ReplanLead/time.Second)
	}
	return s
}
