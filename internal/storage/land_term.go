package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/delivery"
)

// TermedBatch is a batch the server Termed whole (#1691): its id, the outpost
// that sent it, the ledger it carried, and every item it declared under the
// Term's cause. The ledger's outpost loss account (#1692) rides the receipt as
// a landing's would, so the outpost hop reads what the outpost dropped before it.
type TermedBatch struct {
	BatchID string
	Outpost string
	Ledger  delivery.Stamp
	Outcome delivery.Tally
}

// RecordTermedBatch writes the Term receipt of a batch the server Termed
// whole, so its items read under the cause instead of missing. It is marked
// termed: the receipt count skips it and a later copy of the batch that lands
// replaces it (insertReceipt). A batch id that already has a receipt keeps
// it, so a Term never overwrites a landing, nor an earlier Term's cause.
func (p *PG) RecordTermedBatch(ctx context.Context, b TermedBatch) error {
	outcome, err := b.Outcome.Cells()
	if err != nil {
		return fmt.Errorf("storage: term batch %s: outcome: %w", b.BatchID, err)
	}
	var outpost, outpostLoss *string
	if b.Outpost != "" {
		outpost = &b.Outpost
	}
	if !b.Ledger.Loss.Empty() {
		raw, err := json.Marshal(b.Ledger.Loss)
		if err != nil {
			return fmt.Errorf("storage: term batch %s: outpost loss: %w", b.BatchID, err)
		}
		n := string(raw)
		outpostLoss = &n
	}
	if _, err := p.pool.Exec(ctx, `insert into telemetry_receipt
		(batch_id, outpost, boot, seq, minted, resolved, ledger, outcome, outpost_loss, termed)
		values ($1, $2, $3, $4, $5, $6, $7::bigint[], $8::int[], $9::jsonb, true)
		on conflict (batch_id) do nothing`,
		b.BatchID, outpost, b.Ledger.Boot, int64(b.Ledger.Seq), b.Ledger.Minted, int64(b.Ledger.Resolved),
		b.Ledger.Kinds.Cells(), outcome, outpostLoss); err != nil {
		return fmt.Errorf("storage: term batch %s: %w", b.BatchID, err)
	}
	return nil
}
