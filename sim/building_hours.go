package sim

import (
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
