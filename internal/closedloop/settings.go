package closedloop

import (
	"fmt"
	"time"
)

// RawSettings is the closed loop's server settings as the environment
// carries them; empty is unset.
type RawSettings struct {
	// Bracket is OMNIGLASS_CLOSED_LOOP_BRACKET: how often the ring brackets.
	Bracket string
	// Heartbeat is OMNIGLASS_CLOSED_LOOP_HEARTBEAT: the check-in interval the
	// server assumes for each node.
	Heartbeat string
	// ProbeEvery is OMNIGLASS_CLOSED_LOOP_PROBE_EVERY: the probe interval the
	// server assumes for each node.
	ProbeEvery string
}

// Settings is the closed loop's server settings: the ring's bracket interval
// and the cadence the server assumes for every node, which it is not told.
type Settings struct {
	Every   time.Duration
	Cadence NodeCadence
}

// MinBracket is the shortest bracket a server takes: a bracket is a request
// to every connected simulator, so a faster one only loads them.
const MinBracket = time.Second

// ParseSettings reads raw, unset figures taking the defaults: a bracket each
// RingEvery and a node at defaultHeartbeat with a probe on every check-in. A
// figure that is not a positive duration, or a bracket outside MinBracket to
// RingEvery, is an error naming its variable, so it fails the boot.
func ParseSettings(raw RawSettings, defaultHeartbeat time.Duration) (Settings, error) {
	s := Settings{Every: RingEvery, Cadence: NodeCadence{Heartbeat: defaultHeartbeat}}
	for _, f := range []struct {
		name, raw string
		into      *time.Duration
	}{
		{"OMNIGLASS_CLOSED_LOOP_BRACKET", raw.Bracket, &s.Every},
		{"OMNIGLASS_CLOSED_LOOP_HEARTBEAT", raw.Heartbeat, &s.Cadence.Heartbeat},
		{"OMNIGLASS_CLOSED_LOOP_PROBE_EVERY", raw.ProbeEvery, &s.Cadence.ProbeEvery},
	} {
		if f.raw == "" {
			continue
		}
		d, err := time.ParseDuration(f.raw)
		if err != nil || d <= 0 {
			return Settings{}, fmt.Errorf("%s: %q is not a positive duration (say 2s, 1m)", f.name, f.raw)
		}
		*f.into = d
	}
	if s.Every < MinBracket || s.Every > RingEvery {
		return Settings{}, fmt.Errorf("OMNIGLASS_CLOSED_LOOP_BRACKET: %s is outside %s to %s", s.Every, MinBracket, RingEvery)
	}
	return s, nil
}
