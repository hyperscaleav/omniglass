// Package labfootprint is the lab footprint (#1277): what a running lab costs
// per container and per day, measured on a live `omniglass lab run` rather
// than through the ingest handler alone (the ingest footprint, ADR-1272). It
// samples each container's cgroup through the Docker Engine, the database's
// own counters through psql in its container, and the API's series reads
// with the server and Postgres held to one vCPU, over N windows, and reports
// the median of each figure with its spread.
//
// The arithmetic (this file and report.go) is pure; the harness (run.go)
// drives a lab and Docker at the edges. It is a diagnostic (ADR-0094), never
// a gate: on a shared host every figure here moves with the neighbours.
package labfootprint

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
)

// Usage is one container's cgroup at an instant: the CPU time it has used
// since it started, and its anonymous and shared memory.
type Usage struct {
	At       time.Time
	CPUNanos uint64
	Anon     uint64
	Shmem    uint64
}

// WorkingSetBytes is the memory the container holds: anonymous plus shared
// (Postgres's shared buffers are shared memory). File pages are left out:
// they are the binary and the page cache, which the kernel reclaims, so RSS
// and the cgroup's total both overstate what the process needs.
func (u Usage) WorkingSetBytes() uint64 { return u.Anon + u.Shmem }

// UsageFromStats reads a usage from one Engine stats report. A cgroup v1
// report has no anon counter and is refused rather than misread.
func UsageFromStats(s container.StatsResponse) (Usage, error) {
	anon, ok := s.MemoryStats.Stats["anon"]
	if !ok {
		return Usage{}, fmt.Errorf("the container's memory report has no anon counter (cgroup v1?): the lab footprint needs cgroup v2")
	}
	return Usage{
		At:       s.Read,
		CPUNanos: s.CPUStats.CPUUsage.TotalUsage,
		Anon:     anon,
		Shmem:    s.MemoryStats.Stats["shmem"],
	}, nil
}

// Millicores is the CPU a container used between two readings: a thousand
// is one full core. A counter that went backwards means the container was
// restarted between them (a fresh cgroup counts from zero), and the window
// is refused rather than read as idle.
func Millicores(a, b Usage) (float64, error) {
	if b.CPUNanos < a.CPUNanos {
		return 0, fmt.Errorf("its CPU counter went backwards (restarted between %s and %s?)", a.At.Format(time.TimeOnly), b.At.Format(time.TimeOnly))
	}
	dt := b.At.Sub(a.At)
	if dt <= 0 {
		return 0, nil
	}
	return float64(b.CPUNanos-a.CPUNanos) / float64(dt.Nanoseconds()) * 1000, nil
}

// DBCountersQuery reads, in one row, the database counters a window
// differences: rows inserted into each telemetry lane (the cumulative
// statistics, so nothing is counted by a scan), the total size of each lane
// and of the receipts with their indexes, the database's size, the WAL
// position, and the receipts inserted (one per batch landed). A lane's figures sum over its partitions, so a partitioned table
// reads the same way as a plain one (the union keeps the table itself in
// its lane whatever pg_partition_tree lists).
const DBCountersQuery = `with lane(name, relid) as (
    select l, t.relid from unnest(array['metric', 'property']) l,
      lateral (select relid from pg_partition_tree(l::regclass) union select l::regclass) t)
select extract(epoch from clock_timestamp()),
  (select coalesce(sum(s.n_tup_ins), 0) from lane join pg_stat_user_tables s using (relid) where lane.name = 'metric'),
  (select coalesce(sum(s.n_tup_ins), 0) from lane join pg_stat_user_tables s using (relid) where lane.name = 'property'),
  (select coalesce(sum(pg_total_relation_size(relid)), 0) from lane where name = 'metric'),
  (select coalesce(sum(pg_total_relation_size(relid)), 0) from lane where name = 'property'),
  pg_database_size(current_database()),
  coalesce(pg_total_relation_size(to_regclass('telemetry_receipt')), 0),
  pg_wal_lsn_diff(pg_current_wal_lsn(), '0/0')::bigint,
  coalesce((select s.n_tup_ins from pg_stat_user_tables s where s.relid = to_regclass('telemetry_receipt')), 0)`

// DBCounters is one reading of DBCountersQuery.
type DBCounters struct {
	At                          time.Time
	MetricRows, PropertyRows    int64
	MetricBytes, PropertyBytes  int64
	DatabaseBytes, ReceiptBytes int64
	WALBytes                    int64
	// ReceiptsInserted is the cumulative count of telemetry_receipt rows
	// inserted (n_tup_ins), so its delta approximates the batches a window
	// landed. It counts the receipt of a rolled-back pass too, so a batch the
	// consumer landed in three passes after a Postgres refusal counts three.
	ReceiptsInserted int64
}

// ParseDBCounters reads DBCountersQuery's row as psql prints it unaligned
// (-At): pipe-separated, in the query's order.
func ParseDBCounters(out string) (DBCounters, error) {
	f := strings.Split(strings.TrimSpace(out), "|")
	if len(f) != 9 {
		return DBCounters{}, fmt.Errorf("database counters: %d fields in %q, want 9", len(f), out)
	}
	epoch, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return DBCounters{}, fmt.Errorf("database counters: the time %q: %w", f[0], err)
	}
	n := make([]int64, 8)
	for i := range n {
		if n[i], err = strconv.ParseInt(f[i+1], 10, 64); err != nil {
			return DBCounters{}, fmt.Errorf("database counters: field %d %q: %w", i+2, f[i+1], err)
		}
	}
	sec := int64(epoch)
	at := time.Unix(sec, int64((epoch-float64(sec))*1e9+0.5)).UTC()
	return DBCounters{
		At: at, MetricRows: n[0], PropertyRows: n[1], MetricBytes: n[2], PropertyBytes: n[3],
		DatabaseBytes: n[4], ReceiptBytes: n[5], WALBytes: n[6], ReceiptsInserted: n[7],
	}, nil
}

// ContainerWindow is one container's figures over a window.
type ContainerWindow struct {
	Millicores    float64 `json:"millicores"`
	WorkingSetMiB float64 `json:"working_set_mib"`
}

// Window is one measured window's rates.
type Window struct {
	MetricRowsPerMinute   float64 `json:"metric_rows_per_minute"`
	PropertyRowsPerMinute float64 `json:"property_rows_per_minute"`
	MetricBytesPerDay     float64 `json:"metric_bytes_per_day"`
	PropertyBytesPerDay   float64 `json:"property_bytes_per_day"`
	ReceiptBytesPerDay    float64 `json:"receipt_bytes_per_day"`
	DatabaseBytesPerDay   float64 `json:"database_bytes_per_day"`
	WALBytesPerDay        float64 `json:"wal_bytes_per_day"`
	// WALBytes is the WAL the window itself wrote, before extrapolation.
	WALBytes int64 `json:"wal_bytes"`
	// BatchesLanded is the receipts the window inserted, SamplesLanded the
	// metric and property rows it inserted; SamplesPerBatch is their mean and
	// BatchesPerSecond the batch rate (BatchRates).
	BatchesLanded    int64                      `json:"batches_landed"`
	SamplesLanded    int64                      `json:"samples_landed"`
	SamplesPerBatch  float64                    `json:"samples_per_batch"`
	BatchesPerSecond float64                    `json:"batches_per_second"`
	Containers       map[string]ContainerWindow `json:"containers"`
}

// BatchRates is the mean samples a batch carried and the batches landed a
// second, over a window of dt. No batches reads as 0 samples a batch, and a
// window with no length as 0 batches a second, so neither figure divides by
// zero.
func BatchRates(batches, samples int64, dt time.Duration) (samplesPerBatch, batchesPerSecond float64) {
	if batches > 0 {
		samplesPerBatch = float64(samples) / float64(batches)
	}
	if dt > 0 {
		batchesPerSecond = float64(batches) / dt.Seconds()
	}
	return samplesPerBatch, batchesPerSecond
}

// FoldNodes is w with every node container (any but the server and the
// database) summed into one node row, so a lab with a node per site (the
// corporate scenario, #1719) reports the server, node and database rows a
// one-node lab does. CPU and working set add across containers.
func FoldNodes(w Window) Window {
	out := w
	out.Containers = map[string]ContainerWindow{}
	var node ContainerWindow
	nodes := 0
	for name, c := range w.Containers {
		if name == "server" || name == "db" {
			out.Containers[name] = c
			continue
		}
		node.Millicores += c.Millicores
		node.WorkingSetMiB += c.WorkingSetMiB
		nodes++
	}
	if nodes > 0 {
		out.Containers["node"] = node
	}
	return out
}

// WindowOf turns the readings at a window's start and end into its rates.
// The working set is the one read at the end; a scale window replaces it
// with the median of the reads taken across the window (MedianWorkingSets).
func WindowOf(db0, db1 DBCounters, use0, use1 map[string]Usage) (Window, error) {
	dt := db1.At.Sub(db0.At)
	if dt <= 0 {
		return Window{}, fmt.Errorf("a window from %s to %s has no length", db0.At, db1.At)
	}
	// The statistics counters only grow while Postgres runs; one that went
	// backwards means it restarted and reset them.
	if db1.MetricRows < db0.MetricRows || db1.PropertyRows < db0.PropertyRows || db1.ReceiptsInserted < db0.ReceiptsInserted || db1.WALBytes < db0.WALBytes {
		return Window{}, fmt.Errorf("the database's counters went backwards between %s and %s (Postgres restarted?)", db0.At.Format(time.TimeOnly), db1.At.Format(time.TimeOnly))
	}
	perMin := func(a, b int64) float64 { return float64(b-a) / dt.Minutes() }
	perDay := func(a, b int64) float64 { return float64(b-a) / dt.Hours() * 24 }
	batches := db1.ReceiptsInserted - db0.ReceiptsInserted
	samples := (db1.MetricRows - db0.MetricRows) + (db1.PropertyRows - db0.PropertyRows)
	perBatch, perSecond := BatchRates(batches, samples, dt)
	w := Window{
		BatchesLanded: batches, SamplesLanded: samples,
		SamplesPerBatch: perBatch, BatchesPerSecond: perSecond,
		MetricRowsPerMinute:   perMin(db0.MetricRows, db1.MetricRows),
		PropertyRowsPerMinute: perMin(db0.PropertyRows, db1.PropertyRows),
		MetricBytesPerDay:     perDay(db0.MetricBytes, db1.MetricBytes),
		PropertyBytesPerDay:   perDay(db0.PropertyBytes, db1.PropertyBytes),
		ReceiptBytesPerDay:    perDay(db0.ReceiptBytes, db1.ReceiptBytes),
		DatabaseBytesPerDay:   perDay(db0.DatabaseBytes, db1.DatabaseBytes),
		WALBytesPerDay:        perDay(db0.WALBytes, db1.WALBytes),
		WALBytes:              db1.WALBytes - db0.WALBytes,
		Containers:            map[string]ContainerWindow{},
	}
	for name, a := range use0 {
		b, ok := use1[name]
		if !ok {
			return Window{}, fmt.Errorf("the %s container was read at the window's start and not at its end", name)
		}
		mc, err := Millicores(a, b)
		if err != nil {
			return Window{}, fmt.Errorf("the %s container: %w", name, err)
		}
		w.Containers[name] = ContainerWindow{
			Millicores:    mc,
			WorkingSetMiB: float64(b.WorkingSetBytes()) / (1 << 20),
		}
	}
	return w, nil
}

// MedianWorkingSetMiB is the median of the working sets reads took, in MiB:
// the middle read, or the mean of the middle two. A Go process's heap grows
// to its next collection and falls back, so one read lands anywhere in that
// cycle and the median of reads spread across a window does not (#1751). No
// reads read as 0; reads is not reordered.
func MedianWorkingSetMiB(reads []Usage) float64 {
	n := len(reads)
	if n == 0 {
		return 0
	}
	sets := make([]uint64, n)
	for i, u := range reads {
		sets[i] = u.WorkingSetBytes()
	}
	slices.Sort(sets)
	med := float64(sets[n/2])
	if n%2 == 0 {
		med = (float64(sets[n/2-1]) + med) / 2
	}
	return med / (1 << 20)
}

// MedianWorkingSets is w with each container's working set the median of the
// reads taken across the window, its CPU left the window's delta. A
// container w lists with no reads is refused rather than read as holding
// nothing.
func MedianWorkingSets(w Window, reads map[string][]Usage) (Window, error) {
	out := w
	out.Containers = make(map[string]ContainerWindow, len(w.Containers))
	for name, c := range w.Containers {
		rs := reads[name]
		if len(rs) == 0 {
			return Window{}, fmt.Errorf("the %s container has no working set read across the window", name)
		}
		c.WorkingSetMiB = MedianWorkingSetMiB(rs)
		out.Containers[name] = c
	}
	return out, nil
}
