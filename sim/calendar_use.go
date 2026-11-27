package sim

import "github.com/hyperscaleav/omniglass/sim/calendar"

// How a room is used, read off its meetings (#2776). Two small, pure readings
// the room's control system and its sources share, so a processor, a touch
// panel and a switcher in one room agree on them without any asking another:
// whether the room is in use, and whether the meeting running shares content.

// roomInUse is the window a room is in use: from lead seconds before a booked
// meeting someone attends (at the start of a walk-in) to tail seconds after
// the meeting ends. A no-show holds its slot with nobody in it, so it uses
// nothing. ms are the room's meetings as the calendar reads them.
func roomInUse(ms []map[string]any, at, lead, tail int64) bool {
	for _, m := range ms {
		if m["outcome"] == "no-show" {
			continue
		}
		start, _ := m["start"].(int64)
		end, _ := m["end"].(int64)
		l := int64(0)
		if m["origin"] == "booked" {
			l = lead
		}
		if at >= start-l && at < end+tail {
			return true
		}
	}
	return false
}

// sharesContent says whether a meeting that is running at the unix second at
// has someone sharing a laptop: a meeting that took place and lasts at least
// half an hour shares through its middle third, the part of it a presentation
// takes. A pure function of the meeting and the clock, so every device in the
// room reads the same answer.
func sharesContent(m map[string]any, at int64) bool {
	if m == nil || m["outcome"] == "no-show" {
		return false
	}
	start, _ := m["start"].(int64)
	end, _ := m["end"].(int64)
	dur := end - start
	if dur < 30*60 || at < start || at >= end {
		return false
	}
	into := (at - start) * 3
	return into >= dur && into < 2*dur
}

// roomInUse reads the device's own room: the window of roomInUse around now,
// with lead and tail in seconds. False for a device with no calendar.
func (d *Device) roomInUse(lead, tail any) bool {
	l, _, _ := toFloat(lead)
	t, _, _ := toFloat(tail)
	at := d.clock().Unix()
	return roomInUse(d.meetingsBetween(at-int64(t), at+int64(l)+1), at, int64(l), int64(t))
}

// meetingSharing says whether the meeting running in the device's own room
// is sharing content now.
func (d *Device) meetingSharing() bool {
	c := d.calendarSrc()
	if c == nil {
		return false
	}
	at := d.clock()
	e, ok := c.Now(d.room, at, calendar.MeetingKinds)
	if !ok {
		return false
	}
	return sharesContent(e.Map(), at.Unix())
}
