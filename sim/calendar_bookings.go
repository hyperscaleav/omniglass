package sim

import (
	"sync"
	"text/template"
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
	for _, e := range c.Entries(room, day, day.Add(bookingHorizon)) {
		m := e.Meeting
		if e.Kind != calendar.KindMeeting || m == nil || m.Origin != calendar.OriginBooked {
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

// bookingFuncs are the template functions that read a room's booked meetings
// through the device's clock (the seam a test pins):
//
//	bookings            -> the device's own room, as RoomBookingsAt reads it
//	roomBookings s b r  -> the room site s, building b, room r
//	servedRooms         -> the rooms a service speaks for (coveredRooms, each with
//	                       its system's name as "system"), else the device's own
//	                       room when it has one, else none
//	ownerBookings o     -> the room of the system o in the world (a push's
//	                       .Owner); a system the world does not name reads as a
//	                       room with no booking
func (d *Device) bookingFuncs() template.FuncMap {
	// A behavior's mutations each read the room in the same tick, and a read
	// generates three days of the calendar, so the readings of one second are
	// kept for their rooms (and dropped with the second or the calendar): a
	// service reading thousands of rooms reads each once a tick.
	var mu sync.Mutex
	var cacheSec int64
	var cacheCal *calendar.Calendar
	cache := map[calendar.Room]map[string]any{}
	read := func(room calendar.Room) map[string]any {
		c, at := d.calendarSrc(), d.clock()
		if room == (calendar.Room{}) {
			c = nil // no room: nothing is booked in it
		}
		mu.Lock()
		defer mu.Unlock()
		if cacheSec != at.Unix() || cacheCal != c {
			clear(cache)
			cacheSec, cacheCal = at.Unix(), c
		}
		if out, ok := cache[room]; ok {
			return out
		}
		out := RoomBookingsAt(c, room, at)
		cache[room] = out
		return out
	}
	return template.FuncMap{
		"servedRooms": func() []map[string]any {
			var out []map[string]any
			if w := d.world(); w != nil {
				for _, s := range w.Services {
					if s.Name != d.Hostname {
						continue
					}
					for _, c := range s.Covers {
						out = append(out, map[string]any{"system": c.System, "site": c.Site, "building": c.Building, "room": c.Room})
					}
				}
			}
			if len(out) == 0 && d.room != (calendar.Room{}) {
				out = append(out, map[string]any{"site": d.room.Site, "building": d.room.Building, "room": d.room.Room})
			}
			return out
		},
		"bookings": func() map[string]any { return read(d.room) },
		"roomBookings": func(site, building, room string) map[string]any {
			return read(calendar.Room{Site: site, Building: building, Room: room})
		},
		"ownerBookings": func(owner string) map[string]any {
			p, ok := d.world().PlaceSystem(owner)
			if !ok {
				return read(calendar.Room{})
			}
			return read(p.CalRoom())
		},
	}
}
