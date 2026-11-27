package simoutpost

import (
	"time"

	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// WorldEnv carries the path of an outpost's world file (#2773) to its
// simulator, as PushersEnv carries its pushers: the lab writes the file from
// the scenario and names it here. Unset, the simulator holds no world.
const WorldEnv = "OMNIGLASS_SIM_WORLD"

// ClockOffsetEnv carries the offset of the simulated world's clock (#2800), a
// Go duration ("36h", "-90m"), to the simulator. Unset, the world runs on real
// time. It is the first knob of the simulator's clock (#1362 adds a speed).
const ClockOffsetEnv = "OMNIGLASS_SIM_CLOCK_OFFSET"

// LoadClockOffset reads the offset a ClockOffsetEnv value names; empty is none.
func LoadClockOffset(raw string) (time.Duration, error) { return sim.ParseClockOffset(raw) }

// LoadWorld reads the world file a path names; an empty path is no world.
func LoadWorld(path string) (*world.World, error) {
	if path == "" {
		return nil, nil
	}
	return world.Load(path)
}

// installWorld gives an engine the world it simulates: the world itself, for a
// service to resolve a system to a room, and the lab calendar the world's
// seed and meeting knobs imply. A nil world leaves the engine as it is, with
// no calendar and no rooms.
func installWorld(e *sim.Engine, w *world.World) {
	if w == nil {
		return
	}
	e.SetWorld(w)
	e.SetCalendar(w.Calendar())
}
