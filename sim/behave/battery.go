package behave

import (
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// BatteryParams are a wireless pack's charge model.
type BatteryParams struct {
	// DrainPerMinute is the percent a pack loses a minute while its room is
	// in a meeting someone attends; ChargePerMinute what it gains on its dock
	// otherwise.
	DrainPerMinute, ChargePerMinute float64
	// Window is how far back the charge is computed from, full at its start.
	Window time.Duration
	// IdleDrainPerTick is what a pack with no calendar loses a tick.
	IdleDrainPerTick float64
}

// DefaultBattery drains in a meeting in about four hours from full and
// recharges in about two from empty, over the 48 hours before now.
var DefaultBattery = BatteryParams{DrainPerMinute: 0.4, ChargePerMinute: 0.8, Window: 48 * time.Hour, IdleDrainPerTick: 0.1}

// Span is a meeting as the battery reads it: when it ran (unix seconds) and
// who came, zero for a no-show.
type Span struct {
	Start, End int64
	Actual     int
}

// SpansBetween is the room's meetings overlapping [from, to), in start order.
func SpansBetween(c *calendar.Calendar, room calendar.Room, from, to time.Time) []Span {
	var out []Span
	for _, e := range c.Entries(room, from, to, calendar.MeetingKinds) {
		m, _ := e.AsMeeting()
		out = append(out, Span{Start: e.Start.Unix(), End: e.End.Unix(), Actual: m.Actual})
	}
	return out
}

// Battery is the pack's charge in percent. With a calendar it is a function of
// the room's meetings alone: computed over the Window before now, full at its
// start, draining while a meeting someone attends runs and charging between
// them, so it is the same at an instant on every run and for every pack in the
// room, with nothing accumulated in the device (meetings are those SpansBetween
// reads for [now-Window, now)). A no-show drains nothing. With no calendar it
// drains IdleDrainPerTick a tick.
func Battery(prev float64, hasCalendar bool, meetings []Span, now time.Time, p BatteryParams) float64 {
	if !hasCalendar {
		return Round(1, Clamp(0, 100, prev-p.IdleDrainPerTick))
	}
	t1 := now.Unix()
	cur := t1 - int64(p.Window/time.Second)
	lvl := 100.0
	drain, charge := p.DrainPerMinute/60.0, p.ChargePerMinute/60.0
	for _, m := range meetings {
		if m.Actual <= 0 {
			continue
		}
		s, e := max(m.Start, cur), min(m.End, t1)
		if e > s {
			lvl = Clamp(0, 100, lvl+charge*float64(s-cur))
			lvl = Clamp(0, 100, lvl-drain*float64(e-s))
			cur = e
		}
	}
	return Round(1, Clamp(0, 100, lvl+charge*float64(t1-cur)))
}
