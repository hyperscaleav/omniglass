package sim

import (
	"text/template"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// buildingSpan is the hours the building a room sits in keeps (#2768): the
// widest span among the hours the world's meeting knobs declare for the
// rooms of that site and building, so a room that declares none (a signage
// room) still reads its building's day. With no world, or none of the
// building's rooms with knobs, it is the room's own hours through the
// calendar, and false when it has none.
func buildingSpan(w *world.World, cal *calendar.Calendar, room calendar.Room) (calendar.Hours, bool) {
	var span calendar.Hours
	found := false
	if w != nil {
		for _, s := range w.Sites {
			if s.Name != room.Site {
				continue
			}
			for _, r := range s.Rooms {
				k, ok := w.Meetings[r.Standard]
				if r.Building != room.Building || !ok {
					continue
				}
				start, end := k.Hours.Span()
				if !found {
					span, found = calendar.Hours{Start: start, End: end}, true
					continue
				}
				span.Start, span.End = min(span.Start, start), max(span.End, end)
			}
		}
	}
	if found {
		return span, true
	}
	return cal.Hours(room)
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
