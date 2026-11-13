package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/jackc/pgx/v5"
)

// insertReceipt writes the batch's receipt, the row its delivery accounting
// lives on (#1641, ADR-1641), and reports whether this delivery is its first.
// A batch id already landed counts one more delivery on its receipt and lands
// nothing: the count is what a read reports as deduplicated. A Term receipt
// (#1691) is not a landing: this delivery replaces it, as the batch's first,
// with its own ledger, node loss and landing time. A batch with a ledger
// records the ledger on it, and the node's own loss account when the node
// lost anything (#1692); one without (a publisher that stamps none) leaves
// those columns empty. A pushed batch's ledger has no node: the push lane's
// boot is the server's own (#1696).
func insertReceipt(ctx context.Context, tx pgx.Tx, b IngestBatch) (first bool, err error) {
	var node, boot, nodeLoss *string
	var seq, resolved *int64
	var ledger []*int64
	var minted any
	if b.Node != "" {
		node = &b.Node
	}
	if l := b.Ledger; l != nil {
		s, r := int64(l.Seq), int64(l.Resolved)
		boot, seq, resolved, ledger, minted = &l.Boot, &s, &r, l.Kinds.Cells(), l.Minted
		if !l.Loss.Empty() {
			raw, err := json.Marshal(l.Loss)
			if err != nil {
				return false, fmt.Errorf("storage: land batch %s: receipt node loss: %w", b.BatchID, err)
			}
			n := string(raw)
			nodeLoss = &n
		}
	}
	err = tx.QueryRow(ctx, insertReceiptSQL, b.BatchID, node, boot, seq, minted, resolved, ledger, nodeLoss).Scan(&first)
	if err != nil {
		return false, fmt.Errorf("storage: land batch %s: receipt: %w", b.BatchID, err)
	}
	return first, nil
}

// insertReceiptSQL is insertReceipt's statement. Only a landing leaves
// deliveries at 1 (an insert, or the replace of a Term receipt), so that is
// the first flag; a redelivery counts on.
const insertReceiptSQL = `insert into telemetry_receipt as r (batch_id, node, boot, seq, minted, resolved, ledger, node_loss)
	values ($1, $2, $3, $4, $5, $6, $7::bigint[], $8::jsonb)
	on conflict (batch_id) do update set
		deliveries = case when r.termed then 1 else r.deliveries + 1 end,
		node = case when r.termed then excluded.node else r.node end,
		boot = case when r.termed then excluded.boot else r.boot end,
		seq = case when r.termed then excluded.seq else r.seq end,
		minted = case when r.termed then excluded.minted else r.minted end,
		resolved = case when r.termed then excluded.resolved else r.resolved end,
		ledger = case when r.termed then excluded.ledger else r.ledger end,
		node_loss = case when r.termed then excluded.node_loss else r.node_loss end,
		landed_at = case when r.termed then excluded.landed_at else r.landed_at end,
		outcome = case when r.termed then null else r.outcome end,
		termed = false
	returning deliveries = 1`

// recordBatchOutcome writes the batch's outcome on its receipt, in the
// transaction that landed it, so the receipt never names an outcome the batch
// did not commit.
func recordBatchOutcome(ctx context.Context, tx pgx.Tx, batchID string, t delivery.Tally) error {
	cells, err := t.Cells()
	if err != nil {
		return fmt.Errorf("storage: land batch %s: outcome: %w", batchID, err)
	}
	if _, err := tx.Exec(ctx, `update telemetry_receipt set outcome = $2::int[] where batch_id = $1`, batchID, cells); err != nil {
		return fmt.Errorf("storage: land batch %s: outcome: %w", batchID, err)
	}
	return nil
}

// outcome is the batch's items by kind and outcome as the committed pass
// landed them: what the consumer counted (Tally), every live group's rows
// stored (its properties less the coalesced ones), every refused group's
// rows refused-write, and the node's self-logs stored. groups is every group
// the pass walked, so a refusal names the rows it dropped.
func (b IngestBatch) outcome(groups, live []landGroup, refused []GroupRefusal, coalesced int) delivery.Tally {
	var t delivery.Tally
	t.Merge(b.Tally)
	props := 0
	for _, g := range live {
		props += tallyGroup(&t, g.IngestGroup, delivery.Stored)
	}
	t.Add(delivery.Property, delivery.Stored, props-coalesced)
	t.Add(delivery.Property, delivery.Coalesced, coalesced)
	byIndex := make(map[int]IngestGroup, len(groups))
	for _, g := range groups {
		byIndex[g.index] = g.IngestGroup
	}
	for _, r := range refused {
		if g, ok := byIndex[r.Index]; ok {
			t.Add(delivery.Property, delivery.RefusedWrite, tallyGroup(&t, g, delivery.RefusedWrite))
		} else if r.Index == NodeSelfIndex && b.NodeSelf != nil {
			t.Add(delivery.Metric, delivery.RefusedWrite, len(b.NodeSelf.Metrics))
		}
	}
	t.Add(delivery.Log, delivery.Stored, len(b.NodeLogs))
	return t
}

// tallyGroup counts g's metrics, events and log lines, its own and its fanned
// ones, under o, and returns its properties for the caller to place: a
// landed group's are stored or coalesced, which only the lane knows.
func tallyGroup(t *delivery.Tally, g IngestGroup, o delivery.Outcome) (properties int) {
	t.Add(delivery.Metric, o, len(g.Metrics))
	t.Add(delivery.Event, o, len(g.Events))
	t.Add(delivery.Log, o, len(g.LogLines))
	properties = len(g.Properties)
	if f := g.Fanned; f != nil {
		t.Add(delivery.Metric, o, len(f.Metrics))
		t.Add(delivery.Event, o, len(f.Events))
		properties += len(f.Properties)
	}
	return properties
}
