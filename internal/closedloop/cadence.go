package closedloop

import "time"

// NodeCadence is how often a node ticks and probes. The lab harness reads it
// off a node's command; the server, which is not told a node's flags, takes
// the node's defaults.
type NodeCadence struct {
	Heartbeat  time.Duration
	ProbeEvery time.Duration
}

// Interval is how often a poll task with schedule every actually runs: a
// driver poll at its own interval, checked each tick, so never faster than
// the heartbeat; a bare reachability probe (every empty) at the probe
// interval, every tick when there is none.
func (c NodeCadence) Interval(every string) time.Duration {
	var d time.Duration
	if every == "" {
		d = c.ProbeEvery
	} else if p, err := time.ParseDuration(every); err == nil && p > 0 {
		d = p
	}
	return max(d, c.Heartbeat)
}
