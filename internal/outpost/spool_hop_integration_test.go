package node_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/labfootprint"
	"github.com/hyperscaleav/omniglass/internal/node"
)

// TestASpoolOverflowReadsAsTheNodeHopsLoss (#1692): the bus is away while
// the node mints five batches into a spool that holds three, so the spool
// drops the two oldest. When the bus is back, the three it held land, and
// the next batch carries the dropped run. The lab's read, over real
// receipts, names the two batches' items the node hop's loss under
// spool-overflow, the ingest hop lost nothing of what it was handed, and the
// end to end is the two composed.
func TestASpoolOverflowReadsAsTheNodeHopsLoss(t *testing.T) {
	t.Parallel()
	r := node.NewHopRig(t, 3)
	from := time.Now().UTC().Add(-time.Second)
	at := from

	r.SetDown(true)
	for v := 1; v <= 5; v++ {
		at = at.Add(time.Millisecond)
		r.Tick(t, float64(v), at)
	}
	if n := r.Pending(); n != 3 {
		t.Fatalf("held through the outage: %d, want the spool's cap of 3", n)
	}
	r.SetDown(false)
	r.Retry(t)
	r.Tick(t, 6, at.Add(time.Millisecond)) // minted after both drops: it carries the run
	if n := r.Pending(); n != 0 {
		t.Fatalf("held after the bus came back: %d", n)
	}
	time.Sleep(5 * time.Millisecond)
	to := time.Now().UTC()
	time.Sleep(5 * time.Millisecond)
	r.Tick(t, 7, at.Add(2*time.Millisecond)) // minted after the window: the boot settles
	waitReceipts(t, r, 5)

	w := delivery.Window{From: from, To: to}
	rows, unsettled, ended := labfootprint.DeliveryOf(labfootprint.DeliveryRead{
		Read: true, Window: w, Receipts: readReceipts(t, r, w), StreamClear: true})
	if len(unsettled) != 0 || len(ended) != 0 {
		t.Fatalf("unsettled %v ended %v, want the boot settled by its batch after the window", unsettled, ended)
	}

	nodeRow := rowOf(t, rows, delivery.Node, delivery.Metric)
	if nodeRow.In != 6 || nodeRow.Out != 4 || nodeRow.Outcomes[delivery.SpoolOverflow] != 2 || nodeRow.Outcomes[delivery.Shipped] != 4 {
		t.Fatalf("node row %+v, want 6 minted, 4 shipped, 2 lost to spool-overflow", nodeRow)
	}
	if nodeRow.Cause() != delivery.SpoolOverflow || nodeRow.Loss == nil || *nodeRow.Loss != 2.0/6 {
		t.Fatalf("node row loss %v cause %q, want 2/6 under spool-overflow", nodeRow.Loss, nodeRow.Cause())
	}
	ingestRow := rowOf(t, rows, delivery.Ingest, delivery.Metric)
	if ingestRow.In != 4 || ingestRow.Out != 4 || ingestRow.Loss == nil || *ingestRow.Loss != 0 {
		t.Fatalf("ingest row %+v, want the 4 shipped all stored, loss 0", ingestRow)
	}
	if ingestRow.Outcomes[delivery.Missing] != 0 {
		t.Fatalf("ingest row reads %d missing: the node's drops leaked into the ingest hop", ingestRow.Outcomes[delivery.Missing])
	}
	if got := labfootprint.EndToEndOf(rows)[delivery.Metric]; got != delivery.EndToEnd(2.0/6, 0) {
		t.Fatalf("metric end to end %v, want the two hops composed, %v", got, delivery.EndToEnd(2.0/6, 0))
	}
}

// rowOf is the read's row of hop h for kind k.
func rowOf(t *testing.T, rows []labfootprint.DeliveryRow, h delivery.Hop, k delivery.Kind) labfootprint.DeliveryRow {
	t.Helper()
	for _, row := range rows {
		if row.Hop == h && row.Kind == k {
			return row
		}
	}
	t.Fatalf("no %s %s row in %+v", h, k, rows)
	return labfootprint.DeliveryRow{}
}

// waitReceipts waits until edge-1 has n receipts.
func waitReceipts(t *testing.T, r *node.HopRig, n int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var got int
		if err := r.Conn.QueryRow(context.Background(), `select count(*) from telemetry_receipt where node = 'edge-1'`).Scan(&got); err != nil {
			t.Fatalf("count receipts: %v", err)
		}
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("edge-1 has %d receipts, want %d", got, n)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// readReceipts is the lab's receipts read over w, as lab measure makes it.
func readReceipts(t *testing.T, r *node.HopRig, w delivery.Window) []delivery.Receipt {
	t.Helper()
	rows, err := r.Conn.Query(context.Background(), labfootprint.ReceiptsQuery(w.From, w.To))
	if err != nil {
		t.Fatalf("read receipts: %v", err)
	}
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan a receipt: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read receipts: %v", err)
	}
	receipts, err := labfootprint.ParseReceipts(strings.Join(lines, "\n"))
	if err != nil {
		t.Fatalf("parse receipts: %v", err)
	}
	return receipts
}
