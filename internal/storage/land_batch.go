package storage

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
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
	// Groups is a node's tick (#1582): each task's rows under its own
	// writer, all landed in the batch's one transaction under its one
	// receipt. A group a row of which is refused (IsRefusedWrite) is dropped
	// on its own and reported in LandResult.Refused; its siblings land.
	Groups []IngestGroup
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
// every sink. It reports Landed false, writing nothing, when the batch id
// already landed. A refused or failed row among the batch's top-level rows
// fails the whole batch, so no sink's rows are left behind for a redelivery
// to write a second time, log lines included (a log line has no natural key a
// retry could dedupe on, so the transaction is the only thing that can make
// it exactly once). A refused row in one of its Groups (#1582) drops that
// group alone, named in Refused; the rest of the batch and its receipt land,
// so the redelivery of a tick lands nothing twice and loses no sibling's rows.
//
// The receipt goes first: two deliveries of one batch at once serialize on
// its key, and the second sees the first's commit and lands nothing. Then
// every owner the batch names resolves in one statement, every group is held
// to its writer, and the batch takes its locks in the platform's order for a
// sample write, held for the batch as a whole (lockGroups): the event rule
// rows reading any of its keys, then every lane's calc marks, then the
// property lane's transition locks, and only then, lane by lane, the series
// and health locks judging takes. Each lane then lands every group's rows in
// the statements one group's would take. A deadlock abort retries the whole
// batch (sampleWrite).
//
// A refusal Postgres decides only as the rows land (a value its column will
// not take) is known only after the statement that carried every group's
// rows failed, so the batch is tried again, each group in a savepoint of its
// own, only to learn which groups Postgres refuses, and rolled back whole.
// The batch then lands a third time without them, in the statements and the
// lock order of any batch: what the refused groups would have marked or
// locked (a calc rule's pending mark on their owners) is never taken.
func (p *PG) LandBatch(ctx context.Context, b IngestBatch) (LandResult, error) {
	res, err := p.landRetryingDays(ctx, b, false, nil)
	var lanes *groupLanesRefused
	if errors.As(err, &lanes) {
		res, err = p.landRetryingDays(ctx, b, true, nil)
		var found *groupsRefused
		if errors.As(err, &found) {
			res, err = p.landRetryingDays(ctx, b, false, found.drop())
		}
	}
	if errors.As(err, &lanes) {
		err = lanes.err
	}
	return res, err
}

// landRetryingDays lands the batch, and on a metric sample whose day has no
// partition (the roll is late, or a node's clock is far out) makes the days
// the batch needs and lands it again, once (#420). A batch that misses again
// is the ingest's to redeliver.
func (p *PG) landRetryingDays(ctx context.Context, b IngestBatch, isolate bool, drop map[int]error) (LandResult, error) {
	res, err := p.landBatch(ctx, b, isolate, drop)
	if isPartitionRoutingMiss(err) {
		if perr := p.landMetricDays(ctx, b.allMetrics()); perr != nil {
			return LandResult{}, errors.Join(err, perr)
		}
		res, err = p.landBatch(ctx, b, isolate, drop)
	}
	return res, err
}

// groupLanesRefused is a refusal a lane met landing many groups' rows in one
// statement: some group's, not knowably which, so LandBatch lands the batch
// again with each group isolated.
type groupLanesRefused struct{ err error }

func (e *groupLanesRefused) Error() string { return e.err.Error() }
func (e *groupLanesRefused) Unwrap() error { return e.err }

// groupsRefused is what the isolated pass learned, returned as an error so
// its transaction rolls back whole: the groups Postgres refuses, which the
// batch's last pass drops before it takes a lock.
type groupsRefused struct{ refused []GroupRefusal }

func (e *groupsRefused) Error() string {
	return fmt.Sprintf("storage: land batch: %d groups refused", len(e.refused))
}

// drop is the refused groups by index, each with its refusal.
func (e *groupsRefused) drop() map[int]error {
	out := make(map[int]error, len(e.refused))
	for _, r := range e.refused {
		out[r.Index] = r.Err
	}
	return out
}

// landBatch makes one attempt at the batch. isolate tries each group in a
// savepoint of its own and, when any is refused, rolls the attempt back with
// what it learned (groupsRefused); drop names the groups a pass before
// learned are refused, which this one drops with that refusal unread.
func (p *PG) landBatch(ctx context.Context, b IngestBatch, isolate bool, drop map[int]error) (LandResult, error) {
	var res LandResult
	err := p.sampleWrite(ctx, "land batch", func(ctx context.Context, attempt pgx.Tx) error {
		res = LandResult{}
		// Each attempt (a deadlock retries the whole batch) resolves its owners
		// and reads its clock afresh, once, for every lane and group (#1274).
		bt := newBatchTx(attempt)
		bt.targeted = b.CommandTargeted
		if b.BatchID != "" {
			tag, err := bt.Exec(ctx, `insert into telemetry_receipt (batch_id) values ($1) on conflict (batch_id) do nothing`, b.BatchID)
			if err != nil {
				return fmt.Errorf("storage: land batch %s: receipt: %w", b.BatchID, err)
			}
			if tag.RowsAffected() == 0 {
				return nil // landed before: a redelivery or a retry
			}
		}
		groups := b.groups()
		if err := p.preloadArcs(ctx, bt, groups, b.NodeLogs); err != nil {
			return err
		}
		if err := fanGroups(ctx, bt, groups); err != nil {
			return err
		}
		if err := primeKeyWriters(ctx, bt, groups); err != nil {
			return err
		}
		live := make([]landGroup, 0, len(groups))
		for _, g := range groups {
			if err, ok := drop[g.index]; ok && !g.whole {
				res.Refused = append(res.Refused, GroupRefusal{Index: g.index, TaskID: g.TaskID, Err: err})
				continue
			}
			err := p.admitGroup(ctx, bt, g)
			switch {
			case err == nil:
				live = append(live, g)
			case g.whole || !IsRefusedWrite(err):
				return err
			default:
				res.Refused = append(res.Refused, GroupRefusal{Index: g.index, TaskID: g.TaskID, Err: err})
			}
		}
		if err := p.lockGroups(ctx, bt, live); err != nil {
			return err
		}
		if isolate {
			if err := p.landIsolated(ctx, bt, live, &res); err != nil {
				return err
			}
			if len(res.Refused) > 0 {
				return &groupsRefused{refused: res.Refused}
			}
		} else if err := p.landGroups(ctx, bt, live); err != nil {
			if IsRefusedWrite(err) && slices.ContainsFunc(live, func(g landGroup) bool { return !g.whole }) {
				return &groupLanesRefused{err: err}
			}
			return err
		}
		if len(b.NodeLogs) > 0 {
			if err := p.writeNodeLogs(ctx, bt, b.NodeLogs); err != nil {
				return err
			}
		}
		res.Landed = true
		return nil
	})
	if err != nil {
		return LandResult{}, err
	}
	slices.SortFunc(res.Refused, func(a, b GroupRefusal) int { return a.Index - b.Index })
	return res, nil
}

// landIsolated lands each group in a savepoint of its own: a refusal rolls
// back its group's savepoint alone and is recorded in res; any other failure
// fails the batch. A pass that records a refusal is rolled back whole
// (landBatch), so nothing it wrote, marked or logged survives it.
func (p *PG) landIsolated(ctx context.Context, bt *batchTx, live []landGroup, res *LandResult) error {
	// Read once on the batch, so no savepoint reads them again.
	if _, err := bt.clock(ctx); err != nil {
		return err
	}
	if _, err := commandTargetedProperties(ctx, bt); err != nil {
		return err
	}
	for _, g := range live {
		sp, err := bt.Begin(ctx)
		if err != nil {
			return fmt.Errorf("storage: land batch: open a group's savepoint: %w", err)
		}
		err = p.landGroups(ctx, bt.savepoint(sp), []landGroup{g})
		if err == nil {
			if err := sp.Commit(ctx); err != nil {
				return fmt.Errorf("storage: land batch: release a group's savepoint: %w", err)
			}
			continue
		}
		if rerr := sp.Rollback(ctx); rerr != nil {
			return errors.Join(err, rerr)
		}
		if g.whole || !IsRefusedWrite(err) {
			return err
		}
		res.Refused = append(res.Refused, GroupRefusal{Index: g.index, TaskID: g.TaskID, Err: err})
	}
	return nil
}

// landGroups lands the groups' rows lane by lane, each lane in the statements
// one group's rows would take: the sets of each row identity (a node's tasks
// share one, a conduit's fanned rows another) in one writer call, every log
// line in one, every unbound identity in one.
func (p *PG) landGroups(ctx context.Context, tx pgx.Tx, groups []landGroup) error {
	var props []propertySet
	var metrics []metricSet
	var events []eventSet
	var lines []LogLineWrite
	var unbound []UnboundIdentityWrite
	for _, g := range groups {
		if len(g.Properties) > 0 {
			props = append(props, propertySet{w: g.Writer, evs: g.Properties})
		}
		if len(g.Metrics) > 0 {
			metrics = append(metrics, metricSet{w: g.Writer, evs: g.Metrics})
		}
		if len(g.Events) > 0 {
			events = append(events, eventSet{w: g.Writer, evs: g.Events})
		}
		if f := g.Fanned; f != nil {
			if len(f.Properties) > 0 {
				props = append(props, propertySet{w: f.Writer, evs: f.Properties})
			}
			if len(f.Metrics) > 0 {
				metrics = append(metrics, metricSet{w: f.Writer, evs: f.Metrics})
			}
			if len(f.Events) > 0 {
				events = append(events, eventSet{w: f.Writer, evs: f.Events})
			}
		}
		lines = append(lines, g.LogLines...)
		unbound = append(unbound, g.Unbound...)
	}
	for _, sets := range byWriteIdentity(props, func(s propertySet) KeyWriter { return s.w }) {
		if err := p.writePropertySets(ctx, tx, sets, true); err != nil {
			return err
		}
	}
	for _, sets := range byWriteIdentity(metrics, func(s metricSet) KeyWriter { return s.w }) {
		if err := p.writeMetricSets(ctx, tx, sets); err != nil {
			return err
		}
	}
	for _, sets := range byWriteIdentity(events, func(s eventSet) KeyWriter { return s.w }) {
		if err := p.writeEventSets(ctx, tx, sets); err != nil {
			return err
		}
	}
	if len(lines) > 0 {
		if err := p.writeLogLines(ctx, tx, lines); err != nil {
			return err
		}
	}
	if len(unbound) > 0 {
		if err := writeUnboundIdentities(ctx, tx, mergeUnbound(unbound)); err != nil {
			return err
		}
	}
	return nil
}

// lockGroups takes the live groups' rule rows, calc marks and property
// transition locks before any lane writes: the event rule rows reading any
// of their sample keys FOR SHARE (newRuleEvaluator's lock), then every sample
// lane's marks (markCalcPending), the order rule_eval.go and calc_eval.go
// state for a sample write, then every property series' transition lock in
// one sorted order, own rows and fanned ones together, so two batches whose
// series cross never take them in opposite orders. Each lane takes them again
// as it writes, already held, and its series and health locks after them.
// Taken lane by lane instead, the property lane's judging would hold an
// owner's health lock while the metric lane waited on a rule row, the reverse
// of a rule edit's order (the row FOR UPDATE, then health), and the two would
// deadlock.
func (p *PG) lockGroups(ctx context.Context, tx pgx.Tx, live []landGroup) error {
	keys := map[string]bool{}
	marks := map[string]*calcMarks{}
	var props []PropertySampleWrite
	var propArcs []string
	mark := func(w KeyWriter, ownerKind, ownerID string, lane rules.Lane, key string) (string, error) {
		arc, err := p.ownerArcValue(ctx, tx, ownerKind, ownerID)
		if err != nil {
			return "", fmt.Errorf("storage: land batch: owner %s/%s: %w", ownerKind, ownerID, err)
		}
		m := marks[w.rule()]
		if m == nil {
			m = newCalcMarks()
			marks[w.rule()] = m
		}
		m.add(ownerKind, arc, lane, key)
		keys[key] = true
		return arc, nil
	}
	lane := func(w KeyWriter, ps []PropertySampleWrite, ms []MetricSampleWrite) error {
		for _, ev := range cleanPropertyWrites(ps) {
			arc, err := mark(w, ev.OwnerKind, ev.OwnerID, rules.LaneProperty, ev.Key)
			if err != nil {
				return err
			}
			props, propArcs = append(props, ev), append(propArcs, arc)
		}
		for _, ev := range ms {
			if _, err := mark(w, ev.OwnerKind, ev.OwnerID, rules.LaneMetric, ev.Key); err != nil {
				return err
			}
		}
		return nil
	}
	for _, g := range live {
		if err := lane(g.Writer, g.Properties, g.Metrics); err != nil {
			return err
		}
		if f := g.Fanned; f != nil {
			if err := lane(f.Writer, f.Properties, f.Metrics); err != nil {
				return err
			}
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sorted := slices.Sorted(maps.Keys(keys))
	if _, err := tx.Exec(ctx, `select id from event_rule where enabled and sample_key = any($1) order by id for share`, sorted); err != nil {
		return fmt.Errorf("storage: land batch: lock event rules: %w", err)
	}
	for _, rule := range slices.Sorted(maps.Keys(marks)) {
		if err := p.markCalcPending(ctx, tx, marks[rule], rule); err != nil {
			return err
		}
	}
	if len(props) == 0 {
		return nil
	}
	return lockPropertySeries(ctx, tx, props, propArcs)
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
