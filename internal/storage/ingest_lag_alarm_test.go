package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	"github.com/jackc/pgx/v5"
)

// TestIngestLagHighOpensAndClears proves the seeded ingest-lag-high rule
// (#1628) on the server's own platform component: a window's oldest landing
// over 30 s opens nothing at once, held past the rule's 2 minute sustain it
// opens the alarm, and a window back under the threshold clears it.
func TestIngestLagHighOpensAndClears(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	id, err := gw.EnsureServerComponent(ctx, "lag-test")
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err := conn.QueryRow(ctx, `select name from component where id = $1`, id).Scan(&name); err != nil {
		t.Fatal(err)
	}

	lag := func(v float64) {
		t.Helper()
		if err := gw.RecordPlatformMetrics(ctx, []storage.PlatformMetric{{ComponentID: id, Key: writerclass.IngestLagMax, Value: v, TS: time.Now().UTC()}}); err != nil {
			t.Fatalf("record ingest-lag-max %v: %v", v, err)
		}
	}
	open := func() int {
		t.Helper()
		alarms, err := gw.ListAlarms(ctx, name, storage.AlarmFilter{})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, a := range alarms {
			if a.RaisedByRule != nil && *a.RaisedByRule == "ingest-lag-high" {
				n++
			}
		}
		return n
	}
	sweep := func(after time.Duration) {
		t.Helper()
		if err := gw.SweepEventRules(ctx, time.Now().Add(after)); err != nil {
			t.Fatal(err)
		}
	}

	lag(60)
	sweep(time.Minute)
	if n := open(); n != 0 {
		t.Fatalf("one minute into a 2 minute sustain the rule opened %d alarms", n)
	}
	sweep(3 * time.Minute)
	if n := open(); n != 1 {
		t.Fatalf("lag held past the sustain left %d ingest-lag-high alarms open on %s, want 1", n, name)
	}
	lag(1)
	sweep(time.Minute)
	if n := open(); n != 0 {
		t.Fatalf("lag back under the threshold left %d alarms open, want it cleared", n)
	}
}
