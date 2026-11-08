package labfootprint

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

// A percentile is the nearest rank: the smallest value at or above p percent
// of the sample, so it is always a value that was observed, never an
// interpolation between two.
func TestPercentile(t *testing.T) {
	ten := []float64{9, 1, 8, 2, 7, 3, 6, 4, 5, 10}
	for _, c := range []struct {
		p    float64
		want float64
	}{{50, 5}, {95, 10}, {10, 1}, {11, 2}, {100, 10}, {0, 1}} {
		if got := Percentile(ten, c.p); got != c.want {
			t.Errorf("p%v of 1..10 = %v, want %v", c.p, got, c.want)
		}
	}
	if got := Percentile([]float64{42}, 95); got != 42 {
		t.Errorf("p95 of one value = %v, want it", got)
	}
	if got := Percentile(nil, 50); got != 0 {
		t.Errorf("p50 of nothing = %v, want 0", got)
	}
	// The input is not reordered: a caller's series keeps its order.
	if ten[0] != 9 || ten[9] != 10 {
		t.Errorf("Percentile sorted its input: %v", ten)
	}
	hundred := make([]float64, 100)
	for i := range hundred {
		hundred[i] = float64(i + 1)
	}
	if got := Percentile(hundred, 95); got != 95 {
		t.Errorf("p95 of 1..100 = %v, want 95", got)
	}
}

func TestQuantilesOf(t *testing.T) {
	q := QuantilesOf([]float64{10, 20, 30, 40})
	if q.P50 != 20 || q.P95 != 40 || q.N != 4 {
		t.Fatalf("quantiles %+v, want p50 20, p95 40 of 4", q)
	}
	ms := DurationQuantiles([]time.Duration{4 * time.Millisecond, 2 * time.Millisecond, 250 * time.Microsecond})
	if !near(ms.P50, 2) || !near(ms.P95, 4) || ms.N != 3 {
		t.Fatalf("duration quantiles %+v, want p50 2 ms, p95 4 ms", ms)
	}
	if got := QuantilesOf(nil); got != (Quantiles{}) {
		t.Fatalf("no values: %+v, want the zero value", got)
	}
}

// The lag query prints one number of seconds a line; a row with no commit
// time (a frozen one) prints an empty line and is left out.
func TestParseLagSeconds(t *testing.T) {
	got, err := ParseLagSeconds("0.250\n\n1.5\n -0.001\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{0.25, 1.5, -0.001}
	if len(got) != len(want) {
		t.Fatalf("lags %v, want %v", got, want)
	}
	for i := range want {
		if !near(got[i], want[i]) {
			t.Errorf("lag %d = %v, want %v", i, got[i], want[i])
		}
	}
	if _, err := ParseLagSeconds("0.25\nsoon\n"); err == nil || !strings.Contains(err.Error(), "soon") {
		t.Fatalf("a non-number: %v, want an error naming it", err)
	}
}

// A redelivery is the server saying it will see a batch again, or giving up
// on one after its deliveries ran out; other lines, lines of other facilities
// included, are not.
func TestCountRedeliveries(t *testing.T) {
	log := strings.Join([]string{
		`time=2026-11-08T10:00:00Z level=INFO msg="embedded nats-server listening" addr=0.0.0.0:4222`,
		`time=2026-11-08T10:00:01Z level=WARN msg="telemetry ack not confirmed, the batch will be redelivered and held by its receipt" subject=og.t.n1 error=timeout`,
		`time=2026-11-08T10:00:02Z level=WARN msg="telemetry message dropped after repeated failures" subject=og.t.n2 delivered=5`,
		`time=2026-11-08T10:00:03Z level=WARN msg="telemetry batch refused, dropped" subject=og.t.n3`,
		`{"level":"WARN","msg":"telemetry ack not confirmed, the batch will be redelivered and held by its receipt"}`,
		"",
	}, "\n")
	n, err := CountRedeliveries(strings.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	if n.Unconfirmed != 2 || n.Abandoned != 1 || n.Total() != 3 {
		t.Fatalf("redeliveries %+v, want 2 unconfirmed acks and 1 abandoned (3 in all)", n)
	}
	if n, _ := CountRedeliveries(strings.NewReader("")); n.Total() != 0 {
		t.Fatalf("an empty log: %+v", n)
	}
}

func at(s int) time.Time { return time.Date(2026, 11, 8, 12, 0, s, 0, time.UTC) }

// The drain rate is the rows the retention pass removed over the time it took
// from the first reading that showed it working to the last, so the server's
// restart and the pass's compaction before its first delete are not counted
// as slow deleting; the time they cost is reported beside it.
func TestDrainOf(t *testing.T) {
	samples := []DrainSample{
		{At: at(0), Remaining: 100_000},
		{At: at(2), Remaining: 100_000},
		{At: at(4), Remaining: 100_000}, // still starting
		{At: at(6), Remaining: 70_000},  // the first delete is seen here
		{At: at(8), Remaining: 30_000},
		{At: at(10), Remaining: 0},
	}
	d := DrainOf(100_000, samples)
	if !d.Complete || d.Drained != 100_000 || d.Remaining != 0 {
		t.Fatalf("drain %+v, want all 100,000 drained", d)
	}
	// 100,000 rows from the reading before the first delete (4 s) to the last (10 s).
	if !near(d.Seconds, 6) || !near(d.RowsPerMinute, 1_000_000) {
		t.Fatalf("drain took %v s at %v a minute, want 6 s at 1,000,000", d.Seconds, d.RowsPerMinute)
	}
	if !near(d.StartSeconds, 4) || !near(d.TotalSeconds, 10) {
		t.Fatalf("start %v s, total %v s, want 4 and 10", d.StartSeconds, d.TotalSeconds)
	}
}

func TestDrainOfIncomplete(t *testing.T) {
	// A pass that is behind: the budget of one pass drained, the rest waits
	// for the next, a minute on.
	d := DrainOf(300_000, []DrainSample{
		{At: at(0), Remaining: 300_000},
		{At: at(30), Remaining: 100_000},
		{At: at(60), Remaining: 100_000},
	})
	if d.Complete || d.Drained != 200_000 || d.Remaining != 100_000 {
		t.Fatalf("drain %+v, want 200,000 drained and 100,000 left", d)
	}
	if !near(d.RowsPerMinute, 400_000) {
		t.Fatalf("rate %v, want 200,000 rows over the 30 s from the last reading before the first delete", d.RowsPerMinute)
	}
	// Nothing ever moved: no rate, and the table says so rather than 0 a minute.
	none := DrainOf(1000, []DrainSample{{At: at(0), Remaining: 1000}, {At: at(10), Remaining: 1000}})
	if none.Drained != 0 || none.RowsPerMinute != 0 || none.Complete {
		t.Fatalf("a drain that never started: %+v", none)
	}
	if got := DrainOf(1000, nil); got.Drained != 0 || got.Complete {
		t.Fatalf("no readings: %+v", got)
	}
}

func scaleWindowFixture() (DBCounters, DBCounters, map[string]Usage, map[string]Usage) {
	t0 := at(0)
	t1 := t0.Add(10 * time.Minute)
	db0 := DBCounters{At: t0, MetricRows: 1000, PropertyRows: 50, MetricBytes: 1 << 20, PropertyBytes: 1 << 10,
		DatabaseBytes: 100 << 20, ReceiptBytes: 1 << 20, WALBytes: 10 << 20}
	db1 := DBCounters{At: t1, MetricRows: 1000 + 27_330, PropertyRows: 50 + 150, MetricBytes: 1<<20 + 10<<20, PropertyBytes: 1<<10 + 100<<10,
		DatabaseBytes: 100<<20 + 12<<20, ReceiptBytes: 1<<20 + 1<<20, WALBytes: 10<<20 + 30<<20}
	use0 := map[string]Usage{
		"server":  {At: t0, CPUNanos: 0, Anon: 10 << 20},
		"db":      {At: t0, CPUNanos: 0, Anon: 10 << 20},
		"edge-hq": {At: t0, CPUNanos: 0, Anon: 10 << 20},
	}
	use1 := map[string]Usage{
		"server":  {At: t1, CPUNanos: 60_000_000_000, Anon: 100 << 20, Shmem: 4 << 20},   // 100 millicores
		"db":      {At: t1, CPUNanos: 120_000_000_000, Anon: 200 << 20, Shmem: 56 << 20}, // 200 millicores
		"edge-hq": {At: t1, CPUNanos: 30_000_000_000, Anon: 50 << 20},                    // 50 millicores
	}
	return db0, db1, use0, use1
}

func TestScaleRunExtrapolates(t *testing.T) {
	db0, db1, use0, use1 := scaleWindowFixture()
	w, err := WindowOf(db0, db1, use0, use1)
	if err != nil {
		t.Fatal(err)
	}
	rep := SummarizeScale(ScaleRun{
		Build: "abc123", Scenario: "corporate", Systems: 100, Sites: 3, Components: 900, Tasks: 3000,
		Started: at(0), Warmup: time.Minute, Window: 10 * time.Minute, Measured: w,
		Lag: []float64{0.1, 0.2, 0.3, 0.4, 2.0}, Redeliveries: Redeliveries{Unconfirmed: 1},
		FleetView: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 90 * time.Millisecond},
		Drain:     DrainOf(100, []DrainSample{{At: at(0), Remaining: 100}, {At: at(6), Remaining: 0}}),
	})
	if !near(rep.MetricRowsPerMinute, 2733) || !near(rep.PropertyRowsPerMinute, 15) {
		t.Errorf("rows a minute metric %v property %v, want 2733 and 15 (27,330 and 150 rows over 10 minutes)", rep.MetricRowsPerMinute, rep.PropertyRowsPerMinute)
	}
	// 10 MiB of metric table in 10 minutes is 1,440 MiB a day; the WAL's 30 MiB is 4,320.
	if !near(rep.MetricBytesPerDay, 10*144*(1<<20)) || !near(rep.WALBytesPerDay, 30*144*(1<<20)) {
		t.Errorf("a day: metric %v WAL %v, want %v and %v", rep.MetricBytesPerDay, rep.WALBytesPerDay, 1440*(1<<20), 4320*(1<<20))
	}
	if rep.MetricRowsPerDay != rep.MetricRowsPerMinute*1440 {
		t.Errorf("rows a day %v, want the minute's rate times 1,440", rep.MetricRowsPerDay)
	}
	names := []string{}
	for _, c := range rep.Containers {
		names = append(names, c.Name)
	}
	// Server and Postgres first, then the nodes by name, the total last.
	if got := strings.Join(names, ","); got != "server,db,edge-hq,total" {
		t.Errorf("containers listed %s, want server,db,edge-hq,total", got)
	}
	last := rep.Containers[len(rep.Containers)-1]
	if !near(last.Millicores, 350) || !near(last.WorkingSetMiB, 100+4+200+56+50) {
		t.Errorf("total %+v, want 350 millicores and 410 MiB", last)
	}
	if !near(rep.IngestLag.P50, 300) || !near(rep.IngestLag.P95, 2000) {
		t.Errorf("lag %+v, want p50 300 ms, p95 2000 ms (the lag is read in seconds, reported in milliseconds)", rep.IngestLag)
	}
	if rep.FleetView.P50 != 20 || rep.FleetView.P95 != 90 {
		t.Errorf("fleet view %+v, want p50 20 ms, p95 90 ms", rep.FleetView)
	}
	if rep.WindowS != 600 || rep.WarmupS != 60 {
		t.Errorf("window %d s warm-up %d s, want 600 and 60", rep.WindowS, rep.WarmupS)
	}
}

// The table carries every row the harness promises, and the JSON the same
// figures so two runs can be compared.
func TestScaleReportTableAndJSON(t *testing.T) {
	db0, db1, use0, use1 := scaleWindowFixture()
	w, _ := WindowOf(db0, db1, use0, use1)
	rep := SummarizeScale(ScaleRun{
		Build: "abc123", Scenario: "corporate", Systems: 100, Sites: 3, Components: 900, Tasks: 3000,
		Started: at(0), Window: 10 * time.Minute, Measured: w,
		Lag: []float64{0.5}, FleetView: []time.Duration{time.Millisecond},
		Drain: DrainOf(100, []DrainSample{{At: at(0), Remaining: 100}, {At: at(6), Remaining: 0}}),
	})
	table := rep.Table()
	for _, row := range []string{
		"metric rows a minute", "property rows a minute", "metric table a day", "WAL a day",
		"server cpu", "db cpu", "edge-hq cpu", "total cpu", "server memory", "edge-hq memory",
		"ingest lag p50", "ingest lag p95", "consumer redeliveries",
		"fleet view p50", "fleet view p95", "retention drain",
	} {
		if !strings.Contains(table, row) {
			t.Errorf("the table has no %q row:\n%s", row, table)
		}
	}
	if !strings.Contains(table, "2,733") {
		t.Errorf("the table does not print the metric rate with separators:\n%s", table)
	}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var back ScaleReport
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.MetricRowsPerMinute != rep.MetricRowsPerMinute || back.IngestLag != rep.IngestLag ||
		back.Drain.RowsPerMinute != rep.Drain.RowsPerMinute || len(back.Containers) != len(rep.Containers) {
		t.Errorf("the JSON did not round trip:\n%s", b)
	}
}

// A drain that never began, or no backlog asked for, reads as what it is.
func TestScaleTableDrainStates(t *testing.T) {
	db0, db1, use0, use1 := scaleWindowFixture()
	w, _ := WindowOf(db0, db1, use0, use1)
	run := ScaleRun{Window: 10 * time.Minute, Measured: w, Lag: []float64{1}, FleetView: []time.Duration{time.Millisecond}}
	run.Drain = DrainOf(1000, []DrainSample{{At: at(0), Remaining: 1000}, {At: at(10), Remaining: 1000}})
	if got := SummarizeScale(run).Table(); !strings.Contains(got, "never started") {
		t.Errorf("a drain that never started:\n%s", got)
	}
	run.Drain = Drain{}
	if got := SummarizeScale(run).Table(); !strings.Contains(got, "not measured") {
		t.Errorf("no backlog:\n%s", got)
	}
}

// The gap query prints three bar-separated numbers; a cell is empty when the
// metric table has no observed row to read it from.
func TestParseLagAges(t *testing.T) {
	got, err := ParseLagAges("3.5|130.25|190.75\n")
	if err != nil {
		t.Fatal(err)
	}
	if got.CommitAgeS == nil || got.ProbeAgeS == nil || got.ProbeBeforeWindowEndS == nil ||
		!near(*got.CommitAgeS, 3.5) || !near(*got.ProbeAgeS, 130.25) || !near(*got.ProbeBeforeWindowEndS, 190.75) {
		t.Fatalf("ages %+v, want 3.5, 130.25 and 190.75", got)
	}
	empty, err := ParseLagAges("||\n")
	if err != nil || empty != (LagAges{}) {
		t.Fatalf("no rows: %+v, %v, want the zero value", empty, err)
	}
	for _, bad := range []string{"1|2\n", "1|soon|3\n", ""} {
		if _, err := ParseLagAges(bad); err == nil {
			t.Errorf("%q parsed, want an error", bad)
		}
	}
}

func f64(v float64) *float64 { return &v }

// A window no sample probed in had landed by the end of is not a failed run:
// the table prints every figure, the lag cell reads not measured, and the
// JSON carries the reason and the ages.
func TestScaleReportWithoutALag(t *testing.T) {
	db0, db1, use0, use1 := scaleWindowFixture()
	w, _ := WindowOf(db0, db1, use0, use1)
	gap := NewLagGap(LagAges{CommitAgeS: f64(3), ProbeAgeS: f64(40), ProbeBeforeWindowEndS: f64(55)}, 10*time.Minute)
	rep := SummarizeScale(ScaleRun{
		Build: "abc123", Scenario: "corporate", Systems: 1000, Started: at(0), Window: 10 * time.Minute, Measured: w,
		LagGap: &gap, FleetView: []time.Duration{time.Millisecond},
	})
	table := rep.Table()
	for _, want := range []string{
		"metric rows a minute", "fleet view p50", "retention drain", // the other figures still print
		"no sample probed in the window had landed by its end",
		"landed 3 s before the read", "probed 40 s before it",
	} {
		if !strings.Contains(table, want) {
			t.Errorf("the table lacks %q:\n%s", want, table)
		}
	}
	for _, row := range []string{"ingest lag p50", "ingest lag p95"} {
		if !regexp.MustCompile(row + `\s+not measured\n`).MatchString(table) {
			t.Errorf("the %s cell does not read not measured:\n%s", row, table)
		}
	}
	if strings.Contains(table, "exceeds the window") {
		t.Errorf("a lag within the window reads as past it:\n%s", table)
	}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Gap struct {
			Reason          string   `json:"reason"`
			CommitAge       *float64 `json:"newest_commit_age_seconds"`
			ProbeAge        *float64 `json:"newest_probe_age_seconds"`
			BeforeWindowEnd *float64 `json:"newest_probe_before_window_end_seconds"`
			Exceeds         bool     `json:"exceeds_window"`
		} `json:"ingest_lag_not_measured"`
		Lag Quantiles `json:"ingest_lag_ms"`
	}
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	g := back.Gap
	if g.Reason != "no sample probed in the window had landed by its end" || g.CommitAge == nil || *g.CommitAge != 3 ||
		g.ProbeAge == nil || *g.ProbeAge != 40 || g.BeforeWindowEnd == nil || *g.BeforeWindowEnd != 55 || g.Exceeds {
		t.Errorf("the JSON gap is %+v, want the reason and the ages:\n%s", g, b)
	}
	if back.Lag != (Quantiles{}) {
		t.Errorf("a lag was reported with none measured: %+v", back.Lag)
	}
}

// A newest landed sample probed before the window began is a finding: the
// footer says by how much, measured from the window's end.
func TestScaleTableLagPastTheWindow(t *testing.T) {
	db0, db1, use0, use1 := scaleWindowFixture()
	w, _ := WindowOf(db0, db1, use0, use1)
	run := ScaleRun{Window: 2 * time.Minute, Measured: w, FleetView: []time.Duration{time.Millisecond}}

	gap := NewLagGap(LagAges{CommitAgeS: f64(1), ProbeAgeS: f64(190), ProbeBeforeWindowEndS: f64(185)}, 2*time.Minute)
	if !gap.ExceedsWindow {
		t.Fatalf("a sample probed 185 s before a 120 s window's end: %+v, want past the window", gap)
	}
	run.LagGap = &gap
	want := "ingest lag exceeds the window: newest landed sample probed 185 s before the window ended"
	if got := SummarizeScale(run).Table(); !strings.Contains(got, want) {
		t.Errorf("the footer lacks %q:\n%s", want, got)
	}

	// Probed at the window's very start is inside it; with no sample at all
	// there is nothing to measure against.
	if NewLagGap(LagAges{ProbeBeforeWindowEndS: f64(120)}, 2*time.Minute).ExceedsWindow {
		t.Error("a sample probed at the window's start reads as past it")
	}
	none := NewLagGap(LagAges{}, 2*time.Minute)
	run.LagGap = &none
	got := SummarizeScale(run).Table()
	if none.ExceedsWindow || strings.Contains(got, "exceeds the window") || !strings.Contains(got, "holds no observed sample") {
		t.Errorf("an empty metric table:\n%s", got)
	}
}
