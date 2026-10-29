package labfootprint

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Stat is one figure over the windows: the median, with the lowest and
// highest beside it, so the spread a shared host put on it is visible.
type Stat struct {
	Median float64 `json:"median"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	N      int     `json:"n"`
}

// StatOf is the median and range of xs.
func StatOf(xs []float64) Stat {
	if len(xs) == 0 {
		return Stat{}
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	m := s[len(s)/2]
	if len(s)%2 == 0 {
		m = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	return Stat{Median: m, Min: s[0], Max: s[len(s)-1], N: len(s)}
}

// LatencyStat is StatOf in milliseconds.
func LatencyStat(ds []time.Duration) Stat {
	xs := make([]float64, len(ds))
	for i, d := range ds {
		xs[i] = float64(d) / float64(time.Millisecond)
	}
	return StatOf(xs)
}

// Run is everything one harness run measured, before summary.
type Run struct {
	Build    string
	Scenario string
	Cadence  string
	Started  time.Time
	Warmup   time.Duration
	Window   time.Duration
	// The fleet the lab collected from.
	Systems, Components, Tasks int
	Windows                    []Window
	// Reads are the timed series reads by range in hours.
	Reads map[int][]time.Duration
}

// The containers in the order the report lists them, then their total.
var containerOrder = []string{"server", "node", "db"}

// ContainerStat is one container's figures over the windows.
type ContainerStat struct {
	Name          string `json:"name"`
	Millicores    Stat   `json:"millicores"`
	WorkingSetMiB Stat   `json:"working_set_mib"`
}

// ReadStat is the series read latency at one range.
type ReadStat struct {
	Hours int  `json:"hours"`
	Stat  Stat `json:"ms"`
}

// Report is a run's summary: its conditions and the median of each figure.
// It is the JSON a generator renders from.
type Report struct {
	Build      string    `json:"build"`
	Scenario   string    `json:"scenario"`
	Cadence    string    `json:"cadence"`
	Started    time.Time `json:"started"`
	WarmupS    int       `json:"warmup_seconds"`
	WindowS    int       `json:"window_seconds"`
	Systems    int       `json:"systems"`
	Components int       `json:"components"`
	Tasks      int       `json:"tasks"`

	MetricRowsPerMinute   Stat            `json:"metric_rows_per_minute"`
	MetricRowsPerDay      Stat            `json:"metric_rows_per_day"`
	PropertyRowsPerMinute Stat            `json:"property_rows_per_minute"`
	MetricBytesPerDay     Stat            `json:"metric_bytes_per_day"`
	PropertyBytesPerDay   Stat            `json:"property_bytes_per_day"`
	ReceiptBytesPerDay    Stat            `json:"receipt_bytes_per_day"`
	DatabaseBytesPerDay   Stat            `json:"database_bytes_per_day"`
	WALBytesPerDay        Stat            `json:"wal_bytes_per_day"`
	Containers            []ContainerStat `json:"containers"`
	ReadLatencyMS         []ReadStat      `json:"read_latency_ms"`
}

// Summarize takes the median of every figure over a run's windows. The
// total is summed per window before its median is taken, so it is a total
// some window really had.
func Summarize(r Run) Report {
	col := func(f func(Window) float64) Stat {
		xs := make([]float64, len(r.Windows))
		for i, w := range r.Windows {
			xs[i] = f(w)
		}
		return StatOf(xs)
	}
	out := Report{
		Build: r.Build, Scenario: r.Scenario, Cadence: r.Cadence, Started: r.Started.UTC(),
		WarmupS: int(r.Warmup.Seconds()), WindowS: int(r.Window.Seconds()),
		Systems: r.Systems, Components: r.Components, Tasks: r.Tasks,
		MetricRowsPerMinute:   col(func(w Window) float64 { return w.MetricRowsPerMinute }),
		MetricRowsPerDay:      col(func(w Window) float64 { return w.MetricRowsPerMinute * 60 * 24 }),
		PropertyRowsPerMinute: col(func(w Window) float64 { return w.PropertyRowsPerMinute }),
		MetricBytesPerDay:     col(func(w Window) float64 { return w.MetricBytesPerDay }),
		PropertyBytesPerDay:   col(func(w Window) float64 { return w.PropertyBytesPerDay }),
		ReceiptBytesPerDay:    col(func(w Window) float64 { return w.ReceiptBytesPerDay }),
		DatabaseBytesPerDay:   col(func(w Window) float64 { return w.DatabaseBytesPerDay }),
		WALBytesPerDay:        col(func(w Window) float64 { return w.WALBytesPerDay }),
	}
	for _, name := range containerOrder {
		out.Containers = append(out.Containers, ContainerStat{
			Name:          name,
			Millicores:    col(func(w Window) float64 { return w.Containers[name].Millicores }),
			WorkingSetMiB: col(func(w Window) float64 { return w.Containers[name].WorkingSetMiB }),
		})
	}
	sum := func(f func(ContainerWindow) float64) func(Window) float64 {
		return func(w Window) float64 {
			var t float64
			for _, name := range containerOrder {
				t += f(w.Containers[name])
			}
			return t
		}
	}
	out.Containers = append(out.Containers, ContainerStat{
		Name:          "total",
		Millicores:    col(sum(func(c ContainerWindow) float64 { return c.Millicores })),
		WorkingSetMiB: col(sum(func(c ContainerWindow) float64 { return c.WorkingSetMiB })),
	})
	hours := make([]int, 0, len(r.Reads))
	for h := range r.Reads {
		hours = append(hours, h)
	}
	sort.Ints(hours)
	for _, h := range hours {
		out.ReadLatencyMS = append(out.ReadLatencyMS, ReadStat{Hours: h, Stat: LatencyStat(r.Reads[h])})
	}
	return out
}

// Table renders the report for a terminal: the run's conditions, then one
// line per figure, its median and its range.
func (r Report) Table() string {
	var b strings.Builder
	fmt.Fprintf(&b, "lab footprint: build %s, scenario %s, cadence %s; %d systems, %d components, %d tasks\n",
		r.Build, r.Scenario, r.Cadence, r.Systems, r.Components, r.Tasks)
	n := r.MetricRowsPerMinute.N
	fmt.Fprintf(&b, "%d windows of %s after a %s warm-up; each figure is the median (lowest to highest)\n\n",
		n, time.Duration(r.WindowS)*time.Second, time.Duration(r.WarmupS)*time.Second)
	line := func(name string, s Stat, f func(float64) string) {
		fmt.Fprintf(&b, "  %-28s %14s   (%s to %s)\n", name, f(s.Median), f(s.Min), f(s.Max))
	}
	line("metric rows a minute", r.MetricRowsPerMinute, count)
	line("metric rows a day", r.MetricRowsPerDay, count)
	line("property rows a minute", r.PropertyRowsPerMinute, count)
	line("metric table a day", r.MetricBytesPerDay, size)
	line("property table a day", r.PropertyBytesPerDay, size)
	line("receipts a day", r.ReceiptBytesPerDay, size)
	line("database a day", r.DatabaseBytesPerDay, size)
	line("WAL a day", r.WALBytesPerDay, size)
	for _, c := range r.Containers {
		line(c.Name+" cpu", c.Millicores, func(v float64) string { return fmt.Sprintf("%.0f m", v) })
	}
	for _, c := range r.Containers {
		line(c.Name+" memory", c.WorkingSetMiB, func(v float64) string { return fmt.Sprintf("%.0f MiB", v) })
	}
	for _, rd := range r.ReadLatencyMS {
		line("series read, "+Range(rd.Hours), rd.Stat, func(v float64) string { return fmt.Sprintf("%.1f ms", v) })
	}
	return b.String()
}

// Range names a read's range: whole days as days, else hours.
func Range(hours int) string {
	if hours >= 48 && hours%24 == 0 {
		return fmt.Sprintf("%d d", hours/24)
	}
	return fmt.Sprintf("%d h", hours)
}

// count renders a count with thousands separators.
func count(v float64) string {
	n := int64(math.Round(v))
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprint(n)
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	out := strings.Join(parts, ",")
	if neg {
		out = "-" + out
	}
	return out
}

// size renders a size in binary units.
func size(v float64) string {
	switch a := math.Abs(v); {
	case a >= 1<<30:
		return fmt.Sprintf("%.2f GiB", v/(1<<30))
	case a >= 1<<20:
		return fmt.Sprintf("%.0f MiB", v/(1<<20))
	case a >= 1<<10:
		return fmt.Sprintf("%.0f KiB", v/(1<<10))
	}
	return fmt.Sprintf("%.0f B", v)
}
