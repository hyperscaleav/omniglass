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

// CurrentValue is the current value of one series: its latest row per (type,
// owner arc, instance, provenance), read from the series itself (#591 retired
// the maintained cache). Value is the row's jsonb form, TS the value's own
// time (the sample time for observed, the effect time for intended).
type CurrentValue struct {
	Value      json.RawMessage
	TS         time.Time
	Provenance string
}

// PropertyReconciliation is one property's want/told/is pivot for an owner: the
// declared value (want, resolved live from the cascade), the intended value (told,
// nil until a command writes one), and the observed value (is). All three are
// series reads. Drift is config-drift: is present and differs from want. The
// windowed command-settlement verdict (is vs told within a settle window) is a
// later slice.
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
// not-found); the told and is sides are latest-row series reads per provenance.
func (p *PG) Reconciliation(ctx context.Context, ownerKind, ownerID string, read scope.Set) ([]PropertyReconciliation, error) {
	effs, err := p.EffectiveProperties(ctx, ownerKind, ownerID, read)
	if err != nil {
		return nil, err
	}
	out := make([]PropertyReconciliation, 0, len(effs))
	for _, e := range effs {
		is, err := p.latestValue(ctx, p.pool, ownerKind, ownerID, e.PropertyTypeName, "", "observed", read)
		if err != nil {
			return nil, err
		}
		told, err := p.latestValue(ctx, p.pool, ownerKind, ownerID, e.PropertyTypeName, "", "intended", read)
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

// LatestValue reads the current value of one series: its latest row by (type,
// owner, instance, provenance). Scope-guarded like EffectiveProperties: an
// out-of-scope owner is that kind's non-disclosing not-found. A series with no
// rows (or whose current declared row is the unset tombstone) returns nil, nil.
func (p *PG) LatestValue(ctx context.Context, ownerKind, ownerID, key, instance, provenance string, read scope.Set) (*CurrentValue, error) {
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
	return p.latestValue(ctx, p.pool, ownerKind, ownerID, key, instance, provenance, read)
}

// latestValue is the unguarded series lookup, so a caller that has already
// scope-checked the owner (the reconciliation resolver, command settlement) can
// read many provenances without re-checking each time. ts desc, id desc: the
// value's own time orders the series and the row id breaks same-instant ties,
// the same rule LatestProperty applies.
//
// The tiebreak is INSERTION ORDER, and deliberately: `property.id` is a bigint
// identity column, so `id desc` is the sequence descending, not an arbitrary
// identifier (#712 read this as a random uuid and filed a nondeterminism that
// does not exist; series_order_test.go asserts the column's shape out of the
// live catalog so a change to it fails loudly rather than quietly making that
// reading true). `ts` LEADS because the observed lane accepts a caller-supplied
// timestamp, so a late-arriving older sample must not become current merely by
// being written last; the cost of that ordering is that a database clock
// stepping backwards between two writes to one series resolves to the
// earlier-written row, which no tiebreak can reach, because there is no tie.
// ownerID is resolved once here, via ownerArcValueInScope (uuid-or-name,
// ADR-0062; ambiguity judged within s, ruling 2, #627): both of this
// function's callers (Reconciliation, LatestValue) already scope-checked the
// owner via EffectiveProperties or ownerInScope before reaching it, but
// re-resolving the same bare name a second time SCOPE-BLIND would still leak
// an out-of-scope row's uuid on a name unique to the caller's own scope but
// ambiguous estate-wide, which is exactly what calling the scope-blind
// ownerArcValue here used to do even after the first check passed. An
// unknown (or entirely out-of-scope) owner folds to nil, no error, matching
// the old inline subquery's silent no-match; a future caller that has not
// already scope-checked the owner must not start hard-erroring on a check
// this function never used to perform.
func (p *PG) latestValue(ctx context.Context, q querier, ownerKind, ownerID, key, instance, provenance string, s scope.Set) (*CurrentValue, error) {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	arc, err := p.ownerArcValueInScope(ctx, q, ownerKind, ownerID, s)
	if isOwnerNotFound(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	sql := fmt.Sprintf(`select `+declaredCurrentExpr+`, ts, provenance from property
		where owner_kind = $1 and %s = $2
		  and property_type_id = (select id from property_type where name = $3)
		  and instance = $4 and provenance = $5
		order by ts desc, id desc
		limit 1`, col)
	var (
		cv    CurrentValue
		value []byte
	)
	err = q.QueryRow(ctx, sql, ownerKind, arc, key, instance, provenance).Scan(&value, &cv.TS, &cv.Provenance)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: latest value %s/%s[%s]: %w", ownerID, key, instance, err)
	}
	if value == nil {
		// The latest declared row is the unset tombstone: no current value.
		return nil, nil
	}
	cv.Value = copyRaw(value)
	return &cv, nil
}
