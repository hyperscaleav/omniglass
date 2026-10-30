package storage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/hyperscaleav/omniglass/internal/rules"

	"github.com/jackc/pgx/v5"
)

// PropertySampleWrite is one observed property verdict to persist. OwnerKind
// picks the arc; OwnerID is the fleet address (component/system/location/node
// name); Instance discriminates many verdicts of one key on one owner (the
// interface name for interface-reachable). Value is the categorical text
// (up/down), the mirror of MetricSampleWrite but with a value domain, not a
// number; the column stores it as its jsonb encoding.
type PropertySampleWrite struct {
	OwnerKind string
	OwnerID   string
	// Namespace is read as MetricSampleWrite.Namespace.
	Namespace string
	Key       string
	Instance  string
	Value     string
	Source    string
	TS        time.Time
}

// PropertySample is a stored observed/derived property row (read side). Value
// is the text scalar the jsonb column carries for this lane's categorical
// verdicts.
type PropertySample struct {
	TS         time.Time
	OwnerKind  string
	Key        string
	Instance   string
	Value      string
	Provenance string
	Source     string
}

// InsertPropertySamples writes observed property rows in one transaction, for
// w: a node's lane (NodeWriter) or the push lane (PushWriter). Each row sets
// exactly its owner arc column (the CHECK enforces the rest) and provenance
// observed. Callers apply reject-not-project (collection.Registry) and the
// transition-only guard before calling; this is the durable write, and it
// refuses the whole batch, writing nothing, when a key's writer class does not
// admit w (ADR-0161).
func (p *PG) InsertPropertySamples(ctx context.Context, w KeyWriter, evs []PropertySampleWrite) error {
	if len(evs) == 0 {
		return nil
	}
	return p.sampleWrite(ctx, "insert property samples", func(tx pgx.Tx) error { return p.writePropertySamples(ctx, tx, w, evs, false) })
}

// writePropertySamples is the property lane's sample-write path, in the
// caller's transaction: the ingest's observed batch or a calc rule's
// calculated output (a calc rule's writer), as writeMetricSamples is for
// metrics, held to each key's writer class first. An observation may also
// settle a pending command.
//
// transitionOnly is the ingest's transition guard (#455, ADR-0311), held in
// the insert itself: a row whose value equals its series' latest is not
// written, and a row not written settles nothing and is judged by no rule.
// The comparison is a conditional insert, and each series it touches is
// locked first (lockPropertySeries), so the latest it compares against is
// the committed one: two batches carrying one unchanged value, landed at
// once, write it once, however the consumer dispatches.
func (p *PG) writePropertySamples(ctx context.Context, tx pgx.Tx, w KeyWriter, evs []PropertySampleWrite, transitionOnly bool) error {
	evs = cleanPropertyWrites(evs)
	arcs := make([]string, len(evs))
	rows := make([]keyedWrite, len(evs))
	marks := newCalcMarks()
	for i, ev := range evs {
		if _, err := ownerColumn(ev.OwnerKind); err != nil {
			return fmt.Errorf("storage: property sample %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
		// The arc stores the owner's primary key. Resolving here, once per
		// row (mirrors InsertMetricSamples/InsertEvents/InsertLogLines),
		// rather than through the shared ownerArcExprN's inline subquery,
		// means an unknown owner is this same named error instead of a NULL
		// that trips the arc CHECK opaquely, AND a component/system/location
		// owner whose name now collides with another row (#627) fails
		// cleanly here rather than raising SQLSTATE 21000 on ingest, the
		// collection hot path.
		arc, err := p.ownerArcValue(ctx, tx, ev.OwnerKind, ev.OwnerID)
		if err != nil {
			return fmt.Errorf("storage: property sample %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
		arcs[i] = arc
		rows[i] = keyedWrite{namespace: ev.Namespace, key: ev.Key, instance: ev.Instance, ownerKind: ev.OwnerKind, owner: arc}
		if ev.Namespace == "" {
			marks.add(ev.OwnerKind, arc, rules.LaneProperty, ev.Key)
		}
	}
	// Every row is held to its key's writer class, and to the owner it will
	// land on, before anything lands.
	if err := admitWrites(ctx, tx, writeProperty, w, rows); err != nil {
		return err
	}
	if transitionOnly {
		if err := lockPropertySeries(ctx, tx, evs, arcs); err != nil {
			return err
		}
	}
	// Settlement only cares about properties a command type targets; learn
	// which once per batch, and read the transaction's clock once.
	now, err := dbNow(ctx, tx)
	if err != nil {
		return err
	}
	targeted, err := commandTargetedProperties(ctx, tx)
	if err != nil {
		return err
	}
	// The event rules reading this batch's keys judge each sample as it lands
	// (#919), in this transaction.
	eval, err := p.newRuleEvaluator(ctx, tx, sampleKeys(bareWrites(evs, func(e PropertySampleWrite) string { return e.Namespace }), func(e PropertySampleWrite) string { return e.Key }))
	if err != nil {
		return err
	}
	// Marked before any sample lands (writeMetricSamples says why).
	if err := p.markCalcPending(ctx, tx, marks, w.rule()); err != nil {
		return err
	}
	provenance, sourceRule, sourceVersion, sourceRuleID := w.provenanceArgs()
	clock := sampleClock{q: tx, now: now}
	for i, ev := range evs {
		col, _ := ownerColumn(ev.OwnerKind)
		ts := ev.TS
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		if ts, err = clock.cap(ctx, ev.Key, ts); err != nil {
			return err
		}
		// The transition guard is the insert's own condition: the series'
		// latest row (ts, then id, the latest-value read's order) holding this
		// value means nothing is written.
		guard := ""
		if transitionOnly {
			guard = fmt.Sprintf(`
			where not exists (
				select 1 from (
					select value from property
					where owner_kind = $2 and %s = $3
					  and property_type_id = `+propertyTypeIn+`
					  and instance = $5 and provenance = $7
					order by ts desc, id desc
					limit 1) latest
				where latest.value = to_jsonb($6::text))`, col)
		}
		sql := fmt.Sprintf(`insert into property (ts, owner_kind, %s, property_type_id, instance, value, provenance, source, source_rule, source_rule_version, source_rule_id, source_conduit_id)
			select $1::timestamptz, $2::text, $3::uuid, `+propertyTypeIn+`, $5::text, to_jsonb($6::text),
				$7::text, $8::text, $9::text, $10::bigint, $11::uuid, $12::uuid%s
			returning id`, col, guard)
		var id int64
		err := tx.QueryRow(ctx, sql, ts, ev.OwnerKind, arcs[i], ev.Key, ev.Instance, ev.Value, provenance, ev.Source, sourceRule, sourceVersion, sourceRuleID, w.sourceConduit(), ev.Namespace).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // an unchanged value: the series is transition-only
		} else if err != nil {
			return fmt.Errorf("storage: insert property sample %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
		// A qualified row is one no rule and no command type addresses: each
		// reads the key by its bare name, which reaches another row.
		if ev.Namespace != "" {
			continue
		}
		// An observation may be what a pending command was waiting on.
		if targeted[ev.Key] {
			if err := p.settleOnObservation(ctx, tx, ev.OwnerKind, arcs[i], ev.Key, ev.Instance, now); err != nil {
				return err
			}
		}
		if err := eval.observe(ctx, tx, rules.LaneProperty, ev.OwnerKind, arcs[i], ev.Key, ev.Instance, ts, ev.Value); err != nil {
			return err
		}
	}
	return eval.stampJudged(ctx, tx)
}

// propertyTypeIn is the property write's type id: $4 read along the search
// path when the namespace $13 is empty, else in $13 only (#1433).
const propertyTypeIn = `coalesce(
	(select id from property_type_in_effect() where name = $4::text and $13::text = ''),
	(select id from property_type where namespace = $13::text and name = $4::text and $13::text <> ''))`

// lockPropertySeries takes the transition lock of every property series the
// batch writes, for the rest of the caller's transaction, in sorted order (so
// two batches over overlapping series never deadlock on each other), after
// the batch's rule rows and marks (LandBatch takes them first) and before any
// series or health lock its judging takes. It is what makes the
// guard's compare-then-insert atomic per series (lockAdvisory): a second
// batch writing the series waits here, and its conditional insert then reads
// the first one's committed row rather than a snapshot that predates it.
// Only the ingest's guarded write takes it.
func lockPropertySeries(ctx context.Context, tx pgx.Tx, evs []PropertySampleWrite, arcs []string) error {
	keys := make([]string, 0, len(evs))
	seen := make(map[string]bool, len(evs))
	for i, ev := range evs {
		k := propertySeriesLockKey(ev.OwnerKind, arcs[i], ev.Key, ev.Instance)
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		if err := lockAdvisory(ctx, tx, k); err != nil {
			return err
		}
	}
	return nil
}

// propertySeriesLockKey is the string one property series' transition lock
// hashes: `property/<kind>/<owner id>/<type>/<instance>`. The owner is its id,
// never its name, so two owners sharing a name never share a lock (#627).
func propertySeriesLockKey(ownerKind, ownerID, key, instance string) string {
	return "property/" + ownerKind + "/" + ownerID + "/" + key + "/" + instance
}

// LatestProperty returns the most recent observed property row for a component
// series (key + instance), or nil if none. It backs the reachability panel and
// session pill, and tests. Observed only (ADR-0161): the panel reads each
// value as the node's own observation, so a declared, intended or calculated
// row beside it never stands in for the node's. (The ingest's transition guard
// is the insert's own condition, writePropertySamples, and reads no row here.)
//
// ts orders it, because an observed series carries the OBSERVATION time and a
// late arrival must not displace a newer reading. id breaks the tie: a poll cycle
// stamping several rows in the same instant would otherwise resolve to an
// arbitrary one, a row that is not the current value (the guard's own order).
// componentRef is resolved once (name or uuid, ADR-0062). An unknown
// component folds into the same nil-no-error "nothing yet" result the old
// inline subquery gave: a panel asking about a component with no readings
// shows none rather than an error. ErrAmbiguousName, which could not have
// occurred before scoped uniqueness exists (#627), is not folded in.
func (p *PG) LatestProperty(ctx context.Context, componentRef, key, instance string) (*PropertySample, error) {
	c, err := scopedByName(ctx, p.pool, componentConfig, componentRef)
	if errors.Is(err, ErrComponentNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var dp PropertySample
	err = p.pool.QueryRow(ctx, `
		select ts, owner_kind,
			(select p.name from property_type p where p.id = property.property_type_id), instance, value #>> '{}', provenance, `+sourceRendered("property")+`
		from property
		where component_id = $1::uuid
		  and property_type_id = (select id from property_type_in_effect() property_type where name = $2) and instance = $3
		  and provenance = 'observed'
		order by ts desc, id desc
		limit 1`, c.ID, key, instance).Scan(&dp.TS, &dp.OwnerKind, &dp.Key, &dp.Instance, &dp.Value, &dp.Provenance, &dp.Source)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("storage: latest property %s/%s[%s]: %w", componentRef, key, instance, err)
	}
	return &dp, nil
}

// PropertyTransitions returns a component series' observed property rows
// inside window, ordered oldest-first: the ordered flip sequence the
// availability strip reads (each row is one transition, since the write path is
// transition-only), the node's observations alone (ADR-0161). The
// window is counted back from the DATABASE's clock (tsSince, #719); a zero
// window returns the whole series.
// componentRef is resolved once, and an unknown component folds into a
// nil-no-error empty result (see LatestProperty's comment: this backs the
// same ingest path in spirit, and the old inline subquery's silent no-match
// is worth preserving exactly here too).
func (p *PG) PropertyTransitions(ctx context.Context, componentRef, key, instance string, window time.Duration) ([]PropertySample, error) {
	c, err := scopedByName(ctx, p.pool, componentConfig, componentRef)
	if errors.Is(err, ErrComponentNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	bound, args := tsSince("ts", window, c.ID, key, instance)
	rows, err := p.pool.Query(ctx, fmt.Sprintf(`
		select ts, owner_kind,
			(select p.name from property_type p where p.id = property.property_type_id), instance, value #>> '{}', provenance, `+sourceRendered("property")+`
		from property
		where component_id = $1::uuid
		  and property_type_id = (select id from property_type_in_effect() property_type where name = $2) and instance = $3
		  and provenance = 'observed' and %s
		order by ts asc`, bound), args...)
	if err != nil {
		return nil, fmt.Errorf("storage: property transitions %s/%s[%s]: %w", componentRef, key, instance, err)
	}
	defer rows.Close()
	var out []PropertySample
	for rows.Next() {
		var dp PropertySample
		if err := rows.Scan(&dp.TS, &dp.OwnerKind, &dp.Key, &dp.Instance, &dp.Value, &dp.Provenance, &dp.Source); err != nil {
			return nil, fmt.Errorf("storage: scan property transition %s/%s[%s]: %w", componentRef, key, instance, err)
		}
		out = append(out, dp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate property transitions %s/%s[%s]: %w", componentRef, key, instance, err)
	}
	return out, nil
}

// ListPropertySeries returns one property series' change history newest
// first: the samples behind the effective value (#794). Mirrors
// ListMetricSeries over the property lane.
func (p *PG) ListPropertySeries(ctx context.Context, ownerKind, ownerRef, key string, window time.Duration, limit int) ([]PropertySample, error) {
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
			(select pt.name from property_type pt where pt.id = property.property_type_id), instance, value #>> '{}', provenance, `+sourceRendered("property")+`
		from property
		where %s = $1::uuid
		  and property_type_id = (select id from property_type_in_effect() property_type where name = $2)
		  and %s
		order by ts desc
		limit $3`, col, bound), args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list property series %s/%s: %w", ownerRef, key, err)
	}
	defer rows.Close()
	var out []PropertySample
	for rows.Next() {
		var dp PropertySample
		if err := rows.Scan(&dp.TS, &dp.OwnerKind, &dp.Key, &dp.Instance, &dp.Value, &dp.Provenance, &dp.Source); err != nil {
			return nil, fmt.Errorf("storage: scan property series: %w", err)
		}
		out = append(out, dp)
	}
	return out, rows.Err()
}
