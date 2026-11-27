package simoutpost

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/impair"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// The problem executor (#2769) is the one place a scripted problem becomes
// something a device does. A problem is a fact of the world on the lab
// calendar: the executor reads the calendar through the clock, and for each
// device a problem is active on (the room it sits in and the slot it fills,
// never its product) it applies the problem's impairment when the window opens
// and removes it when the window closes:
//
//   - a pin holds a state path at a value and puts the path back as it was
//     when the window closes;
//   - unplugged and packet loss are the network's, applied by the impairer
//     (NET_ADMIN only).
//
// The executor writes to neither seam. It registers each active problem's wants
// with the impairment arbiter (sim.Arbiter, #2799) as a source of its own,
// "problem:<name>", and releases it when the window closes; the arbiter merges
// them with the operator's and applies the result.
//
// Devices do not re-implement problems; omniglass is told nothing and learns
// of a problem only by collecting from the device it broke.

// problemSource names the arbiter source one scripted problem registers as.
const problemSource = "problem:"

type problemExecutor struct {
	cal   *calendar.Calendar
	w     *world.World
	arb   *sim.Arbiter
	clock func() time.Time
	log   *slog.Logger
	// held is the problem sources registered with the arbiter, so one whose
	// window closed is released.
	held map[string]bool
	// refused is the net problems already logged as refused, by key.
	refused map[string]bool
}

// newProblemExecutor is nil for a world with no scripted problems: a device
// with no calendar, or a calendar with no problems, is untouched.
func newProblemExecutor(w *world.World, cal *calendar.Calendar, arb *sim.Arbiter, clock func() time.Time, log *slog.Logger) *problemExecutor {
	if w == nil || cal == nil || arb == nil || len(w.Problems) == 0 {
		return nil
	}
	if clock == nil {
		clock = time.Now
	}
	return &problemExecutor{cal: cal, w: w, arb: arb, clock: clock, log: log, held: map[string]bool{}, refused: map[string]bool{}}
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

// reconcile registers with the arbiter what the calendar says now, one source
// per active problem, and releases the problems whose window closed. netOK says
// whether the simulator may apply network impairments; without it they are
// refused with a log line, once per window (the pins still apply). It does not
// settle: the arbiter's Settle applies.
func (x *problemExecutor) reconcile(devices []*sim.Device, netOK bool) {
	now := x.clock()
	wants := map[string]map[string]sim.Want{}
	for _, d := range devices {
		for _, e := range x.activeOn(d, now) {
			src := problemSource + e.Problem.Name
			imp := e.Problem.Impairment
			w := sim.Want{Since: e.Start}
			if imp.Pin != nil {
				w.Pins = []sim.Pin{{Path: imp.Pin.Path, Value: imp.Pin.Value}}
			} else {
				if !netOK {
					key := fmt.Sprintf("%s|%s|%d", e.Problem.Name, d.Hostname, e.Start.Unix())
					if !x.refused[key] {
						x.refused[key] = true
						x.log.Warn("a scripted problem needs the network and is not applied: the simulator lacks NET_ADMIN or --impairments",
							"problem", e.Problem.Name, "device", d.Hostname, "role", e.Problem.Role)
					}
				}
				w.Impairment = impair.Impairment{Unplugged: imp.Unplugged, LossPct: imp.LossPct}
			}
			if wants[src] == nil {
				wants[src] = map[string]sim.Want{}
			}
			wants[src][d.DeviceID] = w
		}
	}
	for src, byDev := range wants {
		x.arb.Set(src, byDev)
		x.held[src] = true
	}
	for src := range x.held {
		if _, ok := wants[src]; !ok {
			x.arb.Release(src)
			delete(x.held, src)
		}
	}
}

// release lets go of every problem, for a simulator that stops. The arbiter
// puts the pinned paths back.
func (x *problemExecutor) release() {
	for src := range x.held {
		x.arb.Release(src)
		delete(x.held, src)
	}
}
