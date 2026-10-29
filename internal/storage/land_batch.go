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
	// CommandTargeted is the property type names some command type targets,
	// as the caller last read them (CommandTargetedProperties), so the
	// property lane need not read them in the batch's transaction (#1299).
	// Nil reads them there. Read only, so a caller may share one set across
	// batches.
	CommandTargeted map[string]bool
	// Unbound is what the batch heard of identities its conduit has no
	// binding for (#1282), upserted into the conduit's inbox in the batch's
	// transaction, so a redelivery counts nothing twice.
	Unbound []UnboundIdentityWrite
	// Fanned is the batch's values a conduit fans out to the owners its
	// identity bindings name (#1282, ADR-1282), landed in the same
	// transaction under the fan-out writer. Nil on a batch that names no
	// bound identity, which then costs exactly what it did before.
	Fanned *FannedRows
}

// ErrFanoutStale marks a batch whose fanned rows name an owner the
// conduit no longer binds: transient, so the ingest redelivers it and
// resolves the bindings again, never a refusal that would drop the batch.
var ErrFanoutStale = errors.New("storage: a fanned row's owner is no longer bound; resolve again")

// FannedRows are a batch's fanned values, each row on its bound owner's
// arc, at the empty instance, naming the conduit in its source, all for
// Writer (a FanoutWriter).
type FannedRows struct {
	Writer     KeyWriter
	Metrics    []MetricSampleWrite
	Properties []PropertySampleWrite
	Events     []EventWrite
}

// fannedKeyed is every fanned row as the writer check reads it.
func (f *FannedRows) keyed() []keyedWrite {
	var out []keyedWrite
	for _, r := range f.Metrics {
		out = append(out, keyedWrite{key: r.Key, ownerKind: r.OwnerKind, owner: r.OwnerID})
	}
	for _, r := range f.Properties {
		out = append(out, keyedWrite{key: r.Key, ownerKind: r.OwnerKind, owner: r.OwnerID})
	}
	for _, r := range f.Events {
		out = append(out, keyedWrite{key: r.Key, ownerKind: r.OwnerKind, owner: r.OwnerID})
	}
	return out
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
	landed, err := p.landBatch(ctx, b)
	// A metric sample whose month has no partition (the roll is late, or a
	// node's clock is far out): make the months the batch needs and land it
	// again, once (#420). A batch that misses again is the ingest's to
	// redeliver.
	if isPartitionRoutingMiss(err) {
		if perr := p.landMetricMonths(ctx, b.Metrics); perr != nil {
			return false, errors.Join(err, perr)
		}
		landed, err = p.landBatch(ctx, b)
	}
	return landed, err
}

func (p *PG) landBatch(ctx context.Context, b IngestBatch) (bool, error) {
	landed := false
	err := p.sampleWrite(ctx, "land batch", func(attempt pgx.Tx) error {
		landed = false
		// Each attempt (a deadlock retries the whole batch) resolves its owners
		// and reads its clock afresh, once, for every lane (#1274).
		bt := newBatchTx(attempt)
		bt.targeted = b.CommandTargeted
		var tx pgx.Tx = bt
		if b.BatchID != "" {
			tag, err := tx.Exec(ctx, `insert into telemetry_receipt (batch_id) values ($1) on conflict (batch_id) do nothing`, b.BatchID)
			if err != nil {
				return fmt.Errorf("storage: land batch %s: receipt: %w", b.BatchID, err)
			}
			if tag.RowsAffected() == 0 {
				return nil // landed before: a redelivery or a retry
			}
		}
		// The fanned rows' fence first: one read of which owners the conduit
		// has bound, held on the writer for every lane, and each bound owner's
		// arc known without a read of its own (a binding's owner exists, by
		// its foreign key), so a batch about many owners resolves them in one
		// statement however many there are.
		fw := KeyWriter{}
		if b.Fanned != nil {
			fw = b.Fanned.Writer
			owners, err := fanBinding(ctx, tx, fw.Component, b.Fanned.keyed())
			if err != nil {
				return err
			}
			fw.fanOwners = owners
			// A row whose owner is no longer bound (an operator unbound it, or
			// deleted the owner, after the ingest resolved it) is stale, not
			// refused: refusing it would drop the conduit's own rows and every
			// other owner's with it. The batch fails transiently, writing
			// nothing, and its redelivery resolves the bindings afresh.
			for _, r := range b.Fanned.keyed() {
				if !owners[fanOwnerKey(r.ownerKind, r.owner)] {
					return fmt.Errorf("%w: %s/%s", ErrFanoutStale, r.ownerKind, r.owner)
				}
			}
			for k := range owners {
				bt.arcs[k] = k[strings.IndexByte(k, 0)+1:]
			}
		}
		if err := p.lockInOrder(ctx, tx, b); err != nil {
			return err
		}
		// The property lane's transition locks across the conduit's own rows
		// and its fanned ones, taken once in one sorted order, so two batches
		// whose own and fanned series cross never take them in opposite
		// orders.
		if b.Fanned != nil && len(b.Fanned.Properties) > 0 && len(b.Properties) > 0 {
			if err := p.lockAllPropertySeries(ctx, tx, append(append([]PropertySampleWrite(nil), b.Properties...), b.Fanned.Properties...)); err != nil {
				return err
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
		if f := b.Fanned; f != nil {
			if len(f.Properties) > 0 {
				if err := p.writePropertySamples(ctx, tx, fw, f.Properties, true); err != nil {
					return err
				}
			}
			if len(f.Metrics) > 0 {
				if err := p.writeMetricSamples(ctx, tx, fw, f.Metrics); err != nil {
					return err
				}
			}
			if len(f.Events) > 0 {
				if err := p.writeEvents(ctx, tx, fw, f.Events); err != nil {
					return err
				}
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
		if len(b.Unbound) > 0 {
			if err := writeUnboundIdentities(ctx, tx, b.Unbound); err != nil {
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
	props, metrics := b.Properties, b.Metrics
	if b.Fanned != nil {
		props = append(append([]PropertySampleWrite(nil), props...), b.Fanned.Properties...)
		metrics = append(append([]MetricSampleWrite(nil), metrics...), b.Fanned.Metrics...)
	}
	keys := append(sampleKeys(props, func(e PropertySampleWrite) string { return e.Key }),
		sampleKeys(metrics, func(e MetricSampleWrite) string { return e.Key })...)
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
	for _, ev := range props {
		if err := mark(ev.OwnerKind, ev.OwnerID, rules.LaneProperty, ev.Key); err != nil {
			return err
		}
	}
	for _, ev := range metrics {
		if err := mark(ev.OwnerKind, ev.OwnerID, rules.LaneMetric, ev.Key); err != nil {
			return err
		}
	}
	return p.markCalcPending(ctx, tx, marks, b.Writer.rule())
}

// lockAllPropertySeries takes the transition lock of every property series
// in evs, in one sorted order, each owner's arc resolved through the batch.
func (p *PG) lockAllPropertySeries(ctx context.Context, tx pgx.Tx, evs []PropertySampleWrite) error {
	arcs := make([]string, len(evs))
	for i, ev := range evs {
		arc, err := p.ownerArcValue(ctx, tx, ev.OwnerKind, ev.OwnerID)
		if err != nil {
			return fmt.Errorf("storage: land batch: owner %s/%s: %w", ev.OwnerKind, ev.OwnerID, err)
		}
		arcs[i] = arc
	}
	return lockPropertySeries(ctx, tx, evs, arcs)
}

// IsRefusedWrite reports whether a write failed for a reason no retry
// changes: its owner is gone or ambiguous, its key's writer class refuses the
// writer, or Postgres refused its data (a data exception, a CHECK). A row no metric partition
// covers is not refused (#420): its partition is made and the write retried,
// so a late roll delays a batch and never drops it. The
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
	if isPartitionRoutingMiss(err) {
		return false
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
