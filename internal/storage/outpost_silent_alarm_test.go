package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// TestOutpostSilentOpensAndClears proves the seeded outpost-silent rule (#1707) on
// a claimed outpost's platform component, the component the server writes its
// checkin-age against: an age of 90 s (three 30 s heartbeats, the threshold
// itself) opens nothing, 120 s opens the alarm with no sustain to wait out,
// and the next reading after the outpost speaks again clears it.
func TestOutpostSilentOpensAndClears(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	gw, ctx, dsn := platformGatewayDSN(t)
	platformOutpost(t, ctx, gw, "edge-silent")
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	if _, err := conn.Exec(ctx, `update outpost set enrolled_at = now() where name = 'edge-silent'`); err != nil {
		t.Fatalf("claim edge-silent: %v", err)
	}
	checkins, err := gw.OutpostCheckins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(checkins) != 1 || checkins[0].Outpost != "edge-silent" {
		t.Fatalf("outpost checkins = %+v, want the one claimed outpost", checkins)
	}
	comp := checkins[0].ComponentID

	age := func(v float64) {
		t.Helper()
		if err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{{ComponentID: comp, Key: writerclass.CheckinAge, Value: v, TS: time.Now().UTC()}}); err != nil {
			t.Fatalf("record checkin-age %v: %v", v, err)
		}
		if err := gw.SweepEventRules(ctx, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	age(90)
	if n := openAlarmsByRule(t, ctx, gw, "edge-silent", "outpost-silent"); n != 0 {
		t.Fatalf("a check-in age of exactly 90 s opened %d outpost-silent alarms; the threshold is past three heartbeats", n)
	}
	age(120)
	if n := openAlarmsByRule(t, ctx, gw, "edge-silent", "outpost-silent"); n != 1 {
		t.Fatalf("a check-in age of 120 s left %d outpost-silent alarms open on edge-silent's platform component, want 1", n)
	}
	alarms, err := gw.ListAlarms(ctx, "edge-silent", storage.AlarmFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range alarms {
		if a.RaisedByRule != nil && *a.RaisedByRule == "outpost-silent" && a.Severity != "high" {
			t.Errorf("outpost-silent raised at %q, want high", a.Severity)
		}
	}
	age(10)
	if n := openAlarmsByRule(t, ctx, gw, "edge-silent", "outpost-silent"); n != 0 {
		t.Fatalf("an outpost heard from again left %d outpost-silent alarms open, want it cleared", n)
	}
}

// openAlarmsByRule counts the open alarms rule raised on the named component.
func openAlarmsByRule(t *testing.T, ctx context.Context, gw *storage.PG, component, rule string) int {
	t.Helper()
	alarms, err := gw.ListAlarms(ctx, component, storage.AlarmFilter{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range alarms {
		if a.RaisedByRule != nil && *a.RaisedByRule == rule {
			n++
		}
	}
	return n
}
