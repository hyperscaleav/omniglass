package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// IngestBatch is everything one ingest batch writes, landed as one unit
// (#311, ADR-0311): a node's self-logs, a component's log lines, and the
// three typed sample lanes, all for Writer.
//
// BatchID is the batch's idempotency key: the publishing subject and the
// batch's Nats-Msg-Id, or its stream sequence when it carries none. A batch
// id lands once; the same id again (a JetStream redelivery, a node's retry
// past the stream's duplicate window) writes nothing. Empty lands the batch
// with no receipt: atomic, but not idempotent.
//
// Properties are the transition-only lane: a value equal to its series'
// latest is not written (the guard is the insert's own condition, held per
// series by a lock, writePropertySamples).
type IngestBatch struct {
	BatchID    string
	Writer     KeyWriter
	NodeLogs   []NodeLogWrite
	LogLines   []LogLineWrite
	Metrics    []MetricSampleWrite
	Events     []EventWrite
	Properties []PropertySampleWrite
}

// LandBatch writes one ingest batch in ONE transaction: its receipt, then
// every sink, all or nothing. It reports false, writing nothing, when the
// batch id already landed. A refused or failed row fails the whole batch, so
// no sink's rows are left behind for a redelivery to write a second time,
// log lines included (a log line has no natural key a retry could dedupe
// on, so the transaction is the only thing that can make it exactly once).
//
// The receipt goes first: two deliveries of one batch at once serialize on
// its key, and the second sees the first's commit and lands nothing. The
// property lane goes next, so its series locks are taken before any mark or
// rule lock the other lanes take (the order writeMetricSamples states). A
// deadlock abort retries the whole batch (sampleWrite).
func (p *PG) LandBatch(ctx context.Context, b IngestBatch) (bool, error) {
	landed := false
	err := p.sampleWrite(ctx, "land batch", func(tx pgx.Tx) error {
		landed = false
		if b.BatchID != "" {
			tag, err := tx.Exec(ctx, `insert into telemetry_receipt (batch_id) values ($1) on conflict (batch_id) do nothing`, b.BatchID)
			if err != nil {
				return fmt.Errorf("storage: land batch %s: receipt: %w", b.BatchID, err)
			}
			if tag.RowsAffected() == 0 {
				return nil // landed before: a redelivery or a retry
			}
		}
		if len(b.Properties) > 0 {
			if err := p.writePropertySamples(ctx, tx, b.Writer, b.Properties, true); err != nil {
				return err
			}
		}
		if len(b.Metrics) > 0 {
			if err := p.writeMetricSamples(ctx, tx, b.Writer, b.Metrics); err != nil {
				return err
			}
		}
		if len(b.Events) > 0 {
			if err := p.writeEvents(ctx, tx, b.Writer, b.Events); err != nil {
				return err
			}
		}
		if len(b.LogLines) > 0 {
			if err := p.writeLogLines(ctx, tx, b.LogLines); err != nil {
				return err
			}
		}
		if len(b.NodeLogs) > 0 {
			if err := p.writeNodeLogs(ctx, tx, b.NodeLogs); err != nil {
				return err
			}
		}
		landed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return landed, nil
}

// pruneReceiptsBatch is how many receipts one prune statement deletes, so a
// long backlog is many short statements rather than one long lock.
const pruneReceiptsBatch = 10000

// PruneTelemetryReceipts deletes the receipts of batches landed before
// before, at most batch per statement (zero takes pruneReceiptsBatch), and
// returns how many it deleted. A receipt only has to outlive every copy of
// its batch that could still arrive: the server prunes past a horizon well
// beyond the stream's redelivery and the node's retry (ADR-0311).
func (p *PG) PruneTelemetryReceipts(ctx context.Context, before time.Time, batch int) (int64, error) {
	if batch <= 0 {
		batch = pruneReceiptsBatch
	}
	var total int64
	for {
		tag, err := p.pool.Exec(ctx, `delete from telemetry_receipt where batch_id in (
			select batch_id from telemetry_receipt where landed_at < $1 limit $2)`, before, batch)
		if err != nil {
			return total, fmt.Errorf("storage: prune telemetry receipts: %w", err)
		}
		total += tag.RowsAffected()
		if tag.RowsAffected() < int64(batch) {
			return total, nil
		}
	}
}
