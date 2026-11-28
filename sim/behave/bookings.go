package behave

import (
	"fmt"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// Booking states a room reads.
const (
	StateBooked = "booked"
	StateFree   = "free"
)

// bookingHorizon is how far past the start of the day a room's next booked
// meeting is looked for.
const bookingHorizon = 3 * 24 * time.Hour

// Booking is a booked meeting as a scheduling surface reads it: when it ran
// and the slot it was booked for (unix seconds), and who booked it.
type Booking struct {
	Start, End, SlotStart, SlotEnd int64
	Organizer                      string
}

// Line is "slot_start slot_end organizer", or "none".
func (b *Booking) Line() string {
	if b == nil {
		return "none"
	}
	return fmt.Sprintf("%d %d %s", b.SlotStart, b.SlotEnd, b.Organizer)
}

// Bookings is what a scheduling surface says about a room at an instant.
type Bookings struct {
	// Today is the booked meetings whose slot falls in the UTC day; Remaining
	// those of them not yet over, the one running included.
	Today, Remaining int
	// Booked says a booked meeting holds the room now; State is StateBooked
	// or StateFree.
	Booked bool
	State  string
	// Now is the booked meeting holding the room now, Next the next to begin.
	Now, Next *Booking
}

// BookingsAt reads a room's booked entries alone at an instant: a walk-in is
// someone using the room unbooked, so it is on no booking. A booked meeting
// holds its room from the earlier of its slot's start and its own to the later
// of its slot's end and its own, so a no-show, which ran nothing, is still
// booked for its slot, and a booked meeting that starts a minute late still
// has the room reserved. A room with no calendar reads free and empty.
func BookingsAt(c *calendar.Calendar, room calendar.Room, at time.Time) Bookings {
	out := Bookings{State: StateFree}
	at = at.UTC()
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	for _, e := range c.Entries(room, day, day.Add(bookingHorizon), calendar.MeetingKinds) {
		m, ok := e.AsMeeting()
		if !ok || m.Origin != calendar.OriginBooked {
			continue
		}
		from, to := e.Start, e.End
		if m.SlotStart.Before(from) {
			from = m.SlotStart
		}
		if m.SlotEnd.After(to) {
			to = m.SlotEnd
		}
		if !m.SlotStart.Before(day) && m.SlotStart.Before(day.Add(24*time.Hour)) {
			out.Today++
			if to.After(at) {
				out.Remaining++
			}
		}
		b := &Booking{Start: e.Start.Unix(), End: e.End.Unix(), SlotStart: m.SlotStart.Unix(), SlotEnd: m.SlotEnd.Unix(), Organizer: m.Organizer}
		switch {
		case !at.Before(from) && at.Before(to):
			if out.Now == nil {
				out.Now, out.Booked, out.State = b, true, StateBooked
			}
		case from.After(at):
			if out.Next == nil {
				out.Next = b
			}
		}
	}
	return out
}

// RoomBookingState is a room's booking as a panel reports it.
type RoomBookingState struct {
	// Booking is what it reports; CalBooking what the calendar last said.
	Booking, CalBooking string
	// Current and Next are the booked meetings' lines.
	Current, Next string
}

// RoomBooking is the next report of a room booking panel: it follows the
// calendar's booked or free at each change of it, and an operator's change
// holds until the calendar next changes. Current and Next read the booked
// meeting now and the next.
func RoomBooking(prev RoomBookingState, b Bookings) RoomBookingState {
	next := RoomBookingState{CalBooking: b.State, Current: b.Now.Line(), Next: b.Next.Line()}
	next.Booking = prev.Booking
	if b.State != prev.CalBooking {
		next.Booking = b.State
	}
	return next
}

// ServiceBookings is what a scheduling service reports across the rooms it
// serves.
type ServiceBookings struct{ Today, Remaining, InProgress int }

// SumBookings is a service's totals over its rooms: the meetings booked today
// and left, and the rooms booked now. A service with no room to serve keeps
// the day it had (prevToday) and reports nothing left or running.
func SumBookings(prevToday int, rooms []Bookings) ServiceBookings {
	if len(rooms) == 0 {
		return ServiceBookings{Today: prevToday}
	}
	var s ServiceBookings
	for _, r := range rooms {
		s.Today += r.Today
		s.Remaining += r.Remaining
		if r.Booked {
			s.InProgress++
		}
	}
	return s
}

// RoomSummary is one room as a scheduling service lists it.
type RoomSummary struct {
	State            string
	Today, Remaining int
	// Current is who booked the meeting holding the room, "" when none;
	// NextStart the slot start of the next one, 0 when none.
	Current   string
	NextStart int64
}

// Summary is the room as a service lists it.
func (b Bookings) Summary() RoomSummary {
	s := RoomSummary{State: b.State, Today: b.Today, Remaining: b.Remaining}
	if b.Now != nil {
		s.Current = b.Now.Organizer
	}
	if b.Next != nil {
		s.NextStart = b.Next.SlotStart
	}
	return s
}

// Window is a booked window of a fixed meeting day, as offsets from midnight
// UTC.
type Window struct{ Start, End time.Duration }

// ParseWindows reads a fixed meeting day, a list of "HH:MM-HH:MM" windows in
// UTC that repeat every day, the booking a service with no calendar keeps.
func ParseWindows(spec []string) ([]Window, error) {
	out := make([]Window, 0, len(spec))
	for _, s := range spec {
		from, to, ok := strings.Cut(s, "-")
		if !ok {
			return nil, fmt.Errorf("booking window %q is not HH:MM-HH:MM", s)
		}
		start, err := clockOffset(from)
		if err != nil {
			return nil, fmt.Errorf("booking window %q: %w", s, err)
		}
		end, err := clockOffset(to)
		if err != nil {
			return nil, fmt.Errorf("booking window %q: %w", s, err)
		}
		if start >= end {
			return nil, fmt.Errorf("booking window %q ends before it starts", s)
		}
		out = append(out, Window{Start: start, End: end})
	}
	return out, nil
}

// clockOffset is an "HH:MM" as an offset from midnight, 24:00 the latest.
func clockOffset(s string) (time.Duration, error) {
	var h, m int
	if len(s) != 5 || s[2] != ':' {
		return 0, fmt.Errorf("%q is not HH:MM", s)
	}
	if _, err := fmt.Sscanf(s, "%02d:%02d", &h, &m); err != nil {
		return 0, fmt.Errorf("%q is not HH:MM", s)
	}
	if h < 0 || m < 0 || m > 59 || h > 24 || (h == 24 && m > 0) {
		return 0, fmt.Errorf("%q is not a time of day", s)
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute, nil
}

// FixedDay reads a fixed meeting day at now: the windows not yet over and
// whether one holds the room.
func FixedDay(now time.Time, windows []Window) (state string, remaining int) {
	now = now.UTC()
	since := now.Sub(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC))
	state = StateFree
	for _, w := range windows {
		if since < w.End {
			remaining++
		}
		if since >= w.Start && since < w.End {
			state = StateBooked
		}
	}
	return state, remaining
}
