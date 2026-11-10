package storage

import (
	"context"
	"fmt"
)

// RetentionStatus is what the retention pass reports of storage after it
// runs (#1629): the metric table's day partitions, oldest first, and the
// on-disk bytes of each stored table, with its indexes and toast, and of the
// whole database.
type RetentionStatus struct {
	Partitions    []MetricPartition
	TableBytes    map[string]int64
	DatabaseBytes int64
}

// RetentionStatus reads the metric partitions and the sizes. metric is sized
// as the sum of its partitions (the parent holds no rows of its own); the
// others are plain tables. The log tables' oldest row is not read: log_line
// has no index leading on ts, so its min(ts) is a full scan (#1417).
func (p *PG) RetentionStatus(ctx context.Context) (RetentionStatus, error) {
	parts, err := metricPartitions(ctx, p.pool)
	if err != nil {
		return RetentionStatus{}, err
	}
	st := RetentionStatus{Partitions: parts, TableBytes: map[string]int64{}}
	var metric, trend, block, property, logLine, nodeLog int64
	if err := p.pool.QueryRow(ctx, `select
			(select coalesce(sum(pg_total_relation_size(inhrelid)), 0)::bigint from pg_inherits where inhparent = 'metric'::regclass),
			pg_total_relation_size('metric_trend'), pg_total_relation_size('metric_block'),
			pg_total_relation_size('property'), pg_total_relation_size('log_line'),
			pg_total_relation_size('node_log'), pg_database_size(current_database())`).
		Scan(&metric, &trend, &block, &property, &logLine, &nodeLog, &st.DatabaseBytes); err != nil {
		return RetentionStatus{}, fmt.Errorf("storage: retention status sizes: %w", err)
	}
	st.TableBytes["metric"], st.TableBytes["metric_trend"], st.TableBytes["metric_block"] = metric, trend, block
	st.TableBytes["property"], st.TableBytes["log_line"], st.TableBytes["node_log"] = property, logLine, nodeLog
	return st, nil
}
