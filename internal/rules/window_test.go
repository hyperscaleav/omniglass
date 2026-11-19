package rules

import (
	"testing"
	"time"
)

// trendSeries is six hours of a sample every 30s: a steady rise from 50MB to
// 75MB, and a flat 50MB with a fixed jitter of up to 10% either way.
func trendSeries() (rising, noisy []Point) {
	for i := range 721 {
		at := float64(1_700_000_000 + 30*i)
		rising = append(rising, Point{Value: 50e6 + 35e3*float64(i), At: at})
		noise := float64((i*7919)%201-100) / 100
		noisy = append(noisy, Point{Value: 50e6 * (1 + 0.1*noise), At: at})
	}
	return rising, noisy
}

// A windowed rule (#1985) judges the sample with its series' window bound:
// a fixed rising series fires the trend criterion, a flat noisy one clears
// it, and an empty window (a rule with none, a series just begun) has no
// trend, so it never fires.
func TestWindowedJudgeRiseVersusNoise(t *testing.T) {
	j, err := Compile(LaneMetric, "", Criteria{Expr: "span(window) >= 18000 && growth(window) > 0.25"}, &Criteria{Expr: "growth(window) <= 0.1"})
	if err != nil {
		t.Fatal(err)
	}
	rising, noisy := trendSeries()
	for _, tc := range []struct {
		name   string
		window []Point
		want   Action
	}{
		{"a steady rise", rising, Fire},
		{"a flat noisy series", noisy, Clear},
		{"no window", nil, Clear},
		{"the rise's first two hours", rising[:241], Hold},
	} {
		s := Sample{Value: 1.0, Key: "process-heap-size", Window: tc.window}
		got, err := j.Decide(s)
		if err != nil || got != tc.want {
			t.Errorf("%s: Decide = %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
}

// ReadsWindow tells a rule whose criteria read its window from one whose
// criteria read only the sample, so a store reads a window only for a rule
// that asks for one.
func TestJudgeReadsWindow(t *testing.T) {
	for _, tc := range []struct {
		fire  string
		clear string
		want  bool
	}{
		{"value > 1", "", false},
		{"growth(window) > 0.2", "", true},
		{"value > 1", "slope(window) <= 0", true},
		{"len(window) > 0", "", true},
	} {
		var clear *Criteria
		if tc.clear != "" {
			clear = &Criteria{Expr: tc.clear}
		}
		j, err := Compile(LaneMetric, "", Criteria{Expr: tc.fire}, clear)
		if err != nil {
			t.Fatalf("%q: %v", tc.fire, err)
		}
		if got := j.ReadsWindow(); got != tc.want {
			t.Errorf("%q / %q: ReadsWindow = %v, want %v", tc.fire, tc.clear, got, tc.want)
		}
	}
	// Only a metric has a window: a property's criteria cannot read one.
	if _, err := Compile(LaneProperty, "string", Criteria{Expr: "len(window) > 0"}, nil); err == nil {
		t.Error("a property's criteria compiled reading window")
	}
}

// A window's bucket keeps any window to at most MaxWindowPoints points,
// whatever the series' cadence.
func TestWindowBucket(t *testing.T) {
	for _, tc := range []struct {
		window, want time.Duration
	}{
		{6 * time.Hour, time.Minute},
		{24 * time.Hour, 4 * time.Minute},
		{MaxSampleWindow, MaxSampleWindow / MaxWindowPoints},
		{0, 0},
	} {
		if got := WindowBucket(tc.window); got != tc.want {
			t.Errorf("WindowBucket(%s) = %s, want %s", tc.window, got, tc.want)
		}
	}
}

// A rule's age (#1985) is how long the series it judges has run: an age
// gate holds a trend criterion until the series is old enough that its
// window is not a fill from empty or a refill after a restart.
func TestAgeGatesATrend(t *testing.T) {
	j, err := Compile(LaneMetric, "", Criteria{Expr: "age >= 36000 && growth(window) > 0.25"}, &Criteria{Expr: "growth(window) <= 0.1"})
	if err != nil {
		t.Fatal(err)
	}
	rising, _ := trendSeries()
	for _, tc := range []struct {
		name string
		age  float64
		want Action
	}{
		{"a series younger than the gate", 21600, Hold},
		{"a series past the gate", 36000, Fire},
	} {
		got, err := j.Decide(Sample{Value: 1.0, Key: "process-heap-size", Window: rising, Age: tc.age})
		if err != nil || got != tc.want {
			t.Errorf("%s: Decide = %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
}

// ReadsAge tells a rule whose criteria read the series' age, so a store
// reads one only for a rule that asks for it.
func TestJudgeReadsAge(t *testing.T) {
	for _, tc := range []struct {
		fire  string
		clear string
		want  bool
	}{
		{"value > 1", "", false},
		{"growth(window) > 0.2", "", false},
		{"age > 60 && value > 1", "", true},
		{"value > 1", "age < 60", true},
	} {
		var clear *Criteria
		if tc.clear != "" {
			clear = &Criteria{Expr: tc.clear}
		}
		j, err := Compile(LaneMetric, "", Criteria{Expr: tc.fire}, clear)
		if err != nil {
			t.Fatalf("%q: %v", tc.fire, err)
		}
		if got := j.ReadsAge(); got != tc.want {
			t.Errorf("%q / %q: ReadsAge = %v, want %v", tc.fire, tc.clear, got, tc.want)
		}
	}
	if _, err := Compile(LaneProperty, "string", Criteria{Expr: "age > 0"}, nil); err == nil {
		t.Error("a property's criteria compiled reading age")
	}
}
