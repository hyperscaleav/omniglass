package storage_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The narrow metric lane's partitions (#1275, #420, ADR-1275): a month per
// partition, two ahead of the current one, a missing month made on demand
// rather than a batch dropped, and a month past every window dropped whole.

func monthName(t time.Time) string { return "metric_p" + t.UTC().Format("200601") }

func monthStart(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// TestMetricPartitionsRollAhead: the roll keeps this month and the next two,
// creates nothing it already has, and a dropped month ahead comes back.
func TestMetricPartitionsRollAhead(t *testing.T) {
	f := newPruneFixture(t)
	now := time.Now().UTC()
	want := []string{monthName(now), monthName(monthStart(now).AddDate(0, 1, 0)), monthName(monthStart(now).AddDate(0, 2, 0))}
	if created, err := f.gw.EnsureMetricPartitions(f.ctx); err != nil || len(created) != 0 {
		t.Fatalf("a roll over a fresh install created %v (%v), want nothing: the migration made them", created, err)
	}
	mustExec(t, f.conn, `drop table `+want[2])
	created, err := f.gw.EnsureMetricPartitions(f.ctx)
	if err != nil || len(created) != 1 || created[0] != want[2] {
		t.Fatalf("the roll created %v (%v), want %s back", created, err, want[2])
	}
	parts := partitionsOf(t, f.conn, "metric")
	for _, w := range want {
		if !strings.Contains(strings.Join(parts, ","), w) {
			t.Errorf("partitions %v, want %s", parts, w)
		}
	}
	// A partition made by the roll carries the parent's indexes.
	var idx int
	if err := f.conn.QueryRow(f.ctx, `select count(*) from pg_indexes where tablename = $1`, want[2]).Scan(&idx); err != nil || idx != 3 {
		t.Errorf("%s has %d indexes (%v), want the parent's 3", want[2], idx, err)
	}
}

// TestAMissingMonthDropsNoBatch: a batch carrying a sample whose month has no
// partition (the roll was late at the month boundary) lands whole, its month
// made on the way; the miss itself is never classified as a refusal, so a
// batch that misses again is redelivered, not dropped.
func TestAMissingMonthDropsNoBatch(t *testing.T) {
	f := newPruneFixture(t)
	next := monthStart(time.Now()).AddDate(0, 1, 0)
	mustExec(t, f.conn, `drop table `+monthName(next))
	landed, err := f.gw.LandBatch(f.ctx, storage.IngestBatch{BatchID: "missed-month", Writer: storage.PushWriter(),
		Metrics: []storage.MetricSampleWrite{
			{OwnerKind: "component", OwnerID: "prune-c1", Key: "prune-rtt", Value: 7, TS: next.Add(time.Minute)},
			{OwnerKind: "component", OwnerID: "prune-c1", Key: "prune-rtt", Value: 8, TS: time.Now().UTC()},
		}})
	if err != nil || !landed {
		t.Fatalf("the batch over a missing month: landed %v, err %v; want it landed", landed, err)
	}
	var n int
	if err := f.conn.QueryRow(f.ctx, `select count(*) from `+storagetest.MetricRows+` m where m.source = '' and m.value in (7, 8)`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("%d of the batch's samples stored (%v), want both", n, err)
	}
	mustExec(t, f.conn, `drop table `+monthName(next.AddDate(0, 1, 0)))
	_, err = f.conn.Exec(f.ctx, `insert into metric (series_id, ts, value) values (1, $1, 1)`, next.AddDate(0, 1, 0))
	if err == nil || !storage.ExportIsPartitionRoutingMiss(err) || storage.IsRefusedWrite(err) {
		t.Fatalf("a routing miss: err %v, routing miss %v, refused %v; want a miss the ingest retries", err,
			storage.ExportIsPartitionRoutingMiss(err), storage.IsRefusedWrite(err))
	}
}

// TestDropMetricPartitionsHonorsEveryWindow: a month wholly past the metric
// window is dropped whole, its rows with it; a month a type's own longer
// window still reaches is kept until that window passes too; the current
// month is never dropped.
func TestDropMetricPartitionsHonorsEveryWindow(t *testing.T) {
	f := newPruneFixture(t)
	now := time.Now().UTC()
	old := monthStart(now).AddDate(0, -3, 0).Add(24 * time.Hour)
	f.row("metric", "component", old, "observed", 1)
	mustExec(t, f.conn, `insert into metric_type (name, data_type) values ('rt-year', 'float')`)
	days := 365
	if _, err := f.gw.SetMetricTypeRetention(f.ctx, "", "rt-year", &days); err != nil {
		t.Fatal(err)
	}
	if dropped, err := f.gw.DropMetricPartitions(f.ctx, now.AddDate(0, 0, -14)); err != nil || len(dropped) != 0 {
		t.Fatalf("with a type kept a year, dropped %v (%v), want nothing", dropped, err)
	}
	if _, err := f.gw.SetMetricTypeRetention(f.ctx, "", "rt-year", nil); err != nil {
		t.Fatal(err)
	}
	dropped, err := f.gw.DropMetricPartitions(f.ctx, now.AddDate(0, 0, -14))
	if err != nil || !strings.Contains(strings.Join(dropped, ","), monthName(old)) {
		t.Fatalf("dropped %v (%v), want %s", dropped, err, monthName(old))
	}
	if strings.Contains(strings.Join(dropped, ","), monthName(now)) {
		t.Fatalf("dropped the current month: %v", dropped)
	}
	if v := f.values("metric", "component"); len(v) != 0 {
		t.Fatalf("rows of a dropped month still read: %v", v)
	}
}

// TestMetricNewestReadsTheNewestMonthFirst: a series' newest sample is an
// index probe of each month's series index, newest month first, stopping at
// the first that holds one: an ordered Append, never a Merge Append or a Sort
// across every month (the "LIMIT 1 with no bound" shape the review warned of).
func TestMetricNewestReadsTheNewestMonthFirst(t *testing.T) {
	f := newPruneFixture(t)
	mustExec(t, f.conn, "set enable_seqscan = off")
	rows, err := f.conn.Query(f.ctx, "explain (costs off) "+storage.ExportMetricNewestSQL(), int64(1))
	if err != nil {
		t.Fatal(err)
	}
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	rows.Close()
	text := strings.Join(plan, "\n")
	if strings.Contains(text, "Merge Append") || strings.Contains(text, "Sort") || !strings.Contains(text, "Append") {
		t.Fatalf("the newest read is not an ordered Append:\n%s", text)
	}
	newest := monthName(monthStart(time.Now()).AddDate(0, 2, 0))
	for _, line := range plan {
		if strings.Contains(line, "Index Scan") {
			if !strings.Contains(line, newest) {
				t.Fatalf("the first month probed is not the newest (%s):\n%s", newest, text)
			}
			break
		}
	}
}

// TestACollectingNodeStartsItsOwnSeries: a node's samples record the node that
// collected them on their series, so the same key, owner and instance
// collected by another node is a series of its own; every read of the key
// merges them, reading the newest of either.
func TestACollectingNodeStartsItsOwnSeries(t *testing.T) {
	f := newWriterFixture(t)
	comp, err := f.gw.GetComponent(f.ctx, "disp-1", scope.Set{All: true})
	if err != nil {
		t.Fatal(err)
	}
	var nodeA string
	if err := f.conn.QueryRow(f.ctx, `select principal_id::text from node where name = 'node-a'`).Scan(&nodeA); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for i, node := range []string{nodeA, "00000000-0000-7000-8000-000000000001"} {
		w := storage.NodeWriter(comp.ID, "ssh")
		w.Node = node
		if err := f.gw.InsertMetricSamples(f.ctx, w, []storage.MetricSampleWrite{
			{OwnerKind: "component", OwnerID: comp.ID, Key: "tcp-connect-time", Instance: "ssh", Value: float64(10 + i), TS: now.Add(time.Duration(i) * time.Second)}}); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.count(t, `select count(*) from metric_series s join metric_type t on t.id = s.metric_type_id
		where t.name = 'tcp-connect-time' and s.collector_node_id is not null`); n != 2 {
		t.Fatalf("%d collected series, want one per collecting node", n)
	}
	if m, err := f.gw.LatestMetricInstance(f.ctx, "disp-1", "tcp-connect-time", "ssh"); err != nil || m == nil || m.Value != 11 {
		t.Fatalf("the latest across both nodes' series = %+v (%v), want the newer 11", m, err)
	}
}

// TestADeletedOwnersSamplesReadAsNothing: deleting an owner removes its series
// in the delete's transaction and leaves its samples, which no read can reach
// (every read joins the series), for retention to remove.
func TestADeletedOwnersSamplesReadAsNothing(t *testing.T) {
	f := newPruneFixture(t)
	now := time.Now().UTC()
	f.row("metric", "component", now.Add(-48*time.Hour), "observed", 1)
	f.row("metric", "component", now.Add(-time.Hour), "observed", 2)
	all := scope.Set{All: true}
	if err := f.gw.DeleteComponent(context.Background(), "", "prune-c1", all, all); err != nil {
		t.Fatal(err)
	}
	var series, hidden, orphans int
	if err := f.conn.QueryRow(f.ctx, `select (select count(*) from metric_series), (select count(*) from `+storagetest.MetricRows+` m),
		(select count(*) from metric)`).Scan(&series, &hidden, &orphans); err != nil {
		t.Fatal(err)
	}
	if series != 0 || hidden != 0 || orphans != 2 {
		t.Fatalf("after the delete: %d series, %d readable samples, %d stored; want 0, 0 and the 2 left for retention", series, hidden, orphans)
	}
	if n := f.prune("metric", now.Add(-24*time.Hour), 100); n != 1 {
		t.Fatalf("the lane prune took %d orphaned samples past the window, want 1", n)
	}
}

// TestARoutingMissIsReadFromItsFieldsNotItsText: the classifier reads the
// error's code, table and constraint, never its message, which a server with
// a translated lc_messages words differently; a CHECK on the table names its
// constraint and stays a refusal.
func TestARoutingMissIsReadFromItsFieldsNotItsText(t *testing.T) {
	miss := &pgconn.PgError{Code: "23514", TableName: "metric", Message: "keine Partition der Relation gefunden"}
	if !storage.ExportIsPartitionRoutingMiss(miss) || storage.IsRefusedWrite(miss) {
		t.Fatal("a translated routing miss was read as a refusal")
	}
	check := &pgconn.PgError{Code: "23514", TableName: "metric_series", ConstraintName: "metric_series_owner_arc_check"}
	if storage.ExportIsPartitionRoutingMiss(check) || !storage.IsRefusedWrite(check) {
		t.Fatal("a CHECK violation was read as a routing miss")
	}
}

// TestAHalfDetachedMonthStillLands: a retention pass's concurrent detach that
// stopped halfway (a reader held the table past its lock timeout) leaves the
// month routing nothing; a batch carrying a sample in it finishes the detach,
// makes the month afresh and lands, rather than missing on every delivery.
func TestAHalfDetachedMonthStillLands(t *testing.T) {
	f := newPruneFixture(t)
	old := monthStart(time.Now()).AddDate(0, -4, 0)
	if err := f.gw.MakeMetricMonths(f.ctx, old); err != nil {
		t.Fatal(err)
	}
	reader := connectDSN(t, f.dsn)
	tx, err := reader.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(f.ctx, `select count(*) from metric`); err != nil {
		t.Fatal(err)
	}
	mustExec(t, f.conn, `set lock_timeout = '300ms'`)
	if _, err := f.conn.Exec(f.ctx, `alter table metric detach partition `+monthName(old)+` concurrently`); err == nil {
		t.Fatal("the detach finished under a held reader; the test needs it halfway")
	}
	mustExec(t, f.conn, `reset lock_timeout`)
	_ = tx.Rollback(f.ctx)
	var pending bool
	if err := f.conn.QueryRow(f.ctx, `select exists (select 1 from pg_inherits where inhdetachpending)`).Scan(&pending); err != nil || !pending {
		t.Fatalf("no detach pending (%v)", err)
	}
	landed, err := f.gw.LandBatch(f.ctx, storage.IngestBatch{BatchID: "half-detached", Writer: storage.PushWriter(),
		Metrics: []storage.MetricSampleWrite{{OwnerKind: "component", OwnerID: "prune-c1", Key: "prune-rtt", Value: 5, TS: old.Add(time.Hour)}}})
	if err != nil || !landed {
		t.Fatalf("a batch into a half-detached month: landed %v, err %v", landed, err)
	}
	if v := f.values("metric", "component"); len(v) != 1 {
		t.Fatalf("rows %v, want the one sample landed", v)
	}
}
