package sim

import (
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// A meeting platform lists devices as well as rooms (#2790), as a real
// account's admin plane does: every codec, bar, controller, scheduling panel,
// camera and microphone in the rooms it covers, one record each, a page at a
// time. The world owns the ids and the kinds; the platform only reads them, so
// nothing here asks Omniglass what a device is.

// unpluggedSince says whether the arbiter has the listed device unplugged now,
// and if so since when (unix seconds): by an operator, or by a scripted problem
// on the lab calendar, whose own start the platform reads, so it learns what
// the world did and not what the dark device would say (#2799).
func (d *Device) unpluggedSince(l world.Listing) (int64, bool) {
	if d.arbSrc == nil {
		return 0, false
	}
	return d.arbSrc().Offline(l, d.clock())
}

// scriptedOffline is the arbiter's derivation for scripted problems: the lab
// calendar says a problem has the listed device unplugged, whichever simulator
// serves it.
func (e *Engine) scriptedOffline(l world.Listing, now time.Time) (int64, bool) {
	c := e.Calendar()
	if c == nil {
		return 0, false
	}
	room := calendar.Room{Site: l.Site, Building: l.Building, Room: l.Room}
	for _, e := range c.Active(room, now, calendar.ProblemKinds) {
		if p, ok := e.AsProblem(); ok && p.Impairment.Unplugged && p.Role == l.Slot {
			return e.Start.Unix(), true
		}
	}
	return 0, false
}

// roomOffline says whether the room of a system reads offline on the platform:
// a codec or a bar in it that a scripted problem has unplugged.
func (d *Device) roomOffline(system string) bool {
	if !d.calendarSrc().HasProblems() && (d.arbSrc == nil || len(d.arbSrc().Sources()) == 0) {
		return false // the scan is per room every tick: skip it where nothing impairs a device
	}
	for _, l := range d.world().Listed(d.Hostname) {
		if l.System != system || (l.Type != "codec" && l.Type != "bar") {
			continue
		}
		if _, down := d.unpluggedSince(l); down {
			return true
		}
	}
	return false
}
