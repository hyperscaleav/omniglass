package simoutpost

import (
	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// WorldEnv carries the path of an outpost's world file (#2773) to its
// simulator, as PushersEnv carries its pushers: the lab writes the file from
// the scenario and names it here. Unset, the simulator holds no world.
const WorldEnv = "OMNIGLASS_SIM_WORLD"

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
