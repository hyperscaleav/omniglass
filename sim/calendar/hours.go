package calendar

import "time"

// Span is the hours as a half-open span of whole UTC hours, the whole day
// (0 to 24) when none are declared.
func (h Hours) Span() (start, end int) {
	s, e := h.bounds()
	return int(s / time.Hour), int(e / time.Hour)
}

// Open says whether t falls inside the hours of its UTC day.
func (h Hours) Open(t time.Time) bool {
	s, e := h.bounds()
	t = t.UTC()
	since := t.Sub(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC))
	return since >= s && since < e
}

// Hours is the hours the room's knobs declare, and false for a room with no
// knobs (or no calendar). A device that follows the building's day, not a
// meeting, reads it.
func (c *Calendar) Hours(room Room) (Hours, bool) {
	if c == nil || c.knobs == nil {
		return Hours{}, false
	}
	k, ok := c.knobs(room)
	return k.Hours, ok
}
