package calendar

import "time"

// Phase is what a room is doing at an instant (#2797): one derived reading
// every device in the room shares, so a processor, a panel, a display, a bar
// and a PDU cannot disagree about it.
type Phase string

const (
	// PhaseIdle is a room nothing is happening in.
	PhaseIdle Phase = "idle"
	// PhaseWaking is the lead before a booked meeting someone will attend.
	PhaseWaking Phase = "waking"
	// PhaseInMeeting is a meeting someone attends, running.
	PhaseInMeeting Phase = "in-meeting"
	// PhaseSharing is a running meeting that is sharing content.
	PhaseSharing Phase = "sharing"
	// PhaseCooldown is the tail after a meeting that took place.
	PhaseCooldown Phase = "cooldown"
	// PhaseImpaired is a room a scripted problem is acting on.
	PhaseImpaired Phase = "impaired"
)

// RoomState is a room at one instant: its phase and the facts the readers
// need. A pure function of the calendar, the room and the instant (StateAt),
// so every process computes the same one.
type RoomState struct {
	// Phase is the room's phase: impaired while a scripted problem acts on it,
	// else Occupancy.
	Phase Phase
	// Occupancy is the phase the meetings alone give (never impaired), the
	// one a reader that keeps its reading whatever goes wrong reads.
	Occupancy Phase
	// HasCalendar says the room has meeting knobs: a device whose room has
	// none keeps its earlier behavior.
	HasCalendar bool
	// Scheduled says a meeting runs now or one lies ahead.
	Scheduled bool
	// InUse is waking, in a meeting, sharing or cooling down: the window a
	// room is on, from the wake lead before a booked meeting someone attends
	// (at the start of a walk-in) to the cooldown tail after it.
	InUse bool
	// CallActive says a meeting someone attends is running; a no-show holds
	// its slot with nobody in it.
	CallActive bool
	// Attendees is how many are in the running meeting, zero when none.
	Attendees int
	// Sharing says the running meeting is sharing content.
	Sharing bool
	// Impaired says a scripted problem acts on the room.
	Impaired bool
	// BuildingOpen says the building the room sits in is inside its hours;
	// set by the caller that knows the building (StateAt leaves it false).
	BuildingOpen bool
	// UseUntil is when the running meeting's cooldown ends (unix seconds),
	// zero unless CallActive.
	UseUntil int64
	// RecordingPending says a recorded meeting ended within the recording
	// tail, so its recording is still being processed.
	RecordingPending bool
	// Meeting is the meeting running now, a no-show included; HasMeeting says
	// there is one.
	Meeting    Entry
	HasMeeting bool
}

// Map is the state as a template reads it: lower-case keys, every key always
// present so a profile never branches on a missing one.
func (s RoomState) Map() map[string]any {
	var meeting any
	if s.HasMeeting {
		meeting = s.Meeting.Map()
	}
	return map[string]any{
		"phase": string(s.Phase), "occupancy": string(s.Occupancy), "has_calendar": s.HasCalendar,
		"scheduled": s.Scheduled, "in_use": s.InUse, "call_active": s.CallActive, "attendees": s.Attendees,
		"sharing": s.Sharing, "impaired": s.Impaired, "building_open": s.BuildingOpen,
		"use_until": s.UseUntil, "recording_pending": s.RecordingPending, "meeting": meeting,
	}
}

// StateAt is the room's state at t (whole seconds), read from the calendar's
// meetings and problems through the room's own knobs. Pure. A nil calendar, or
// a room with no knobs, is idle with HasCalendar false (a problem can still
// impair it).
func (c *Calendar) StateAt(room Room, t time.Time) RoomState {
	t = t.UTC().Truncate(time.Second)
	s := RoomState{Phase: PhaseIdle, Occupancy: PhaseIdle}
	if c == nil {
		return s
	}
	if len(c.Active(room, t, ProblemKinds)) > 0 {
		s.Impaired, s.Phase = true, PhaseImpaired
	}
	k, ok := c.Knobs(room)
	if !ok {
		return s
	}
	tm := k.Timing()
	meetings := c.Entries(room, t.Add(-tm.CooldownTail-time.Second), t.Add(tm.WakeLead+time.Second), MeetingKinds)
	s = meetingPhase(s, meetings, t, tm)
	s.HasCalendar = true
	s.Scheduled = s.HasMeeting
	if !s.Scheduled {
		_, s.Scheduled = c.Next(room, t, MeetingKinds)
	}
	s.RecordingPending = c.recordingPending(room, t, tm.RecordingTail)
	return s
}

// attendees is how many the meeting entry holds, zero for another kind.
func (e Entry) attendees() int {
	m, _ := e.AsMeeting()
	return m.Actual
}

// recordingPending says the last meeting of the room to have ended by t was
// recorded and ended within tail of t.
func (c *Calendar) recordingPending(room Room, t time.Time, tail time.Duration) bool {
	var last Entry
	found := false
	for _, e := range c.Entries(room, t.Add(-tail-time.Second), t.Add(time.Second), MeetingKinds) {
		if !e.End.After(t) {
			last, found = e, true
		}
	}
	if !found || t.Sub(last.End) > tail {
		return false
	}
	m, _ := last.AsMeeting()
	return m.Recorded
}

// meetingPhase reads the meetings around t into s: the running meeting, who
// attends, whether it shares, and the occupancy (waking, in a meeting,
// sharing, cooling down or idle) the lead and the tail give. Pure over the
// meetings it is handed, which are those overlapping the lead before t and
// the tail after.
func meetingPhase(s RoomState, meetings []Entry, t time.Time, tm Timing) RoomState {
	var waking, cooling bool
	for _, e := range meetings {
		m, ok := e.AsMeeting()
		if !ok {
			continue
		}
		running := !t.Before(e.Start) && t.Before(e.End)
		if running && !s.HasMeeting {
			s.Meeting, s.HasMeeting = e, true
		}
		if m.Outcome == OutcomeNoShow {
			continue
		}
		lead := time.Duration(0)
		if m.Origin == OriginBooked {
			lead = tm.WakeLead
		}
		switch {
		case running:
			s.CallActive = true
			s.UseUntil = e.End.Add(tm.CooldownTail).Unix()
			s.Sharing = tm.Sharing.shares(m, e.Start, e.End, t)
		case !t.Before(e.Start.Add(-lead)) && t.Before(e.Start):
			waking = true
		case !t.Before(e.End) && t.Before(e.End.Add(tm.CooldownTail)):
			cooling = true
		}
	}
	if s.HasMeeting {
		s.Attendees = s.Meeting.attendees()
	}
	switch {
	case s.Sharing:
		s.Occupancy = PhaseSharing
	case s.CallActive:
		s.Occupancy = PhaseInMeeting
	case waking:
		s.Occupancy = PhaseWaking
	case cooling:
		s.Occupancy = PhaseCooldown
	}
	s.InUse = s.Occupancy != PhaseIdle
	if !s.Impaired {
		s.Phase = s.Occupancy
	}
	return s
}
