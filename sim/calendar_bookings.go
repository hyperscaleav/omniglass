package sim

import (
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// bookingHorizon is how far past the start of the day a room's next booked
// meeting is looked for.
const bookingHorizon = 3 * 24 * time.Hour

// RoomBookingsAt is what a scheduling surface says about a room at one
// instant, read from the lab calendar's booked entries alone: a walk-in is
// someone using the room unbooked, so it is on no booking. A booked meeting
// holds its room from the earlier of its slot's start and its own to the
// later of its slot's end and its own, so a no-show, which ran nothing, is
// still booked for its slot, and a booked meeting that starts a minute late
// still has the room reserved. Pure: the calendar, the room and the instant
// decide it.
//
//	today      booked meetings whose slot falls in the UTC day of at
//	remaining  of those, the ones not yet over, the one running included
//	booked     a booked meeting holds the room now
//	state      "booked" or "free"
//	now        the booked meeting holding the room now (its map), nil when none
//	next       the next booked meeting to begin after at (its map), nil when none
//
// Every key is always present, so a template reads a room with no calendar as
// free and empty.
func RoomBookingsAt(c *calendar.Calendar, room calendar.Room, at time.Time) map[string]any {
	out := map[string]any{"today": 0, "remaining": 0, "booked": false, "state": "free", "now": nil, "next": nil}
	at = at.UTC()
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	today, remaining := 0, 0
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
			today++
			if to.After(at) {
				remaining++
			}
		}
		switch {
		case !at.Before(from) && at.Before(to):
			if out["now"] == nil {
				out["now"], out["booked"], out["state"] = e.Map(), true, "booked"
			}
		case from.After(at):
			if out["next"] == nil {
				out["next"] = e.Map()
			}
		}
	}
	out["today"], out["remaining"] = today, remaining
	return out
}
