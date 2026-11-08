package labfootprint

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This file is the scale lab's measurement (#1556): the arithmetic of one
// table, pure and tested with fixed inputs. It reuses the window rates and the
// number formats of the single-node footprint (measure.go, report.go) and adds
// what only a fleet shows: percentiles, the ingest lag, the consumer's
// redeliveries, and the retention pass's drain rate. The harness that reads a
// running lab (scale_run.go) feeds it.

// Percentile is the nearest-rank p-th percentile of xs: the smallest value at
// or above p percent of them, so it is always a value that was observed. xs is
// not reordered. No values reads as 0.
func Percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	rank := int(math.Ceil(p * float64(len(s)) / 100))
	rank = max(1, min(rank, len(s)))
	return s[rank-1]
}

// Quantiles is a sample's median and 95th percentile, with its size.
type Quantiles struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	N   int     `json:"n"`
}

// QuantilesOf is the p50 and p95 of xs.
func QuantilesOf(xs []float64) Quantiles {
	if len(xs) == 0 {
		return Quantiles{}
	}
	return Quantiles{P50: Percentile(xs, 50), P95: Percentile(xs, 95), N: len(xs)}
}

// DurationQuantiles is QuantilesOf in milliseconds.
func DurationQuantiles(ds []time.Duration) Quantiles {
	xs := make([]float64, len(ds))
	for i, d := range ds {
		xs[i] = float64(d) / float64(time.Millisecond)
	}
	return QuantilesOf(xs)
}

// ParseLagSeconds reads the lag query's output: one number of seconds a line,
// an empty line for a row with no commit time, which is left out.
func ParseLagSeconds(out string) ([]float64, error) {
	var lags []float64
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		v, err := strconv.ParseFloat(line, 64)
		if err != nil {
			return nil, fmt.Errorf("ingest lag: %q is not a number of seconds", line)
		}
		lags = append(lags, v)
	}
	return lags, nil
}

// Redeliveries is what the server's log says about batches the consumer saw
// more than once. A Nak redelivery logs nothing, so this is a floor: the acks
// the stream did not confirm (the batch comes back after its ack wait, and
// its receipt holds the second landing) and the batches given up on after
// their deliveries ran out.
type Redeliveries struct {
	Unconfirmed int `json:"unconfirmed_acks"`
	Abandoned   int `json:"abandoned"`
}

// Total is every redelivery the log shows.
func (r Redeliveries) Total() int { return r.Unconfirmed + r.Abandoned }

const (
	logAckNotConfirmed = "telemetry ack not confirmed, the batch will be redelivered"
	logAbandoned       = "telemetry message dropped after repeated failures"
)

// CountRedeliveries counts the redelivery lines in a server's log.
func CountRedeliveries(r io.Reader) (Redeliveries, error) {
	var n Redeliveries
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.Contains(line, logAckNotConfirmed):
			n.Unconfirmed++
		case strings.Contains(line, logAbandoned):
			n.Abandoned++
		}
	}
	return n, sc.Err()
}

// DrainSample is the retention backlog still in the table at an instant.
type DrainSample struct {
	At        time.Time
	Remaining int64
}

// Drain is how the retention pass cleared a backlog of aged rows.
type Drain struct {
	// Backlog is the rows planted past their retention window; 0 means the
	// probe did not run.
	Backlog int64 `json:"backlog_rows"`
	// Drained and Remaining are the rows removed and the rows left at the
	// last reading.
	Drained   int64 `json:"drained_rows"`
	Remaining int64 `json:"remaining_rows"`
	Complete  bool  `json:"complete"`
	// Seconds is the time deleting took: from the last reading before the
	// first delete was seen to the last reading that showed one. The server's
	// restart and the pass's work before its first delete sit before that
	// and are StartSeconds; TotalSeconds is the whole probe.
	Seconds       float64 `json:"seconds"`
	RowsPerMinute float64 `json:"rows_per_minute"`
	StartSeconds  float64 `json:"start_seconds"`
	TotalSeconds  float64 `json:"total_seconds"`
}

// DrainOf turns the readings of a backlog's remaining rows into its drain.
// The rate is over the span deleting was seen in, so the pass's startup is not
// read as slow deleting; a pass that is behind (a budget of one pass, the rest
// left for the next) is a rate over its first pass.
func DrainOf(backlog int64, samples []DrainSample) Drain {
	d := Drain{Backlog: backlog, Remaining: backlog}
	if len(samples) == 0 {
		return d
	}
	last := samples[len(samples)-1]
	d.Remaining = last.Remaining
	d.Drained = max(backlog-last.Remaining, 0)
	d.Complete = d.Drained > 0 && last.Remaining <= 0
	d.TotalSeconds = last.At.Sub(samples[0].At).Seconds()
	if d.Drained == 0 {
		return d
	}
	// The first reading below the one before it is where deleting is seen;
	// the reading before it starts the span.
	prev, first, end := samples[0], -1, samples[0]
	for i, s := range samples {
		if i > 0 && s.Remaining < samples[i-1].Remaining {
			if first < 0 {
				first, prev = i, samples[i-1]
			}
			end = s
		}
	}
	if first < 0 {
		// The first reading is already below the backlog: deleting began
		// before the probe could see it, so the span starts there.
		prev, end = samples[0], last
	}
	d.StartSeconds = prev.At.Sub(samples[0].At).Seconds()
	d.Seconds = end.At.Sub(prev.At).Seconds()
	if d.Seconds > 0 {
		d.RowsPerMinute = float64(prev.Remaining-end.Remaining) / d.Seconds * 60
	}
	return d
}

// ScaleRun is everything one scale measurement read, before summary.
type ScaleRun struct {
	Build    string
	Scenario string
	Started  time.Time
	Warmup   time.Duration
	Window   time.Duration
	// The fleet the lab collected from.
	Systems, Sites, Components, Tasks int
	Measured                          Window
	// Lag is each sampled row's seconds from its probe to its commit.
	Lag          []float64
	Redeliveries Redeliveries
	FleetView    []time.Duration
	Drain        Drain
}

// ScaleContainer is one container's figures over the window.
type ScaleContainer struct {
	Name          string  `json:"name"`
	Millicores    float64 `json:"millicores"`
	WorkingSetMiB float64 `json:"working_set_mib"`
}

// ScaleReport is a scale run's summary, and the JSON two runs are compared
// by. Latencies are in milliseconds.
type ScaleReport struct {
	Build      string    `json:"build"`
	Scenario   string    `json:"scenario"`
	Started    time.Time `json:"started"`
	WarmupS    int       `json:"warmup_seconds"`
	WindowS    int       `json:"window_seconds"`
	Systems    int       `json:"systems"`
	Sites      int       `json:"sites"`
	Components int       `json:"components"`
	Tasks      int       `json:"tasks"`

	MetricRowsPerMinute   float64 `json:"metric_rows_per_minute"`
	MetricRowsPerDay      float64 `json:"metric_rows_per_day"`
	PropertyRowsPerMinute float64 `json:"property_rows_per_minute"`
	MetricBytesPerDay     float64 `json:"metric_bytes_per_day"`
	PropertyBytesPerDay   float64 `json:"property_bytes_per_day"`
	ReceiptBytesPerDay    float64 `json:"receipt_bytes_per_day"`
	DatabaseBytesPerDay   float64 `json:"database_bytes_per_day"`
	WALBytesPerDay        float64 `json:"wal_bytes_per_day"`

	Containers   []ScaleContainer `json:"containers"`
	IngestLag    Quantiles        `json:"ingest_lag_ms"`
	Redeliveries Redeliveries     `json:"redeliveries"`
	FleetView    Quantiles        `json:"fleet_view_ms"`
	Drain        Drain            `json:"retention_drain"`
}

// SummarizeScale turns a run into its report. The containers list the server
// and Postgres first, then each node by name, then their total.
func SummarizeScale(r ScaleRun) ScaleReport {
	w := r.Measured
	out := ScaleReport{
		Build: r.Build, Scenario: r.Scenario, Started: r.Started.UTC(),
		WarmupS: int(r.Warmup.Seconds()), WindowS: int(r.Window.Seconds()),
		Systems: r.Systems, Sites: r.Sites, Components: r.Components, Tasks: r.Tasks,
		MetricRowsPerMinute:   w.MetricRowsPerMinute,
		MetricRowsPerDay:      w.MetricRowsPerMinute * 60 * 24,
		PropertyRowsPerMinute: w.PropertyRowsPerMinute,
		MetricBytesPerDay:     w.MetricBytesPerDay,
		PropertyBytesPerDay:   w.PropertyBytesPerDay,
		ReceiptBytesPerDay:    w.ReceiptBytesPerDay,
		DatabaseBytesPerDay:   w.DatabaseBytesPerDay,
		WALBytesPerDay:        w.WALBytesPerDay,
		Redeliveries:          r.Redeliveries,
		FleetView:             DurationQuantiles(r.FleetView),
		Drain:                 r.Drain,
	}
	ms := make([]float64, len(r.Lag))
	for i, s := range r.Lag {
		ms[i] = s * 1000
	}
	out.IngestLag = QuantilesOf(ms)

	var names []string
	for name := range w.Containers {
		names = append(names, name)
	}
	rank := func(name string) int {
		switch name {
		case "server":
			return 0
		case "db":
			return 1
		}
		return 2
	}
	sort.Slice(names, func(i, j int) bool {
		if ri, rj := rank(names[i]), rank(names[j]); ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
	total := ScaleContainer{Name: "total"}
	for _, name := range names {
		c := w.Containers[name]
		out.Containers = append(out.Containers, ScaleContainer{Name: name, Millicores: c.Millicores, WorkingSetMiB: c.WorkingSetMiB})
		total.Millicores += c.Millicores
		total.WorkingSetMiB += c.WorkingSetMiB
	}
	out.Containers = append(out.Containers, total)
	return out
}

// Table renders the report for a terminal: the run's conditions, then one
// line per figure.
func (r ScaleReport) Table() string {
	var b strings.Builder
	fmt.Fprintf(&b, "scale lab: build %s, scenario %s; %d systems at %d sites, %d components, %d tasks\n",
		r.Build, r.Scenario, r.Systems, r.Sites, r.Components, r.Tasks)
	fmt.Fprintf(&b, "one window of %s after a %s warm-up; table and WAL growth are extrapolated from it to a day\n\n",
		time.Duration(r.WindowS)*time.Second, time.Duration(r.WarmupS)*time.Second)
	line := func(name, value string) { fmt.Fprintf(&b, "  %-28s %s\n", name, value) }
	line("metric rows a minute", count(r.MetricRowsPerMinute))
	line("property rows a minute", count(r.PropertyRowsPerMinute))
	line("metric rows a day", count(r.MetricRowsPerDay))
	line("metric table a day", size(r.MetricBytesPerDay))
	line("property table a day", size(r.PropertyBytesPerDay))
	line("receipts a day", size(r.ReceiptBytesPerDay))
	line("database a day", size(r.DatabaseBytesPerDay))
	line("WAL a day", size(r.WALBytesPerDay))
	for _, c := range r.Containers {
		line(c.Name+" cpu", fmt.Sprintf("%.0f m", c.Millicores))
	}
	for _, c := range r.Containers {
		line(c.Name+" memory", fmt.Sprintf("%.0f MiB", c.WorkingSetMiB))
	}
	ms := func(v float64) string { return fmt.Sprintf("%.1f ms", v) }
	line("ingest lag p50", ms(r.IngestLag.P50))
	line("ingest lag p95", ms(r.IngestLag.P95)+fmt.Sprintf("   (%s rows sampled)", count(float64(r.IngestLag.N))))
	line("consumer redeliveries", fmt.Sprintf("%s   (%s unconfirmed acks, %s abandoned; a floor, see the lab guide)",
		count(float64(r.Redeliveries.Total())), count(float64(r.Redeliveries.Unconfirmed)), count(float64(r.Redeliveries.Abandoned))))
	line("fleet view p50", ms(r.FleetView.P50))
	line("fleet view p95", ms(r.FleetView.P95)+fmt.Sprintf("   (%d reads)", r.FleetView.N))
	line("retention drain", r.Drain.describe())
	return b.String()
}

// describe is the drain's table cell.
func (d Drain) describe() string {
	switch {
	case d.Backlog == 0:
		return "not measured"
	case d.Drained == 0:
		return fmt.Sprintf("never started (%s rows waited %.0f s)", count(float64(d.Backlog)), d.TotalSeconds)
	case d.Complete:
		return fmt.Sprintf("%s rows a minute   (%s rows in %.0f s, after %.0f s to start)",
			count(d.RowsPerMinute), count(float64(d.Drained)), d.Seconds, d.StartSeconds)
	}
	return fmt.Sprintf("%s rows a minute   (%s of %s rows in %.0f s, %s left)",
		count(d.RowsPerMinute), count(float64(d.Drained)), count(float64(d.Backlog)), d.Seconds, count(float64(d.Remaining)))
}
