package labfootprint

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
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
	if got := Millicores(a, b); !near(got, 250) {
		t.Fatalf("millicores %v, want 250", got)
	}
	if got := Millicores(b, b); got != 0 {
		t.Fatalf("no elapsed time: %v, want 0", got)
	}
}

// The database counters come back from one psql row, pipe-separated, in the
// order DBCountersQuery selects them.
func TestParseDBCounters(t *testing.T) {
	c, err := ParseDBCounters("1798545600.5|1200|30|81920|16384|9000000|4096|123456789\n")
	if err != nil {
		t.Fatal(err)
	}
	want := DBCounters{
		At:         time.Unix(1798545600, 500_000_000).UTC(),
		MetricRows: 1200, PropertyRows: 30,
		MetricBytes: 81920, PropertyBytes: 16384, DatabaseBytes: 9000000, ReceiptBytes: 4096,
		WALBytes: 123456789,
	}
	if c != want {
		t.Fatalf("counters %+v, want %+v", c, want)
	}
	if _, err := ParseDBCounters("1|2|3"); err == nil {
		t.Fatal("a short row parsed")
	}
	if _, err := ParseDBCounters("x|1|2|3|4|5|6|7"); err == nil {
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
	if _, err := WindowOf(db0, db1, use0, map[string]Usage{}); err == nil {
		t.Fatal("a container read at the start and not at the end was accepted")
	}
}
