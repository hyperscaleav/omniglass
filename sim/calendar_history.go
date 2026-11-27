package sim

import (
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
)

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
