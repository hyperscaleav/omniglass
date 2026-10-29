package storage_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/db"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The narrow metric lane (#1275, ADR-1275) is one create-copy-swap over three
// migrations: the narrow tables are created beside the wide one, every wide row
// is copied (and the count verified), and the wide table is dropped. These
// tests stand a database just below the first of the three, plant the rows a
// release before it could hold, and migrate forward over them.

const metricNarrowVersion = "20261029132025"

const metricBackfillFile = "migrations/20261029132028_metric_series_backfill.sql"

// wideMetricCols is the wide table's every column, in the order the snapshot
// reads them.
const wideMetricCols = `id, ts, owner_kind, component_id, system_id, location_id, node_id, metric_type_id,
	instance, value, provenance, source, source_rule, source_rule_version, source_rule_id, event_id, command_id, source_conduit_id`

// narrowAsWideSQL reads the narrow lane back in the wide table's shape: each
// sample and declared row beside its series' columns.
const narrowAsWideSQL = `select m.id, m.ts, s.owner_kind, s.component_id, s.system_id, s.location_id, s.node_id, s.metric_type_id,
		s.instance, m.value, s.provenance, s.source, m.source_rule, m.source_rule_version, m.source_rule_id,
		null::bigint as event_id, null::bigint as command_id, s.source_conduit_id
	from metric m join metric_series s on s.id = m.series_id
	union all
	select d.id, d.ts, s.owner_kind, s.component_id, s.system_id, s.location_id, s.node_id, s.metric_type_id,
		s.instance, d.value, s.provenance, s.source, null, null, null, d.event_id, d.command_id, s.source_conduit_id
	from metric_declared d join metric_series s on s.id = d.series_id`

// snapshotRows reads a query's rows as text, one line a row, in id order
// (the first column); withID false leaves the id out.
func snapshotRows(t *testing.T, conn *pgx.Conn, sql string, withID bool) []string {
	t.Helper()
	rows, err := conn.Query(context.Background(), `select * from (`+sql+`) x order by 1`)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		if !withID {
			vals = vals[1:]
		}
		cols := make([]string, len(vals))
		for i, v := range vals {
			cols[i] = fmt.Sprint(v)
		}
		out = append(out, strings.Join(cols, "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return out
}

// plantWideMetrics writes the rows a release before the narrow lane could
// hold: a component's observed series under two instances and two sources
// over three months, two samples stamped in one instant (ADR-0061), a
// calculated sample with its rule lineage, a system's and a node's samples,
// and a command's intended row.
func plantWideMetrics(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	f := newCommandFixture(t, conn)
	var sysID, nodeID string
	if err := conn.QueryRow(ctx, `insert into system (name) values ('sys1') returning id`).Scan(&sysID); err != nil {
		t.Fatalf("system: %v", err)
	}
	if err := conn.QueryRow(ctx, `insert into principal (kind) values ('node') returning id`).Scan(&nodeID); err != nil {
		t.Fatalf("principal: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into node (principal_id, name) values ($1, 'edge-x')`, nodeID); err != nil {
		t.Fatalf("node: %v", err)
	}
	var cmdID int64
	if err := conn.QueryRow(ctx, `insert into command (owner_kind, component_id, command_type_id, caused_event_id)
		values ('component', $1, $2, $3) returning id`, f.compID, f.ctID, f.evID).Scan(&cmdID); err != nil {
		t.Fatalf("command: %v", err)
	}
	stmts := []struct {
		sql  string
		args []any
	}{
		{`insert into metric (ts, owner_kind, component_id, metric_type_id, instance, value, source)
			select timestamptz '2026-08-20T00:00:00Z' + g * interval '7 hours', 'component', $1, $2,
				case when g % 3 = 0 then 'icmp' else '' end, g * 1.5, case when g % 5 = 0 then 'push' else 'snmp' end
			from generate_series(1, 300) g`, []any{f.compID, f.mtID}},
		{`insert into metric (ts, owner_kind, component_id, metric_type_id, instance, value, source)
			values ('2026-09-01T00:00:00Z', 'component', $1, $2, '', 7, 'snmp'),
			       ('2026-09-01T00:00:00Z', 'component', $1, $2, '', 8, 'snmp')`, []any{f.compID, f.mtID}},
		{`insert into metric (ts, owner_kind, component_id, metric_type_id, instance, value, provenance, source,
				source_rule, source_rule_version, source_rule_id)
			values ('2026-09-02T00:00:00Z', 'component', $1, $2, '', 42, 'calculated', '', 'avg-rule', 3, gen_random_uuid())`,
			[]any{f.compID, f.mtID}},
		{`insert into metric (ts, owner_kind, system_id, metric_type_id, value) values ('2026-10-03T00:00:00Z', 'system', $1, $2, 1)`,
			[]any{sysID, f.mtID}},
		// A fanned sample (#1282): its conduit by id beside its source text.
		{`insert into metric (ts, owner_kind, system_id, metric_type_id, value, source, source_conduit_id)
			values ('2026-10-03T01:00:00Z', 'system', $1, $2, 3, 'conduit-1/http', $3)`,
			[]any{sysID, f.mtID, f.compID}},
		{`insert into metric (ts, owner_kind, node_id, metric_type_id, value) values ('2026-10-04T00:00:00Z', 'node', $1, $2, 2)`,
			[]any{nodeID, f.mtID}},
		{`insert into metric (ts, owner_kind, component_id, metric_type_id, value, provenance, source, event_id, command_id)
			values ('2026-10-05T00:00:00Z', 'component', $1, $2, 99, 'intended', 'command', $3, $4)`,
			[]any{f.compID, f.mtID, f.evID, cmdID}},
	}
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("plant wide metric rows: %v", err)
		}
	}
}

// TestNarrowMetricKeepsEveryRow: every wide row arrives in the narrow lane
// unchanged (each sample and declared row read beside its series reproduces
// the wide row exactly, id included), observed and calculated rows land in
// the partitioned table and the intended row in the declared tier, each
// month a row was in has its partition, and a new sample's id carries on
// past the wide table's. Rolled back, the wide table holds the same rows
// again, and migrated forward a second time it arrives again.
func TestNarrowMetricKeepsEveryRow(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, metricNarrowVersion); err != nil {
		t.Fatalf("roll back below the narrow lane: %v", err)
	}
	ctx := context.Background()
	conn := connectDSN(t, dsn)
	plantWideMetrics(t, conn)
	want := snapshotRows(t, conn, `select `+wideMetricCols+` from metric`, true)
	var maxWide int64
	if err := conn.QueryRow(ctx, `select max(id) from metric`).Scan(&maxWide); err != nil {
		t.Fatal(err)
	}

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward: %v", err)
	}
	if got := snapshotRows(t, conn, narrowAsWideSQL, true); !slices.Equal(got, want) {
		t.Fatalf("the narrow lane read as wide rows differs from the wide table:\n got  %d rows\n want %d rows\n first got  %v\n first want %v",
			len(got), len(want), first(got), first(want))
	}
	var kind string
	var samples, declared, series int
	if err := conn.QueryRow(ctx, `select relkind::text, (select count(*) from metric), (select count(*) from metric_declared),
			(select count(*) from metric_series) from pg_class where oid = 'metric'::regclass`).Scan(&kind, &samples, &declared, &series); err != nil {
		t.Fatal(err)
	}
	if kind != "p" || declared != 1 || samples != len(want)-1 {
		t.Errorf("metric relkind %q with %d samples and %d declared rows, want a partitioned table with %d and 1", kind, samples, declared, len(want)-1)
	}
	// One series per (owner, type, instance, provenance, source, conduit): the
	// component's 2 instances x 2 sources observed, its calculated and intended
	// series, the system's own and fanned ones, and the node's.
	if series != 9 {
		t.Errorf("%d series, want 9", series)
	}
	parts := partitionsOf(t, conn, "metric")
	for _, m := range []string{"metric_p202608", "metric_p202609", "metric_p202610", "metric_p202611"} {
		if !slices.Contains(parts, m) {
			t.Errorf("partitions %v, want %s among them", parts, m)
		}
	}
	var sid, next int64
	if err := conn.QueryRow(ctx, `select series_id from metric limit 1`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `insert into metric (series_id, value) values ($1, 1) returning id`, sid).Scan(&next); err != nil {
		t.Fatalf("a new sample: %v", err)
	}
	if next <= maxWide {
		t.Errorf("a new sample's id %d, want past the wide table's %d", next, maxWide)
	}
	if _, err := conn.Exec(ctx, `delete from metric where id = $1`, next); err != nil {
		t.Fatal(err)
	}

	if err := migrate.RollbackBelow(dsn, metricNarrowVersion); err != nil {
		t.Fatalf("roll back the narrow lane: %v", err)
	}
	// Rolled back, the wide table holds every row again; the intended row
	// (the newest, so the last) takes a fresh id on the way back.
	back := snapshotRows(t, conn, `select `+wideMetricCols+` from metric`, true)
	if len(back) != len(want) || !slices.Equal(back[:len(back)-1], want[:len(want)-1]) || withoutID(back[len(back)-1]) != withoutID(want[len(want)-1]) {
		t.Errorf("rolled back, the wide table holds %d rows, want the %d it held:\n last got  %s\n last want %s",
			len(back), len(want), back[len(back)-1], want[len(want)-1])
	}
	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward again: %v", err)
	}
	if got := snapshotRows(t, conn, narrowAsWideSQL, true); len(got) != len(want) {
		t.Errorf("forward again: %d rows, want %d", len(got), len(want))
	}
}

// TestNarrowMetricBackfillRefusesALostRow: the copy is refused, whole, when
// the narrow lane does not hold every wide row: the count check is what makes
// the swap's drop of the wide table safe.
func TestNarrowMetricBackfillRefusesALostRow(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, metricNarrowVersion); err != nil {
		t.Fatalf("roll back below the narrow lane: %v", err)
	}
	ctx := context.Background()
	conn := connectDSN(t, dsn)
	plantWideMetrics(t, conn)
	raw, err := db.FS.ReadFile(metricBackfillFile)
	if err != nil {
		t.Fatal(err)
	}
	up, _, _ := strings.Cut(string(raw), "-- migrate:down")
	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward: %v", err)
	}
	// The swap's last step undone: the wide table back, empty, beside a full
	// narrow lane. One row in it the narrow lane does not hold, and the copy
	// (which copies nothing into a lane that is not empty) must refuse.
	if err := migrate.RollbackOne(dsn); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `insert into metric_wide (owner_kind, component_id, metric_type_id, value)
		select owner_kind, component_id, metric_type_id, 1 from metric_series where owner_kind = 'component' limit 1`); err != nil {
		t.Fatalf("a wide row: %v", err)
	}
	if _, err := conn.Exec(ctx, strings.TrimPrefix(up, "-- migrate:up")); err == nil || !strings.Contains(err.Error(), "metric backfill") {
		t.Fatalf("the copy over a lane that does not match: err = %v, want the count refusal", err)
	}
}

func partitionsOf(t *testing.T, conn *pgx.Conn, parent string) []string {
	t.Helper()
	rows, err := conn.Query(context.Background(), `select c.relname::text from pg_inherits i join pg_class c on c.oid = i.inhrelid
		where i.inhparent = $1::regclass order by 1`, parent)
	if err != nil {
		t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func first(s []string) string {
	if len(s) == 0 {
		return "(none)"
	}
	return s[0]
}

// withoutID is a snapshot row less its first column.
func withoutID(row string) string {
	_, rest, _ := strings.Cut(row, "|")
	return rest
}
