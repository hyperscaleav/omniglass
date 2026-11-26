package sim

import (
	"text/template"

	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// SetCalendar gives the engine the lab calendar (#2757) its devices read: the
// meetings each room holds, derived from a seed and the scenario's knobs. Nil
// takes it away. Safe at any time; a device reads the calendar when a
// template calls for it, never at compile time, so a calendar set after the
// devices load reaches them.
func (e *Engine) SetCalendar(c *calendar.Calendar) { e.cal.Store(c) }

// Calendar is the engine's lab calendar, nil when none is set.
func (e *Engine) Calendar() *calendar.Calendar { return e.cal.Load() }

// Room is the room the device sits in, as its snapshot located it.
func (d *Device) Room() calendar.Room { return d.room }

// calendarFuncs are the template functions that read the lab calendar through
// the device's clock (the seam a test pins):
//
//	meetingNow            -> the meeting in the device's own room now, nil when none
//	meetingNext           -> the room's next meeting after now, nil when none
//	roomMeetingNow s b r  -> the same for the room site s, building b, room r
//	roomMeetingNext s b r
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
			e, ok = c.Next(room, at)
		} else {
			e, ok = c.Now(room, at)
		}
		if !ok {
			return nil
		}
		return e.Map()
	}
	return template.FuncMap{
		"meetingNow":  func() map[string]any { return read(d.room, false) },
		"meetingNext": func() map[string]any { return read(d.room, true) },
		"roomMeetingNow": func(site, building, room string) map[string]any {
			return read(calendar.Room{Site: site, Building: building, Room: room}, false)
		},
		"roomMeetingNext": func(site, building, room string) map[string]any {
			return read(calendar.Room{Site: site, Building: building, Room: room}, true)
		},
	}
}

// calendarSrc is how the device reaches its engine's calendar; a device that
// stands alone has none.
func (d *Device) calendarSrc() *calendar.Calendar {
	if d.calSrc == nil {
		return nil
	}
	return d.calSrc()
}

// behaviorFuncs is the FuncMap this device's behaviors and overrides compile
// against: the package's behaviorFuncs over the device's source and clock,
// plus the calendar.
func (d *Device) behaviorFuncs() template.FuncMap {
	funcs := behaviorFuncs(d.rand, d.clock)
	for name, fn := range d.calendarFuncs() {
		funcs[name] = fn
	}
	return funcs
}
