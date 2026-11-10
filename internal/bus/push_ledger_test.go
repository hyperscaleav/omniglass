package bus_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/labfootprint"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The push lane's ledger (#1696), driven through a real JetStream and a real
// Postgres: the server mints a ledger on every batch it publishes for the
// API's push route and webhooks, under its own boot, and the lab's read
// reconciles their receipts into a push-lane row apart from the nodes'.

// pushReceipt is one push-lane receipt as stored.
type pushReceipt struct {
	boot     string
	seq      uint64
	resolved uint64
	minted   time.Time
	ledger   delivery.Declared
	outcome  delivery.Tally
}

// pushReceipts is every ledgered push-lane receipt (no node), in seq order.
func (f deliveryFixture) pushReceipts(t *testing.T) []pushReceipt {
	t.Helper()
	rows, err := f.conn.Query(f.ctx, `select boot, seq, resolved, minted, ledger::text, coalesce(outcome::text, '{}')
		from telemetry_receipt where node is null and ledger is not null order by seq`)
	if err != nil {
		t.Fatalf("read push receipts: %v", err)
	}
	defer rows.Close()
	var out []pushReceipt
	for rows.Next() {
		var r pushReceipt
		var ledger, outcome string
		if err := rows.Scan(&r.boot, &r.seq, &r.resolved, &r.minted, &ledger, &outcome); err != nil {
			t.Fatalf("scan a push receipt: %v", err)
		}
		if err := json.Unmarshal([]byte(ledger), &r.ledger); err != nil {
			t.Fatalf("read a push receipt's ledger %s: %v", ledger, err)
		}
		if err := json.Unmarshal([]byte(outcome), &r.outcome); err != nil {
			t.Fatalf("read a push receipt's outcome %s: %v", outcome, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read push receipts: %v", err)
	}
	return out
}

// waitPushReceipts waits until the push lane has n receipts, ledgered or
// not, and the stream is empty.
func (f deliveryFixture) waitPushReceipts(t *testing.T, srv *bus.Server, n int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var got int
		if err := f.conn.QueryRow(f.ctx, `select count(*) from telemetry_receipt where node is null`).Scan(&got); err != nil {
			t.Fatalf("count push receipts: %v", err)
		}
		if got >= n {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the push lane has %d receipts, want %d", got, n)
		}
		time.Sleep(50 * time.Millisecond)
	}
	f.waitDrained(t, srv)
}

// pushWindow publishes in-window batches through the server's push lane,
// closes the window, then publishes one more after it so the boot settles,
// and returns the window.
func pushWindow(t *testing.T, f deliveryFixture, srv *bus.Server, in []*ogv1.TelemetryBatch) delivery.Window {
	t.Helper()
	from := time.Now().UTC().Add(-time.Millisecond)
	for _, b := range in {
		if err := srv.PublishTelemetry(f.ctx, b); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	to := time.Now().UTC()
	time.Sleep(5 * time.Millisecond)
	if err := srv.PublishTelemetry(f.ctx, pushBatch("push-settle")); err != nil {
		t.Fatalf("publish the settling batch: %v", err)
	}
	return delivery.Window{From: from, To: to}
}

// pushRows is the lab's read over w, every row the push lane's.
func pushRows(t *testing.T, f deliveryFixture, w delivery.Window) []labfootprint.DeliveryRow {
	t.Helper()
	rows, unsettled := f.readDelivery(t, w)
	if len(unsettled) != 0 {
		t.Fatalf("unsettled %+v, want the push boot settled by the batch after the window", unsettled)
	}
	for _, r := range rows {
		if r.Lane != labfootprint.LanePush {
			t.Fatalf("row %+v is not the push lane's: only pushed batches went in", r)
		}
	}
	return rows
}

// TestPushedBatchCarriesItsLedger: each batch the push lane publishes
// carries the server boot's ledger (dense seq, every kind's running offset,
// resolved the seq below), its receipt records that ledger and the batch's
// outcome, and the read is a push-lane row per kind at loss 0. A batch that
// reaches the stream with no ledger (a publisher from before #1696, or a
// replay of one) lands, and its receipt is skipped by the read, never a
// boot of its own.
func TestPushedBatchCarriesItsLedger(t *testing.T) {
	f := newDeliveryFixture(t)
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, f.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	if _, err := srv.PublishTelemetryAs(f.ctx, pushBatch("push-bare"), collection.NewBatchID()); err != nil {
		t.Fatalf("publish a batch with no ledger: %v", err)
	}
	w := pushWindow(t, f, srv, []*ogv1.TelemetryBatch{pushBatch("push-a"), pushBatch("push-b")})
	f.waitPushReceipts(t, srv, 4)
	f.waitLanded(t, "push-bare")

	got := f.pushReceipts(t)
	if len(got) != 3 {
		t.Fatalf("push receipts %+v, want 3 ledgered (the bare batch has none)", got)
	}
	for i, r := range got {
		seq := uint64(i + 1)
		if r.boot == "" || r.boot != got[0].boot || r.seq != seq || r.resolved != seq-1 {
			t.Fatalf("receipt %d = boot %q seq %d resolved %d, want one boot, seq %d, resolved %d", i, r.boot, r.seq, r.resolved, seq, seq-1)
		}
		for _, k := range delivery.Kinds {
			if c := r.ledger[k]; c != (delivery.Count{Count: 1, Before: seq - 1}) {
				t.Fatalf("receipt %d %s ledger = %+v, want count 1 before %d", i, k, c, seq-1)
			}
			if n := r.outcome.Count(k, delivery.Stored); n != 1 {
				t.Fatalf("receipt %d %s outcome = %v, want 1 stored", i, k, r.outcome)
			}
		}
	}
	if !w.Contains(got[0].minted) || !w.Contains(got[1].minted) || w.Contains(got[2].minted) {
		t.Fatalf("minted %v %v %v, want the first two in %v and the third after", got[0].minted, got[1].minted, got[2].minted, w)
	}

	rows := pushRows(t, f, w)
	if len(rows) != len(delivery.Kinds) {
		t.Fatalf("rows %+v, want one per kind", rows)
	}
	for _, k := range delivery.Kinds {
		wantRow(t, rows, k, 2, 2, 0, map[delivery.Outcome]uint64{delivery.Stored: 2})
	}
}

// TestPushedBatchWithheldReadsMissing: a pushed batch the stream stored and
// the ingest Termed (a system owns no log lines, so the whole batch is
// dropped) leaves no receipt, a hole in the server boot's seq read from its
// neighbors' offsets as missing. A batch beside it refused in part (a metric
// no catalog registers) reads that part under its own outcome, still a loss.
func TestPushedBatchWithheldReadsMissing(t *testing.T) {
	f := newDeliveryFixture(t)
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, f.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	termed := pushBatch("push-termed")
	termed.Owner = &ogv1.Owner{Kind: "system", Ref: "no-such-system"}
	partial := pushBatch("push-partial")
	partial.Metrics = append(partial.Metrics, &ogv1.MetricSample{Name: "no-such-metric", Value: 1})
	w := pushWindow(t, f, srv, []*ogv1.TelemetryBatch{pushBatch("push-a"), termed, partial})
	f.waitPushReceipts(t, srv, 3)

	seqs := []uint64{}
	for _, r := range f.pushReceipts(t) {
		seqs = append(seqs, r.seq)
	}
	if len(seqs) != 3 || seqs[0] != 1 || seqs[1] != 3 || seqs[2] != 4 {
		t.Fatalf("push receipt seqs %v, want 1, 3 and 4: the Termed batch spent seq 2 and left no receipt", seqs)
	}

	rows := pushRows(t, f, w)
	wantRow(t, rows, delivery.Metric, 4, 2, 0, map[delivery.Outcome]uint64{delivery.Stored: 2, delivery.Missing: 1, delivery.RefusedUnregistered: 1})
	for _, k := range []delivery.Kind{delivery.Property, delivery.Event, delivery.Log} {
		wantRow(t, rows, k, 3, 2, 0, map[delivery.Outcome]uint64{delivery.Stored: 2, delivery.Missing: 1})
	}
}

// TestPushAndNodeLanesReadApart: a node's batches and pushed batches in the
// same window read as a row per lane and kind, each lane reconciled over its
// own boots: the node boot's hole reads as the node lane's loss, never the
// push lane's.
func TestPushAndNodeLanesReadApart(t *testing.T) {
	r := newLedgerRig(t)
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, r.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	nc := r.connect(t, srv)
	// The node's batch 3 never reaches the stream.
	r.publish(t, nc, r.ledgered(1, 0), r.ledgered(2, 1), r.ledgered(4, 3))
	w := pushWindow(t, r.deliveryFixture, srv, []*ogv1.TelemetryBatch{pushBatch("push-a")})
	w.From = r.window().From
	after := r.ledgered(5, 4)
	after.Ledger.Minted = timestamppb.New(w.To.Add(time.Second))
	r.publish(t, nc, after)
	r.waitReceipts(t, srv, 4)
	r.waitPushReceipts(t, srv, 2)

	rows, unsettled := r.readDelivery(t, w)
	if len(unsettled) != 0 {
		t.Fatalf("unsettled %+v, want both boots settled", unsettled)
	}
	lanes := map[labfootprint.DeliveryLane][]labfootprint.DeliveryRow{}
	for _, row := range rows {
		lanes[row.Lane] = append(lanes[row.Lane], row)
	}
	wantRow(t, lanes[labfootprint.LaneNode], delivery.Metric, 4, 3, 0, map[delivery.Outcome]uint64{delivery.Stored: 3, delivery.Missing: 1})
	wantRow(t, lanes[labfootprint.LanePush], delivery.Metric, 1, 1, 0, map[delivery.Outcome]uint64{delivery.Stored: 1})
	if rows[0].Lane != labfootprint.LaneNode || rows[len(rows)-1].Lane != labfootprint.LanePush {
		t.Fatalf("rows %+v, want the node lane's first", rows)
	}
}

// TestPushedBatchShortOfItsLedgerReadsMissing: a pushed batch that carries
// fewer items than its ledger declares (a publisher that lost some between
// minting and sending) reads the shortfall as missing on the push lane, as
// the node lane does, and the items it does carry as stored.
func TestPushedBatchShortOfItsLedgerReadsMissing(t *testing.T) {
	f := newDeliveryFixture(t)
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, f.gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	now := time.Now().UTC()
	w := delivery.Window{From: now.Add(-time.Minute), To: now}
	stamped := func(tag string, seq uint64, minted time.Time, metrics uint64) *ogv1.TelemetryBatch {
		b := pushBatch(tag)
		b.Ledger = &ogv1.DeliveryLedger{Boot: "hand-boot", Seq: seq, Minted: timestamppb.New(minted), Resolved: seq - 1,
			Kinds: []*ogv1.KindCount{
				{Kind: string(delivery.Metric), Count: metrics, Before: 2 * (seq - 1)},
				{Kind: string(delivery.Property), Count: 1, Before: seq - 1},
				{Kind: string(delivery.Event), Count: 1, Before: seq - 1},
				{Kind: string(delivery.Log), Count: 1, Before: seq - 1},
			}}
		return b
	}
	for _, b := range []*ogv1.TelemetryBatch{stamped("push-short", 1, now.Add(-time.Second), 2), stamped("push-after", 2, now.Add(time.Second), 1)} {
		if _, err := srv.PublishTelemetryAs(f.ctx, b, collection.NewBatchID()); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	f.waitPushReceipts(t, srv, 2)

	rows := pushRows(t, f, w)
	wantRow(t, rows, delivery.Metric, 2, 1, 0, map[delivery.Outcome]uint64{delivery.Stored: 1, delivery.Missing: 1})
	wantRow(t, rows, delivery.Log, 1, 1, 0, map[delivery.Outcome]uint64{delivery.Stored: 1})
}
