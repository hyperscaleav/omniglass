package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// A node's receipts are counted by landed_at only inside the read's span,
// and split into the margin before the window's start and the run after its
// end; another node's receipts never count, however its name begins.
func TestCountTelemetryReceipts(t *testing.T) {
	t.Parallel()
	f := newLandFixture(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	start, end, next := base, base.Add(10*time.Minute), base.Add(11*time.Minute)
	from := start.Add(-30 * time.Second)
	for id, at := range map[string]time.Time{
		"og.v1.telemetry.edge-1 before":  from.Add(-time.Second), // before the span
		"og.v1.telemetry.edge-1 margin":  start.Add(-10 * time.Second),
		"og.v1.telemetry.edge-1 in-1":    start,
		"og.v1.telemetry.edge-1 in-2":    end,
		"og.v1.telemetry.edge-1 after":   end.Add(time.Second),
		"og.v1.telemetry.edge-1 late":    next.Add(time.Second), // after the span
		"og.v1.telemetry.edge-10 in":     start.Add(time.Minute),
		"og.v1.telemetry.edge-1x in":     start.Add(time.Minute),
		"og.v1.telemetry.edge_1 in-like": start.Add(time.Minute),
	} {
		if _, err := f.gw.LandBatch(ctx, storage.IngestBatch{BatchID: id}); err != nil {
			t.Fatalf("land %s: %v", id, err)
		}
		if _, err := f.conn.Exec(ctx, `update telemetry_receipt set landed_at = $2 where batch_id = $1`, id, at); err != nil {
			t.Fatalf("stamp %s: %v", id, err)
		}
	}
	got, err := f.gw.CountTelemetryReceipts(ctx, "og.v1.telemetry.edge-1 ", from, start, end, next)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	want := storage.ReceiptWindow{Landed: 4, Margin: 1, AfterEnd: 1}
	if got != want {
		t.Fatalf("count = %+v, want %+v", got, want)
	}
}

// The poll targets are every enabled poll task's endpoint with a component,
// by the node that holds it, with the schedule a driver declares, and empty
// for a reachability probe.
func TestListClosedLoopPollTargets(t *testing.T) {
	t.Parallel()
	f := newLandFixture(t)
	ctx := context.Background()
	for _, q := range []string{
		`insert into endpoint (name, transport, component, node_name, params) values ('disp-1-http', 'http', (select id from component where name = 'disp-1'), (select principal_id from node where name = 'edge-1'), '{}'::jsonb)`,
		`insert into endpoint (name, transport, component, node_name, params) values ('disp-1-icmp', 'icmp', (select id from component where name = 'disp-1'), (select principal_id from node where name = 'edge-1'), '{}'::jsonb)`,
		`insert into task (id, mode, endpoint_id, spec, enabled) values ('t-drv', 'poll', (select id from endpoint where name = 'disp-1-http'), '{"driver":"d","schedule":{"every":"15s"}}'::jsonb, true)`,
		`insert into task (id, mode, endpoint_id, spec, enabled) values ('t-probe', 'poll', (select id from endpoint where name = 'disp-1-icmp'), '{}'::jsonb, true)`,
		`insert into task (id, mode, endpoint_id, spec, enabled) values ('t-off', 'poll', (select id from endpoint where name = 'disp-1-icmp'), '{}'::jsonb, false)`,
	} {
		if _, err := f.conn.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	got, err := f.gw.ListClosedLoopPollTargets(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var mine []storage.ClosedLoopPollTarget
	for _, p := range got {
		if p.Node == "edge-1" {
			mine = append(mine, p)
		}
	}
	if len(mine) != 2 || mine[0].Transport != "http" || mine[0].Every != "15s" || mine[1].Transport != "icmp" || mine[1].Every != "" {
		t.Fatalf("targets = %+v, want the driver at 15s and the probe", mine)
	}
	if mine[0].ComponentID == "" || mine[0].ComponentID != mine[1].ComponentID {
		t.Fatalf("targets = %+v, want both on disp-1's id", mine)
	}
}
