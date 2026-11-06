package storage_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A metric sample's ts is whole milliseconds everywhere (#1526), so a raw row
// and the compacted block that later holds it (int4 millisecond offsets,
// #1265) agree exactly. The ingest truncates rather than refuses, so a spool
// replayed from an older node lands; the other lanes keep their timestamps.

// TestIngestTruncatesAMetricSampleToTheMillisecond: a sample sent with a
// microsecond ts is stored on its millisecond, its batch lands whole, and the
// property, event and log lanes of the same batch keep the ts they were sent.
func TestIngestTruncatesAMetricSampleToTheMillisecond(t *testing.T) {
	f := newLandFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Minute).Truncate(time.Second).Add(123456 * time.Microsecond)

	landed, err := f.gw.LandBatch(ctx, fullBatch("og.v1.api.telemetry ms-1", "on", ts))
	if err != nil || !landed {
		t.Fatalf("a batch carrying a microsecond ts: landed %v, err %v; want it landed", landed, err)
	}
	var metric, property, event, logLine time.Time
	if err := f.conn.QueryRow(ctx, `select
		(select ts from `+storagetest.MetricRows+` metric),
		(select ts from property where provenance = 'observed'),
		(select ts from event where origin = 'caught'),
		(select ts from log_line)`).Scan(&metric, &property, &event, &logLine); err != nil {
		t.Fatal(err)
	}
	if want := ts.Truncate(time.Millisecond); !metric.Equal(want) {
		t.Errorf("metric ts %s, want %s: the millisecond it was sent in", metric.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}
	for lane, got := range map[string]time.Time{"property": property, "event": event, "log": logLine} {
		if !got.Equal(ts) {
			t.Errorf("%s ts %s, want %s unchanged", lane, got.Format(time.RFC3339Nano), ts.Format(time.RFC3339Nano))
		}
	}
}

// TestSamplesOfOneMillisecondBothLandInOrder: two samples of one series that
// truncate to one millisecond are both stored (no unique key on (series, ts),
// ADR-0061), the later-sent one with the higher id.
func TestSamplesOfOneMillisecondBothLandInOrder(t *testing.T) {
	f := newLandFixture(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	sample := func(v float64, us int) storage.MetricSampleWrite {
		return storage.MetricSampleWrite{OwnerKind: "component", OwnerID: "disp-1", Key: "room-temperature",
			Value: v, Source: "push", TS: base.Add(time.Duration(us) * time.Microsecond)}
	}
	landed, err := f.gw.LandBatch(ctx, storage.IngestBatch{BatchID: "ms-pair", Writer: storage.PushWriter(),
		Metrics: []storage.MetricSampleWrite{sample(1, 1200), sample(2, 1900)}})
	if err != nil || !landed {
		t.Fatalf("landed %v, err %v; want the batch landed", landed, err)
	}
	rows, err := f.conn.Query(ctx, `select ts, value from `+storagetest.MetricRows+` metric order by id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var values []float64
	for rows.Next() {
		var ts time.Time
		var v float64
		if err := rows.Scan(&ts, &v); err != nil {
			t.Fatal(err)
		}
		if want := base.Add(time.Millisecond); !ts.Equal(want) {
			t.Errorf("value %v stored at %s, want %s", v, ts.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
		}
		values = append(values, v)
	}
	if !slices.Equal(values, []float64{1, 2}) {
		t.Errorf("stored values in landing order %v, want [1 2]", values)
	}
}

// TestMetricTimestampMigrationTruncatesExistingRows: after the migration no
// metric or metric_declared row has a sub-millisecond ts, ids and values are
// untouched, and a row written without a ts takes the database's now on a
// millisecond.
func TestMetricTimestampMigrationTruncatesExistingRows(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, metricMillisecondVersion); err != nil {
		t.Fatalf("roll back below the millisecond truncation: %v", err)
	}
	ctx := context.Background()
	conn := connectDSN(t, dsn)
	f := newCommandFixture(t, conn)
	var cmdID, seriesID, declaredID int64
	if err := conn.QueryRow(ctx, `insert into command (owner_kind, component_id, command_type_id, caused_event_id)
		values ('component', $1, $2, $3) returning id`, f.compID, f.ctID, f.evID).Scan(&cmdID); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `insert into metric_series (metric_type_id, owner_kind, component_id, provenance, source)
		values ($1, 'component', $2, 'observed', 'snmp') returning id`, f.mtID, f.compID).Scan(&seriesID); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `insert into metric_series (metric_type_id, owner_kind, component_id, provenance, source)
		values ($1, 'component', $2, 'intended', 'command') returning id`, f.mtID, f.compID).Scan(&declaredID); err != nil {
		t.Fatal(err)
	}
	// Two microsecond instants one millisecond apart, a pair that will share a
	// millisecond, the next month's first instant plus microseconds (a
	// truncation never leaves its partition), and a row already on its
	// millisecond. The partitions are the current month's and the next two.
	day := monthStart(time.Now()).Add(48 * time.Hour)
	next := monthStart(time.Now()).AddDate(0, 1, 0)
	mustExec(t, conn, `insert into metric (series_id, ts, value) values
		($1, $2::timestamptz + interval '123456 microseconds', 1), ($1, $2::timestamptz + interval '123999 microseconds', 2),
		($1, $2::timestamptz + interval '124001 microseconds', 3), ($1, $3::timestamptz + interval '500 microseconds', 4),
		($1, $2::timestamptz + interval '1250 milliseconds', 5)`, seriesID, day, next)
	mustExec(t, conn, `insert into metric_declared (series_id, ts, value, command_id) values
		($1, $2::timestamptz + interval '654321 microseconds', 9, $3)`, declaredID, day, cmdID)

	idsBefore := snapshotRows(t, conn, `select id, value from metric`, true)

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward: %v", err)
	}
	for _, table := range []string{"metric", "metric_declared"} {
		if n := count(t, conn, "select count(*) from "+table); n == 0 {
			t.Fatalf("%s lost its rows", table)
		}
		if n := count(t, conn, "select count(*) from "+table+" where date_trunc('milliseconds', ts) <> ts"); n != 0 {
			t.Errorf("%s has %d rows with a sub-millisecond ts after the migration", table, n)
		}
	}
	got := snapshotRows(t, conn, `select value, to_char(ts at time zone 'utc', 'YYYY-MM-DD"T"HH24:MI:SS.MS') from metric`, true)
	at := func(t time.Time, us int) string {
		return t.UTC().Add(time.Duration(us) * time.Microsecond).Format("2006-01-02T15:04:05.000")
	}
	want := []string{
		"1|" + at(day, 123000), "2|" + at(day, 123000), "3|" + at(day, 124000),
		"4|" + at(next, 0), "5|" + at(day, 1250000),
	}
	if !slices.Equal(got, want) {
		t.Errorf("metric rows after the migration:\n got  %v\n want %v", got, want)
	}
	if idsAfter := snapshotRows(t, conn, `select id, value from metric`, true); !slices.Equal(idsAfter, idsBefore) {
		t.Errorf("the migration changed ids or values: %v, was %v", idsAfter, idsBefore)
	}
	// A row written with no ts takes the database's now, on its millisecond.
	mustExec(t, conn, `insert into metric (series_id, value) values ($1, 6)`, seriesID)
	mustExec(t, conn, `insert into metric_declared (series_id, value, command_id) values ($1, 7, $2)`, declaredID, cmdID)
	for _, table := range []string{"metric", "metric_declared"} {
		if n := count(t, conn, "select count(*) from "+table+" where date_trunc('milliseconds', ts) <> ts"); n != 0 {
			t.Errorf("%s: a row written with no ts has a sub-millisecond default", table)
		}
	}
}

// metricMillisecondVersion is the migration that truncates metric timestamps.
const metricMillisecondVersion = "20261106050800"

// millisecondScenario runs one recorded scenario on a fresh database: a
// room-temperature series read by a calc rule (its mean) and by an event rule
// that fires once the temperature has held above 90 for 2 seconds, then
// clears. The samples are the same instants either way; with noisy each
// carries the microsecond digits a node's clock would have given it. It
// returns what the rules did, as the rows they wrote.
func millisecondScenario(t *testing.T, noisy bool) []string {
	r := newEvalRig(t)
	if _, err := r.create(storage.CalcRuleSpec{Name: "temp-mean", OwnerKind: rules.OwnerComponent,
		Inputs: []rules.CalcInput{{Alias: "t", Key: "room-temperature"}}, Reduce: rules.CalcReduce{Expr: "avg(t)"}, OutputKey: "avg-temp"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.gw.CreateEventRule(r.ctx, "", storage.EventRuleSpec{Name: "room-hot", SampleKey: "room-temperature",
		Fire: criteria("gt", 90), Clear: &rules.Criteria{Op: "le", Value: []byte("90")},
		Severity: "high", FireEventType: "condition-started", ClearEventType: "condition-ended"}, r.all); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	for i, v := range []float64{80, 95, 96, 97, 98, 70, 71} {
		ts := base.Add(time.Duration(i) * time.Second)
		if noisy {
			ts = ts.Add(time.Duration(137+211*i) * time.Microsecond)
		}
		if err := r.gw.InsertMetricSamples(r.ctx, storage.PushWriter(), []storage.MetricSampleWrite{{OwnerKind: "component",
			OwnerID: r.comp, Key: "room-temperature", Value: v, Source: "test", TS: ts}}); err != nil {
			t.Fatal(err)
		}
		r.drain()
	}
	out := r.calculated("avg-temp")
	rows, err := r.conn.Query(r.ctx, `select et.name from event e join event_type et on et.id = e.event_type_id where e.origin <> 'caught' order by e.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// TestRulesJudgeAMillisecondScenarioAsBefore: an event rule
// and a calc rule read one recorded series twice, once stamped on whole
// milliseconds and once with the microseconds a node's clock would have left
// on it. They wrote the same rows, and the scenario fired at all (so the
// comparison is not of two empty runs).
func TestRulesJudgeAMillisecondScenarioAsBefore(t *testing.T) {
	aligned := millisecondScenario(t, false)
	noisy := millisecondScenario(t, true)
	if !slices.Equal(aligned, noisy) {
		t.Errorf("rules over microsecond stamps wrote\n %v\nwant what they wrote over millisecond stamps\n %v", noisy, aligned)
	}
	var fired bool
	for _, s := range aligned {
		fired = fired || strings.HasPrefix(s, "condition-started")
	}
	if !fired {
		t.Errorf("the scenario's event rule never fired: %v", aligned)
	}
}
