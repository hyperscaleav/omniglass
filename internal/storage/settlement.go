package storage

import (
	"bytes"
	"time"
)

// SettlementVerdict is the computed state of a command's effect: never stored,
// always derived from the intended value it opened, the observed value the device
// reports, and the command_type's settle window (the driver's fact about how long
// the device takes to actuate). This is what turns the series' want/told/is
// pivot into a command-settlement judgment.
type SettlementVerdict string

const (
	// SettlementNone: nothing was commanded (no intended value), so there is
	// nothing to settle.
	SettlementNone SettlementVerdict = "none"
	// SettlementPending: still within the settle window since the command was
	// issued, so a difference from observed is not yet drift (the device is given
	// time to actuate).
	SettlementPending SettlementVerdict = "pending"
	// SettlementSettled: past the window, the observed value matches the intended
	// one, so the command took effect.
	SettlementSettled SettlementVerdict = "settled"
	// SettlementFailed: past the window, the observed value does not match the
	// intended one (or is absent), so the command did not take effect.
	SettlementFailed SettlementVerdict = "failed"
)

// Settle computes a command's settlement verdict. Pure: the whole judgment is a
// function of the intended value and its time, the observed value, the window, and
// now. Within the window the verdict is pending regardless of the observed value;
// past it, a match is settled and anything else is failed. A nil intended value is
// none (nothing was told).
func Settle(intended, observed *CurrentValue, windowSeconds int, now time.Time) SettlementVerdict {
	if intended == nil {
		return SettlementNone
	}
	if now.Sub(intended.TS) < time.Duration(windowSeconds)*time.Second {
		return SettlementPending
	}
	if observed != nil && bytes.Equal(observed.Value, intended.Value) {
		return SettlementSettled
	}
	return SettlementFailed
}
