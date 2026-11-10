package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/delivery"
)

// TermedBatch is a batch the server Termed whole (#1691): its id, the node
// that sent it, the ledger it carried, and every item it declared under the
// Term's cause. The ledger's node loss account (#1692) rides the receipt as
// a landing's would, so the node hop reads what the node dropped before it.
type TermedBatch struct {
	BatchID string
	Node    string
	Ledger  delivery.Stamp
	Outcome delivery.Tally
}

// RecordTermedBatch writes the Term receipt of a batch the server Termed
// whole, so its items read under the cause instead of missing. It is marked
// termed: the receipt count skips it and a later copy of the batch that lands
// replaces it (insertReceipt). A batch id that already has a receipt keeps
// it, so a Term never overwrites a landing, nor an earlier Term's cause.
func (p *PG) RecordTermedBatch(ctx context.Context, b TermedBatch) error {
	ledger, err := json.Marshal(b.Ledger.Kinds)
	if err != nil {
		return fmt.Errorf("storage: term batch %s: ledger: %w", b.BatchID, err)
	}
	outcome, err := json.Marshal(b.Outcome)
	if err != nil {
		return fmt.Errorf("storage: term batch %s: outcome: %w", b.BatchID, err)
	}
	var node, nodeLoss *string
	if b.Node != "" {
		node = &b.Node
	}
	if !b.Ledger.Loss.Empty() {
		raw, err := json.Marshal(b.Ledger.Loss)
		if err != nil {
			return fmt.Errorf("storage: term batch %s: node loss: %w", b.BatchID, err)
		}
		n := string(raw)
		nodeLoss = &n
	}
	if _, err := p.pool.Exec(ctx, `insert into telemetry_receipt
		(batch_id, node, boot, seq, minted, resolved, ledger, outcome, node_loss, termed)
		values ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9::jsonb, true)
		on conflict (batch_id) do nothing`,
		b.BatchID, node, b.Ledger.Boot, int64(b.Ledger.Seq), b.Ledger.Minted, int64(b.Ledger.Resolved),
		string(ledger), string(outcome), nodeLoss); err != nil {
		return fmt.Errorf("storage: term batch %s: %w", b.BatchID, err)
	}
	return nil
}
