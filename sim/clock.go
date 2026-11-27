package sim

import (
	"fmt"
	"strings"
	"time"
)

// Clock is the simulated world's clock (#2800), the first, fixed-speed step of
// the simulator's accelerated clock (#1362): the time the simulated world
// reads, which is real time and an Offset. The calendar's funcs, `now`, a
// device's uptime anchor and every stamp the simulator writes in the world's
// own time read it; the outpost's collection cadence and every timestamp
// Omniglass stamps stay on real time, because they never read it. A lab test
// uses the offset to stand "now" inside a chosen meeting without waiting for
// it. #1362 adds a speed beside Offset; a Clock of the zero value is real
// time, the simulator's behavior without one.
type Clock struct {
	Offset time.Duration
}

// Now is the simulated world's time.
func (c Clock) Now() time.Time { return time.Now().Add(c.Offset) }

// ParseClockOffset reads the duration the lab hands the simulator (Go's
// duration syntax, signed: "36h", "-90m"). Empty is no offset.
func ParseClockOffset(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("sim clock offset %q: %w", s, err)
	}
	return d, nil
}

// SetClock gives the engine the simulated world's clock. Every device the
// engine builds reads time through it, so a device built before or after the
// call agrees. Safe to call at any time.
func (e *Engine) SetClock(c Clock) { e.clock.Store(&c) }

// Clock is the engine's simulated clock; the zero Clock (real time) when none
// was set.
func (e *Engine) Clock() Clock {
	if c := e.clock.Load(); c != nil {
		return *c
	}
	return Clock{}
}

// Now is the simulated world's time, the clock each of the engine's devices
// reads.
func (e *Engine) Now() time.Time { return e.Clock().Now() }
