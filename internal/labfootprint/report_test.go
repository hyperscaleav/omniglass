package labfootprint

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A figure is the median of its windows, with the lowest and highest beside
// it so a reader sees the spread a shared host put on it.
func TestStatOf(t *testing.T) {
	s := StatOf([]float64{5, 1, 3, 100})
	if s.Median != 4 || s.Min != 1 || s.Max != 100 || s.N != 4 {
		t.Fatalf("stat %+v, want median 4 of 1..100 over 4", s)
	}
	if s := StatOf([]float64{7, 2, 9}); s.Median != 7 {
		t.Fatalf("odd count median %v, want 7", s.Median)
	}
	if s := StatOf(nil); s.N != 0 || s.Median != 0 {
		t.Fatalf("no samples: %+v", s)
	}
}

// Read latency is the median of every timed read at a range, in
// milliseconds.
func TestLatencyStat(t *testing.T) {
	s := LatencyStat([]time.Duration{30 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond})
	if s.Median != 20 || s.Min != 10 || s.Max != 30 {
		t.Fatalf("latency %+v", s)
	}
}

func testReport() Report {
	t0 := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	w := func(rows, cpu float64) Window {
		return Window{
			MetricRowsPerMinute: rows, PropertyRowsPerMinute: 3,
			MetricBytesPerDay: 900 << 20, PropertyBytesPerDay: 1 << 20, ReceiptBytesPerDay: 2 << 20,
			DatabaseBytesPerDay: 1 << 30, WALBytesPerDay: 3 << 30,
			Containers: map[string]ContainerWindow{
				"server": {Millicores: cpu, WorkingSetMiB: 60},
				"node":   {Millicores: cpu / 2, WorkingSetMiB: 140},
				"db":     {Millicores: cpu * 2, WorkingSetMiB: 80},
			},
		}
	}
	return Summarize(Run{
		Build: "abc1234", Scenario: "dev", Cadence: "lab", Started: t0,
		Warmup: 2 * time.Minute, Window: time.Minute,
		Tasks: 609, Systems: 41, Components: 213,
		Windows: []Window{w(2700, 50), w(2800, 70), w(2750, 60)},
		Reads:   map[int][]time.Duration{24: {5 * time.Millisecond, 7 * time.Millisecond}, 720: {9 * time.Millisecond}},
	})
}

// The report carries the run's conditions (build, scenario, cadence, the
// fleet's size) and the median of every figure, and a container's total is
// the three summed per window before the median is taken.
func TestSummarize(t *testing.T) {
	r := testReport()
	if r.MetricRowsPerMinute.Median != 2750 || r.MetricRowsPerMinute.N != 3 {
		t.Fatalf("metric rows %+v", r.MetricRowsPerMinute)
	}
	var server, total *ContainerStat
	for i := range r.Containers {
		switch r.Containers[i].Name {
		case "server":
			server = &r.Containers[i]
		case "total":
			total = &r.Containers[i]
		}
	}
	if server == nil || total == nil {
		t.Fatalf("containers %+v, want server and a total", r.Containers)
	}
	if server.Millicores.Median != 60 || total.Millicores.Median != 210 || total.WorkingSetMiB.Median != 280 {
		t.Fatalf("server %+v total %+v", server, total)
	}
	if names := []string{r.Containers[0].Name, r.Containers[1].Name, r.Containers[2].Name, r.Containers[3].Name}; strings.Join(names, ",") != "server,node,db,total" {
		t.Fatalf("container order %v", names)
	}
	if r.ReadLatencyMS[0].Hours != 24 || r.ReadLatencyMS[0].Stat.Median != 6 || r.ReadLatencyMS[1].Hours != 720 {
		t.Fatalf("reads %+v", r.ReadLatencyMS)
	}
	if !near(r.MetricRowsPerDay.Median, 2750*60*24) {
		t.Fatalf("rows a day %v", r.MetricRowsPerDay.Median)
	}
}

// The table is what `make lab-footprint` prints: the conditions, then one
// row per figure with its median and spread.
func TestTable(t *testing.T) {
	out := testReport().Table()
	for _, want := range []string{
		"build abc1234", "scenario dev", "cadence lab", "609 tasks", "3 windows of 1m0s",
		"metric rows a minute", "2,750", "(2,700 to 2,800)",
		"metric table a day", "900 MiB",
		"server cpu", "60 m", "(50 m to 70 m)",
		"total memory", "280 MiB",
		"series read, 24 h", "6.0 ms",
		"series read, 30 d",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
}

// The report round-trips as JSON, the form a generator renders from.
func TestReportJSON(t *testing.T) {
	r := testReport()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Build != "abc1234" || back.MetricRowsPerMinute != r.MetricRowsPerMinute || len(back.Containers) != 4 {
		t.Fatalf("round trip %+v", back)
	}
}
