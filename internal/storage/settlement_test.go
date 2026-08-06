package storage_test

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestSettle proves the pure settlement verdict: no intended value is none; within
// the window the verdict is pending regardless of the observed value; past the
// window a match is settled and anything else (mismatch or absent) is failed.
func TestSettle(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	cv := func(v string, ts time.Time) *storage.CurrentValue {
		return &storage.CurrentValue{Value: []byte(v), TS: ts}
	}
	cases := []struct {
		name               string
		intended, observed *storage.CurrentValue
		window             int
		want               storage.SettlementVerdict
	}{
		{"no intended is none", nil, cv(`"HDMI1"`, now), 30, storage.SettlementNone},
		{"within window is pending", cv(`"HDMI1"`, now.Add(-10*time.Second)), cv(`"HDMI2"`, now), 30, storage.SettlementPending},
		{"within window pending even if matched", cv(`"HDMI1"`, now.Add(-10*time.Second)), cv(`"HDMI1"`, now), 30, storage.SettlementPending},
		{"past window matched is settled", cv(`"HDMI1"`, now.Add(-60*time.Second)), cv(`"HDMI1"`, now), 30, storage.SettlementSettled},
		{"past window mismatched is failed", cv(`"HDMI1"`, now.Add(-60*time.Second)), cv(`"HDMI2"`, now), 30, storage.SettlementFailed},
		{"past window absent observed is failed", cv(`"HDMI1"`, now.Add(-60*time.Second)), nil, 30, storage.SettlementFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := storage.Settle(c.intended, c.observed, c.window, now); got != c.want {
				t.Fatalf("Settle: want %q, got %q", c.want, got)
			}
		})
	}
}
