//go:build linux

// The simulator serves on 127.x addresses beyond 127.0.0.1, which Linux
// treats as local without configuration.

package node_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/labfootprint"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
)

// TestADispatcherSkippingDuesReadsScheduleLossAndNoIngestLoss drives #1693
// over the real wire: the shipped http-json driver re-authored to poll every
// second, against a device that accepts and never answers, so each poll
// waits out its timeout and the dues behind it pass over. The lab's own
// read of the window, the same queries the scale run makes, shows the loss
// at the schedule hop, runs due against runs made, while every item the node
// did mint landed: the ingest hop reads 0.
func TestADispatcherSkippingDuesReadsScheduleLossAndNoIngestLoss(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server + the simulator")
	}
	const nodeName = "edge-schedule-hop"
	s := newHTTPStack(t, nodeName, nil, func(store *profile.Store) []string {
		return []string{driverProfile(t, store, "http-json")}
	})
	s.everyDriver("http-json", "sensor-1s", "1s")
	s.secret("census-key", "api-key", map[string]string{"key": "census-lab-key"})
	s.component("sensor-dead")
	dead, accepted := silentListener(t)
	host, port, err := net.SplitHostPort(dead)
	if err != nil {
		t.Fatal(err)
	}
	s.attachHTTP("sensor-dead", "sensor-1s", nodeName, map[string]string{"host": host, "port": port, "key": "census-key"})

	from := time.Now()
	s.runNodeReporting(nodeName, time.Second)
	time.Sleep(7 * time.Second)
	w := delivery.Window{From: from, To: time.Now()}

	conn, err := pgx.Connect(s.ctx, s.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(s.ctx)
	var rows []labfootprint.DeliveryRow
	var counts []labfootprint.ScheduleCount
	var unsettled []labfootprint.UnsettledBoot
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		receipts, err := labfootprint.ParseReceipts(queryLines(t, conn, labfootprint.ReceiptsQuery(w.From, w.To)))
		if err != nil {
			t.Fatal(err)
		}
		counts, err = labfootprint.ParseSchedule(queryLines(t, conn, labfootprint.ScheduleQuery(w.From, w.To)))
		if err != nil {
			t.Fatal(err)
		}
		var ingest []labfootprint.DeliveryRow
		ingest, unsettled, _ = labfootprint.DeliveryOf(labfootprint.DeliveryRead{Read: true, Window: w, Receipts: receipts, StreamClear: true})
		rows = labfootprint.WithSchedule(labfootprint.ScheduleRowsOf(counts), ingest)
		if len(unsettled) == 0 && len(ingest) > 0 && len(counts) > 0 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if len(unsettled) > 0 {
		t.Fatalf("boots %v never settled; rows %+v", unsettled, rows)
	}
	if accepted() == 0 {
		t.Fatal("the dead device was never asked; no poll ran to hold the dues behind it")
	}
	for _, r := range rows {
		t.Logf("%s %s in %d out %d loss %v outcomes %v", r.Kind, r.Hop, r.In, r.Out, deref(r.Loss), r.Outcomes)
	}
	var schedule, ingest int
	for _, r := range rows {
		switch r.Hop {
		case delivery.Schedule:
			if r.Loss != nil && *r.Loss > 0 {
				schedule++
			}
		case delivery.Ingest:
			ingest++
			if r.Loss == nil || *r.Loss != 0 {
				t.Errorf("the %s ingest hop lost %v (outcomes %v), want 0: every item the node minted landed", r.Kind, r.Loss, r.Outcomes)
			}
		}
	}
	if schedule == 0 {
		t.Errorf("no kind read a schedule loss above 0 with every poll outrunning its 1s interval: counts %+v, rows %+v", counts, rows)
	}
	if ingest == 0 {
		t.Errorf("no ingest row read in the window: rows %+v", rows)
	}
}

// queryLines runs a query that writes one text column and joins its rows by
// line, as psql -At prints them for the lab's parsers.
func queryLines(t *testing.T, conn *pgx.Conn, q string) string {
	t.Helper()
	got, err := conn.Query(context.Background(), "select q::text from ("+q+") as r(q)")
	if err != nil {
		t.Fatal(err)
	}
	lines, err := pgx.CollectRows(got, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return strings.Join(lines, "\n")
}

func deref(l *float64) any {
	if l == nil {
		return "-"
	}
	return *l
}
