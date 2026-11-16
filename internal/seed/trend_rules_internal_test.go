package seed

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/hyperscaleav/omniglass/internal/rules"
)

// trendWindow is a window's worth of points as the store hands a rule one
// (#1985): MaxWindowPoints of them, oldest first, ending now, each the level
// at its share x of the window (0 the oldest, 1 the newest).
func trendWindow(window time.Duration, level func(x float64) float64) []rules.Point {
	n := rules.MaxWindowPoints
	end := float64(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC).Unix())
	pts := make([]rules.Point, n)
	for i := range pts {
		x := float64(i) / float64(n-1)
		pts[i] = rules.Point{Value: level(x), At: end - (1-x)*window.Seconds()}
	}
	return pts
}

// jitter is a fixed noise in [-1, 1] at share x, so a test series is the same
// every run.
func jitter(x float64) float64 {
	i := int(x * 10000)
	return float64((i*7919)%201-100) / 100
}

// TestTheShippedTrendRulesFireOnASteadyRise judges the shipped trend rules
// (#1985) on fixed series over their own windows: a steady rise of half the
// level fires each, a flat series with a tenth's noise clears each, a
// process's first hour (its warm-up, too short a series to judge) holds, and
// a database at its retention window, climbing through the window and
// dropping a fourteenth (a day's partition), clears wherever the drop falls.
func TestTheShippedTrendRulesFireOnASteadyRise(t *testing.T) {
	var doc eventRulesDoc
	if err := yaml.Unmarshal(eventRulesYAML, &doc); err != nil {
		t.Fatal(err)
	}
	windowed := map[string]bool{}
	for _, r := range doc.Rules {
		if r.SampleWindow == "" {
			continue
		}
		windowed[r.Name] = true
		window, err := parseSeedDuration(r.SampleWindow)
		if err != nil {
			t.Fatal(err)
		}
		clear := rules.Criteria{Expr: r.Clear}
		j, err := rules.Compile(rules.LaneMetric, "int", rules.Criteria{Expr: r.Fire}, &clear)
		if err != nil {
			t.Fatalf("%s: %v", r.Name, err)
		}
		if !j.ReadsWindow() {
			t.Errorf("%s has a sample_window its criteria do not read", r.Name)
		}
		const base = 50e6
		cases := []struct {
			name   string
			window []rules.Point
			want   rules.Action
		}{
			{"a steady rise", trendWindow(window, func(x float64) float64 { return base * (1 + 0.5*x) }), rules.Fire},
			{"a flat noisy series", trendWindow(window, func(x float64) float64 { return base * (1 + 0.1*jitter(x)) }), rules.Clear},
		}
		// A warm-up: a process's first hour, climbing to its level in ten
		// minutes and holding, too short a series to judge either way.
		cases = append(cases, struct {
			name   string
			window []rules.Point
			want   rules.Action
		}{"a warm-up", trendWindow(time.Hour, func(x float64) float64 { return base * min(1, 0.2+6*x) }), rules.Hold})
		// A database at its retention window: a day's metric partition, a
		// fourteenth of it, filled through the window and dropped at share f.
		for _, f := range []float64{0.02, 0.25, 0.5, 0.75, 0.98} {
			saw := trendWindow(window, func(x float64) float64 {
				day := x
				if x > f {
					day -= 1
				}
				return base * (1 + day/14)
			})
			cases = append(cases, struct {
				name   string
				window []rules.Point
				want   rules.Action
			}{"retention's sawtooth", saw, rules.Clear})
		}
		for _, tc := range cases {
			got, err := j.Decide(rules.Sample{Value: tc.window[len(tc.window)-1].Value, Key: r.SampleKey, Window: tc.window})
			if err != nil {
				t.Errorf("%s on %s: %v", r.Name, tc.name, err)
				continue
			}
			if got != tc.want {
				t.Errorf("%s on %s: %v, want %v", r.Name, tc.name, got, tc.want)
			}
		}
	}
	for _, name := range []string{"process-heap-growing", "process-goroutines-growing", "db-size-growing"} {
		if !windowed[name] {
			t.Errorf("%s ships with no sample_window", name)
		}
	}
}
