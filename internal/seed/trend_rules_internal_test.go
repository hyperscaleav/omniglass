package seed

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
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

// seriesWindow is the window a rule reads at second now of a series begun at
// second start, as the store hands it: the window's even points, oldest
// first, the ones before the series began absent, each the level at its time.
func seriesWindow(window time.Duration, now, start float64, level func(t float64) float64) []rules.Point {
	n := rules.MaxWindowPoints
	var pts []rules.Point
	for i := range n {
		at := now - (1-float64(i)/float64(n-1))*window.Seconds()
		if at >= start {
			pts = append(pts, rules.Point{Value: level(at), At: at})
		}
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
// Then each is walked through the growth it must not alarm on: a process's
// heap and goroutines refilling after an ordinary restart, and a fresh
// database filling to its retention window.
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
		if !j.ReadsAge() {
			t.Errorf("%s does not gate on its series' age", r.Name)
		}
		const base = 50e6
		// A series long past any age gate.
		const aged = 30 * 24 * 3600
		type judged struct {
			name   string
			window []rules.Point
			age    float64
			want   rules.Action
		}
		cases := []judged{
			{"a steady rise", trendWindow(window, func(x float64) float64 { return base * (1 + 0.5*x) }), aged, rules.Fire},
			{"a flat noisy series", trendWindow(window, func(x float64) float64 { return base * (1 + 0.1*jitter(x)) }), aged, rules.Clear},
		}
		// A warm-up: a process's first hour, climbing to its level in ten
		// minutes and holding, too short a series to judge either way.
		cases = append(cases, judged{"a warm-up", trendWindow(time.Hour, func(x float64) float64 { return base * min(1, 0.2+6*x) }), 3600, rules.Hold})
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
			cases = append(cases, judged{"retention's sawtooth", saw, aged, rules.Clear})
		}
		for _, tc := range cases {
			got, err := j.Decide(rules.Sample{Value: tc.window[len(tc.window)-1].Value, Key: r.SampleKey, Window: tc.window, Age: tc.age})
			if err != nil {
				t.Errorf("%s on %s: %v", r.Name, tc.name, err)
				continue
			}
			if got != tc.want {
				t.Errorf("%s on %s: %v, want %v", r.Name, tc.name, got, tc.want)
			}
		}
		walk := func(name string, start float64, hours int, level func(t float64) float64, age func(t float64) float64) {
			t.Helper()
			for h := 1; h <= hours; h++ {
				now := float64(h) * 3600
				w := seriesWindow(window, now, start, level)
				got, err := j.Decide(rules.Sample{Value: level(now), Key: r.SampleKey, Window: w, Age: age(now)})
				if err != nil || got == rules.Fire {
					t.Errorf("%s on %s at hour %d: %v, %v; want no fire", r.Name, name, h, got, err)
					return
				}
			}
		}
		switch {
		case writerclass.ProcessSelf(r.SampleKey):
			// An ordinary restart at second 0 of a process that held its
			// level for days: the series spans the restart, so it reads the
			// level, the drop to a fifth, and the four hours its caches take
			// to refill, then the level again; its age is the new process's
			// uptime. Walked for half a day.
			walk("a restart", -7*24*3600, 12, func(t float64) float64 {
				if t < 0 {
					return base
				}
				return base * (0.2 + 0.8*min(1, t/(4*3600)))
			}, func(t float64) float64 { return t })
		case r.SampleKey == writerclass.DBSize:
			// A fresh database: its schema, then a day's samples a day until
			// the metric lane's 14-day retention starts dropping a day as the
			// next fills, then level; its age is its oldest kept sample's,
			// so at most the 14 days. Walked for three weeks.
			const retained = 14 * 24 * 3600
			walk("a fresh install", 0, 21*24, func(t float64) float64 {
				return base * (0.02 + min(t, retained)/retained)
			}, func(t float64) float64 { return min(t, retained) })
		default:
			t.Errorf("%s has no growth walk it must not alarm on", r.Name)
		}
	}
	for _, name := range []string{"process-heap-growing", "process-goroutines-growing", "db-size-growing"} {
		if !windowed[name] {
			t.Errorf("%s ships with no sample_window", name)
		}
	}
}
