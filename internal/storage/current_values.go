package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	"github.com/jackc/pgx/v5"
)

// CurrentValue is the current value of one series: its latest row per (type,
// owner arc, instance, provenance), read from the series itself (#591 retired
// the maintained cache). Value is the row's jsonb form, TS the value's own
// time (the sample time for observed, the effect time for intended), and
// Instance the series it was read from (ADR-0456: for an observed row, the
// endpoint or sender that observed it).
type CurrentValue struct {
	Value      json.RawMessage
	TS         time.Time
	Provenance string
	Instance   string
}

// PropertyReconciliation is one property's want/told/is pivot for an owner, the
// block a row of the owner's properties read carries (#1297): the declared
// value (want, resolved live from the cascade, nil when nothing declares the
// key), the intended value (told, nil until a command writes one), and the
// observed value (is). All three are series reads, and an owner-level question
// (ADR-0456): want is the owner's declaration, told the newest intended value on
// any instance, and is resolved across every live observed instance worst-case,
// IsInstance naming the instance it came from. Drift is config-drift: some
// observed instance differs from want, so it is false when nothing is declared.
// The windowed form (is vs told within a settle window) is command settlement.
type PropertyReconciliation struct {
	Want       json.RawMessage
	Told       json.RawMessage
	Is         json.RawMessage
	IsInstance string
	Drift      bool
}

// reconcileProperties folds the reconciliation into the declared resolution
// (#1297). Each declared row (the set value, else the contract default) gains
// its pivot; a row a calc rule writes that nothing declares gains none, since a
// calculated value is not what anyone asked for. Then every key the owner has a
// live observation of or a command targeted, but nothing declares or
// calculates, joins as a row of its own with want nil: an observed-only key is
// a property the owner has. A key whose only observer is retired, and that no
// command told, has no live value and no row. arc is the owner's resolved arc,
// already scope-checked by the caller. Rows return ordered by key.
func (p *PG) reconcileProperties(ctx context.Context, q querier, ownerKind, arc string, effs []EffectiveProperty) ([]EffectiveProperty, error) {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	have := make(map[string]bool, len(effs))
	for i := range effs {
		e := &effs[i]
		have[e.PropertyTypeName] = true
		if e.SourceRuleID != "" && !e.FromContract {
			continue
		}
		want := e.SetValue
		if want == nil {
			want = e.DefaultValue
		}
		if e.Reconciliation, err = p.reconcileKey(ctx, q, ownerKind, col, arc, e.PropertyTypeName, want); err != nil {
			return nil, err
		}
	}
	extra, err := p.seriesKeys(ctx, q, ownerKind, col, arc)
	if err != nil {
		return nil, err
	}
	for _, e := range extra {
		if have[e.PropertyTypeName] {
			continue
		}
		rec, err := p.reconcileKey(ctx, q, ownerKind, col, arc, e.PropertyTypeName, nil)
		if err != nil {
			return nil, err
		}
		if rec.Is == nil && rec.Told == nil {
			continue
		}
		e.Reconciliation = rec
		effs = append(effs, e)
	}
	sort.SliceStable(effs, func(i, j int) bool { return effs[i].PropertyTypeName < effs[j].PropertyTypeName })
	return effs, nil
}

// reconcileKey pivots one key against want: told is the newest intended value on
// any instance a command named, is the worst case over the live observed
// instances (worstCaseValue). arc is a resolved, scope-checked arc, so the
// series reads bind it directly rather than re-resolving the owner per key.
func (p *PG) reconcileKey(ctx context.Context, q querier, ownerKind, col, arc, key string, want json.RawMessage) (*PropertyReconciliation, error) {
	observed, err := readPropertyValues(ctx, q, arc, key, latestObservedPerInstanceSQL("property", declaredCurrentExpr, col), ownerKind, arc, key, transportNames())
	if err != nil {
		return nil, err
	}
	intended, err := readPropertyValues(ctx, q, arc, key, latestIntendedSQL(col), ownerKind, arc, key)
	if err != nil {
		return nil, err
	}
	rec := &PropertyReconciliation{Want: want}
	is, drift := worstCaseValue(want, observed)
	if is != nil {
		rec.Is, rec.IsInstance = is.Value, is.Instance
	}
	if len(intended) > 0 {
		rec.Told = intended[0].Value
	}
	rec.Drift = drift
	return rec, nil
}

// seriesKeysSQL lists the catalog rows of every property key an owner arc ($1
// owner kind, $2 arc) has an observed row of or a command targeted: the keys
// beyond the declared cascade that the properties read can carry. The reserved
// family ($3, writerclass.ReservedNames: a native probe key, health, a command
// lifecycle key) is left out: a probe key is the node's verdict on an
// endpoint, whose own surface is the reachability read (which withholds a
// gated rung's frozen fact), and health is the rollup's, not a property an
// operator could ask for. One probe per catalog key, each an index prefix
// (the component series index leads with component and type; the system and
// location owner indexes with type and owner; the command owner indexes with
// the arc), stopping at the first row. Whether the observation is live, and
// whether the command left an intended value, is the per-key reconcileKey's to
// judge.
func seriesKeysSQL(col string) string {
	return fmt.Sprintf(`select pt.name, pt.id::text, pt.label, pt.data_type from property_type pt
		where (exists (select 1 from property s
				where s.owner_kind = $1 and s.%[1]s = $2 and s.property_type_id = pt.id and s.provenance = 'observed')
		   or exists (select 1 from command c join command_type ct on ct.id = c.command_type_id
				where c.owner_kind = $1 and c.%[1]s = $2 and ct.target_property_type_id = pt.id))
		  and not (pt.name = any($3::text[]))
		order by pt.name`, col)
}

// seriesKeys reads seriesKeysSQL into catalog-only rows: no contract, no
// declared value, nothing set.
func (p *PG) seriesKeys(ctx context.Context, q querier, ownerKind, col, arc string) ([]EffectiveProperty, error) {
	rows, err := q.Query(ctx, seriesKeysSQL(col), ownerKind, arc, writerclass.ReservedNames())
	if err != nil {
		return nil, fmt.Errorf("storage: series keys %s/%s: %w", ownerKind, arc, err)
	}
	defer rows.Close()
	var out []EffectiveProperty
	for rows.Next() {
		var (
			e     EffectiveProperty
			label *string
		)
		if err := rows.Scan(&e.PropertyTypeName, &e.PropertyTypeID, &label, &e.DataType); err != nil {
			return nil, fmt.Errorf("storage: scan series key: %w", err)
		}
		if label != nil {
			e.Label = *label
		}
		out = append(out, e)
	}
	return out, rows.Err()
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
// scope-checked the owner (command settlement) can
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
// ADR-0062; ambiguity judged within s, ruling 2, #627): LatestValue
// already scope-checked the owner via ownerInScope before reaching it (and
// settlement reads an already-resolved arc), but
// re-resolving the same bare name a second time SCOPE-BLIND would still leak
// an out-of-scope row's uuid on a name unique to the caller's own scope but
// ambiguous fleet-wide, which is exactly what calling the scope-blind
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
	cv.Instance = instance
	return &cv, nil
}
