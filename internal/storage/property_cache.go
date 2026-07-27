package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
)

// PropertyUpsert is one producer value to land in the latest-value cache: the
// newest value of a series (owner, property_type, instance, provenance). The
// firehose passes provenance observed; a command (#396) passes intended. TS is the
// value's own time (the sample time for observed, the effect time for intended),
// which the out-of-order guard compares, not the row-write time.
type PropertyUpsert struct {
	OwnerKind  string
	OwnerID    string
	Key        string
	Instance   string
	Provenance string
	Value      json.RawMessage
	TS         time.Time
}

// CachedValue is one row of the latest-value cache (read side): the current value
// of a series, its own time, and which provenance produced it. Distinct from
// Property (the declared value store's row shape) so the declared path is untouched.
type CachedValue struct {
	Value      json.RawMessage
	TS         time.Time
	Provenance string
}

// PropertyReconciliation is one property's want/told/is pivot for an owner: the
// declared value (want, resolved live from the cascade), the intended value (told,
// from the cache, nil until a command writes one), and the observed value (is, from
// the cache). Drift is config-drift: is present and differs from want. The windowed
// command-settlement verdict (is vs told within a settle window) is a later slice.
type PropertyReconciliation struct {
	PropertyTypeName string
	Want             json.RawMessage
	Told             json.RawMessage
	Is               json.RawMessage
	Drift            bool
}

// Reconciliation pivots want/told/is for every declared property of an owner. The
// want side is the cascade's effective declared value (EffectiveProperties, which
// also scope-guards the owner, so an out-of-scope owner is its non-disclosing
// not-found); the told and is sides are single-lookup cache reads. Declared is
// never cached: it is resolved live here, so the pivot is always current.
func (p *PG) Reconciliation(ctx context.Context, ownerKind, ownerID string, read scope.Set) ([]PropertyReconciliation, error) {
	effs, err := p.EffectiveProperties(ctx, ownerKind, ownerID, read)
	if err != nil {
		return nil, err
	}
	out := make([]PropertyReconciliation, 0, len(effs))
	for _, e := range effs {
		is, err := p.latestValue(ctx, p.pool, ownerKind, ownerID, e.PropertyTypeName, "", "observed")
		if err != nil {
			return nil, err
		}
		told, err := p.latestValue(ctx, p.pool, ownerKind, ownerID, e.PropertyTypeName, "", "intended")
		if err != nil {
			return nil, err
		}
		rec := PropertyReconciliation{PropertyTypeName: e.PropertyTypeName, Want: e.Value}
		if is != nil {
			rec.Is = is.Value
		}
		if told != nil {
			rec.Told = told.Value
		}
		rec.Drift = rec.Want != nil && rec.Is != nil && !bytes.Equal(rec.Want, rec.Is)
		out = append(out, rec)
	}
	return out, nil
}

// UpsertProperties writes the newest value per series into the property cache, one
// transaction over the batch. The series key (owner arc, property_type, instance,
// provenance) is unique, so a repeat of the same series updates in place. The
// out-of-order guard on the conflict update keeps a late-arriving older value from
// displacing a newer latest: the update only fires when the incoming TS is at least
// the stored TS (or the stored row has no TS yet). Declared values never come
// through here; they resolve live from the cascade.
func (p *PG) UpsertProperties(ctx context.Context, ups []PropertyUpsert) error {
	if len(ups) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin upsert properties: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, u := range ups {
		col, err := ownerColumn(u.OwnerKind)
		if err != nil {
			return fmt.Errorf("storage: upsert property %s/%s: %w", u.OwnerID, u.Key, err)
		}
		ts := u.TS
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		sql := fmt.Sprintf(`insert into property (owner_kind, %s, property_type_id, instance, provenance, value, ts)
			values ($1, %s, (select id from property_type where name = $3), $4, $5, $6, $7)
			on conflict (owner_kind, component_id, system_id, location_id, node_id, property_type_id, instance, provenance)
			do update set value = excluded.value, ts = excluded.ts, updated_at = now()
			where property.ts is null or excluded.ts >= property.ts`, col, ownerArcExprN(u.OwnerKind, 2))
		if _, err := tx.Exec(ctx, sql, u.OwnerKind, u.OwnerID, u.Key, u.Instance, u.Provenance, []byte(u.Value), ts); err != nil {
			return fmt.Errorf("storage: upsert property %s/%s: %w", u.OwnerID, u.Key, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit upsert properties: %w", err)
	}
	return nil
}

// LatestValue reads the current value of one series in a single lookup, replacing
// an order-by-ts scan over the append-only sample tables. Scope-guarded like
// EffectiveProperties: an out-of-scope owner is that kind's non-disclosing
// not-found. A series with no cached row returns nil, nil.
func (p *PG) LatestValue(ctx context.Context, ownerKind, ownerID, key, instance, provenance string, read scope.Set) (*CachedValue, error) {
	oc, ok := ownerContracts[ownerKind]
	if !ok {
		return nil, ErrUnknownOwnerKind
	}
	inScope, err := p.ownerInScope(ctx, p.pool, ownerKind, ownerID, read)
	if err != nil {
		return nil, err
	}
	if !inScope {
		return nil, oc.notFound
	}
	return p.latestValue(ctx, p.pool, ownerKind, ownerID, key, instance, provenance)
}

// latestValue is the unguarded series lookup, so a caller that has already
// scope-checked the owner (the reconciliation resolver) can read many provenances
// without re-checking each time.
func (p *PG) latestValue(ctx context.Context, q querier, ownerKind, ownerID, key, instance, provenance string) (*CachedValue, error) {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	sql := fmt.Sprintf(`select value, ts, provenance from property
		where owner_kind = $1 and %s = %s
		  and property_type_id = (select id from property_type where name = $3)
		  and instance = $4 and provenance = $5`, col, ownerArcExprN(ownerKind, 2))
	var (
		cv    CachedValue
		value []byte
		ts    *time.Time
	)
	err = q.QueryRow(ctx, sql, ownerKind, ownerID, key, instance, provenance).Scan(&value, &ts, &cv.Provenance)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: latest value %s/%s[%s]: %w", ownerID, key, instance, err)
	}
	cv.Value = copyRaw(value)
	if ts != nil {
		cv.TS = *ts
	}
	return &cv, nil
}
