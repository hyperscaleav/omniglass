package simoutpost

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// The problem executor (#2769) is the one place a scripted problem becomes
// something a device does. A problem is a fact of the world on the lab
// calendar: the executor reads the calendar through the clock, and for each
// device a problem is active on (the room it sits in and the slot it fills,
// never its product) it applies the problem's impairment when the window opens
// and removes it when the window closes:
//
//   - a pin holds a state path at a value, through the behavior-override seam,
//     and puts the path back as it was when the window closes;
//   - unplugged and packet loss are the network's: the executor hands the
//     impairer the same per-device impairment an operator's would, merged with
//     what the server asks for, and the impairer applies it (NET_ADMIN only).
//
// Devices do not re-implement problems; omniglass is told nothing and learns
// of a problem only by collecting from the device it broke.

// pinPrefix marks the overrides the executor owns. The executor owns a
// device's whole override set while it runs: nothing else installs one.
const pinPrefix = "problem:"

// pinInterval is how often a held pin rewrites its path, so a write by
// anything else is undone within it.
const pinInterval = 10 * time.Second

type pinned struct {
	path string
	prev any
}

type problemExecutor struct {
	cal   *calendar.Calendar
	w     *world.World
	clock func() time.Time
	log   *slog.Logger
	// pins is what the executor holds on each device (by component id), by
	// override id.
	pins map[string]map[string]pinned
	// refused is the net problems already logged as refused, by key.
	refused map[string]bool
}

// newProblemExecutor is nil for a world with no scripted problems: a device
// with no calendar, or a calendar with no problems, is untouched.
func newProblemExecutor(w *world.World, cal *calendar.Calendar, clock func() time.Time, log *slog.Logger) *problemExecutor {
	if w == nil || cal == nil || len(w.Problems) == 0 {
		return nil
	}
	if clock == nil {
		clock = time.Now
	}
	return &problemExecutor{cal: cal, w: w, clock: clock, log: log, pins: map[string]map[string]pinned{}, refused: map[string]bool{}}
}

// activeOn is the problems active now on the device: those of its room whose
// role is the slot it fills.
func (x *problemExecutor) activeOn(d *sim.Device, now time.Time) []calendar.Entry {
	slot := x.w.SlotOf(d.Hostname)
	if slot == "" {
		return nil
	}
	var out []calendar.Entry
	for _, e := range x.cal.ProblemsNow(d.Room(), now) {
		if e.Problem.Role == slot {
			out = append(out, e)
		}
	}
	return out
}

// reconcile brings every device to what the calendar says now and returns the
// network impairments the active problems ask for, by component id. netOK says
// whether the simulator may apply them; without it they are refused with a log
// line, once per window, and the pins still apply.
func (x *problemExecutor) reconcile(devices []*sim.Device, netOK bool) map[string]collection.SimImpairment {
	now := x.clock()
	net := map[string]collection.SimImpairment{}
	live := map[string]bool{}
	for _, d := range devices {
		live[d.DeviceID] = true
		want := x.activeOn(d, now)
		specs := map[string]calendar.Pin{}
		for _, e := range want {
			imp := e.Problem.Impairment
			switch {
			case imp.Pin != nil:
				specs[pinPrefix+e.Problem.Name] = *imp.Pin
			default:
				key := fmt.Sprintf("%s|%s|%d", e.Problem.Name, d.Hostname, e.Start.Unix())
				if !netOK {
					if !x.refused[key] {
						x.refused[key] = true
						x.log.Warn("a scripted problem needs the network and is not applied: the simulator lacks NET_ADMIN or --impairments",
							"problem", e.Problem.Name, "device", d.Hostname, "role", e.Problem.Role)
					}
					continue
				}
				cur := net[d.DeviceID]
				cur.Unplugged = cur.Unplugged || imp.Unplugged
				cur.LossPct = max(cur.LossPct, imp.LossPct)
				net[d.DeviceID] = cur
			}
		}
		x.settlePins(d, specs)
	}
	for id := range x.pins {
		if !live[id] {
			delete(x.pins, id)
		}
	}
	return net
}

// settlePins makes the device's pins the specs: a pin that opened is applied
// at once, one that closed puts its path back, and the device's override set
// is replaced only when it changed.
func (x *problemExecutor) settlePins(d *sim.Device, specs map[string]calendar.Pin) {
	held := x.pins[d.DeviceID]
	same := len(held) == len(specs)
	for id := range specs {
		if _, ok := held[id]; !ok {
			same = false
		}
	}
	if same {
		return
	}
	ids := make([]string, 0, len(specs))
	for id := range specs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	next := map[string]pinned{}
	overrides := make([]sim.BehaviorOverrideSpec, 0, len(ids))
	for _, id := range ids {
		pin := specs[id]
		prev := held[id].prev
		if _, ok := held[id]; !ok {
			prev = d.ReadPath(pin.Path)
		}
		next[id] = pinned{path: pin.Path, prev: prev}
		overrides = append(overrides, sim.BehaviorOverrideSpec{OverrideID: id, Path: pin.Path, Expression: pin.Value, IntervalMs: int32(pinInterval / time.Millisecond)})
	}
	if err := d.SetBehaviorOverrides(overrides); err != nil {
		x.log.Warn("a scripted problem's pin did not apply", "device", d.Hostname, "error", err)
		return
	}
	for _, id := range ids {
		if _, ok := held[id]; !ok {
			if err := d.WritePath(id, specs[id].Path, specs[id].Value); err != nil {
				x.log.Warn("a scripted problem's pin did not apply", "device", d.Hostname, "error", err)
			}
		}
	}
	// What closed puts its path back, unless another pin still holds it.
	stillHeld := map[string]bool{}
	for _, p := range next {
		stillHeld[p.path] = true
	}
	for id, p := range held {
		if _, ok := next[id]; ok || stillHeld[p.path] || p.prev == nil || strings.Contains(fmt.Sprint(p.prev), "{{") {
			continue
		}
		if err := d.WritePath(id, p.path, fmt.Sprint(p.prev)); err != nil {
			x.log.Warn("a scripted problem's pin did not release", "device", d.Hostname, "error", err)
		}
	}
	x.pins[d.DeviceID] = next
}

// release puts every pin back, for a simulator that stops.
func (x *problemExecutor) release(devices []*sim.Device) {
	for _, d := range devices {
		x.settlePins(d, nil)
	}
}

// mergeNet folds the impairments the problems ask for into what the server
// asks for, by component id: unplugged if either unplugs, the larger loss.
func mergeNet(wants map[string]collection.SimImpairment, problems map[string]collection.SimImpairment) map[string]collection.SimImpairment {
	if len(problems) == 0 {
		return wants
	}
	out := make(map[string]collection.SimImpairment, len(wants)+len(problems))
	for id, w := range wants {
		out[id] = w
	}
	for id, p := range problems {
		w := out[id]
		w.Unplugged = w.Unplugged || p.Unplugged
		w.LossPct = max(w.LossPct, p.LossPct)
		out[id] = w
	}
	return out
}
