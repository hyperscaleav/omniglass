package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// InsertPropertySamples writes observed property rows in one transaction. Each
// row sets exactly its owner arc column (the CHECK enforces the rest) and
// provenance observed. Callers apply reject-not-project (collection.Registry)
// and the transition-only guard before calling; this is the durable write.
func (p *PG) InsertPropertySamples(ctx context.Context, evs []PropertySampleWrite) error {
	if len(evs) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin insert property samples: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, ev := range evs {
		col, err := ownerColumn(ev.OwnerKind)
		if err != nil {
			return fmt.Errorf("storage: property sample %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
		ts := ev.TS
		if ts.IsZero() {
			ts = time.Now().UTC()
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
		sql := fmt.Sprintf(`insert into property (ts, owner_kind, %s, property_type_id, instance, value, provenance, source)
			values ($1, $2, $3, (select id from property_type where name = $4), $5, to_jsonb($6::text), 'observed', $7)`, col)
		if _, err := tx.Exec(ctx, sql, ts, ev.OwnerKind, arc, ev.Key, ev.Instance, ev.Value, ev.Source); err != nil {
			return fmt.Errorf("storage: insert property sample %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit insert property samples: %w", err)
	}
	return nil
}

// LatestProperty returns the most recent property row for a component series
// (key + instance), or nil if none. It backs the ingest-side transition guard
// (skip a write whose value equals the latest stored value) and the
// reachability panel.
//
// ts orders it, because an observed series carries the OBSERVATION time and a
// late arrival must not displace a newer reading. id breaks the tie: a poll cycle
// stamping several rows in the same instant would otherwise resolve to an
// arbitrary one, and the transition guard would compare against a row that is not
// the current value.
// componentRef is resolved once (name or uuid, ADR-0062). An unknown
// component folds into the same nil-no-error "nothing yet" result the old
// inline subquery gave, because this read backs the ingest-side transition
// guard (bus.dedupeProperties): any error there leaves a telemetry message
// unacked for redelivery, so an unrecognized owner must stay silent rather
// than start rejecting live traffic the old code silently accepted.
// ErrAmbiguousName, which could not have occurred before scoped uniqueness
// exists (#627), is not folded in.
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
			(select p.name from property_type p where p.id = property.property_type_id), instance, value #>> '{}', provenance, source
		from property
		where component_id = $1::uuid
		  and property_type_id = (select id from property_type where name = $2) and instance = $3
		order by ts desc, id desc
		limit 1`, c.ID, key, instance).Scan(&dp.TS, &dp.OwnerKind, &dp.Key, &dp.Instance, &dp.Value, &dp.Provenance, &dp.Source)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("storage: latest property %s/%s[%s]: %w", componentRef, key, instance, err)
	}
	return &dp, nil
}

// PropertyTransitions returns a component series' property rows inside window,
// ordered oldest-first: the ordered flip sequence the availability strip reads
// (each row is one transition, since the write path is transition-only). The
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
			(select p.name from property_type p where p.id = property.property_type_id), instance, value #>> '{}', provenance, source
		from property
		where component_id = $1::uuid
		  and property_type_id = (select id from property_type where name = $2) and instance = $3 and %s
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
			(select pt.name from property_type pt where pt.id = property.property_type_id), instance, value #>> '{}', provenance, source
		from property
		where %s = $1::uuid
		  and property_type_id = (select id from property_type where name = $2)
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
