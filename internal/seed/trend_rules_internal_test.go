package seed

import (
	"fmt"
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
// heap and goroutines refilling over up to 18 hours after an ordinary
// restart, and a fresh database filling to its retention window, with its
// fleet there from day one or onboarded on days 4 to 6. A rule that one of
// those alarms ships disabled.
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
		// walk judges the rule hourly across a growth it must not alarm on,
		// and reports the first hour it fires (0 for none).
		walk := func(start float64, hours int, level func(t float64) float64, age func(t float64) float64) int {
			for h := 1; h <= hours; h++ {
				now := float64(h) * 3600
				w := seriesWindow(window, now, start, level)
				got, err := j.Decide(rules.Sample{Value: level(now), Key: r.SampleKey, Window: w, Age: age(now)})
				if err != nil {
					t.Fatalf("%s at hour %d: %v", r.Name, h, err)
				}
				if got == rules.Fire {
					return h
				}
			}
			return 0
		}
		type growth struct {
			name  string
			fired int
		}
		var walks []growth
		switch {
		case writerclass.ProcessSelf(r.SampleKey):
			// An ordinary restart at second 0 of a process that held its
			// level for days: the series spans the restart, so it reads the
			// level, the drop to a fifth, and the hours its caches and
			// sessions take to come back, then the level again; its age is
			// the new process's uptime. A refill of up to 18 hours, walked
			// for two days.
			for _, refill := range []float64{4, 8, 12, 18} {
				fired := walk(-7*24*3600, 48, func(t float64) float64 {
					if t < 0 {
						return base
					}
					return base * (0.2 + 0.8*min(1, t/(refill*3600)))
				}, func(t float64) float64 { return t })
				walks = append(walks, growth{fmt.Sprintf("a restart refilling over %vh", refill), fired})
			}
		case r.SampleKey == writerclass.DBSize:
			// A database's size is its schema and the last 14 days of its
			// fill (the metric lane's retention), its age its oldest kept
			// sample's, so at most the 14 days. Walked for three weeks.
			const day, retained = 24 * 3600.0, 14 * 24 * 3600.0
			// size is the database at second t filling at rate(t) a day.
			size := func(rate func(t float64) float64) func(t float64) float64 {
				return func(t float64) float64 {
					sum := 0.0
					for at := max(0, t-retained); at < t; at += 600 {
						sum += rate(at) * 600 / day
					}
					return base * (0.02 + sum/14)
				}
			}
			age := func(t float64) float64 { return min(t, retained) }
			// A fresh install whose fleet is all there from its first day.
			steady := size(func(float64) float64 { return 1 })
			walks = append(walks, growth{"a fresh install", walk(0, 21*24, steady, age)})
			// A fresh install that onboards most of its fleet on days 4 to
			// 6, its fill rising to three times its first days'.
			onboard := size(func(t float64) float64 { return 1 + 2*min(1, max(0, t-4*day)/(2*day)) })
			walks = append(walks, growth{"a fleet onboarded on days 4 to 6", walk(0, 21*24, onboard, age)})
		default:
			t.Errorf("%s has no growth walk it must not alarm on", r.Name)
		}
		// A rule ships on only if none of the healthy growths alarms it, and
		// off only for one that does: the walk is the reason it ships off.
		shippedOff := r.Enabled != nil && !*r.Enabled
		var alarmed []string
		for _, g := range walks {
			if g.fired != 0 {
				alarmed = append(alarmed, fmt.Sprintf("%s (fired at hour %d)", g.name, g.fired))
			}
		}
		switch {
		case !shippedOff && len(alarmed) > 0:
			t.Errorf("%s ships enabled yet alarms on healthy growth: %v", r.Name, alarmed)
		case shippedOff && len(alarmed) == 0:
			t.Errorf("%s ships disabled though no healthy growth alarms it: enable it", r.Name)
		}
	}
	for _, name := range []string{"process-heap-growing", "process-goroutines-growing", "db-size-growing"} {
		if !windowed[name] {
			t.Errorf("%s ships with no sample_window", name)
		}
	}
}
