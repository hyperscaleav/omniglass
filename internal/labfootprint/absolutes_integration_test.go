package labfootprint

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// readAbsolutes runs AbsolutesQuery as psql -At prints it: the row's
// fields joined by a pipe.
func readAbsolutes(t *testing.T, conn *pgx.Conn) Absolutes {
	t.Helper()
	rows, err := conn.Query(context.Background(), AbsolutesQuery)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("the absolutes query returned no row: %v", rows.Err())
	}
	vals, err := rows.Values()
	if err != nil {
		t.Fatal(err)
	}
	f := make([]string, len(vals))
	for i, v := range vals {
		if v != nil {
			f[i] = fmt.Sprint(v)
		}
	}
	a, err := ParseAbsolutes(strings.Join(f, "|"))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// The absolutes read off a migrated database: its size, the metric
// partitions and the oldest of them, the rollup tables' bytes and the dead
// tuples of the tables retention and the log lanes churn. Adding a metric
// partition moves the count.
func TestAbsolutesQuery(t *testing.T) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	before := readAbsolutes(t, conn)
	if before.DatabaseBytes <= 0 {
		t.Errorf("database bytes = %d, want the database's size", before.DatabaseBytes)
	}
	if before.MetricBlockBytes <= 0 || before.MetricTrendBytes <= 0 {
		t.Errorf("metric_block %d and metric_trend %d bytes, want their sizes (an empty table still has its indexes)",
			before.MetricBlockBytes, before.MetricTrendBytes)
	}

	day := time.Date(2001, 1, 2, 0, 0, 0, 0, time.UTC)
	if _, err := conn.Exec(ctx, fmt.Sprintf(`create table metric_p20010102 partition of metric
		for values from ('%s') to ('%s')`, day.Format(time.RFC3339), day.AddDate(0, 0, 1).Format(time.RFC3339))); err != nil {
		t.Fatal(err)
	}
	var nodeID string
	if err := conn.QueryRow(ctx, `insert into principal (kind) values ('node') returning id`).Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `insert into node (principal_id, name) values ($1, 'edge-x')`, nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `insert into node_log (node_id, message) select $1, 'x' from generate_series(1, 50)`, nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `delete from node_log`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `select pg_stat_force_next_flush()`); err != nil {
		t.Fatal(err)
	}

	after := readAbsolutes(t, conn)
	if after.MetricPartitions != before.MetricPartitions+1 {
		t.Errorf("metric partitions %d after adding one to %d", after.MetricPartitions, before.MetricPartitions)
	}
	if after.MetricOldestDay != "2001-01-02" {
		t.Errorf("oldest metric partition = %q, want the one just added", after.MetricOldestDay)
	}
	if after.DeadTuples.NodeLog < 50 {
		t.Errorf("node_log dead tuples = %d after deleting 50 rows", after.DeadTuples.NodeLog)
	}
}

// ParseAbsolutes reads the row and refuses a short one.
func TestParseAbsolutes(t *testing.T) {
	got, err := ParseAbsolutes("1000|4|2026-11-06|200|300|5|6|7\n")
	if err != nil {
		t.Fatal(err)
	}
	want := Absolutes{DatabaseBytes: 1000, MetricPartitions: 4, MetricOldestDay: "2026-11-06", MetricBlockBytes: 200, MetricTrendBytes: 300,
		DeadTuples: DeadTuples{Property: 5, LogLine: 6, NodeLog: 7}}
	if got != want {
		t.Errorf("parsed %+v, want %+v", got, want)
	}
	if _, err := ParseAbsolutes("1|2|3"); err == nil {
		t.Error("a three-field row parsed")
	}
	empty, err := ParseAbsolutes("1000|0||0|0|0|0|0")
	if err != nil || empty.MetricOldestDay != "" {
		t.Errorf("no partitions: %+v, %v", empty, err)
	}
}
