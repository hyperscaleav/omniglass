package labfootprint

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"

	"github.com/hyperscaleav/omniglass/internal/lab"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// A container's usage comes from its cgroup as the Engine reports it: CPU
// time consumed so far, and the working set as anonymous plus shared memory.
// File pages (the binary, the page cache) are not working set: a process that
// read a large file looks no bigger for it.
func TestUsageFromStats(t *testing.T) {
	at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	var s container.StatsResponse
	s.Read = at
	s.CPUStats.CPUUsage.TotalUsage = 5_000_000_000
	s.MemoryStats.Stats = map[string]uint64{"anon": 40 << 20, "shmem": 8 << 20, "file": 900 << 20}
	u, err := UsageFromStats(s)
	if err != nil {
		t.Fatal(err)
	}
	if u.CPUNanos != 5_000_000_000 || !u.At.Equal(at) {
		t.Fatalf("usage %+v", u)
	}
	if got := u.WorkingSetBytes(); got != 48<<20 {
		t.Fatalf("working set %d, want anon plus shmem (48 MiB)", got)
	}
	s.MemoryStats.Stats = map[string]uint64{"rss": 10}
	if _, err := UsageFromStats(s); err == nil || !strings.Contains(err.Error(), "anon") {
		t.Fatalf("a cgroup v1 report: %v, want a refusal naming the missing anon counter", err)
	}
}

// Millicores are the CPU time consumed between two readings over the wall
// time between them: one full core is 1,000.
func TestMillicores(t *testing.T) {
	at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	a := Usage{At: at, CPUNanos: 1_000_000_000}
	b := Usage{At: at.Add(10 * time.Second), CPUNanos: 3_500_000_000}
	if got, err := Millicores(a, b); err != nil || !near(got, 250) {
		t.Fatalf("millicores %v %v, want 250", got, err)
	}
	if got, err := Millicores(b, b); err != nil || got != 0 {
		t.Fatalf("no elapsed time: %v %v, want 0", got, err)
	}
	// A container restarted inside the window starts a fresh cgroup, its
	// counter from zero: the window is refused, never read as idle.
	if _, err := Millicores(b, a); err == nil {
		t.Fatal("a CPU counter that went backwards was read as a rate")
	}
}

// The database counters come back from one psql row, pipe-separated, in the
// order DBCountersQuery selects them.
func TestParseDBCounters(t *testing.T) {
	c, err := ParseDBCounters("1798545600.5|1200|30|81920|16384|9000000|4096|123456789|77\n")
	if err != nil {
		t.Fatal(err)
	}
	want := DBCounters{
		At:         time.Unix(1798545600, 500_000_000).UTC(),
		MetricRows: 1200, PropertyRows: 30,
		MetricBytes: 81920, PropertyBytes: 16384, DatabaseBytes: 9000000, ReceiptBytes: 4096,
		WALBytes: 123456789, ReceiptsInserted: 77,
	}
	if c != want {
		t.Fatalf("counters %+v, want %+v", c, want)
	}
	if _, err := ParseDBCounters("1|2|3"); err == nil {
		t.Fatal("a short row parsed")
	}
	if _, err := ParseDBCounters("1798545600.5|1200|30|81920|16384|9000000|4096|123456789"); err == nil {
		t.Fatal("a row without the receipt count parsed")
	}
	if _, err := ParseDBCounters("1798545600.5|1200|30|81920|16384|9000000|4096|123456789|x"); err == nil {
		t.Fatal("a non-numeric receipt count parsed")
	}
	if _, err := ParseDBCounters("x|1|2|3|4|5|6|7|8"); err == nil {
		t.Fatal("a non-numeric row parsed")
	}
}

// A window turns two readings of every counter into rates: rows a minute
// per lane, bytes a day per relation and of WAL, millicores per container,
// and the working set read at the window's end.
func TestWindowOf(t *testing.T) {
	t0 := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(2 * time.Minute)
	db0 := DBCounters{At: t0, MetricRows: 1000, PropertyRows: 10, MetricBytes: 1 << 20, PropertyBytes: 1 << 10, DatabaseBytes: 10 << 20, ReceiptBytes: 0, WALBytes: 0}
	db1 := DBCounters{At: t1, MetricRows: 3000, PropertyRows: 14, MetricBytes: 2 << 20, PropertyBytes: 2 << 10, DatabaseBytes: 12 << 20, ReceiptBytes: 1 << 20, WALBytes: 4 << 20}
	use0 := map[string]Usage{"db": {At: t0, CPUNanos: 0}}
	use1 := map[string]Usage{"db": {At: t1, CPUNanos: 60_000_000_000, Anon: 30 << 20, Shmem: 2 << 20}}
	w, err := WindowOf(db0, db1, use0, use1)
	if err != nil {
		t.Fatal(err)
	}
	day := float64(24 * 60 / 2)
	checks := []struct {
		name      string
		got, want float64
	}{
		{"metric rows a minute", w.MetricRowsPerMinute, 1000},
		{"property rows a minute", w.PropertyRowsPerMinute, 2},
		{"metric bytes a day", w.MetricBytesPerDay, (1 << 20) * day},
		{"property bytes a day", w.PropertyBytesPerDay, (1 << 10) * day},
		{"receipt bytes a day", w.ReceiptBytesPerDay, (1 << 20) * day},
		{"database bytes a day", w.DatabaseBytesPerDay, (2 << 20) * day},
		{"wal bytes a day", w.WALBytesPerDay, (4 << 20) * day},
		{"db millicores", w.Containers["db"].Millicores, 500},
		{"db working set", w.Containers["db"].WorkingSetMiB, 32},
	}
	for _, c := range checks {
		if !near(c.got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, c.got, c.want)
		}
	}
	if _, err := WindowOf(db1, db0, use0, use1); err == nil {
		t.Fatal("a window that ends before it starts was accepted")
	}
	restarted := db1
	restarted.MetricRows = 10
	if _, err := WindowOf(db0, restarted, use0, use1); err == nil {
		t.Fatal("a row counter that went backwards (Postgres restarted) was accepted")
	}
	if _, err := WindowOf(db0, db1, use0, map[string]Usage{}); err == nil {
		t.Fatal("a container read at the start and not at the end was accepted")
	}
}

// A window's working set is the median of the reads taken across it, so a
// read that lands on a collection peak moves nothing: the Go heap grows to
// its next cycle and falls back, and one read at the window's end landed
// anywhere in that cycle (the ci baseline's runs spread 23 percent, #1751).
func TestMedianWorkingSetMiB(t *testing.T) {
	reads := func(mibs ...uint64) []Usage {
		out := make([]Usage, len(mibs))
		for i, m := range mibs {
			out[i] = Usage{Anon: m << 20}
		}
		return out
	}
	cases := []struct {
		name  string
		reads []Usage
		want  float64
	}{
		{"a collection peak last reads the median, not the peak", reads(60, 58, 61, 59, 90), 60},
		{"an even count reads the mean of the middle two", reads(64, 58, 60, 62), 61},
		{"one read is its own median", reads(70), 70},
		{"no reads read as 0", nil, 0},
		{"shared memory counts", []Usage{{Anon: 30 << 20, Shmem: 2 << 20}}, 32},
	}
	for _, c := range cases {
		if got := MedianWorkingSetMiB(c.reads); !near(got, c.want) {
			t.Errorf("%s: %v MiB, want %v", c.name, got, c.want)
		}
	}
	in := reads(90, 58, 60)
	MedianWorkingSetMiB(in)
	if in[0].Anon != 90<<20 {
		t.Error("the reads were reordered")
	}
}

// A scale window's containers take the median of the reads across it as
// their working set; their CPU stays the window's delta, and a container the
// window read at its ends with no reads across it is refused, never read as
// empty.
func TestMedianWorkingSets(t *testing.T) {
	w := Window{MetricRowsPerMinute: 10, Containers: map[string]ContainerWindow{
		"server": {Millicores: 100, WorkingSetMiB: 90},
		"db":     {Millicores: 200, WorkingSetMiB: 300},
	}}
	mib := func(m uint64) Usage { return Usage{Anon: m << 20} }
	reads := map[string][]Usage{
		"server": {mib(60), mib(58), mib(61), mib(59), mib(90)},
		"db":     {mib(250), mib(256), mib(252)},
	}
	got, err := MedianWorkingSets(w, reads)
	if err != nil {
		t.Fatal(err)
	}
	if s := got.Containers["server"]; !near(s.WorkingSetMiB, 60) || !near(s.Millicores, 100) {
		t.Errorf("server %+v, want the median 60 MiB and its 100 millicores", s)
	}
	if d := got.Containers["db"]; !near(d.WorkingSetMiB, 252) || !near(d.Millicores, 200) {
		t.Errorf("db %+v, want the median 252 MiB and its 200 millicores", d)
	}
	if got.MetricRowsPerMinute != 10 {
		t.Errorf("the window's rates moved: %+v", got)
	}
	if w.Containers["server"].WorkingSetMiB != 90 {
		t.Error("the window passed in was changed")
	}
	delete(reads, "db")
	if _, err := MedianWorkingSets(w, reads); err == nil || !strings.Contains(err.Error(), "db") {
		t.Errorf("a container with no reads: err = %v, want one naming it", err)
	}
}

// The batch figures come from the receipt counter and the lane row counters:
// a batch is a receipt, a sample is a metric or property row.
func TestWindowOfBatchFigures(t *testing.T) {
	t0 := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(2 * time.Minute)
	db0 := DBCounters{At: t0, MetricRows: 1000, PropertyRows: 10, ReceiptsInserted: 40}
	db1 := DBCounters{At: t1, MetricRows: 1000 + 4800, PropertyRows: 10 + 240, ReceiptsInserted: 40 + 120}
	w, err := WindowOf(db0, db1, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if w.BatchesLanded != 120 || w.SamplesLanded != 5040 {
		t.Errorf("landed %d batches %d samples, want 120 and 5040", w.BatchesLanded, w.SamplesLanded)
	}
	if !near(w.SamplesPerBatch, 42) || !near(w.BatchesPerSecond, 1) {
		t.Errorf("samples per batch %v batches a second %v, want 42 and 1", w.SamplesPerBatch, w.BatchesPerSecond)
	}
	restarted := db1
	restarted.ReceiptsInserted = 5
	if _, err := WindowOf(db0, restarted, nil, nil); err == nil {
		t.Fatal("a receipt counter that went backwards (Postgres restarted) was accepted")
	}
}

// The derivation is pure and guards both divisions: no batches reads as zero
// samples per batch, no time as zero batches a second.
func TestBatchRates(t *testing.T) {
	cases := []struct {
		name              string
		batches, samples  int64
		dt                time.Duration
		wantPer, wantRate float64
	}{
		{"steady", 100, 4000, 10 * time.Second, 40, 10},
		{"no batches", 0, 500, 10 * time.Second, 0, 0},
		{"no time", 100, 4000, 0, 40, 0},
		{"negative time", 100, 4000, -time.Second, 40, 0},
		{"idle", 0, 0, 10 * time.Second, 0, 0},
	}
	for _, c := range cases {
		per, rate := BatchRates(c.batches, c.samples, c.dt)
		if !near(per, c.wantPer) || !near(rate, c.wantRate) {
			t.Errorf("%s: %v per batch, %v a second; want %v and %v", c.name, per, rate, c.wantPer, c.wantRate)
		}
	}
}

// A lab with a node per site (the corporate scenario the footprint measures,
// #1719) reports its nodes as one node row: their CPU and memory summed, so
// the report keeps its server, node and database rows whatever the sites.
func TestFoldNodes(t *testing.T) {
	w := Window{Containers: map[string]ContainerWindow{
		"server":           {Millicores: 100, WorkingSetMiB: 200},
		"db":               {Millicores: 50, WorkingSetMiB: 300},
		"edge-hq":          {Millicores: 10, WorkingSetMiB: 40},
		"edge-regional-01": {Millicores: 4, WorkingSetMiB: 36},
		"edge-branch-01":   {Millicores: 2, WorkingSetMiB: 30},
	}}
	got := FoldNodes(w).Containers
	want := map[string]ContainerWindow{
		"server": {Millicores: 100, WorkingSetMiB: 200},
		"db":     {Millicores: 50, WorkingSetMiB: 300},
		"node":   {Millicores: 16, WorkingSetMiB: 106},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("folded %v, want %v", got, want)
	}
	if _, ok := w.Containers["node"]; ok {
		t.Fatal("FoldNodes changed the window it was given")
	}
}

// The footprint brings its lab up at the stated size (#1719): a scale
// scenario passes --systems, and a dev scenario, which has no size, does not.
func TestLabRunArgs(t *testing.T) {
	cfg := Config{Project: "ogfoot-1", Scenario: "corporate", Systems: 20, Cadence: "default"}
	want := []string{"lab", "run", "--ephemeral", "--project", "ogfoot-1", "--port", "0",
		"--ready-file", "/r/lab.env", "--while-pid", "7", "--scenario", "corporate", "--cadence", "default", "--systems", "20"}
	if got := labRunArgs(cfg, "/r/lab.env", 7); !reflect.DeepEqual(got, want) {
		t.Fatalf("args %q, want %q", got, want)
	}
	cfg.Scenario, cfg.Systems = "dev", 0
	if got := labRunArgs(cfg, "/r/lab.env", 7); slices.Contains(got, "--systems") {
		t.Fatalf("a dev scenario passed a size: %q", got)
	}
}

// The footprint's report names the size the lab was brought up at, read from
// its labels like its scenario (#1719), so a figure is quoted at its ladder
// size; a dev lab has no stated size and reports the systems it seeded.
func TestFootprintSystems(t *testing.T) {
	for _, c := range []struct {
		name          string
		fleet         lab.Fleet
		counted, want int
	}{
		{"corporate at its label", lab.Fleet{Scenario: "corporate", Systems: 20}, 20, 20},
		{"the label over a drifted count", lab.Fleet{Scenario: "corporate", Systems: 20}, 23, 20},
		{"dev, which has no size", lab.Fleet{Scenario: "dev"}, 7, 7},
	} {
		if got := footprintSystems(c.fleet, c.counted); got != c.want {
			t.Errorf("%s: footprintSystems = %d, want %d", c.name, got, c.want)
		}
	}
}
