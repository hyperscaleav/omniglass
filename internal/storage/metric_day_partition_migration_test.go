package storage_test

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/db"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The metric lane's day partitions (#1527, ADR-1527) are one create-copy-swap
// over the month-partitioned table #1275 left. These tests stand a database
// just below the swap, plant samples over several days and months, and
// migrate forward over them.

const metricDayVersion = "20261106050925"

var (
	monthPartition = regexp.MustCompile(`^metric_p[0-9]{6}$`)
	dayPartition   = regexp.MustCompile(`^metric_p[0-9]{8}$`)
)

// standMonthLayout rolls a migrated database back below the swap and plants
// a component's samples in the month layout: two instances every five hours
// over the last six weeks (so the newest-first readers have more than one
// month to probe), a calculated sample with its rule lineage, and two older
// samples in August. It answers the gateway and a connection.
func standMonthLayout(t *testing.T) (*storage.PG, *pgx.Conn, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, metricDayVersion); err != nil {
		t.Fatalf("roll back below the day partitions: %v", err)
	}
	conn := connectDSN(t, dsn)
	if parts := partitionsOf(t, conn, "metric"); len(parts) == 0 || !monthPartition.MatchString(parts[0]) {
		t.Fatalf("below the swap, partitions %v, want months", parts)
	}
	mustExec(t, conn, `do $$
		declare m date;
		begin
			for m in select generate_series(date '2026-08-01', date_trunc('month', now() at time zone 'UTC')::date, interval '1 month')::date loop
				execute format('create table if not exists %I partition of metric for values from (%L) to (%L)',
					'metric_p' || to_char(m, 'YYYYMM'), m::text || ' 00:00:00+00', (m + interval '1 month')::date::text || ' 00:00:00+00');
			end loop;
		end $$`)
	mustExec(t, conn, `insert into component (name, product_id) values ('swap-c1', (select id from product where name = 'generic-device'))`)
	mustExec(t, conn, `insert into metric_type (name, data_type) values ('swap-rtt', 'float')`)
	// Half an hour before the last hour, so no sample sits on a window's edge
	// as the clock moves between the reads before and after.
	anchor := time.Now().UTC().Truncate(time.Hour).Add(-30 * time.Minute)
	cols := `ts, owner_kind, component_id, metric_type_id, instance, provenance, value, source_rule, source_rule_version, source_rule_id`
	owner := `'component', (select id from component where name = 'swap-c1'), (select id from metric_type where name = 'swap-rtt')`
	mustExec(t, conn, storagetest.WideMetricInsert(cols,
		`select $1::timestamptz - g * interval '5 hours', `+owner+`, case when g % 4 = 0 then 'icmp' else '' end,
			'observed', g * 1.25, null, null, null
		from generate_series(0, 200) g`), anchor)
	mustExec(t, conn, storagetest.WideMetricInsert(cols,
		`values ($1::timestamptz, `+owner+`, '', 'calculated', 42, 'avg-rule', 3, gen_random_uuid()),
			('2026-08-20T00:00:00Z', `+owner+`, '', 'observed', 7, null, null, null),
			('2026-08-31T23:59:59Z', `+owner+`, '', 'observed', 8, null, null, null)`), anchor.Add(-90*time.Minute))
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	return gw, conn, dsn
}

// metricReads is every metric reader's answer over the planted series, as
// JSON, so before and after compare whole.
func metricReads(t *testing.T, gw *storage.PG) map[string]string {
	t.Helper()
	ctx := context.Background()
	icmp := "icmp"
	out := map[string]string{}
	add := func(name string, v any, err error) {
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = string(b)
	}
	latest, err := gw.LatestMetric(ctx, "swap-c1", "swap-rtt")
	add("LatestMetric", latest, err)
	inst, err := gw.LatestMetricInstance(ctx, "swap-c1", "swap-rtt", "icmp")
	add("LatestMetricInstance", inst, err)
	series, err := gw.ListMetricSeries(ctx, "component", "swap-c1", "swap-rtt", 21*24*time.Hour, 1000)
	add("ListMetricSeries", series, err)
	buckets, err := gw.ListMetricBuckets(ctx, "component", "swap-c1", "swap-rtt", &icmp, 30*24*time.Hour, 24*time.Hour)
	add("ListMetricBuckets", []any{buckets.Instance, buckets.Buckets}, err)
	return out
}

// TestMetricDayPartitionsKeepEveryRow: across the swap every sample stays as
// it was, id included; every reader answers as it did; every partition is a
// day's, one for each day a sample is in and today through three days ahead;
// and a new sample's id carries on past the old table's. Rolled back, the
// month layout holds the same rows again, and forward again the days do.
func TestMetricDayPartitionsKeepEveryRow(t *testing.T) {
	gw, conn, dsn := standMonthLayout(t)
	ctx := context.Background()
	want := snapshotRows(t, conn, `select * from metric`, true)
	reads := metricReads(t, gw)
	for name, v := range reads {
		if v == "null" || v == "[]" || strings.Contains(v, `"Buckets":null`) {
			t.Fatalf("%s reads nothing over the planted series: %s", name, v)
		}
	}
	var maxID int64
	if err := conn.QueryRow(ctx, `select max(id) from metric`).Scan(&maxID); err != nil {
		t.Fatal(err)
	}

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward: %v", err)
	}
	if got := snapshotRows(t, conn, `select * from metric`, true); !slices.Equal(got, want) {
		t.Fatalf("across the swap the samples differ:\n got  %d rows\n want %d rows\n first got  %v\n first want %v",
			len(got), len(want), first(got), first(want))
	}
	after := metricReads(t, gw)
	for name, before := range reads {
		if after[name] != before {
			t.Errorf("%s across the swap:\n before %s\n after  %s", name, before, after[name])
		}
	}
	parts := partitionsOf(t, conn, "metric")
	for _, p := range parts {
		if !dayPartition.MatchString(p) {
			t.Errorf("partition %s is not a day's", p)
		}
	}
	var days []string
	rows, err := conn.Query(ctx, `select distinct 'metric_p' || to_char(ts at time zone 'UTC', 'YYYYMMDD') from metric
		union select 'metric_p' || to_char((now() at time zone 'UTC')::date + g, 'YYYYMMDD') from generate_series(0, 3) g`)
	if err != nil {
		t.Fatal(err)
	}
	if days, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		t.Fatal(err)
	}
	for _, d := range days {
		if !slices.Contains(parts, d) {
			t.Errorf("partitions %v, want %s among them", parts, d)
		}
	}
	if len(parts) != len(days) {
		t.Errorf("%d partitions, want the %d days", len(parts), len(days))
	}
	var idx int
	if err := conn.QueryRow(ctx, `select count(*) from pg_indexes where tablename = 'metric'`).Scan(&idx); err != nil || idx != 3 {
		t.Errorf("metric has %d indexes (%v), want the 3 it had", idx, err)
	}
	var sid, next int64
	if err := conn.QueryRow(ctx, `select series_id from metric limit 1`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `insert into metric (series_id, value) values ($1, 1) returning id`, sid).Scan(&next); err != nil {
		t.Fatalf("a new sample: %v", err)
	}
	if next <= maxID {
		t.Errorf("a new sample's id %d, want past the old table's %d", next, maxID)
	}
	if _, err := conn.Exec(ctx, `delete from metric where id = $1`, next); err != nil {
		t.Fatal(err)
	}

	if err := migrate.RollbackBelow(dsn, metricDayVersion); err != nil {
		t.Fatalf("roll back the day partitions: %v", err)
	}
	if got := snapshotRows(t, conn, `select * from metric`, true); !slices.Equal(got, want) {
		t.Errorf("rolled back, %d rows, want the %d it held", len(got), len(want))
	}
	for _, p := range partitionsOf(t, conn, "metric") {
		if !monthPartition.MatchString(p) {
			t.Errorf("rolled back, partition %s is not a month's", p)
		}
	}
	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward again: %v", err)
	}
	if got := snapshotRows(t, conn, `select * from metric`, true); !slices.Equal(got, want) {
		t.Errorf("forward again: %d rows, want %d", len(got), len(want))
	}
}

// TestMetricDayPartitionsRefuseALostRow: the swap is refused, whole, when the
// day-partitioned table does not hold every sample: the count check is what
// makes dropping the month table safe. The refused swap leaves the month
// layout and its rows as they were.
func TestMetricDayPartitionsRefuseALostRow(t *testing.T) {
	_, conn, _ := standMonthLayout(t)
	ctx := context.Background()
	want := snapshotRows(t, conn, `select * from metric`, true)
	raw, err := db.FS.ReadFile("migrations/" + metricDayMigration)
	if err != nil {
		t.Fatal(err)
	}
	up, _, _ := strings.Cut(string(raw), "-- migrate:down")
	copySQL := `source_rule_id FROM public.metric_month;`
	if strings.Count(up, copySQL) != 1 {
		t.Fatalf("the up migration's copy (%q) is not where the test expects it", copySQL)
	}
	lossy := strings.Replace(up, copySQL, `source_rule_id FROM public.metric_month OFFSET 1;`, 1)
	if _, err := conn.Exec(ctx, strings.TrimPrefix(lossy, "-- migrate:up")); err == nil || !strings.Contains(err.Error(), "metric day partitions") {
		t.Fatalf("a copy that loses a sample: err = %v, want the count refusal", err)
	}
	if got := snapshotRows(t, conn, `select * from metric`, true); !slices.Equal(got, want) {
		t.Errorf("after the refusal, %d rows, want the %d it held", len(got), len(want))
	}
	for _, p := range partitionsOf(t, conn, "metric") {
		if !monthPartition.MatchString(p) {
			t.Errorf("after the refusal, partition %s is not a month's", p)
		}
	}
}
