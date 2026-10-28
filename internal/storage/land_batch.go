package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
// its key, and the second sees the first's commit and lands nothing. Then the
// batch takes its locks in the platform's order for a sample write, held for
// the batch as a whole (lockInOrder): the event rule rows reading any of its
// keys, then every lane's calc marks, and only then, lane by lane, the
// property lane's transition locks and the series and health locks judging
// takes. A deadlock abort retries the whole batch (sampleWrite).
func (p *PG) LandBatch(ctx context.Context, b IngestBatch) (bool, error) {
	landed := false
	err := p.sampleWrite(ctx, "land batch", func(attempt pgx.Tx) error {
		landed = false
		// Each attempt (a deadlock retries the whole batch) resolves its owners
		// and reads its clock afresh, once, for every lane (#1274).
		var tx pgx.Tx = newBatchTx(attempt)
		if b.BatchID != "" {
			tag, err := tx.Exec(ctx, `insert into telemetry_receipt (batch_id) values ($1) on conflict (batch_id) do nothing`, b.BatchID)
			if err != nil {
				return fmt.Errorf("storage: land batch %s: receipt: %w", b.BatchID, err)
			}
			if tag.RowsAffected() == 0 {
				return nil // landed before: a redelivery or a retry
			}
		}
		if err := p.lockInOrder(ctx, tx, b); err != nil {
			return err
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

// lockInOrder takes a batch's rule rows and calc marks before any lane
// writes: the event rule rows reading any of its sample keys FOR SHARE
// (newRuleEvaluator's lock), then every sample lane's marks (markCalcPending),
// the order rule_eval.go and calc_eval.go state for a sample write. Each lane
// takes them again as it writes, already held, and its series and health
// locks after them. Taken lane by lane instead, the property lane's judging
// would hold an owner's health lock while the metric lane waited on a rule
// row, the reverse of a rule edit's order (the row FOR UPDATE, then health),
// and the two would deadlock.
func (p *PG) lockInOrder(ctx context.Context, tx pgx.Tx, b IngestBatch) error {
	keys := append(sampleKeys(b.Properties, func(e PropertySampleWrite) string { return e.Key }),
		sampleKeys(b.Metrics, func(e MetricSampleWrite) string { return e.Key })...)
	if len(keys) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `select id from event_rule where enabled and sample_key = any($1) order by id for share`, keys); err != nil {
		return fmt.Errorf("storage: land batch: lock event rules: %w", err)
	}
	marks := newCalcMarks()
	arcs := map[[2]string]string{}
	mark := func(ownerKind, ownerID string, lane rules.Lane, key string) error {
		ref := [2]string{ownerKind, ownerID}
		arc, ok := arcs[ref]
		if !ok {
			var err error
			if arc, err = p.ownerArcValue(ctx, tx, ownerKind, ownerID); err != nil {
				return fmt.Errorf("storage: land batch: owner %s/%s: %w", ownerKind, ownerID, err)
			}
			arcs[ref] = arc
		}
		marks.add(ownerKind, arc, lane, key)
		return nil
	}
	for _, ev := range b.Properties {
		if err := mark(ev.OwnerKind, ev.OwnerID, rules.LaneProperty, ev.Key); err != nil {
			return err
		}
	}
	for _, ev := range b.Metrics {
		if err := mark(ev.OwnerKind, ev.OwnerID, rules.LaneMetric, ev.Key); err != nil {
			return err
		}
	}
	return p.markCalcPending(ctx, tx, marks, b.Writer.rule())
}

// IsRefusedWrite reports whether a write failed for a reason no retry
// changes: its owner is gone or ambiguous, its key's writer class refuses the
// writer, or Postgres refused its data (a data exception, a CHECK). The
// ingest drops such a batch on its first delivery instead of redelivering it,
// since a series' batches land in order and every batch behind it would wait
// out redeliveries that cannot succeed. Any other integrity violation is not
// a refusal: a foreign key, a not-null type lookup or a unique key can fail
// because a row the batch references was deleted under it (a metric type, a
// calc rule), and the redelivery re-derives against the registry as it is
// then and lands the rest.
func IsRefusedWrite(err error) bool {
	var ambiguous *ErrAmbiguousName
	if isOwnerNotFound(err) || errors.Is(err, ErrKeyWriterRefused) || errors.As(err, &ambiguous) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return strings.HasPrefix(pgErr.Code, "22") || pgErr.Code == "23514"
	}
	return false
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
