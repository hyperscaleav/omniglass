package labfootprint

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The receipt statement counts only the named node's receipts, from the
// window's start minus the margin, splits off the margin, the backlog the
// lane landed before it drained past the start, and the receipts after the
// end, and reads the same after the server's 24 h prune. A backlog that
// outlasts the window stops at its end, so no receipt is masked twice.
func TestIntegrationReceiptCountReadsOneNodesWindow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })

	var now time.Time
	if err := conn.QueryRow(ctx, `select now()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	t0, t1 := now.Add(-10*time.Minute), now.Add(-time.Minute)
	land := func(id string, at time.Time) {
		t.Helper()
		if _, err := gw.LandBatch(ctx, storage.IngestBatch{BatchID: id}); err != nil {
			t.Fatalf("land %s: %v", id, err)
		}
		if _, err := conn.Exec(ctx, `update telemetry_receipt set landed_at = $2 where batch_id = $1`, id, at); err != nil {
			t.Fatalf("age %s: %v", id, err)
		}
	}
	land("og.v1.telemetry.e_ge 1", now.Add(-48*time.Hour))  // past the prune horizon
	land("og.v1.telemetry.e_ge 2", t0.Add(-time.Hour))      // before the margin
	land("og.v1.telemetry.e_ge 3", t0.Add(-20*time.Second)) // in the margin
	land("og.v1.telemetry.e_ge 4", t0.Add(time.Minute))
	land("og.v1.telemetry.e_ge 5", t0.Add(5*time.Minute))
	land("og.v1.telemetry.e_ge 6", t1.Add(30*time.Second)) // after the end
	land("og.v1.telemetry.e_ge-2 1", t0.Add(time.Minute))  // a node whose name extends it
	land("og.v1.telemetry.edge 1", t0.Add(time.Minute))    // a node a LIKE pattern would match
	land("og.v1.telemetry.e_ge", t0.Add(time.Minute))      // no message id

	count := func(drained time.Time) ReceiptCount {
		t.Helper()
		var c ReceiptCount
		if err := conn.QueryRow(ctx, receiptCountQuery("e_ge", t0, drained, t1)).Scan(&c.Landed, &c.Margin, &c.Backlog, &c.AfterEnd); err != nil {
			t.Fatal(err)
		}
		return c
	}
	drained := t0.Add(2 * time.Minute)
	want := ReceiptCount{Landed: 4, Margin: 1, Backlog: 1, AfterEnd: 1}
	if got := count(drained); got != want {
		t.Fatalf("count = %+v, want %+v", got, want)
	}
	if got, want := count(t1.Add(2*time.Minute)), (ReceiptCount{Landed: 4, Margin: 1, Backlog: 2, AfterEnd: 1}); got != want {
		t.Fatalf("drained past the end, count = %+v, want %+v", got, want)
	}
	if n, err := gw.PruneTelemetryReceipts(ctx, now.Add(-24*time.Hour), 0); err != nil || n != 1 {
		t.Fatalf("prune removed %d, %v; want the one past the horizon", n, err)
	}
	if got := count(drained); got != want {
		t.Fatalf("after the prune, count = %+v, want %+v", got, want)
	}
}
