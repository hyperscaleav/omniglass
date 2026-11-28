// Package bind joins the shared behavior primitives (sim/behave) to the state
// paths a family keeps them in (#2832): each function builds a spec.Tick that
// reads a device's typed state, calls one primitive and returns the paths it
// wants written. A primitive is pure and knows no path; a family's ticks are
// mostly one of these. Only a behavior no primitive owns is written inline in
// the family.
//
// The state a bound tick keeps follows one layout, the one the YAML profiles
// used, so a device moved to Go keeps the paths a collector, a test or the
// console already reads:
//
//	status.device.power            the device's power
//	status.sim.{busy,known,awake}  the room-following bookkeeping
//	status.sim.until               when the last call's cooldown ends (unix seconds)
//
// It sits under sim/device/internal, so only families import it and the
// registry scan skips it.
package bind

import (
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

// The state paths of a room-following device.
const (
	PathPower = "status.device.power"
	PathBusy  = "status.sim.busy"
	PathKnown = "status.sim.known"
	PathAwake = "status.sim.awake"
	PathUntil = "status.sim.until"
)

// RoomDefaults is the bookkeeping state a room-following device starts with,
// to be merged into a family's status tree under "sim".
func RoomDefaults() map[string]any {
	return map[string]any{"busy": false, "known": false, "awake": false, "until": int64(0)}
}

// Seconds is an interval written as the profiles wrote it, in whole seconds.
func Seconds(n int) time.Duration { return time.Duration(n) * time.Second }

// PowerFollowsRoom is the tick of a display or projector that wakes for its
// room's meetings and sleeps after them (behave.PowerFollowsRoom and
// behave.InputFollowsMeeting): power and input follow the room, the input is
// the conferencing one on the wake edge only, and a room with no calendar
// leaves both as they were. input is the state path of the device's selected
// input.
func PowerFollowsRoom(input string) spec.Tick {
	return spec.Tick{
		Name: "room_meeting", Interval: Seconds(30), OnStart: true,
		Writes: []string{PathBusy, PathKnown, PathUntil, PathPower, input, PathAwake},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			prev := behave.PowerState{Power: spec.String(d, PathPower), Busy: spec.Bool(d, PathBusy), Known: spec.Bool(d, PathKnown),
				Awake: spec.Bool(d, PathAwake), Until: spec.Int64(d, PathUntil)}
			next, woke := behave.PowerFollowsRoom(prev, c.Room, c.Now, behave.DefaultPower)
			return []surface.Change{
				{Path: PathBusy, Value: next.Busy},
				{Path: PathKnown, Value: next.Known},
				{Path: PathUntil, Value: next.Until},
				{Path: PathPower, Value: next.Power},
				{Path: input, Value: behave.InputFollowsMeeting(spec.String(d, input), woke, behave.DefaultPower)},
				{Path: PathAwake, Value: next.Awake},
			}
		},
	}
}

// Drift is the tick of a reading that wanders inside a band (behave.Drift):
// the value at path, a step at a time, drawn from the device's own source.
func Drift(name string, every time.Duration, path string, band behave.Band, round func(float64) float64) spec.Tick {
	return spec.Tick{
		Name: name, Interval: every, Writes: []string{path},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			return []surface.Change{{Path: path, Value: behave.Drift(spec.Float(d, path), band, c.Rand, round)}}
		},
	}
}

// Wear is the tick of a part that ages only while the device is on
// (behave.Wear): the hours at path grow by the params' share each tick the
// power reads on, and hold in standby.
func Wear(name string, every time.Duration, path string, p behave.WearParams) spec.Tick {
	return spec.Tick{
		Name: name, Interval: every, Writes: []string{path},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			lit := spec.String(d, PathPower) == behave.DefaultPower.On
			return []surface.Change{{Path: path, Value: behave.Wear(spec.Float(d, path), lit, p)}}
		},
	}
}

// DriftWhole is Drift for a reading kept in whole numbers (millivolts,
// milliamps): the value at path, a step at a time, written as the integer it
// is rounded to.
func DriftWhole(name string, every time.Duration, path string, band behave.Band) spec.Tick {
	return spec.Tick{
		Name: name, Interval: every, Writes: []string{path},
		Run: func(d spec.Device, c spec.TickContext) []surface.Change {
			return []surface.Change{{Path: path, Value: int64(behave.Drift(spec.Float(d, path), band, c.Rand, behave.Places(0)))}}
		},
	}
}
