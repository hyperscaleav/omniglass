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
// nothing: the count is what a read reports as deduplicated. A batch with a
// ledger records the ledger on it; one without (the push lane) leaves those
// columns empty.
func insertReceipt(ctx context.Context, tx pgx.Tx, b IngestBatch) (first bool, err error) {
	var node, boot, ledger *string
	var seq, resolved *int64
	var minted any
	if b.Node != "" {
		node = &b.Node
	}
	if l := b.Ledger; l != nil {
		raw, err := json.Marshal(l.Kinds)
		if err != nil {
			return false, fmt.Errorf("storage: land batch %s: receipt ledger: %w", b.BatchID, err)
		}
		s, r, j := int64(l.Seq), int64(l.Resolved), string(raw)
		boot, seq, resolved, ledger, minted = &l.Boot, &s, &r, &j, l.Minted
	}
	err = tx.QueryRow(ctx, `insert into telemetry_receipt (batch_id, node, boot, seq, minted, resolved, ledger)
		values ($1, $2, $3, $4, $5, $6, $7::jsonb)
		on conflict (batch_id) do update set deliveries = telemetry_receipt.deliveries + 1
		returning (xmax = 0)`, b.BatchID, node, boot, seq, minted, resolved, ledger).Scan(&first)
	if err != nil {
		return false, fmt.Errorf("storage: land batch %s: receipt: %w", b.BatchID, err)
	}
	return first, nil
}

// recordBatchOutcome writes the batch's outcome on its receipt, in the
// transaction that landed it, so the receipt never names an outcome the batch
// did not commit.
func recordBatchOutcome(ctx context.Context, tx pgx.Tx, batchID string, t delivery.Tally) error {
	raw, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("storage: land batch %s: outcome: %w", batchID, err)
	}
	if _, err := tx.Exec(ctx, `update telemetry_receipt set outcome = $2::jsonb where batch_id = $1`, batchID, string(raw)); err != nil {
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
