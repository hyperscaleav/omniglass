package behave

import (
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// BuildingSpan is the hours the building a room sits in keeps: the widest
// span among the hours the world's meeting knobs declare for the rooms of that
// site and building, so a room that declares none (a signage room) still reads
// its building's day. With no world, or none of the building's rooms with
// knobs, it is the room's own hours through the calendar, and false when it
// has none.
func BuildingSpan(w *world.World, cal *calendar.Calendar, room calendar.Room) (calendar.Hours, bool) {
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

// HoursReading is the building's day at an instant: whether the lab holds
// hours for it at all, and whether it is inside them.
type HoursReading struct{ Known, Open bool }

// ReadBuildingHours reads a building's hours at an instant (the whole UTC day
// when none are declared).
func ReadBuildingHours(span calendar.Hours, known bool, at time.Time) HoursReading {
	if !known {
		return HoursReading{}
	}
	return HoursReading{Known: true, Open: span.Open(at)}
}

// SignState is a signage player that follows its building's day.
type SignState struct {
	// Open is what the building last read.
	Open     bool
	Playback string
	Index    int
	Content  string
}

// SignParams are a signage player's rules.
type SignParams struct {
	Playing, Stopped string
	// Playlist is the content by index; an index past the end reads the last.
	Playlist []string
}

// DefaultSign is the Cast player's playlist.
var DefaultSign = SignParams{
	Playing: "playing", Stopped: "stopped",
	Playlist: []string{"Welcome loop", "Wayfinding", "Today's events", "Safety notice"},
}

// BuildingDay is the next state of a player that follows the building's day,
// not a room's meetings: it plays while the hours say the building is open and
// stops outside them, and opening starts the playlist from its first item. The
// change at opening and closing is the edge, so an operator's pause holds
// until the next one; a player whose room has no hours to read plays on as it
// was.
func BuildingDay(prev SignState, hours HoursReading, p SignParams) SignState {
	next := prev
	if hours.Known {
		if hours.Open != prev.Open {
			if hours.Open {
				next.Playback = p.Playing
			} else {
				next.Playback = p.Stopped
			}
		}
		if hours.Open && !prev.Open {
			next.Index = 0
		}
		next.Open = hours.Open
	}
	next.Content = p.Playlist[min(max(next.Index, 0), len(p.Playlist)-1)]
	return next
}
