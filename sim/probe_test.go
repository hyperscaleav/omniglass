package sim

import (
	"testing"
	"text/template"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// The tests probe what a device reads of its room, its calendar and its
// building through template expressions: a one-line expression names the
// reading and the clock a test pins decides it. The adapters below are the
// probe, and test-only: what they read is the typed reads a family's tick and
// its rooms service make (roomState, calendarSrc, the world), so a test holds
// the reading, not the way a caller asks for it.

// stampBehavior renders expr against the device's probe functions and writes
// the result to cal_probe, as an override's expression would, and returns what
// it wrote.
func stampBehavior(t *testing.T, d *Device, expr string) any {
	t.Helper()
	funcs := behaviorFuncs(newDeviceRand(d.DeviceID), d.clock)
	for name, fn := range d.calendarFuncs() {
		funcs[name] = fn
	}
	for name, fn := range d.buildingFuncs() {
		funcs[name] = fn
	}
	m, err := compileOneFuncs("cal_probe", expr, false, funcs)
	if err != nil {
		t.Fatal(err)
	}
	d.SetState(map[string]any{"cal_probe": ""}, 2)
	d.mu.Lock()
	_, errs := applyMutations(d.state, []mutation{m}, templateData{State: d.state, Defaults: d.defaults, Hostname: d.Hostname, DeviceID: d.DeviceID, IP: d.LoopbackIP.String()})
	d.mu.Unlock()
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return d.ReadPath("cal_probe")
}

// calendarFuncs are the template functions that read the lab calendar through
// the device's clock (the seam a test pins):
//
//	meetingNow            -> the meeting in the device's own room now, nil when none
//	meetingNext           -> the room's next meeting after now, nil when none
//	hasCalendar           -> whether the device's room holds a calendar at all
//	meetingsBetween a b   -> the room's meetings overlapping [a, b) (unix seconds), in start order
//	roomMeetingNow s b r  -> the same for the room site s, building b, room r
//	roomMeetingNext s b r
//	roomPhase, roomPhaseOf s b r, ownerPhase o
//	                      -> a room's phase and the facts every device in it shares: in use (the
//	                         wake lead, the cooldown tail), call active, attendees, sharing, impaired
//	                         (room_phase.go, #2797); the timing is the standard's meeting knobs,
//	                         never a number in a profile
//	problemsNow           -> the scripted problems active in the device's own room now, each a map
//	                         (kind, name, role, unplugged, loss_pct, pin_path, pin_value, start, end);
//	                         a fact of the world the executor applies (problems.go), for a profile
//	                         that wants to read it, never to re-implement it (#2769)
//	meetingPrev           -> the device's own room's last meeting that has ended by now, nil when none (#2775)
//	roomMeetingPrev s b r -> the last meeting of that room that has ended by now, nil when none (#2775)
//	ownerRoom o           -> the {site, building, room} of the system o in the world, nil when none
//	ownerMeetingNow o     -> the meeting in system o's room now (ownerMeetingNext: the next)
//	coveredRooms          -> the rooms a service device speaks for, each {site, building, room}
//	bookings, roomBookings s b r, ownerBookings o
//	                      -> a room's booked meetings alone (calendar_bookings.go)
//
// A meeting reads as a map: kind, origin, organizer, expected, actual,
// outcome, start, end, slot_start, slot_end (unix seconds) and the room's
// site, building and room, so a profile writes
// {{ with meetingNow }}{{ .outcome }}{{ end }}. A service that speaks for many
// rooms (a booking platform, a meeting platform) reads by room key: the
// calendar is keyed by room, and a push's .Owner is a system, which the
// service resolves to its room key before it reads.
func (d *Device) calendarFuncs() template.FuncMap {
	read := func(room calendar.Room, next bool) map[string]any {
		c := d.calendarSrc()
		if c == nil {
			return nil
		}
		at := d.clock()
		var e calendar.Entry
		var ok bool
		if next {
			e, ok = c.Next(room, at, calendar.MeetingKinds)
		} else {
			e, ok = c.Now(room, at, calendar.MeetingKinds)
		}
		if !ok {
			return nil
		}
		return e.Map()
	}
	prev := func(room calendar.Room) map[string]any {
		c := d.calendarSrc()
		if c == nil {
			return nil
		}
		if e, ok := c.Prev(room, d.clock(), calendar.MeetingKinds); ok {
			return e.Map()
		}
		return nil
	}
	placed := func(p world.Placement) map[string]any {
		return map[string]any{"site": p.Site, "building": p.Building, "room": p.Room}
	}
	// ownerRoom is the room of a push's .Owner system, through the world.
	ownerRoom := func(owner string) (world.Placement, bool) { return d.world().PlaceSystem(owner) }
	funcs := template.FuncMap{
		"ownerRoom": func(owner string) map[string]any {
			if p, ok := ownerRoom(owner); ok {
				return placed(p)
			}
			return nil
		},
		"ownerMeetingNow": func(owner string) map[string]any {
			if p, ok := ownerRoom(owner); ok {
				return read(p.CalRoom(), false)
			}
			return nil
		},
		"ownerMeetingNext": func(owner string) map[string]any {
			if p, ok := ownerRoom(owner); ok {
				return read(p.CalRoom(), true)
			}
			return nil
		},
		"coveredRooms": func() []map[string]any {
			var out []map[string]any
			for _, p := range d.world().Covered(d.Hostname) {
				out = append(out, placed(p))
			}
			return out
		},
		"meetingsBetween": d.meetingsBetween,
		"hasCalendar":     d.hasCalendar,
		"meetingNow":      func() map[string]any { return read(d.room, false) },
		"meetingNext":     func() map[string]any { return read(d.room, true) },
		"roomMeetingNow": func(site, building, room string) map[string]any {
			return read(calendar.Room{Site: site, Building: building, Room: room}, false)
		},
		"roomMeetingNext": func(site, building, room string) map[string]any {
			return read(calendar.Room{Site: site, Building: building, Room: room}, true)
		},
		"problemsNow": func() []map[string]any {
			var out []map[string]any
			for _, e := range d.calendarSrc().Active(d.room, d.clock(), calendar.ProblemKinds) {
				out = append(out, e.Map())
			}
			return out
		},
		"meetingPrev": func() map[string]any { return prev(d.room) },
		"roomMeetingPrev": func(site, building, room string) map[string]any {
			return prev(calendar.Room{Site: site, Building: building, Room: room})
		},
	}
	for name, fn := range d.bookingFuncs() {
		funcs[name] = fn
	}
	for name, fn := range d.roomPhaseFuncs() {
		funcs[name] = fn
	}
	return funcs
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
	// generates three days of the calendar, so the engine's memo keeps the
	// reading of one second for its room (room_phase.go): a service reading
	// thousands of rooms reads each once a tick, however many devices ask.
	read := func(room calendar.Room) map[string]any {
		c, at := d.calendarSrc(), d.clock().UTC().Truncate(time.Second)
		if room == (calendar.Room{}) {
			c = nil // no room: nothing is booked in it
		}
		memo := d.roomMemo()
		e := memo.entry(roomMemoKey{cal: c, w: nil, room: room, sec: at.Unix()})
		e.bookOnce.Do(func() {
			memo.books.Add(1)
			e.book = RoomBookingsAt(c, room, at)
		})
		return e.book
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
						out = append(out, map[string]any{"system": c.System, "site": c.Site, "building": c.Building, "room": c.Room, "label": c.Label})
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

// buildingFuncs are the template functions a device that follows the
// building's day reads (#2768), through the device's clock:
//
//	buildingHours -> {start, end, open}: the hours the building its room sits
//	                 in is open (whole UTC hours, the whole day when none are
//	                 declared) and whether the clock is inside them; nil when
//	                 the lab holds none for it
//
// The hours are the nearest fact the lab holds: the `hours` the scenario
// declares in the meeting knobs of the building's rooms, which also bound
// their meetings. A signage player reads it the way a video bar reads
// meetingNow.
func (d *Device) buildingFuncs() template.FuncMap {
	return template.FuncMap{
		"buildingHours": func() map[string]any {
			h, ok := buildingSpan(d.world(), d.calendarSrc(), d.room)
			if !ok {
				return nil
			}
			s, e := h.Span()
			return map[string]any{"start": s, "end": e, "open": h.Open(d.clock())}
		},
	}
}

// meetingsBetween is the template func a device reads its own room's recent
// history through (#2774): every meeting (never a scripted problem) of the room overlapping the window
// [from, to) (unix seconds), in start order, each a map as meetingNow reads.
// It reads the calendar a derived function of room and window, so a reading
// built over it (a battery's charge, a load's warm-up) is a pure function of
// the calendar and the clock: the same at one instant on every run, with no
// accumulated state to start from. Nil for a device with no calendar.
func (d *Device) meetingsBetween(from, to int64) []map[string]any {
	c := d.calendarSrc()
	if c == nil {
		return nil
	}
	var out []map[string]any
	for _, e := range c.Entries(d.room, time.Unix(from, 0).UTC(), time.Unix(to, 0).UTC(), calendar.MeetingKinds) {
		out = append(out, e.Map())
	}
	return out
}

// hasCalendar says whether the device's room holds a calendar: the engine has
// one and the scenario declared meetings for the room. A device with none
// (the dev fleet has no world file) keeps the drift it had before it read a
// calendar; one with a calendar follows it, an empty room included.
func (d *Device) hasCalendar() bool {
	c := d.calendarSrc()
	if c == nil {
		return false
	}
	_, ok := c.Hours(d.room)
	return ok
}

// roomPhaseFuncs are the template functions that read a room's phase through
// the device's clock (the seam a test pins). Each reads a map with every key
// present (calendar.RoomState.Map), so a profile branches on has_calendar and
// never on a missing key:
//
//	roomPhase        -> the device's own room
//	roomPhaseOf s b r -> the room site s, building b, room r (a service that speaks for many)
//	ownerPhase o     -> the room of the system o in the world (a push's .Owner); a system the
//	                    world does not name reads as a room with no calendar
//
// The keys: phase (idle, waking, in-meeting, sharing, cooldown or impaired),
// occupancy (the phase the meetings alone give), has_calendar, scheduled,
// in_use, call_active, attendees, sharing, impaired, building_open, use_until,
// recording_pending and meeting (the meeting running now, nil when none).
func (d *Device) roomPhaseFuncs() template.FuncMap {
	return template.FuncMap{
		"roomPhase": func() map[string]any { return d.roomState(d.room).Map() },
		"roomPhaseOf": func(site, building, room string) map[string]any {
			return d.roomState(calendar.Room{Site: site, Building: building, Room: room}).Map()
		},
		"ownerPhase": func(owner string) map[string]any {
			p, ok := d.world().PlaceSystem(owner)
			if !ok {
				return d.roomState(calendar.Room{}).Map()
			}
			return d.roomState(p.CalRoom()).Map()
		},
	}
}
