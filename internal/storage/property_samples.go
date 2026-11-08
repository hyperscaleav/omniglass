package storage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
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
	return p.sampleWrite(ctx, "insert property samples", func(ctx context.Context, tx pgx.Tx) error { return p.writePropertySamples(ctx, tx, w, evs, false) })
}

// propertySet is one writer's property samples in a multi-set write
// (metricSet).
type propertySet struct {
	w   KeyWriter
	evs []PropertySampleWrite
}

// writePropertySamples is the property lane's sample-write path for one
// writer, in the caller's transaction (writePropertySets).
func (p *PG) writePropertySamples(ctx context.Context, tx pgx.Tx, w KeyWriter, evs []PropertySampleWrite, transitionOnly bool) error {
	return p.writePropertySets(ctx, tx, []propertySet{{w: w, evs: evs}}, transitionOnly)
}

// propertyOwnerKinds is the order a property batch lands its owner kinds in,
// one statement each (writePropertySets).
var propertyOwnerKinds = []string{"component", "system", "location", "node"}

// writePropertySets is the property lane's sample-write path, in the
// caller's transaction: the ingest's observed batch or a calc rule's
// calculated output (a calc rule's writer), as writeMetricSets is for
// metrics, each set held to its keys' writer class under its own writer
// first and every set sharing one row identity. An observation may also
// settle a pending command.
//
// transitionOnly is the ingest's transition guard (#455, ADR-0311), held in
// the insert itself: a row whose value equals its series' latest is not
// written, and a row not written settles nothing and is judged by no rule.
// The comparison is a conditional insert, and each series it touches is
// locked first (lockPropertySeries), so the latest it compares against is
// the committed one: two batches carrying one unchanged value, landed at
// once, write it once, however the consumer dispatches.
//
// A series the batch carries once lands with every other such series of its
// owner kind in one statement (#1582), so a tick of many task groups costs
// the statements of one; a series it carries more than once lands one row at
// a time, in order, so each row's guard reads the row before it.
func (p *PG) writePropertySets(ctx context.Context, tx pgx.Tx, sets []propertySet, transitionOnly bool) error {
	w, err := sharedWriter(writeProperty, sets, func(s propertySet) KeyWriter { return s.w })
	if err != nil {
		return err
	}
	var evs []PropertySampleWrite
	var arcs []string
	marks := newCalcMarks()
	for _, set := range sets {
		setEvs := cleanPropertyWrites(set.evs)
		rows := make([]keyedWrite, len(setEvs))
		for i, ev := range setEvs {
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
			arcs = append(arcs, arc)
			rows[i] = keyedWrite{key: ev.Key, instance: ev.Instance, ownerKind: ev.OwnerKind, owner: arc}
			marks.add(ev.OwnerKind, arc, rules.LaneProperty, ev.Key)
		}
		// Every row is held to its key's writer class, and to the owner it will
		// land on, before anything lands.
		if err := admitWrites(ctx, tx, writeProperty, set.w, rows); err != nil {
			return err
		}
		evs = append(evs, setEvs...)
	}
	if len(evs) == 0 {
		return nil
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
	eval, err := p.newRuleEvaluator(ctx, tx, sampleKeys(evs, func(e PropertySampleWrite) string { return e.Key }))
	if err != nil {
		return err
	}
	// Marked before any sample lands (writeMetricSets says why).
	if err := p.markCalcPending(ctx, tx, marks, w.rule()); err != nil {
		return err
	}
	provenance, sourceRule, sourceVersion, sourceRuleID := w.provenanceArgs()
	clock := sampleClock{q: tx, now: now}
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
	carried := make(map[string]int, len(evs))
	for i, ev := range evs {
		carried[propertySeriesLockKey(ev.OwnerKind, arcs[i], ev.Key, ev.Instance)]++
	}
	// insert lands the rows at idx, all of one owner kind and each of its own
	// series, in one statement, and marks each row it wrote in landed. The
	// transition guard is the insert's own condition: the series' latest row
	// (ts, then id, the latest-value read's order) holding the row's value
	// means it is not written. An unknown type name fails the insert on
	// property_type_id's NOT NULL, as one row's did.
	landed := make([]bool, len(evs))
	insert := func(kind string, idx []int) error {
		if len(idx) == 0 {
			return nil
		}
		col, _ := ownerColumn(kind)
		guard := ""
		if transitionOnly {
			guard = fmt.Sprintf(`
			where not exists (
				select 1 from (
					select value from property
					where owner_kind = $7 and %s = u.owner
					  and property_type_id = u.type_id
					  and instance = u.instance and provenance = $8
					order by ts desc, id desc
					limit 1) latest
				where latest.value = to_jsonb(u.value))`, col)
		}
		sql := fmt.Sprintf(`with u as (
				select u.ts, u.owner::uuid as owner, pt.id as type_id, u.instance, u.value, u.source, u.ord
				from unnest($1::timestamptz[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[])
					with ordinality as u(ts, owner, key, instance, value, source, ord)
				left join property_type_in_effect() pt on pt.name = u.key
			),
			ins as (
				insert into property (ts, owner_kind, %[1]s, property_type_id, instance, value, provenance, source, source_rule, source_rule_version, source_rule_id, source_conduit_id)
				select u.ts, $7::text, u.owner, u.type_id, u.instance, to_jsonb(u.value),
					$8::text, u.source, $9::text, $10::bigint, $11::uuid, $12::uuid
				from u%[2]s
				order by u.ord
				returning %[1]s as owner, property_type_id, instance)
			select u.ord from ins join u on u.owner = ins.owner and u.type_id = ins.property_type_id and u.instance = ins.instance`, col, guard)
		ts := make([]time.Time, len(idx))
		owners := make([]string, len(idx))
		keys := make([]string, len(idx))
		instances := make([]string, len(idx))
		values := make([]string, len(idx))
		sources := make([]string, len(idx))
		for j, i := range idx {
			ts[j], owners[j], keys[j] = stamps[i], arcs[i], evs[i].Key
			instances[j], values[j], sources[j] = evs[i].Instance, evs[i].Value, evs[i].Source
		}
		rows, err := tx.Query(ctx, sql, ts, owners, keys, instances, values, sources, kind, provenance, sourceRule, sourceVersion, sourceRuleID, w.sourceConduit())
		if err == nil {
			for rows.Next() {
				var ord int64
				if err = rows.Scan(&ord); err != nil {
					break
				}
				landed[idx[ord-1]] = true
			}
			rows.Close()
			if err == nil {
				err = rows.Err()
			}
		}
		if err != nil {
			named := make([]string, 0, len(idx))
			for _, i := range idx {
				named = append(named, evs[i].OwnerID+"/"+evs[i].Key)
			}
			return fmt.Errorf("storage: insert property samples %s: %w", strings.Join(named, ", "), err)
		}
		return nil
	}
	for _, kind := range propertyOwnerKinds {
		var once []int
		for i, ev := range evs {
			if ev.OwnerKind == kind && carried[propertySeriesLockKey(ev.OwnerKind, arcs[i], ev.Key, ev.Instance)] == 1 {
				once = append(once, i)
			}
		}
		if err := insert(kind, once); err != nil {
			return err
		}
	}
	// An observation may be what a pending command was waiting on: the owners
	// with an issued command, read once for the batch, are the only ones worth
	// settling on.
	settles := map[int]bool{}
	{
		var kinds, owners []string
		var at []int
		for i, ev := range evs {
			if targeted[ev.Key] {
				kinds, owners, at = append(kinds, ev.OwnerKind), append(owners, arcs[i]), append(at, i)
			}
		}
		if len(at) > 0 {
			rows, err := tx.Query(ctx, `select distinct t.ord from unnest($1::text[], $2::text[]) with ordinality as t(kind, arc, ord)
				where exists (select 1 from command c where c.status = 'issued' and c.owner_kind = t.kind
					and coalesce(c.component_id, c.system_id, c.location_id, c.node_id) = t.arc::uuid)`, kinds, owners)
			if err != nil {
				return fmt.Errorf("storage: owners awaiting a command: %w", err)
			}
			for rows.Next() {
				var ord int64
				if err := rows.Scan(&ord); err != nil {
					rows.Close()
					return err
				}
				settles[at[ord-1]] = true
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return fmt.Errorf("storage: owners awaiting a command: %w", err)
			}
		}
	}
	for i, ev := range evs {
		if carried[propertySeriesLockKey(ev.OwnerKind, arcs[i], ev.Key, ev.Instance)] > 1 {
			if err := insert(ev.OwnerKind, []int{i}); err != nil {
				return err
			}
		}
		if !landed[i] {
			continue // an unchanged value: the series is transition-only
		}
		if settles[i] {
			if err := p.settleOnObservation(ctx, tx, ev.OwnerKind, arcs[i], ev.Key, ev.Instance, now); err != nil {
				return err
			}
		}
		if err := eval.observe(ctx, tx, rules.LaneProperty, ev.OwnerKind, arcs[i], ev.Key, ev.Instance, stamps[i], ev.Value); err != nil {
			return err
		}
	}
	return eval.stampJudged(ctx, tx)
}

// lockPropertySeries takes the transition lock of every property series the
// batch writes, for the rest of the caller's transaction, in sorted order (so
// two batches over overlapping series never deadlock on each other), after
// the batch's rule rows and marks (LandBatch takes them first) and before any
// series or health lock its judging takes, in one statement however many
// series. It is what makes the guard's compare-then-insert atomic per series
// (lockAdvisory): a second batch writing the series waits here, and its
// conditional insert then reads the first one's committed row rather than a
// snapshot that predates it. Only the ingest's guarded write takes it.
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
	if len(keys) == 0 {
		return nil
	}
	slices.Sort(keys)
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended(t.k, 0))
		from unnest($1::text[]) with ordinality as t(k, ord) order by t.ord`, keys); err != nil {
		return fmt.Errorf("storage: lock property series: %w", err)
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
