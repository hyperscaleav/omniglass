package storage_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// TestRecordPlatformMetricsLandsOnThePlatformComponent: the process's write
// of a platform key (#1625) lands on the component it names, and a component
// gone since (a node deleted between the read and the write) costs only its
// own rows.
func TestRecordPlatformMetricsLandsOnThePlatformComponent(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	kept := platformNode(t, ctx, gw, "edge-kept")
	at := time.Now().UTC().Truncate(time.Millisecond)
	err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{
		{ComponentID: "00000000-0000-0000-0000-000000000001", Key: writerclass.CheckinAge, Value: 9, TS: at},
		{ComponentID: kept.ID, Key: writerclass.CheckinAge, Value: 42, TS: at},
	})
	if err != nil {
		t.Fatalf("record platform metrics: %v", err)
	}
	got, err := gw.LatestMetric(ctx, kept.ID, writerclass.CheckinAge)
	if err != nil || got == nil || got.Value != 42 {
		t.Errorf("edge-kept checkin-age = %+v (%v), want 42", got, err)
	}
}

// A key the process does not write (a collected key) is refused: the
// platform write is the platform writer's, held to its class like any.
func TestRecordPlatformMetricsRefusesAKeyNotThePlatforms(t *testing.T) {
	t.Parallel()
	gw, ctx := platformGateway(t)
	c := platformNode(t, ctx, gw, "edge-x")
	err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{{ComponentID: c.ID, Key: "tcp-connect-time", Value: 1, TS: time.Now()}})
	if !errors.Is(err, storage.ErrKeyWriterRefused) {
		t.Fatalf("record a collected key = %v, want ErrKeyWriterRefused", err)
	}
}

// TestNodeCheckinsReadsEveryClaimedNode: the check-in age's input (#1625) is
// every claimed node with its platform component and the time it last spoke:
// its last heartbeat, else its claim. A node never claimed has not started
// its silence and is absent.
func TestNodeCheckinsReadsEveryClaimedNode(t *testing.T) {
	t.Parallel()
	gw, ctx, dsn := platformGatewayDSN(t)
	live := platformNode(t, ctx, gw, "edge-live")
	quiet := platformNode(t, ctx, gw, "edge-quiet")
	platformNode(t, ctx, gw, "edge-unclaimed")
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	heard := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	claimed := heard.Add(-time.Hour)
	if _, err := conn.Exec(ctx, `update node set enrolled_at = $1,
		last_heartbeat_at = case when name = 'edge-live' then $2::timestamptz end
		where name in ('edge-live', 'edge-quiet')`, claimed, heard); err != nil {
		t.Fatal(err)
	}

	got, err := gw.NodeCheckins(ctx)
	if err != nil {
		t.Fatalf("node checkins: %v", err)
	}
	want := map[string]storage.NodeCheckin{
		"edge-live":  {Node: "edge-live", ComponentID: live.ID, Since: heard},
		"edge-quiet": {Node: "edge-quiet", ComponentID: quiet.ID, Since: claimed},
	}
	if len(got) != len(want) {
		t.Fatalf("checkins = %+v, want the two claimed nodes", got)
	}
	for _, c := range got {
		w, ok := want[c.Node]
		if !ok || c.ComponentID != w.ComponentID || !c.Since.Equal(w.Since) {
			t.Errorf("checkin %+v, want %+v", c, w)
		}
	}
}

// TestRecordPlatformMetricsMakesAMissingDay: a platform write dated into a
// day with no partition (a capture lab's hour of history seeded at 00:20 UTC,
// before the server's boot rolls yesterday in, #1790) makes the day and lands,
// as the ingest's write does, rather than failing the whole component.
func TestRecordPlatformMetricsMakesAMissingDay(t *testing.T) {
	t.Parallel()
	gw, ctx, dsn := platformGatewayDSN(t)
	yesterday := dayStart(time.Now()).AddDate(0, 0, -1)
	mustExec(t, connectDSN(t, dsn), `drop table if exists `+dayName(yesterday))
	server, err := gw.EnsureServerComponent(ctx, "server")
	if err != nil {
		t.Fatal(err)
	}
	at := yesterday.Add(23*time.Hour + 40*time.Minute)
	if err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{
		{ComponentID: server, Key: writerclass.DBPoolMax, Value: 12, TS: at},
		{ComponentID: server, Key: writerclass.DBPoolMax, Value: 12, TS: at.Add(30 * time.Minute)},
	}); err != nil {
		t.Fatalf("record platform metrics into a missing day: %v", err)
	}
	series, err := gw.ListMetricSeries(ctx, "component", server, writerclass.DBPoolMax, 0, 10)
	if err != nil || len(series) != 2 {
		t.Fatalf("%d samples stored (%v), want both", len(series), err)
	}
}
