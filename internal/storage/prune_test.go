package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestPruneSamples pins the retention rule before any retention feature exists
// (#591): a prune deletes samples older than the cutoff from BOTH series tables
// EXCEPT declared rows (for declared provenance the row is the whole truth, not
// a sample) and EXCEPT the latest row of every series (type, owner arc,
// instance, provenance), so a prune can never erase a current value. A re-run
// deletes nothing further.
func TestPruneSamples(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	conn := connectDSN(t, dsn)

	// A minimal estate: one component, one type per lane. Rows are inserted with
	// explicit timestamps so age is exact, not racy.
	mustExec(t, conn, `insert into component (name) values ('prune-c1')`)
	mustExec(t, conn, `insert into property_type (name, data_type) values ('prune-power', 'string')`)
	mustExec(t, conn, `insert into metric_type (name, data_type) values ('prune-rtt', 'float')`)

	now := time.Now().UTC()
	twoYears := now.Add(-2 * 365 * 24 * time.Hour)
	tenDays := now.Add(-10 * 24 * time.Hour)
	fiveDays := now.Add(-5 * 24 * time.Hour)
	cutoff := now.Add(-24 * time.Hour)

	propertyRow := func(ts time.Time, provenance, value string) {
		t.Helper()
		mustExec(t, conn, `
			insert into property (ts, owner_kind, component_id, property_type_id, instance, provenance, value)
			values ($1, 'component', (select id from component where name = 'prune-c1'),
			        (select id from property_type where name = 'prune-power'), '', $2, to_jsonb($3::text))`, ts, provenance, value)
	}
	metricRow := func(ts time.Time, value float64) {
		t.Helper()
		mustExec(t, conn, `
			insert into metric (ts, owner_kind, component_id, metric_type_id, instance, provenance, value)
			values ($1, 'component', (select id from component where name = 'prune-c1'),
			        (select id from metric_type where name = 'prune-rtt'), '', 'observed', $2)`, ts, value)
	}

	// Property lane: a two-year-old declared value (must survive: it is the
	// truth, not a sample), a superseded declared edit even older (history, also
	// never pruned), and an observed series whose every row is older than the
	// cutoff, so only its latest row may survive.
	propertyRow(twoYears.Add(-24*time.Hour), "declared", "edit-1")
	propertyRow(twoYears, "declared", "edit-2")
	propertyRow(tenDays, "observed", "on")
	propertyRow(fiveDays, "observed", "off")

	// Metric lane: the same shape, superseded old rows and an old latest.
	metricRow(tenDays, 12)
	metricRow(fiveDays, 34)

	count := func(table string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, `select count(*) from `+table+`
			where component_id = (select id from component where name = 'prune-c1')`).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}

	deleted, err := gw.PruneSamples(ctx, cutoff)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	// Exactly the two superseded observed rows die: property's ten-day "on" and
	// metric's ten-day 12.
	if deleted != 2 {
		t.Fatalf("pruned %d rows, want 2 (the superseded observed row of each lane)", deleted)
	}
	if n := count("property"); n != 3 {
		t.Fatalf("property rows after prune = %d, want 3 (two declared edits + the latest observed)", n)
	}
	if n := count("metric"); n != 1 {
		t.Fatalf("metric rows after prune = %d, want 1 (the latest observed)", n)
	}

	// The survivors are the right ones: both declared edits however old, and the
	// latest observed row of each series however old.
	var v string
	if err := conn.QueryRow(ctx, `select value #>> '{}' from property
		where component_id = (select id from component where name = 'prune-c1')
		  and provenance = 'observed'`).Scan(&v); err != nil {
		t.Fatalf("read surviving observed property: %v", err)
	}
	if v != "off" {
		t.Fatalf("surviving observed property = %q, want the latest row off", v)
	}
	var mv float64
	if err := conn.QueryRow(ctx, `select value from metric
		where component_id = (select id from component where name = 'prune-c1')`).Scan(&mv); err != nil {
		t.Fatalf("read surviving metric: %v", err)
	}
	if mv != 34 {
		t.Fatalf("surviving metric = %v, want the latest row 34", mv)
	}

	// Idempotent: a re-run finds nothing further to delete.
	deleted, err = gw.PruneSamples(ctx, cutoff)
	if err != nil {
		t.Fatalf("re-prune: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("re-prune deleted %d rows, want 0", deleted)
	}
}
