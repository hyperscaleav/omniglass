package sim

import (
	"text/template"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// SetCalendar gives the engine the lab calendar (#2757) its devices read: the
// meetings each room holds, derived from a seed and the scenario's knobs. Nil
// takes it away. Safe at any time; a device reads the calendar when a
// template calls for it, never at compile time, so a calendar set after the
// devices load reaches them.
func (e *Engine) SetCalendar(c *calendar.Calendar) { e.cal.Store(c) }

// Calendar is the engine's lab calendar, nil when none is set.
func (e *Engine) Calendar() *calendar.Calendar { return e.cal.Load() }

// SetWorld gives the engine the world it simulates (#2773): what a service
// device reads to resolve a system to a room and to list the rooms it speaks
// for. Nil takes it away. Safe at any time.
func (e *Engine) SetWorld(w *world.World) { e.world.Store(w) }

// World is the engine's world, nil when none is set.
func (e *Engine) World() *world.World { return e.world.Load() }

// Room is the room the device sits in, as its snapshot located it.
func (d *Device) Room() calendar.Room { return d.room }

// world is how the device reaches its engine's world; a device that stands
// alone has none (every lookup on a nil world finds nothing).
func (d *Device) world() *world.World {
	if d.worldSrc == nil {
		return nil
	}
	return d.worldSrc()
}

// calendarSrc is how the device reaches its engine's calendar; a device that
// stands alone has none.
func (d *Device) calendarSrc() *calendar.Calendar {
	if d.calSrc == nil {
		return nil
	}
	return d.calSrc()
}

// behaviorFuncs is the FuncMap this device's overrides compile against: the
// package's behaviorFuncs over the device's source and clock.
func (d *Device) behaviorFuncs() template.FuncMap {
	return behaviorFuncs(d.rand, d.clock)
}
