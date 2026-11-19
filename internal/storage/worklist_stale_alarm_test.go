package storage_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// TestWorklistStaleOpensAndClears proves the seeded worklist-stale rule
// (#1868) on a claimed outpost's platform component, where the outpost writes its
// worklist-pull-loss-ratio each report window: a window with some pulls
// served opens nothing, every pull refused past the 2 minute sustain opens a
// warning, and the first window with a served pull clears it.
func TestWorklistStaleOpensAndClears(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw, ctx, dsn := platformGatewayDSN(t)
	platformOutpost(t, ctx, gw, "edge-stale")
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	if _, err := conn.Exec(ctx, `update outpost set enrolled_at = now() where name = 'edge-stale'`); err != nil {
		t.Fatalf("claim edge-stale: %v", err)
	}
	checkins, err := gw.OutpostCheckins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(checkins) != 1 || checkins[0].Outpost != "edge-stale" {
		t.Fatalf("outpost checkins = %+v, want the one claimed outpost", checkins)
	}
	comp := checkins[0].ComponentID

	loss := func(v float64) {
		t.Helper()
		if err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{{ComponentID: comp, Key: writerclass.WorklistPullLossRatio, Value: v, TS: time.Now().UTC()}}); err != nil {
			t.Fatalf("record worklist-pull-loss-ratio %v: %v", v, err)
		}
	}
	sweep := func(after time.Duration) {
		t.Helper()
		if err := gw.SweepEventRules(ctx, time.Now().Add(after)); err != nil {
			t.Fatal(err)
		}
	}
	open := func() int { t.Helper(); return openAlarmsByRule(t, ctx, gw, "edge-stale", "worklist-stale") }

	loss(0.5)
	sweep(3 * time.Minute)
	if n := open(); n != 0 {
		t.Fatalf("a window with half its pulls served opened %d worklist-stale alarms; the outpost still took a worklist", n)
	}
	loss(1)
	sweep(time.Minute)
	if n := open(); n != 0 {
		t.Fatalf("one minute into a 2 minute sustain the rule opened %d alarms", n)
	}
	sweep(3 * time.Minute)
	if n := open(); n != 1 {
		t.Fatalf("every pull refused past the sustain left %d worklist-stale alarms open on edge-stale's platform component, want 1", n)
	}
	alarms, err := gw.ListAlarms(ctx, "edge-stale", storage.AlarmFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range alarms {
		if a.RaisedByRule != nil && *a.RaisedByRule == "worklist-stale" && a.Severity != "warning" {
			t.Errorf("worklist-stale raised at %q, want warning", a.Severity)
		}
	}
	loss(0.75)
	sweep(time.Minute)
	if n := open(); n != 0 {
		t.Fatalf("a window with a served pull left %d worklist-stale alarms open, want it cleared", n)
	}
}
