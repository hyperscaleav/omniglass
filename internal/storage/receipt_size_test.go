package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// receiptRowBudgetBytes is the widest a ledgered receipt's row may be
// (#1699). The packed row measures 303 B here, down from 525 B as jsonb.
// The gate is the live row, not the table's bytes on disk: those carry
// the dead tuple the outcome update leaves, and how much of it is pruned
// by the time of the read depends on what else the cluster is running.
const receiptRowBudgetBytes = 330

// TestReceiptStaysInItsBudget lands 1,000 of a node's ticks, each
// shaped as a production one is (a UUIDv7 boot and batch id, a ledger
// declaring metrics, properties and logs with a boot's running totals, an
// outcome storing them), and holds the receipt's row under its budget,
// logging its bytes on disk beside it: the receipt is the ingest's one row
// a batch beside its samples, so its width is a fixed cost on every one
// (#1699).
func TestReceiptStaysInItsBudget(t *testing.T) {
	t.Parallel()
	f := newGroupFixture(t)
	ctx := context.Background()
	const ticks = 1000
	boot := collection.NewBatchID()
	ts := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	for i := range uint64(ticks) {
		seq := 40_000 + i
		at := ts.Add(time.Duration(i) * time.Second)
		b := storage.IngestBatch{
			BatchID: "og.v1.telemetry.edge-1 " + collection.NewBatchID(),
			Writer:  storage.NodeWriter("", ""),
			Node:    "edge-1",
			Ledger: &delivery.Stamp{Boot: boot, Seq: seq, Minted: at, Resolved: seq - 1, Kinds: delivery.Declared{
				delivery.Metric:   {Count: 1, Before: 58 * (seq - 1)},
				delivery.Property: {Count: 1, Before: seq / 16},
				delivery.Log:      {Count: 1, Before: seq / 4},
			}},
			NodeLogs: []storage.NodeLogWrite{{Node: "edge-1", Source: "node", Severity: "info", Message: "tick", TS: at}},
			Groups:   []storage.IngestGroup{taskGroup("task-a", f.disp1, f.node, "on", at)},
		}
		b.Groups[0].Events = nil
		if res, err := f.gw.LandBatch(ctx, b); err != nil || !res.Landed {
			t.Fatalf("land tick %d: %+v, %v", i, res, err)
		}
	}
	var rows, total, heap int64
	var row, ledger, outcome, id float64
	err := f.conn.QueryRow(ctx, `select count(*), pg_total_relation_size('telemetry_receipt'), pg_relation_size('telemetry_receipt'),
		avg(pg_column_size(r.*)), avg(pg_column_size(ledger)), avg(pg_column_size(outcome)), avg(pg_column_size(batch_id))
		from telemetry_receipt r`).Scan(&rows, &total, &heap, &row, &ledger, &outcome, &id)
	if err != nil {
		t.Fatalf("measure the receipts: %v", err)
	}
	per := total / rows
	t.Logf("%d receipts: %d B each on disk (heap %d), row %.0f, ledger %.0f, outcome %.0f, batch id %.0f",
		rows, per, heap/rows, row, ledger, outcome, id)
	if _, err := f.conn.Exec(ctx, `vacuum full telemetry_receipt`); err != nil {
		t.Fatal(err)
	}
	var full, fullHeap, pk, at int64
	if err := f.conn.QueryRow(ctx, `select pg_total_relation_size('telemetry_receipt'), pg_relation_size('telemetry_receipt'),
		pg_relation_size('telemetry_receipt_pkey'), pg_relation_size('telemetry_receipt_landed_at_idx')`).Scan(&full, &fullHeap, &pk, &at); err != nil {
		t.Fatal(err)
	}
	t.Logf("compacted: %d B each (heap %d, pkey %d, landed_at %d)", full/rows, fullHeap/rows, pk/rows, at/rows)
	if row > receiptRowBudgetBytes {
		t.Fatalf("a receipt's row is %.0f B, over its %d B budget", row, receiptRowBudgetBytes)
	}
}
