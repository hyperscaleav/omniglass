package storage_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// TestDiskNearlyFullOpensAndClears proves the seeded disk-nearly-full rule
// (#1707) on the server's own platform component: a data directory 95% used
// opens nothing at once, held past the rule's 2 minute sustain it opens a
// warning, a reading between the clear and the fire threshold holds it open,
// and one back at half used clears it.
func TestDiskNearlyFullOpensAndClears(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw, ctx, dsn := platformGatewayDSN(t)
	id, err := gw.EnsureServerComponent(ctx, "disk-test")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	var name string
	if err := conn.QueryRow(ctx, `select name from component where id = $1`, id).Scan(&name); err != nil {
		t.Fatal(err)
	}

	used := func(v float64) {
		t.Helper()
		if err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{{ComponentID: id, Key: writerclass.DataDirDiskUsedRatio, Value: v, TS: time.Now().UTC()}}); err != nil {
			t.Fatalf("record data-dir-disk-used-ratio %v: %v", v, err)
		}
	}
	sweep := func(after time.Duration) {
		t.Helper()
		if err := gw.SweepEventRules(ctx, time.Now().Add(after)); err != nil {
			t.Fatal(err)
		}
	}
	open := func() int { t.Helper(); return openAlarmsByRule(t, ctx, gw, name, "disk-nearly-full") }

	used(0.95)
	sweep(time.Minute)
	if n := open(); n != 0 {
		t.Fatalf("one minute into a 2 minute sustain the rule opened %d alarms", n)
	}
	sweep(3 * time.Minute)
	if n := open(); n != 1 {
		t.Fatalf("a data directory 95%% used past the sustain left %d disk-nearly-full alarms open on %s, want 1", n, name)
	}
	alarms, err := gw.ListAlarms(ctx, name, storage.AlarmFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range alarms {
		if a.RaisedByRule != nil && *a.RaisedByRule == "disk-nearly-full" && a.Severity != "warning" {
			t.Errorf("disk-nearly-full raised at %q, want warning", a.Severity)
		}
	}
	used(0.88)
	sweep(time.Minute)
	if n := open(); n != 1 {
		t.Fatalf("a reading above the 0.85 clear left %d alarms open, want it still open", n)
	}
	used(0.5)
	sweep(time.Minute)
	if n := open(); n != 0 {
		t.Fatalf("a data directory half used left %d alarms open, want it cleared", n)
	}
}
