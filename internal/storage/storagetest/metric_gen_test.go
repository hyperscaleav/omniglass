package storagetest

import (
	"math"
	"slices"
	"testing"
	"time"
)

var genMonday = time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)

// TestGenerateIsDeterministicAndInLandingOrder: one spec gives the same
// samples twice, each series in timestamp order on whole milliseconds, every
// shape present, and a poll now and then reported twice at one instant.
func TestGenerateIsDeterministicAndInLandingOrder(t *testing.T) {
	t.Parallel()
	spec := GenSpec{Cadence: 30 * time.Second, Series: 5, Start: genMonday, Span: 24 * time.Hour, Seed: GenSeed}
	a, b := Generate(spec), Generate(spec)
	if len(a) != len(b) || len(a) < 5*2880 {
		t.Fatalf("%d and %d samples, want the same, at least 5 series of 2,880 polls", len(a), len(b))
	}
	dupes := 0
	for i := range a {
		if a[i] != b[i] && !(math.IsNaN(a[i].Value) && math.IsNaN(b[i].Value)) {
			t.Fatalf("sample %d differs between runs: %+v and %+v", i, a[i], b[i])
		}
		if a[i].TS.Truncate(time.Millisecond) != a[i].TS {
			t.Fatalf("sample %d at %v, want a whole millisecond", i, a[i].TS)
		}
		if i > 0 && a[i].Series == a[i-1].Series {
			if a[i].TS.Before(a[i-1].TS) {
				t.Fatalf("sample %d at %v lands before its predecessor at %v", i, a[i].TS, a[i-1].TS)
			}
			if a[i].TS.Equal(a[i-1].TS) {
				dupes++
			}
		}
		if a[i].TS.Before(spec.Start) || !a[i].TS.Before(spec.Start.Add(spec.Span)) {
			t.Fatalf("sample %d at %v is outside the dataset", i, a[i].TS)
		}
	}
	if dupes == 0 {
		t.Error("no poll was reported twice at one instant")
	}
	if a[0].Value != 1 {
		t.Errorf("series 1 is the constant probe, its first value %v", a[0].Value)
	}
}

// TestDaySpansHoldTheRule: a series-day of 4,080 samples or more is cut into
// hours, one of 170 or more is a day, and a sparser one waits for its week;
// Blocks gathers the week's waiting days into one block from its Monday.
func TestDaySpansHoldTheRule(t *testing.T) {
	t.Parallel()
	var samples []GenSample
	plant := func(series int, day time.Time, n int) {
		step := 24 * time.Hour / time.Duration(n)
		for i := range n {
			samples = append(samples, GenSample{series, day.Add(time.Duration(i) * step), float64(i)})
		}
	}
	plant(1, genMonday, 4080)
	plant(1, genMonday.AddDate(0, 0, 1), 4079)
	plant(1, genMonday.AddDate(0, 0, 2), 170)
	plant(1, genMonday.AddDate(0, 0, 3), 169)
	plant(1, genMonday.AddDate(0, 0, 6), 1)
	var got []string
	for _, d := range DaySpans(samples) {
		got = append(got, d.Span)
	}
	if want := []string{"hour", "day", "day", "week", "week"}; !slices.Equal(got, want) {
		t.Fatalf("spans %v, want %v", got, want)
	}
	blocks := Blocks(samples)
	if len(blocks) != 24+2+1 {
		t.Fatalf("%d blocks, want 24 hours, 2 days and a week", len(blocks))
	}
	// Monday's first hour and its week start together; the key orders the
	// hour first.
	week := blocks[1]
	if week.Span != "week" || !week.SpanStart.Equal(genMonday) || len(week.Samples) != 170 {
		t.Errorf("second block %s from %v of %d samples, want the week from Monday of 170", week.Span, week.SpanStart, len(week.Samples))
	}
}

// TestMixedSpecTakesADayAndAWeekSpan: #1522's week of cadence changes gives a
// series both day blocks and the week block of its sparse days, and none of
// its cadences reaches an hour block.
func TestMixedSpecTakesADayAndAWeekSpan(t *testing.T) {
	t.Parallel()
	seen := map[string]int{}
	for _, d := range DaySpans(Generate(MixedSpec(genMonday, GenSeed))) {
		seen[d.Span]++
	}
	if seen["hour"] != 0 || seen["day"] == 0 || seen["week"] == 0 {
		t.Errorf("mixed spec spans %v, want days and weeks, no hours", seen)
	}
}
