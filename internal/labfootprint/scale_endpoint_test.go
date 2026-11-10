package labfootprint

import (
	"testing"
	"time"
)

func TestParseEpochReadsTheMicrosecond(t *testing.T) {
	for in, want := range map[string]time.Time{
		"1762700000.123456\n": time.Unix(1762700000, 123456000),
		"1762700000.5":        time.Unix(1762700000, 500000000),
		"1762700000":          time.Unix(1762700000, 0),
	} {
		got, err := parseEpoch(in)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseEpoch(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "now", "1.x"} {
		if _, err := parseEpoch(bad); err == nil {
			t.Errorf("parseEpoch(%q): want an error", bad)
		}
	}
}

// A window the receipts' horizon could have pruned, or one the clock ran
// backwards across, is refused rather than read as loss.
func TestEndpointWindowRefusesWhatReceiptsCannotCover(t *testing.T) {
	t0 := time.Unix(1762700000, 0)
	mark := func(d time.Duration) endpointMark { return endpointMark{At: t0.Add(d)} }
	if w, err := endpointWindow(mark(0), mark(10*time.Minute)); err != nil || w != 10*time.Minute {
		t.Fatalf("10 m window = %s, %v", w, err)
	}
	if _, err := endpointWindow(mark(0), mark(maxEndpointWindow+time.Second)); err == nil {
		t.Error("a window past the receipt horizon: want an error")
	}
	if _, err := endpointWindow(mark(time.Second), mark(0)); err == nil {
		t.Error("a backwards clock: want an error")
	}
}
