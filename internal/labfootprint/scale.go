package labfootprint

import (
	"bufio"
	"cmp"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/closedloop"
	"github.com/hyperscaleav/omniglass/internal/delivery"
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

// Quantiles is a sample's median, 95th and 99th percentiles, with its size.
// The p99 is the scaling target's figure (the ingest lag under 5 s at p99).
type Quantiles struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	N   int     `json:"n"`
}

// QuantilesOf is the p50, p95 and p99 of xs.
func QuantilesOf(xs []float64) Quantiles {
	if len(xs) == 0 {
		return Quantiles{}
	}
	return Quantiles{P50: Percentile(xs, 50), P95: Percentile(xs, 95), P99: Percentile(xs, 99), N: len(xs)}
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

// LagAges is what the lag gap query reads about the newest observed samples
// when none probed inside the window had landed: seconds, each nil when the
// metric table holds no such row.
type LagAges struct {
	// CommitAgeS is how long before the read the newest row committed.
	CommitAgeS *float64
	// ProbeAgeS is how long before the read the newest landed row was probed.
	ProbeAgeS *float64
	// ProbeBeforeWindowEndS is how long before the window ended that row was
	// probed.
	ProbeBeforeWindowEndS *float64
}

// ParseLagAges reads the gap query's one line: three seconds separated by
// bars, a cell empty when there is no row to read it from.
func ParseLagAges(out string) (LagAges, error) {
	line := strings.TrimSpace(out)
	cells := strings.Split(line, "|")
	if len(cells) != 3 {
		return LagAges{}, fmt.Errorf("ingest lag ages: %q is not three numbers of seconds", line)
	}
	var f [3]*float64
	for i, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		v, err := strconv.ParseFloat(c, 64)
		if err != nil {
			return LagAges{}, fmt.Errorf("ingest lag ages: %q is not a number of seconds", c)
		}
		f[i] = &v
	}
	return LagAges{CommitAgeS: f[0], ProbeAgeS: f[1], ProbeBeforeWindowEndS: f[2]}, nil
}

// LagGap is why the ingest lag has no figure: no observed sample probed in
// the window had committed by its end, and how stale the newest landed
// sample was. A lag that is past the window is a finding, not a blank.
type LagGap struct {
	Reason                string   `json:"reason"`
	NewestCommitAgeS      *float64 `json:"newest_commit_age_seconds,omitempty"`
	NewestProbeAgeS       *float64 `json:"newest_probe_age_seconds,omitempty"`
	ProbeBeforeWindowEndS *float64 `json:"newest_probe_before_window_end_seconds,omitempty"`
	// ExceedsWindow is true when the newest landed sample was probed before
	// the window began.
	ExceedsWindow bool `json:"exceeds_window"`
}

const lagGapReason = "no sample probed in the window had landed by its end"

// NewLagGap is the gap for a window of the given length, from the ages read.
func NewLagGap(a LagAges, window time.Duration) LagGap {
	g := LagGap{
		Reason: lagGapReason, NewestCommitAgeS: a.CommitAgeS, NewestProbeAgeS: a.ProbeAgeS,
		ProbeBeforeWindowEndS: a.ProbeBeforeWindowEndS,
	}
	g.ExceedsWindow = a.ProbeBeforeWindowEndS != nil && *a.ProbeBeforeWindowEndS > window.Seconds()
	return g
}

// footer is the gap's account under the table.
func (g LagGap) footer() string {
	var b strings.Builder
	fmt.Fprintf(&b, "ingest lag not measured: %s", g.Reason)
	switch {
	case g.NewestCommitAgeS == nil || g.NewestProbeAgeS == nil:
		b.WriteString("; the metric table holds no observed sample")
	default:
		fmt.Fprintf(&b, "; the newest committed row landed %.0f s before the read, its sample probed %.0f s before it",
			*g.NewestCommitAgeS, *g.NewestProbeAgeS)
	}
	b.WriteString("\n")
	if g.ExceedsWindow {
		fmt.Fprintf(&b, "ingest lag exceeds the window: newest landed sample probed %.0f s before the window ended\n", *g.ProbeBeforeWindowEndS)
	}
	return b.String()
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

// LaneSample is one "ingest lane" line of the server's log: the consumer
// lane's running total of batches handled and the time its handler held the
// lane for them, since the server started (internal/bus, startLaneLog).
type LaneSample struct {
	At      time.Time
	Batches int64
	Busy    time.Duration
}

const logLane = `msg="ingest lane"`

// ParseLaneLog reads the lane tally lines out of a server's log, as the text
// handler writes them: time=<RFC 3339> ... msg="ingest lane" ... batches=N
// busy_ms=M. Any other line is skipped; a tally line that does not parse is
// an error, since a harness that misread the server's count would print a
// wrong figure with a straight face.
func ParseLaneLog(r io.Reader) ([]LaneSample, error) {
	var out []LaneSample
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.Contains(line, logLane) {
			continue
		}
		var (
			sample LaneSample
			busyMs int64
			err    error
		)
		for _, field := range strings.Fields(line) {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch key {
			case "time":
				sample.At, err = time.Parse(time.RFC3339Nano, value)
			case "batches":
				sample.Batches, err = strconv.ParseInt(value, 10, 64)
			case "busy_ms":
				busyMs, err = strconv.ParseInt(value, 10, 64)
			}
			if err != nil {
				return nil, fmt.Errorf("lane tally line %q: %w", line, err)
			}
		}
		if sample.At.IsZero() {
			return nil, fmt.Errorf("lane tally line %q has no time", line)
		}
		sample.Busy = time.Duration(busyMs) * time.Millisecond
		out = append(out, sample)
	}
	return out, sc.Err()
}

// Lane is the consumer lane's utilisation over a window: how much of the
// wall time its one handler held the lane, and the mean time a batch held it.
// It answers whether the lane is the bottleneck (busy near 1) or waiting on
// its publishers (busy near 0 with the ingest lag at the spool wait).
type Lane struct {
	// Measured is false when the window held fewer than two tallies, or the
	// server restarted inside it; Reason then says which.
	Measured bool   `json:"measured"`
	Reason   string `json:"reason,omitempty"`
	// Batches is the batches handled between the first and last tally in the
	// window, BusyFraction the lane's busy time over the span between them,
	// ServiceMs the mean milliseconds a batch held the lane, SpanSeconds the
	// span itself.
	Batches      int64   `json:"batches"`
	BusyFraction float64 `json:"busy_fraction"`
	ServiceMs    float64 `json:"service_ms"`
	SpanSeconds  float64 `json:"span_seconds"`
}

// LaneOf is the lane's utilisation between the first and last tally inside
// [from, to]. The tallies are running totals, so the difference between two
// is exact for the span between them, and the span, not the window, is the
// denominator: the warm-up's tallies and the partial intervals at the
// window's edges do not count.
func LaneOf(samples []LaneSample, from, to time.Time) Lane {
	var in []LaneSample
	for _, s := range samples {
		if !s.At.Before(from) && !s.At.After(to) {
			in = append(in, s)
		}
	}
	if len(in) < 2 {
		return Lane{Reason: fmt.Sprintf("the server logged %d lane tallies in the window, two are needed", len(in))}
	}
	// Every tally is a running total, so one that falls below the one before
	// it is a fresh process: a restart inside the window, even one whose new
	// count overtook the old by the window's end, measures nothing.
	for i := 1; i < len(in); i++ {
		if in[i].Batches < in[i-1].Batches || in[i].Busy < in[i-1].Busy {
			return Lane{Reason: "the lane's tallies went backwards in the window (the server restarted?)"}
		}
	}
	first, last := in[0], in[len(in)-1]
	span := last.At.Sub(first.At)
	if span <= 0 {
		return Lane{Reason: "the lane's tallies in the window span no time"}
	}
	l := Lane{Measured: true, Batches: last.Batches - first.Batches, SpanSeconds: span.Seconds()}
	// With one message in flight the lane is one handler at a time, so the
	// fraction cannot pass 1. A partitioned consumer (#1585) would hold
	// several lanes at once and need their count as the denominator.
	busy := last.Busy - first.Busy
	l.BusyFraction = float64(busy) / float64(span)
	if l.Batches > 0 {
		l.ServiceMs = float64(busy) / float64(time.Millisecond) / float64(l.Batches)
	}
	return l
}

// describe is the lane's table cell.
func (l Lane) describe() string {
	if !l.Measured {
		return "not measured"
	}
	return fmt.Sprintf("%.1f %%   (%.1f ms a batch, %s batches over %.0f s)",
		l.BusyFraction*100, l.ServiceMs, count(float64(l.Batches)), l.SpanSeconds)
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
	Lag []float64
	// LagGap is set when Lag is empty: why, and how stale the newest landed
	// sample was.
	LagGap       *LagGap
	Redeliveries Redeliveries
	// Lane is the consumer lane's utilisation, from the server's tally lines.
	Lane      Lane
	FleetView []time.Duration
	Drain     Drain
	// Page is the system page loads made during the window.
	Page PageRun
	// Absolutes is the database's standing size, read at the window's end.
	Absolutes Absolutes
	// Endpoints is each node's bracketed endpoint observation; when none was
	// read, EndpointsGap says why.
	Endpoints    []closedloop.NodeBracket
	EndpointsGap string
	// Delivery is the receipts the delivery table reconciles (#1641).
	Delivery DeliveryRead
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
	// WALBytesPerSample is the WAL the window wrote over the samples it
	// landed: the write amplification one row costs.
	WALBytesPerSample float64 `json:"wal_bytes_per_sample"`

	BatchesLanded    int64   `json:"batches_landed"`
	SamplesLanded    int64   `json:"samples_landed"`
	SamplesPerBatch  float64 `json:"samples_per_batch"`
	BatchesPerSecond float64 `json:"batches_per_second"`

	Containers []ScaleContainer `json:"containers"`
	IngestLag  Quantiles        `json:"ingest_lag_ms"`
	// IngestLagGap is set, and IngestLag zero, when no lag was measured.
	IngestLagGap *LagGap      `json:"ingest_lag_not_measured,omitempty"`
	Redeliveries Redeliveries `json:"redeliveries"`
	ConsumerLane Lane         `json:"consumer_lane"`
	FleetView    Quantiles    `json:"fleet_view_ms"`
	Drain        Drain        `json:"retention_drain"`
	// Absolutes is what the database holds at the window's end, where the
	// rates above are what it gains a day.
	Absolutes Absolutes `json:"absolutes"`
	// SystemPageFigures is the system page load, absent when no client
	// loaded it.
	*SystemPageFigures
	// Endpoints is the closed loop, nil when it was not read; EndpointsGap
	// then says why.
	Endpoints    *closedloop.EndpointReport `json:"endpoints,omitempty"`
	EndpointsGap string                     `json:"endpoints_not_measured,omitempty"`

	// Delivery is each kind's loss across each hop it crosses (#1641), over
	// the settled boots, one set of rows per lane (#1696); DeliveryEndToEnd
	// is each kind's loss over all the node lane's hops (the push lane's one
	// row is its own end to end); DeliveryUnsettled names each boot no ratio
	// was read from.
	Delivery          []DeliveryRow             `json:"delivery"`
	DeliveryEndToEnd  map[delivery.Kind]float64 `json:"delivery_end_to_end,omitempty"`
	DeliveryUnsettled []UnsettledBoot           `json:"delivery_unsettled,omitempty"`
	// DeliveryNotRead is why no receipts were read, when none were;
	// DeliveryNote qualifies a read that was.
	DeliveryNotRead string `json:"delivery_not_measured,omitempty"`
	DeliveryNote    string `json:"delivery_note,omitempty"`
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
		BatchesLanded:         w.BatchesLanded,
		SamplesLanded:         w.SamplesLanded,
		SamplesPerBatch:       w.SamplesPerBatch,
		BatchesPerSecond:      w.BatchesPerSecond,
		IngestLagGap:          r.LagGap,
		Redeliveries:          r.Redeliveries,
		ConsumerLane:          r.Lane,
		FleetView:             DurationQuantiles(r.FleetView),
		Drain:                 r.Drain,
		Absolutes:             r.Absolutes,
		SystemPageFigures:     SummarizePage(r.Page),
		EndpointsGap:          r.EndpointsGap,
	}
	if len(r.Endpoints) > 0 {
		e := closedloop.SummarizeEndpoints(r.Endpoints)
		out.Endpoints = &e
	}
	ms := make([]float64, len(r.Lag))
	for i, s := range r.Lag {
		ms[i] = s * 1000
	}
	out.IngestLag = QuantilesOf(ms)
	if d := r.Delivery; !d.Read {
		out.DeliveryNotRead = cmp.Or(d.Reason, "no receipts read")
	} else {
		out.Delivery, out.DeliveryUnsettled = DeliveryOf(d)
		out.DeliveryEndToEnd = EndToEndOf(out.Delivery)
		if !d.StreamClear {
			out.DeliveryNote = fmt.Sprintf("unsettled: the telemetry consumer still held batches when the settle ran out (%s pending, %s unacked)",
				gauge(d.Backlog.Pending, d.Backlog.SeenPending), gauge(d.Backlog.AckPending, d.Backlog.SeenAckPending))
		}
	}
	if w.SamplesLanded > 0 {
		out.WALBytesPerSample = float64(w.WALBytes) / float64(w.SamplesLanded)
	}

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
	line("WAL a sample", count(r.WALBytesPerSample)+" B")
	r.Absolutes.lines(line)
	line("batches landed", count(float64(r.BatchesLanded)))
	line("samples landed", count(float64(r.SamplesLanded)))
	line("samples per batch", fmt.Sprintf("%.1f", r.SamplesPerBatch))
	line("batches a second", fmt.Sprintf("%.2f", r.BatchesPerSecond))
	for _, c := range r.Containers {
		line(c.Name+" cpu", fmt.Sprintf("%.0f m", c.Millicores))
	}
	for _, c := range r.Containers {
		line(c.Name+" memory", fmt.Sprintf("%.0f MiB", c.WorkingSetMiB))
	}
	ms := func(v float64) string { return fmt.Sprintf("%.1f ms", v) }
	if r.IngestLagGap != nil {
		line("ingest lag p50", "not measured")
		line("ingest lag p95", "not measured")
		line("ingest lag p99", "not measured")
	} else {
		line("ingest lag p50", ms(r.IngestLag.P50))
		line("ingest lag p95", ms(r.IngestLag.P95))
		line("ingest lag p99", ms(r.IngestLag.P99)+fmt.Sprintf("   (%s rows sampled)", count(float64(r.IngestLag.N))))
	}
	line("consumer redeliveries", fmt.Sprintf("%s   (%s unconfirmed acks, %s abandoned; a floor, see the lab guide)",
		count(float64(r.Redeliveries.Total())), count(float64(r.Redeliveries.Unconfirmed)), count(float64(r.Redeliveries.Abandoned))))
	line("consumer lane busy", r.ConsumerLane.describe())
	line("fleet view p50", ms(r.FleetView.P50))
	line("fleet view p95", ms(r.FleetView.P95)+fmt.Sprintf("   (%d reads)", r.FleetView.N))
	if p := r.SystemPageFigures; p != nil {
		line("system page p50", ms(p.Ms.P50))
		line("system page p95", ms(p.Ms.P95)+p.describe())
	} else {
		line("system page p50", "not measured")
		line("system page p95", "not measured")
	}
	line("retention drain", r.Drain.describe())
	if r.Endpoints != nil {
		endpointTable(*r.Endpoints, &b)
	} else if r.EndpointsGap != "" {
		b.WriteString("\nendpoints not measured: " + r.EndpointsGap + "\n")
	}
	b.WriteString("\n")
	r.deliveryTable(&b)
	if r.IngestLagGap != nil {
		b.WriteString("\n" + r.IngestLagGap.footer())
	}
	if !r.ConsumerLane.Measured && r.ConsumerLane.Reason != "" {
		b.WriteString("\nconsumer lane not measured: " + r.ConsumerLane.Reason + "\n")
	}
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
