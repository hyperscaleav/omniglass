package api

import (
	"testing"
	"time"
)

// A bucketed series request names a range and a resolution (#1269). The grid
// it implies is bounded, so a fine resolution over a wide range is refused
// rather than answered with a truncated grid.
func TestSeriesBucketing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		hours, res   int
		wantBuckets  int
		wantRefusal  bool
		wantWindow   time.Duration
		wantInterval time.Duration
	}{
		{name: "30 days at an hour is 720", hours: 720, res: 3600, wantBuckets: 720, wantWindow: 720 * time.Hour, wantInterval: time.Hour},
		{name: "the default 24h window", hours: 0, res: 300, wantBuckets: 288, wantWindow: 24 * time.Hour, wantInterval: 5 * time.Minute},
		{name: "a range that is not a whole number of buckets rounds up", hours: 1, res: 1500, wantBuckets: 3, wantWindow: time.Hour, wantInterval: 25 * time.Minute},
		{name: "the cap itself is allowed", hours: 500, res: 360, wantBuckets: seriesMaxBuckets, wantWindow: 500 * time.Hour, wantInterval: 6 * time.Minute},
		{name: "one bucket past the cap is refused", hours: 500, res: 359, wantRefusal: true},
		{name: "a second over 30 days is refused", hours: 720, res: 1, wantRefusal: true},
		{name: "a 30 day bucket is the widest", hours: 720, res: seriesMaxResolution, wantBuckets: 1, wantWindow: 720 * time.Hour, wantInterval: 720 * time.Hour},
		{name: "past 30 days is refused, never an overflowed duration", hours: 24, res: seriesMaxResolution + 1, wantRefusal: true},
		{name: "a resolution that would overflow a Duration is refused", hours: 24, res: 1 << 62, wantRefusal: true},
	} {
		window, interval, n, err := seriesBucketing(tc.hours, tc.res)
		if tc.wantRefusal {
			if err == nil {
				t.Errorf("%s: want a refusal, got %d buckets", tc.name, n)
			}
			continue
		}
		if err != nil || n != tc.wantBuckets || window != tc.wantWindow || interval != tc.wantInterval {
			t.Errorf("%s: got window %s interval %s n %d err %v, want %s %s %d", tc.name, window, interval, n, err, tc.wantWindow, tc.wantInterval, tc.wantBuckets)
		}
	}
}
