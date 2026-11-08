package node

import (
	"testing"
	"time"
)

// The wait after a failed pull doubles from 1 s to the 30 s cap, jittered
// over the upper half of each step (#1577).
func TestAFailedPullBacksOffFromOneSecondToTheCap(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		attempt  int
		low, top time.Duration
	}{
		{1, 500 * time.Millisecond, time.Second},
		{2, time.Second, 2 * time.Second},
		{3, 2 * time.Second, 4 * time.Second},
		{5, 8 * time.Second, 16 * time.Second},
		{6, 15 * time.Second, 30 * time.Second},
		{40, 15 * time.Second, 30 * time.Second},
		{0, 15 * time.Second, 30 * time.Second},
	} {
		if got := pullBackoff(c.attempt, 0); got != c.low {
			t.Errorf("attempt %d, no jitter: %v, want %v", c.attempt, got, c.low)
		}
		if got := pullBackoff(c.attempt, 0.999999); got < c.low || got >= c.top {
			t.Errorf("attempt %d, most jitter: %v, want in [%v, %v)", c.attempt, got, c.low, c.top)
		}
	}
}

// Backoff never grows past the cap, however many pulls fail.
func TestPullBackoffNeverPassesTheCap(t *testing.T) {
	t.Parallel()
	for attempt := 1; attempt < 200; attempt++ {
		if got := pullBackoff(attempt, 0.999999); got > pullBackoffCap || got <= 0 {
			t.Fatalf("attempt %d: %v", attempt, got)
		}
	}
}

// The pull interval is spread ten percent either side.
func TestThePullIntervalIsJitteredTenPercent(t *testing.T) {
	t.Parallel()
	every := 30 * time.Second
	if got := jitterInterval(every, 0); got != 27*time.Second {
		t.Errorf("least jitter: %v, want 27s", got)
	}
	if got := jitterInterval(every, 0.5); got != every {
		t.Errorf("middle jitter: %v, want 30s", got)
	}
	if got := jitterInterval(every, 0.999999); got < 32*time.Second || got > 33*time.Second {
		t.Errorf("most jitter: %v, want under 33s", got)
	}
}
