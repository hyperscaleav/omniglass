package storage

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
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
	write := func() error {
		return p.sampleWrite(ctx, "insert samples", func(tx pgx.Tx) error { return p.writeMetricSamples(ctx, tx, w, evs) })
	}
	err := write()
	if isPartitionRoutingMiss(err) {
		if perr := p.landMetricMonths(ctx, evs); perr != nil {
			return perr
		}
		err = write()
	}
	return err
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
// keys judge every sample, in the batch's order (#919), after it lands: a
// series the batch carries once lands with the others in one statement, a
// series it carries more than once one row at a time (ADR-1274). The calc
// rules reading the keys are marked pending on each owner (#996), never the
// rule that produced the batch.
func (p *PG) writeMetricSamples(ctx context.Context, tx pgx.Tx, w KeyWriter, evs []MetricSampleWrite) error {
	evs = cleanMetricWrites(evs)
	arcs := make([]string, len(evs))
	rows := make([]keyedWrite, len(evs))
	marks := newCalcMarks()
	for i, ev := range evs {
		if _, err := ownerColumn(ev.OwnerKind); err != nil {
			return fmt.Errorf("storage: sample %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
		// The arc stores the owner's primary key. Resolving here rather than in the
		// insert means an unknown owner is a named error instead of a NULL that
		// trips the arc CHECK opaquely, and the writer check holds the row to
		// the owner it will land on.
		arc, err := p.ownerArcValue(ctx, tx, ev.OwnerKind, ev.OwnerID)
		if err != nil {
			return fmt.Errorf("storage: sample %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
		arcs[i] = arc
		rows[i] = keyedWrite{key: ev.Key, instance: ev.Instance, ownerKind: ev.OwnerKind, owner: arc}
		marks.add(ev.OwnerKind, arc, rules.LaneMetric, ev.Key)
	}
	if err := admitWrites(ctx, tx, writeMetric, w, rows); err != nil {
		return err
	}
	keys := sampleKeys(evs, func(e MetricSampleWrite) string { return e.Key })
	eval, err := p.newRuleEvaluator(ctx, tx, keys)
	if err != nil {
		return err
	}
	// Marked before any sample lands, so the batch takes the pending rows'
	// locks before any series or health lock (the calc tick's order).
	if err := p.markCalcPending(ctx, tx, marks, w.rule()); err != nil {
		return err
	}
	provenance, sourceRule, sourceVersion, sourceRuleID := w.provenanceArgs()
	clock := sampleClock{q: tx}
	stamps := make([]time.Time, len(evs))
	for i, ev := range evs {
		ts := ev.TS
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		if stamps[i], err = clock.cap(ctx, ev.Key, ts); err != nil {
			return err
		}
	}
	// A series the batch carries once lands with every other such series in
	// one statement (#1274). A series it carries more than once keeps the
	// per-sample order below, insert then judge, so judging a sample never
	// sees a later sample of its own series from the same batch.
	carried := make(map[string]int, len(evs))
	for i, ev := range evs {
		carried[metricSeriesKey(ev, arcs[i])]++
	}
	once := make([]int, 0, len(evs))
	for i, ev := range evs {
		if carried[metricSeriesKey(ev, arcs[i])] == 1 {
			once = append(once, i)
		}
	}
	// metricInsertSQL lands metric samples in one statement: $1..$7 are the
	// samples' ts, owner kinds, owner ids, type names, instances, values and
	// sources, in the batch's order; $8 the provenance, $9..$11 a calculated
	// sample's rule lineage, $12 the collecting node (null for none).
	//
	// Each sample's series is found by its key, or started in the same statement
	// when the batch is its first sample. A series another transaction started
	// and has not committed is not seen: the insert of the series then waits on
	// it and conflicts, the sample finds no series, and the statement fails on
	// series_id's NOT NULL, as it does for an unknown type name. The ingest
	// redelivers such a batch (IsRefusedWrite), which finds the series committed.
	const metricInsertSQL = `with u as (
		select u.ts, u.owner_kind, u.owner::uuid as owner, mt.id as type_id, u.instance, u.value, u.source, u.ord
		from unnest($1::timestamptz[], $2::text[], $3::text[], $4::text[], $5::text[], $6::float8[], $7::text[])
			with ordinality as u(ts, owner_kind, owner, key, instance, value, source, ord)
		left join metric_type mt on mt.name = u.key
	),
	k as (select distinct type_id, owner_kind, owner, instance, source from u where type_id is not null),
	known as (
		select s.id, s.owner_id, s.metric_type_id, s.instance, s.source
		from k join metric_series s on s.owner_id = k.owner and s.metric_type_id = k.type_id
			and s.instance = k.instance and s.provenance = $8::text and s.source = k.source
			and s.collector_node_id is not distinct from $12::uuid
	),
	fresh as (
		insert into metric_series (metric_type_id, owner_kind, component_id, system_id, location_id, node_id,
			instance, provenance, source, collector_node_id)
		select k.type_id, k.owner_kind,
			case when k.owner_kind = 'component' then k.owner end,
			case when k.owner_kind = 'system' then k.owner end,
			case when k.owner_kind = 'location' then k.owner end,
			case when k.owner_kind = 'node' then k.owner end,
			k.instance, $8::text, k.source, $12::uuid
		from k
		where not exists (select 1 from known n where n.owner_id = k.owner and n.metric_type_id = k.type_id
			and n.instance = k.instance and n.source = k.source)
		on conflict do nothing
		returning id, owner_id, metric_type_id, instance, source
	),
	series as (select * from known union all select * from fresh)
	insert into metric (series_id, ts, value, source_rule, source_rule_version, source_rule_id)
	select s.id, u.ts, u.value, $9::text, $10::bigint, $11::uuid
	from u left join series s on s.owner_id = u.owner and s.metric_type_id = u.type_id
		and s.instance = u.instance and s.source = u.source
	order by u.ord`
	// insert lands the samples at idx in one statement, in their order, so the
	// rows take ids in the order the batch carried them (metricInsertSQL: each
	// sample's series is found, or started, in the same statement). An unknown
	// type name finds no series and fails the insert on series_id's NOT NULL,
	// a failure the ingest redelivers rather than refuses (IsRefusedWrite). A
	// closure, so the write stays inside this admitted write path.
	collector := w.collector()
	insert := func(idx []int) error {
		if len(idx) == 0 {
			return nil
		}
		ts := make([]time.Time, len(idx))
		kinds := make([]string, len(idx))
		owners := make([]string, len(idx))
		keys := make([]string, len(idx))
		instances := make([]string, len(idx))
		values := make([]float64, len(idx))
		sources := make([]string, len(idx))
		for j, i := range idx {
			ts[j], kinds[j], owners[j], keys[j] = stamps[i], evs[i].OwnerKind, arcs[i], evs[i].Key
			instances[j], values[j], sources[j] = evs[i].Instance, evs[i].Value, evs[i].Source
		}
		_, err := tx.Exec(ctx, metricInsertSQL,
			ts, kinds, owners, keys, instances, values, sources, provenance, sourceRule, sourceVersion, sourceRuleID, collector)
		if err != nil {
			named := make([]string, 0, len(idx))
			for _, i := range idx {
				named = append(named, evs[i].OwnerID+"/"+evs[i].Key)
			}
			return fmt.Errorf("storage: insert samples %s: %w", strings.Join(named, ", "), err)
		}
		return nil
	}
	if err := insert(once); err != nil {
		return err
	}
	for i, ev := range evs {
		ts := stamps[i]
		if carried[metricSeriesKey(ev, arcs[i])] > 1 {
			if err := insert([]int{i}); err != nil {
				return err
			}
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
// row), as LatestProperty does: a panel asking about a component with no
// readings shows none rather than an error. ErrAmbiguousName, which could not
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
		select n.ts, s.owner_kind, mt.name, s.instance, n.value, s.provenance, s.source
		from metric_series s
		join metric_type mt on mt.id = s.metric_type_id
		cross join lateral (`+metricNewestSQL("s.id")+`) n
		where s.owner_id = $1::uuid and s.owner_kind = 'component' and mt.name = $2 and s.provenance = 'observed'
		order by n.ts desc, n.id desc
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
		select n.ts, s.owner_kind, mt.name, s.instance, n.value, s.provenance, s.source
		from metric_series s
		join metric_type mt on mt.id = s.metric_type_id
		cross join lateral (`+metricNewestSQL("s.id")+`) n
		where s.owner_id = $1::uuid and s.owner_kind = 'component' and mt.name = $2 and s.instance = $3
		  and s.provenance = 'observed'
		order by n.ts desc, n.id desc
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
	if _, err := ownerColumn(ownerKind); err != nil {
		return nil, err
	}
	arc, err := p.ownerArcValue(ctx, p.pool, ownerKind, ownerRef)
	if err != nil {
		return nil, err
	}
	// Every provenance, as the series view shows it: the hot tier's observed
	// and calculated samples and the declared tier's intended rows.
	bound, args := tsSince("x.ts", window, arc, key, limit, ownerKind)
	rows, err := p.pool.Query(ctx, fmt.Sprintf(`
		select x.ts, s.owner_kind, mt.name, s.instance, x.value, s.provenance, s.source
		from metric_series s
		join metric_type mt on mt.id = s.metric_type_id
		cross join lateral (
			select m.ts, m.id, m.value from metric m where m.series_id = s.id
			union all
			select d.ts, d.id, d.value from metric_declared d where d.series_id = s.id) x
		where s.owner_id = $1::uuid and s.owner_kind = $4 and mt.name = $2
		  and %s
		order by x.ts desc
		limit $3`, bound), args...)
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

// metricSeriesKey names a sample's series within one batch: its owner, key and
// instance (the batch's writer fixes the provenance).
func metricSeriesKey(ev MetricSampleWrite, arc string) string {
	return ev.OwnerKind + "\x00" + arc + "\x00" + ev.Key + "\x00" + ev.Instance
}

// metricNewestSQL is one metric series' newest hot-tier sample (ts, id, value
// and the lineage columns), series an SQL expression (a lateral's column): an
// index probe of metric_series_ts_idx in each month, newest month first,
// stopping at the first month that holds one (TestMetricNewestReadsTheNewestMonthFirst).
func metricNewestSQL(series string) string {
	return `select m.ts, m.id, m.value, m.source_rule, m.source_rule_version, m.source_rule_id
		from metric m where m.series_id = ` + series + `
		order by m.ts desc, m.id desc limit 1`
}
