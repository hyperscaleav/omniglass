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
// is one full core.
func Millicores(a, b Usage) float64 {
	dt := b.At.Sub(a.At)
	if dt <= 0 || b.CPUNanos < a.CPUNanos {
		return 0
	}
	return float64(b.CPUNanos-a.CPUNanos) / float64(dt.Nanoseconds()) * 1000
}

// DBCountersQuery reads, in one row, the database counters a window
// differences: rows inserted into each telemetry lane (the cumulative
// statistics, so nothing is counted by a scan), the total size of each lane
// and of the receipts with their indexes, the database's size, and the WAL
// position. A lane's figures sum over its partitions, so a partitioned table
// reads the same way as a plain one (pg_partition_tree lists
// nothing for a plain table, so the table itself is always in its lane).
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
  pg_wal_lsn_diff(pg_current_wal_lsn(), '0/0')::bigint`

// DBCounters is one reading of DBCountersQuery.
type DBCounters struct {
	At                          time.Time
	MetricRows, PropertyRows    int64
	MetricBytes, PropertyBytes  int64
	DatabaseBytes, ReceiptBytes int64
	WALBytes                    int64
}

// ParseDBCounters reads DBCountersQuery's row as psql prints it unaligned
// (-At): pipe-separated, in the query's order.
func ParseDBCounters(out string) (DBCounters, error) {
	f := strings.Split(strings.TrimSpace(out), "|")
	if len(f) != 8 {
		return DBCounters{}, fmt.Errorf("database counters: %d fields in %q, want 8", len(f), out)
	}
	epoch, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return DBCounters{}, fmt.Errorf("database counters: the time %q: %w", f[0], err)
	}
	n := make([]int64, 7)
	for i := range n {
		if n[i], err = strconv.ParseInt(f[i+1], 10, 64); err != nil {
			return DBCounters{}, fmt.Errorf("database counters: field %d %q: %w", i+2, f[i+1], err)
		}
	}
	sec := int64(epoch)
	at := time.Unix(sec, int64((epoch-float64(sec))*1e9+0.5)).UTC()
	return DBCounters{
		At: at, MetricRows: n[0], PropertyRows: n[1], MetricBytes: n[2], PropertyBytes: n[3],
		DatabaseBytes: n[4], ReceiptBytes: n[5], WALBytes: n[6],
	}, nil
}

// ContainerWindow is one container's figures over a window.
type ContainerWindow struct {
	Millicores    float64 `json:"millicores"`
	WorkingSetMiB float64 `json:"working_set_mib"`
}

// Window is one measured window's rates.
type Window struct {
	MetricRowsPerMinute   float64                    `json:"metric_rows_per_minute"`
	PropertyRowsPerMinute float64                    `json:"property_rows_per_minute"`
	MetricBytesPerDay     float64                    `json:"metric_bytes_per_day"`
	PropertyBytesPerDay   float64                    `json:"property_bytes_per_day"`
	ReceiptBytesPerDay    float64                    `json:"receipt_bytes_per_day"`
	DatabaseBytesPerDay   float64                    `json:"database_bytes_per_day"`
	WALBytesPerDay        float64                    `json:"wal_bytes_per_day"`
	Containers            map[string]ContainerWindow `json:"containers"`
}

// WindowOf turns the readings at a window's start and end into its rates.
// The working set is the one read at the end.
func WindowOf(db0, db1 DBCounters, use0, use1 map[string]Usage) (Window, error) {
	dt := db1.At.Sub(db0.At)
	if dt <= 0 {
		return Window{}, fmt.Errorf("a window from %s to %s has no length", db0.At, db1.At)
	}
	perMin := func(a, b int64) float64 { return float64(b-a) / dt.Minutes() }
	perDay := func(a, b int64) float64 { return float64(b-a) / dt.Hours() * 24 }
	w := Window{
		MetricRowsPerMinute:   perMin(db0.MetricRows, db1.MetricRows),
		PropertyRowsPerMinute: perMin(db0.PropertyRows, db1.PropertyRows),
		MetricBytesPerDay:     perDay(db0.MetricBytes, db1.MetricBytes),
		PropertyBytesPerDay:   perDay(db0.PropertyBytes, db1.PropertyBytes),
		ReceiptBytesPerDay:    perDay(db0.ReceiptBytes, db1.ReceiptBytes),
		DatabaseBytesPerDay:   perDay(db0.DatabaseBytes, db1.DatabaseBytes),
		WALBytesPerDay:        perDay(db0.WALBytes, db1.WALBytes),
		Containers:            map[string]ContainerWindow{},
	}
	for name, a := range use0 {
		b, ok := use1[name]
		if !ok {
			return Window{}, fmt.Errorf("the %s container was read at the window's start and not at its end", name)
		}
		w.Containers[name] = ContainerWindow{
			Millicores:    Millicores(a, b),
			WorkingSetMiB: float64(b.WorkingSetBytes()) / (1 << 20),
		}
	}
	return w, nil
}
