package storage

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hyperscaleav/omniglass/internal/rules"
)

// MetricSampleWrite is one observed metric to persist. OwnerKind picks the
// arc; OwnerID is the fleet address (component/system/location/node name).
// Instance discriminates many values of one key on one owner (default "").
type MetricSampleWrite struct {
	OwnerKind string
	OwnerID   string
	Key       string
	Instance  string
	Value     float64
	Source    string
	TS        time.Time
}

// MetricSample is a stored observed/derived metric row (read side).
type MetricSample struct {
	TS         time.Time
	OwnerKind  string
	Key        string
	Instance   string
	Value      float64
	Provenance string
	Source     string
}

// ErrUnknownOwnerKind guards the owner-arc column mapping.
var ErrUnknownOwnerKind = errors.New("storage: unknown sample owner_kind")

// ownerColumn maps an owner kind to its arc column, so a bad kind fails in Go
// (an explicit error) rather than as a NULL that trips the CHECK opaquely.
func ownerColumn(kind string) (string, error) {
	switch kind {
	case "component":
		return "component_id", nil
	case "system":
		return "system_id", nil
	case "location":
		return "location_id", nil
	case "node":
		return "node_id", nil
	default:
		return "", ErrUnknownOwnerKind
	}
}

// InsertMetricSamples writes observed metric rows in one transaction, for w:
// a node's lane (NodeWriter) or the push lane (PushWriter). Each row sets
// exactly its owner arc column (the CHECK enforces the rest) and provenance
// observed. Callers apply reject-not-project (collection.Registry) before
// calling; this is the durable write, and it refuses the whole batch, writing
// nothing, when a key's writer class does not admit w (ADR-0161).
func (p *PG) InsertMetricSamples(ctx context.Context, w KeyWriter, evs []MetricSampleWrite) error {
	if len(evs) == 0 {
		return nil
	}
	return p.sampleWrite(ctx, "insert samples", func(tx pgx.Tx) error { return p.writeMetricSamples(ctx, tx, w, evs) })
}

// sampleWriteAttempts bounds how often a sample-write transaction is retried
// after Postgres aborts it as a deadlock (40P01). The calc tick and the
// ingest can lock two marks in opposite orders (calc_eval.go's lock order);
// the abort comes before anything commits, so the whole batch is retried,
// after a short jittered pause, rather than lost.
const sampleWriteAttempts = 4

// sampleWrite runs one sample-write transaction, retrying it whole on a
// deadlock abort.
func (p *PG) sampleWrite(ctx context.Context, what string, write func(pgx.Tx) error) error {
	var err error
	for attempt := 1; ; attempt++ {
		err = p.sampleWriteOnce(ctx, what, write)
		if err == nil || !isDeadlock(err) || attempt == sampleWriteAttempts {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(attempt)*10*time.Millisecond + time.Duration(rand.Int64N(int64(20*time.Millisecond)))):
		}
	}
}

func (p *PG) sampleWriteOnce(ctx context.Context, what string, write func(pgx.Tx) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin %s: %w", what, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := write(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit %s: %w", what, err)
	}
	return nil
}

// isDeadlock reports a transaction Postgres aborted to break a deadlock.
func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}

// sampleLineage is a calculated sample's producer: the calc rule, by id (so
// the write never marks its own rule pending, ADR-0069) and by name and
// version (the row's source_rule and source_rule_version). It rides a calc
// rule's KeyWriter; nil is an observed sample.
type sampleLineage struct {
	ruleID   string
	ruleName string
	version  int
}

// provenanceArgs is a row's provenance, source_rule, source_rule_version and
// source_rule_id.
func (l *sampleLineage) provenanceArgs() (string, any, any, any) {
	if l == nil {
		return "observed", nil, nil, nil
	}
	return "calculated", l.ruleName, l.version, l.ruleID
}

func (l *sampleLineage) rule() string {
	if l == nil {
		return ""
	}
	return l.ruleID
}

// writeMetricSamples is the metric lane's sample-write path, in the caller's
// transaction: the ingest's observed batch, or a calc rule's calculated
// output (a calc rule's writer, its lineage set). Every row is held to its
// key's writer class first (ADR-0161). The event rules reading the batch's
// keys judge each sample as it lands (#919), and the calc rules reading them
// are marked pending on each owner (#996), never the rule that produced the
// batch.
func (p *PG) writeMetricSamples(ctx context.Context, tx pgx.Tx, w KeyWriter, evs []MetricSampleWrite) error {
	rows := make([]keyedWrite, len(evs))
	for i, ev := range evs {
		rows[i] = keyedWrite{key: ev.Key, instance: ev.Instance}
	}
	if err := admitWrites(ctx, tx, writeMetric, w, rows); err != nil {
		return err
	}
	keys := sampleKeys(evs, func(e MetricSampleWrite) string { return e.Key })
	eval, err := p.newRuleEvaluator(ctx, tx, keys)
	if err != nil {
		return err
	}
	arcs := make([]string, len(evs))
	marks := newCalcMarks()
	for i, ev := range evs {
		if _, err := ownerColumn(ev.OwnerKind); err != nil {
			return fmt.Errorf("storage: sample %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
		// The arc stores the owner's primary key. Resolving here rather than in the
		// insert means an unknown owner is a named error instead of a NULL that
		// trips the arc CHECK opaquely.
		arc, err := p.ownerArcValue(ctx, tx, ev.OwnerKind, ev.OwnerID)
		if err != nil {
			return fmt.Errorf("storage: sample %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
		arcs[i] = arc
		marks.add(ev.OwnerKind, arc, rules.LaneMetric, ev.Key)
	}
	// Marked before any sample lands, so the batch takes the pending rows'
	// locks before any series or health lock (the calc tick's order).
	if err := p.markCalcPending(ctx, tx, marks, w.rule()); err != nil {
		return err
	}
	provenance, sourceRule, sourceVersion, sourceRuleID := w.provenanceArgs()
	for i, ev := range evs {
		col, _ := ownerColumn(ev.OwnerKind)
		ts := ev.TS
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		sql := fmt.Sprintf(`insert into metric (ts, owner_kind, %s, metric_type_id, instance, value, provenance, source, source_rule, source_rule_version, source_rule_id)
			values ($1, $2, $3, (select id from metric_type where name = $4), $5, $6, $7, $8, $9, $10, $11::uuid)`, col)
		if _, err := tx.Exec(ctx, sql, ts, ev.OwnerKind, arcs[i], ev.Key, ev.Instance, ev.Value, provenance, ev.Source, sourceRule, sourceVersion, sourceRuleID); err != nil {
			return fmt.Errorf("storage: insert sample %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
		if err := eval.observe(ctx, tx, rules.LaneMetric, ev.OwnerKind, arcs[i], ev.Key, ev.Instance, ts, ev.Value); err != nil {
			return err
		}
	}
	return eval.stampJudged(ctx, tx)
}

// LatestMetric returns the most recent observed metric row for a component and
// key, or nil if none. Read helper for the component reachability panel and
// tests. Observed only (ADR-0161): a calculated or intended row beside the
// series is never read as the observation.
// componentRef is resolved once (name or uuid, ADR-0062) rather than left for
// the query to re-derive an id from a name a second row might now share (#627).
// An unknown component folds into the same nil-no-error "nothing yet" result
// the old inline subquery gave (it resolved to no id, which matched no metric
// row): this read backs the ingest-side transition guard
// (bus.dedupeProperties's LatestProperty sibling), where any error leaves a
// telemetry message unacked for redelivery, so an unrecognized owner must stay
// silent here rather than start rejecting live ingest traffic that used to be
// silently accepted as "no prior value". ErrAmbiguousName, which could not
// have occurred before scoped uniqueness exists, is not folded in: it is new
// information a caller that reaches this world needs.
func (p *PG) LatestMetric(ctx context.Context, componentRef, key string) (*MetricSample, error) {
	c, err := scopedByName(ctx, p.pool, componentConfig, componentRef)
	if errors.Is(err, ErrComponentNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var dp MetricSample
	err = p.pool.QueryRow(ctx, `
		select ts, owner_kind,
			(select m.name from metric_type m where m.id = metric.metric_type_id), instance, value, provenance, source
		from metric
		where component_id = $1::uuid
		  and metric_type_id = (select id from metric_type where name = $2)
		  and provenance = 'observed'
		order by ts desc
		limit 1`, c.ID, key).Scan(&dp.TS, &dp.OwnerKind, &dp.Key, &dp.Instance, &dp.Value, &dp.Provenance, &dp.Source)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("storage: latest metric %s/%s: %w", componentRef, key, err)
	}
	return &dp, nil
}

// LatestMetricInstance returns the most recent observed metric row for a
// component series (key + instance), or nil if none. The reachability panel's
// probe metrics (tcp-open, icmp-reachable, and their rtt/connect_time
// companions) are per-interface instance, so the layer signals must resolve one
// interface's latest value, not the newest across every interface as
// LatestMetric does. Observed only (ADR-0161): the panel reads each as the
// node's own observation, so no other provenance stands in for it.
func (p *PG) LatestMetricInstance(ctx context.Context, componentRef, key, instance string) (*MetricSample, error) {
	// See LatestMetric: an unknown component folds into nil-no-error, matching
	// the old inline subquery's silent no-match.
	c, err := scopedByName(ctx, p.pool, componentConfig, componentRef)
	if errors.Is(err, ErrComponentNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var dp MetricSample
	err = p.pool.QueryRow(ctx, `
		select ts, owner_kind,
			(select m.name from metric_type m where m.id = metric.metric_type_id), instance, value, provenance, source
		from metric
		where component_id = $1::uuid
		  and metric_type_id = (select id from metric_type where name = $2) and instance = $3
		  and provenance = 'observed'
		order by ts desc
		limit 1`, c.ID, key, instance).Scan(&dp.TS, &dp.OwnerKind, &dp.Key, &dp.Instance, &dp.Value, &dp.Provenance, &dp.Source)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("storage: latest metric %s/%s[%s]: %w", componentRef, key, instance, err)
	}
	return &dp, nil
}

// ListMetricSeries returns one series' raw samples newest first: the rows
// behind the latest value the effective read serves (#794). Raw with a hard
// cap; downsampling waits for a measured need, not a speculative one. The
// caller resolves the owner in scope first (the same non-disclosing pattern
// every lane read shares) and passes the resolved reference.
func (p *PG) ListMetricSeries(ctx context.Context, ownerKind, ownerRef, key string, window time.Duration, limit int) ([]MetricSample, error) {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	arc, err := p.ownerArcValue(ctx, p.pool, ownerKind, ownerRef)
	if err != nil {
		return nil, err
	}
	bound, args := tsSince("ts", window, arc, key, limit)
	rows, err := p.pool.Query(ctx, fmt.Sprintf(`
		select ts, owner_kind,
			(select m.name from metric_type m where m.id = metric.metric_type_id), instance, value, provenance, source
		from metric
		where %s = $1::uuid
		  and metric_type_id = (select id from metric_type where name = $2)
		  and %s
		order by ts desc
		limit $3`, col, bound), args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list metric series %s/%s: %w", ownerRef, key, err)
	}
	defer rows.Close()
	var out []MetricSample
	for rows.Next() {
		var dp MetricSample
		if err := rows.Scan(&dp.TS, &dp.OwnerKind, &dp.Key, &dp.Instance, &dp.Value, &dp.Provenance, &dp.Source); err != nil {
			return nil, fmt.Errorf("storage: scan metric series: %w", err)
		}
		out = append(out, dp)
	}
	return out, rows.Err()
}
