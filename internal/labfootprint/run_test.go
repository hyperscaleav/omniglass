package labfootprint

import (
	"testing"
	"time"
)

// A harness query runs under what its context has left, capped at the
// ceiling, the ceiling with no deadline, and a millisecond once the deadline
// has passed, never zero, which Postgres reads as no limit.
func TestStatementTimeoutFollowsTheContext(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 12, 14, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		deadline time.Time
		ok       bool
		want     time.Duration
	}{
		{"no deadline", time.Time{}, false, psqlCeiling},
		{"within the ceiling", now.Add(90 * time.Second), true, 90 * time.Second},
		{"past the ceiling", now.Add(time.Hour), true, psqlCeiling},
		{"passed", now.Add(-time.Second), true, time.Millisecond},
		{"now", now, true, time.Millisecond},
	} {
		if got := statementTimeout(tc.deadline, tc.ok, now); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}
