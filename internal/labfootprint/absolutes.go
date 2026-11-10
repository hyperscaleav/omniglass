package labfootprint

import (
	"fmt"
	"strconv"
	"strings"
)

// AbsolutesQuery reads, in one row, the database's levels at a point (as
// against DBCountersQuery's counters, which a window differences): the
// database's size, how many day partitions the metric lane holds and the
// day the oldest starts, the total bytes of the metric_block and
// metric_trend rollups, and the dead tuples awaiting vacuum in the tables
// retention and the log lanes churn. A sampling run's series of these shows
// growth, retention and vacuum keeping up, or not (#1632). A table's figure
// sums over its partitions, and a table this build lacks reads 0.
const AbsolutesQuery = `with tree(name, relid) as (
    select l, t.relid from unnest(array['property', 'log_line', 'node_log', 'metric_block', 'metric_trend']) l,
      lateral (select relid from pg_partition_tree(to_regclass(l)) union select to_regclass(l)) t
    where t.relid is not null),
  part(lo) as (
    select (regexp_match(pg_get_expr(c.relpartbound, c.oid), 'FROM \(''([^'']+)''\)'))[1]::timestamptz
    from pg_inherits i join pg_class c on c.oid = i.inhrelid
    where i.inhparent = to_regclass('public.metric') and not i.inhdetachpending)
select pg_database_size(current_database()),
  (select count(*) from part),
  coalesce((select to_char(min(lo) at time zone 'UTC', 'YYYY-MM-DD') from part), ''),
  (select coalesce(sum(pg_total_relation_size(relid)), 0)::bigint from tree where name = 'metric_block'),
  (select coalesce(sum(pg_total_relation_size(relid)), 0)::bigint from tree where name = 'metric_trend'),
  (select coalesce(sum(s.n_dead_tup), 0)::bigint from tree join pg_stat_user_tables s using (relid) where tree.name = 'property'),
  (select coalesce(sum(s.n_dead_tup), 0)::bigint from tree join pg_stat_user_tables s using (relid) where tree.name = 'log_line'),
  (select coalesce(sum(s.n_dead_tup), 0)::bigint from tree join pg_stat_user_tables s using (relid) where tree.name = 'node_log')`

// DeadTuples is the dead tuples awaiting vacuum per table.
type DeadTuples struct {
	Property int64 `json:"property"`
	LogLine  int64 `json:"log_line"`
	NodeLog  int64 `json:"node_log"`
}

// Absolutes is one reading of AbsolutesQuery.
type Absolutes struct {
	DatabaseBytes    int64 `json:"database_bytes"`
	MetricPartitions int64 `json:"metric_partitions"`
	// MetricOldestDay is the day (UTC, YYYY-MM-DD) the oldest metric
	// partition starts; empty when there is none.
	MetricOldestDay  string     `json:"metric_oldest_partition_day,omitempty"`
	MetricBlockBytes int64      `json:"metric_block_bytes"`
	MetricTrendBytes int64      `json:"metric_trend_bytes"`
	DeadTuples       DeadTuples `json:"dead_tuples"`
}

// ParseAbsolutes reads AbsolutesQuery's row as psql prints it unaligned
// (-At): pipe-separated, in the query's order.
func ParseAbsolutes(out string) (Absolutes, error) {
	f := strings.Split(strings.TrimSpace(out), "|")
	if len(f) != 8 {
		return Absolutes{}, fmt.Errorf("database absolutes: %d fields in %q, want 8", len(f), out)
	}
	var n [7]int64
	for i, j := range []int{0, 1, 3, 4, 5, 6, 7} {
		v, err := strconv.ParseInt(f[j], 10, 64)
		if err != nil {
			return Absolutes{}, fmt.Errorf("database absolutes: field %d %q: %w", j+1, f[j], err)
		}
		n[i] = v
	}
	return Absolutes{
		DatabaseBytes: n[0], MetricPartitions: n[1], MetricOldestDay: f[2],
		MetricBlockBytes: n[2], MetricTrendBytes: n[3],
		DeadTuples: DeadTuples{Property: n[4], LogLine: n[5], NodeLog: n[6]},
	}, nil
}

// lines writes the absolutes' table rows.
func (a Absolutes) lines(line func(name, value string)) {
	oldest := a.MetricOldestDay
	if oldest == "" {
		oldest = "none"
	}
	line("database size", size(float64(a.DatabaseBytes)))
	line("metric partitions", fmt.Sprintf("%d   (oldest %s)", a.MetricPartitions, oldest))
	line("metric blocks and trends", size(float64(a.MetricBlockBytes))+" and "+size(float64(a.MetricTrendBytes)))
	line("dead tuples", fmt.Sprintf("%s property, %s log_line, %s node_log",
		count(float64(a.DeadTuples.Property)), count(float64(a.DeadTuples.LogLine)), count(float64(a.DeadTuples.NodeLog))))
}
