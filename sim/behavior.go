package sim

import (
	"github.com/hyperscaleav/omniglass/sim/spec"
)

// StartBehaviors starts the family's animator, the seam its ticks run on.
// Safe to call more than once.
func (d *Device) StartBehaviors() { d.startAnimator() }

// StopBehaviors stops the animator and waits for it to wind down. Idempotent.
func (d *Device) StopBehaviors() { d.stopAnimator() }

// TickBehavior fires one tick of the named behavior now, as its timer would
// (a Go family's tick), and reports whether the device has it: a test stepping
// a device through its cycle without waiting on the clock.
func (d *Device) TickBehavior(name string) bool {
	d.behaviorMu.Lock()
	var t *spec.Tick
	if d.fam != nil {
		for i := range d.fam.ticks {
			if d.fam.ticks[i].Name == name {
				t = &d.fam.ticks[i]
			}
		}
	}
	d.behaviorMu.Unlock()
	if t == nil {
		return false
	}
	d.applyTick(*t)
	return true
}

// ensureRand lazily initializes the per-device random source on first
// use. Behaviors are the only consumer today; deferring construction
// to the first override keeps unrelated tests / device fixtures
// from paying for a rand.New they never touch.
func (d *Device) ensureRand() {
	d.behaviorMu.Lock()
	defer d.behaviorMu.Unlock()
	if d.rand == nil {
		d.rand = newDeviceRand(d.DeviceID)
	}
}

// Behaviors returns the names of all registered behaviors on the
// device, in declaration order. Used by tests and any consumer that needs
// to enumerate what's running.
func (d *Device) Behaviors() []string {
	d.behaviorMu.Lock()
	defer d.behaviorMu.Unlock()
	var out []string
	if d.fam != nil {
		for _, t := range d.fam.ticks {
			out = append(out, t.Name)
		}
	}
	return out
}
