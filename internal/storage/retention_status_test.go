package storage_test

import (
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// TestRetentionStatusReadsThePartitionsAndTheSizes (#1629): the status read
// lists the metric partitions oldest first, so a day created long ago is the
// oldest, and sizes each stored table and the database.
func TestRetentionStatusReadsThePartitionsAndTheSizes(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw, ctx, dsn := platformGatewayDSN(t)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	old := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -40)
	if _, err := conn.Exec(ctx, `create table metric_p`+old.Format("20060102")+` partition of metric for values from ('`+
		old.Format(time.RFC3339)+`') to ('`+old.AddDate(0, 0, 1).Format(time.RFC3339)+`')`); err != nil {
		t.Fatalf("create an old day's partition: %v", err)
	}

	st, err := gw.RetentionStatus(ctx)
	if err != nil {
		t.Fatalf("read the retention status: %v", err)
	}
	parts, err := gw.MetricPartitions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Partitions) != len(parts) || len(parts) < 2 {
		t.Fatalf("status lists %d partitions, want all %d", len(st.Partitions), len(parts))
	}
	if !st.Partitions[0].From.Equal(old) {
		t.Errorf("oldest partition starts %s, want the day created 40 days back %s", st.Partitions[0].From, old)
	}
	tables := []string{"log_line", "metric", "metric_block", "metric_trend", "node_log", "property"}
	if got := slices.Sorted(maps.Keys(st.TableBytes)); !slices.Equal(got, tables) {
		t.Errorf("sized tables %v, want %v", got, tables)
	}
	// metric is sized as the sum of its partitions: the parent holds no rows,
	// so its own size is zero, and each day's partition carries its indexes.
	if st.TableBytes["metric"] <= 0 {
		t.Errorf("metric sized %d bytes, want its partitions' sum", st.TableBytes["metric"])
	}
	if st.DatabaseBytes <= st.TableBytes["metric"] {
		t.Errorf("database sized %d bytes, want more than metric's %d", st.DatabaseBytes, st.TableBytes["metric"])
	}
}

// TestRetentionKeysAreTheProcesssAlone (#1629): the retention pass's figures
// are platform keys, so the process lands them on a platform component and a
// node, a push or an operator writing one is refused.
func TestRetentionKeysAreTheProcesssAlone(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw, ctx := platformGateway(t)
	comp := platformNode(t, ctx, gw, "edge-r")
	now := time.Now().UTC()
	for _, key := range writerclass.RetentionKeys() {
		if err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{{ComponentID: comp.ID, Key: key, Instance: "metric", Value: 1, TS: now}}); err != nil {
			t.Errorf("the process writing %s: %v", key, err)
		}
		for _, w := range []storage.KeyWriter{storage.NodeWriter(comp.ID, ""), storage.PushWriter(), {Kind: writerclass.Operator}} {
			err := gw.InsertMetricSamples(ctx, w, []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: comp.ID,
				Key: key, Instance: "metric", Value: 1, TS: now}})
			if !errors.Is(err, storage.ErrKeyWriterRefused) {
				t.Errorf("%s writing %s: err = %v, want ErrKeyWriterRefused", w.Kind, key, err)
			}
		}
	}
}
