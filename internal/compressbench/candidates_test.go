package compressbench

import (
	"slices"
	"testing"
	"time"
)

// TestCandidatesAtCadence holds the candidate set to #1518: F0 and F2 hour
// and day blocks and F5 day chunks at every cadence, week and month blocks
// and month chunks at 1 h only, and no pg_deltax.
func TestCandidatesAtCadence(t *testing.T) {
	names := func(c time.Duration) []string {
		var out []string
		for _, x := range Candidates(Spec{Cadence: c, Series: 1, Span: 24 * time.Hour}) {
			out = append(out, x.Name)
		}
		return out
	}
	base := []string{"raw", "F0", "F2", "F0/day", "F2/day", "F5"}
	for _, c := range []time.Duration{10 * time.Second, time.Minute, 5 * time.Minute, 24 * time.Hour} {
		if got := names(c); !slices.Equal(got, base) {
			t.Errorf("%s: %v, want %v", c, got, base)
		}
	}
	hourly := []string{"raw", "F0", "F2", "F0/day", "F2/day", "F0/week", "F2/week", "F0/month", "F2/month", "F5", "F5/month"}
	if got := names(time.Hour); !slices.Equal(got, hourly) {
		t.Errorf("1h: %v, want %v", got, hourly)
	}
}

// TestBlockSpanOffsets: millisecond offsets fit an int4 for a span of up to
// 24.8 days, so a month block's offsets are int8.
func TestBlockSpanOffsets(t *testing.T) {
	for span, want := range map[string]string{"hour": "int4[]", "day": "int4[]", "week": "int4[]", "month": "int8[]"} {
		if got := (Block{Span: span, Offsets: true}).tsType(); got != want {
			t.Errorf("%s: %s, want %s", span, got, want)
		}
	}
	if got := (Block{Span: "month"}).tsType(); got != "timestamptz[]" {
		t.Errorf("F0/month: %s", got)
	}
}
