package spec

import (
	"fmt"
	"time"
)

// Elapsed is a read-derived uptime: the whole seconds between an anchor
// (a Unix-epoch seconds value held at the Since state path) and the device's
// clock, read fresh at Path whenever anything looks. Nothing is stored at
// Path, no timer runs and no event is emitted, so an uptime never floods the
// diff stream; a reboot resets it by writing the anchor. A family declares
// its Elapsed values in Spec.Elapsed (#2845); the engine seeds an unset anchor
// to the device's start so a fresh device reads about zero.
type Elapsed struct {
	// Path is where every read surface (SNMP, HTTP, a session, the console's
	// snapshot) sees the value, e.g. status.device.uptime.
	Path string
	// Since is the state path holding the anchor, e.g. status.device.boot_epoch.
	Since string
}

// At is the value at now for an anchor epoch: whole seconds since it, zero
// while the anchor is unset (zero or negative) and zero, never negative, when
// the clock reads before it.
func (e Elapsed) At(epoch int64, now time.Time) int64 {
	if epoch <= 0 {
		return 0
	}
	if s := now.Unix() - epoch; s > 0 {
		return s
	}
	return 0
}

func validateElapsed(family string, list []Elapsed) error {
	seen := map[string]bool{}
	for _, e := range list {
		if e.Path == "" || e.Since == "" {
			return fmt.Errorf("family %q has an elapsed value with no path or no anchor", family)
		}
		if seen[e.Path] {
			return fmt.Errorf("family %q reads %q as two elapsed values", family, e.Path)
		}
		seen[e.Path] = true
	}
	return nil
}
