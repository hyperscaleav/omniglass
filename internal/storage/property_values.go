package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PropertyValue is the current value of a property on an estate owner, per
// provenance. It carries the same owner exclusive-arc as metric_datapoint and
// event: OwnerKind picks the arc, OwnerID is the estate address (the owner's name).
// A declared value is what used to be a field_value; intended (config), observed,
// and calculated producers land in later slices.
type PropertyValue struct {
	ID           string
	OwnerKind    string
	OwnerID      string
	PropertyName string
	Instance     string
	Provenance   string
	Value        json.RawMessage
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// EffectiveProperty is one property resolved for a component: the set value when
// present, otherwise the product contract's default (Value = coalesce(set,
// default), IsSet marking the override). FromContract distinguishes a property the
// component's product declares from an ad-hoc one set directly on the component
// (including every property on a productless component), so the surface can show
// the contract and the off-contract additions differently.
type EffectiveProperty struct {
	PropertyName string
	DisplayName  string // optional human label; empty when unset
	DataType     string
	Required     bool // from the product contract; always false for an ad-hoc property
	DefaultValue json.RawMessage
	SetValue     json.RawMessage // nil when the component has not set it
	Value        json.RawMessage // coalesce(SetValue, DefaultValue)
	IsSet        bool
	FromContract bool
	ValueID      string // the property_value id when set; empty when unset (what the surface clears)
}

// Property-value sentinels. Clearing a value the owner never set is the explicit
// ErrPropertyValueNotFound; a write naming an owner or property that does not exist
// trips the arc or property FK and surfaces as ErrPropertyRefNotFound (a request
// fault the API reports as 422, not a server error).
var (
	ErrPropertyValueNotFound = errors.New("storage: property value not found")
	ErrPropertyRefNotFound   = errors.New("storage: property value references a missing owner or property")
)

// declaredProvenance is the only provenance this slice writes: a value an operator
// declares. The column carries the other three for the producers that land later.
const declaredProvenance = "declared"

const propertyValueCols = `id, owner_kind, property_name, instance, provenance, value, created_at, updated_at`

// scanPropertyValue reads a row into a PropertyValue. OwnerID is not in the column
// list (it lives in whichever arc column the owner kind selects), so the caller
// stamps it from the address it queried by.
func scanPropertyValue(row pgx.Row) (*PropertyValue, error) {
	var (
		pv    PropertyValue
		value []byte
	)
	if err := row.Scan(&pv.ID, &pv.OwnerKind, &pv.PropertyName, &pv.Instance, &pv.Provenance, &value, &pv.CreatedAt, &pv.UpdatedAt); err != nil {
		return nil, err
	}
	pv.Value = copyRaw(value)
	return &pv, nil
}

// SetPropertyValue sets a declared value for (owner, property, instance),
// idempotently: the first set inserts, a later set updates in place (the series key
// is unique per owner, property, instance, and provenance). The component owner is
// resolved within the write scope, so a caller cannot set a value on a component it
// cannot reach; the write and its audit are one transaction.
func (p *PG) SetPropertyValue(ctx context.Context, actorID, ownerKind, ownerID, propertyName, instance string, value json.RawMessage, write scope.Set) (*PropertyValue, error) {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin set property value: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := p.guardOwnerScope(ctx, tx, ownerKind, ownerID, write); err != nil {
		return nil, err
	}

	// The series key is (owner arc, property, instance, provenance); a repeat set of
	// the same series updates rather than conflicting, so the surface's save is
	// idempotent.
	sql := fmt.Sprintf(`
		insert into property_value (owner_kind, %s, property_name, instance, provenance, value)
		values ($1, $2, $3, $4, '`+declaredProvenance+`', $5)
		on conflict (owner_kind, component_id, system_id, location_id, node_id, property_name, instance, provenance)
		do update set value = excluded.value, updated_at = now()
		returning `+propertyValueCols, col)
	pv, err := scanPropertyValue(tx.QueryRow(ctx, sql, ownerKind, ownerID, propertyName, instance, []byte(value)))
	if err != nil {
		return nil, mapPropertyValueWriteErr(err)
	}
	pv.OwnerID = ownerID
	if err := writeAuditRes(ctx, tx, actorID, "update", "property_value", pv.ID, nil, pv); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit set property value: %w", err)
	}
	return pv, nil
}

// ClearPropertyValue removes a declared value, returning ErrPropertyValueNotFound
// when the owner has not set that property (so clearing an unset property is an
// explicit miss, not a silent no-op). Scope-guarded and audited like the set.
func (p *PG) ClearPropertyValue(ctx context.Context, actorID, ownerKind, ownerID, propertyName, instance string, write scope.Set) error {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin clear property value: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := p.guardOwnerScope(ctx, tx, ownerKind, ownerID, write); err != nil {
		return err
	}

	sql := fmt.Sprintf(`delete from property_value
		where owner_kind = $1 and %s = $2 and property_name = $3 and instance = $4 and provenance = '`+declaredProvenance+`'
		returning id`, col)
	var id string
	if err := tx.QueryRow(ctx, sql, ownerKind, ownerID, propertyName, instance).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPropertyValueNotFound
		}
		return fmt.Errorf("storage: clear property value %s/%s: %w", ownerID, propertyName, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "property_value", id, nil, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit clear property value: %w", err)
	}
	return nil
}

// ownerContract is the only thing that varies between the owner kinds when
// resolving declared properties: where the instance names its classifier, which
// contract table that classifier declares properties in, and which arc column
// carries the instance's own values. The resolution SQL is written once and
// parameterized by this, so component, system, and location resolve through one
// code path (primitive-first) and cannot drift apart.
//
// Every identifier here is a compile-time constant from this table, never caller
// input, so interpolating them into the statement is safe.
type ownerContract struct {
	instanceTable  string // the instance's own table
	classifierCol  string // the instance column naming its classifier ("" = no classifier)
	contractTable  string // where the classifier declares its properties ("" = no contract)
	contractKeyCol string // the contract column matching the classifier
	arcCol         string // the property_value arc column for this owner kind
	notFound       error
}

var ownerContracts = map[string]ownerContract{
	"component": {"component", "product_id", "product_property", "product_id", "component_id", ErrComponentNotFound},
	"system":    {"system", "standard_id", "standard_property", "standard_id", "system_id", ErrSystemNotFound},
	"location":  {"location", "location_type", "location_type_property", "location_type_id", "location_id", ErrLocationNotFound},
	// A node has the arc but no classifier, so it resolves ad-hoc values only.
	"node": {"node", "", "", "", "node_id", ErrNodeNotFound},
}

// EffectiveProperties resolves an instance's declared properties: every property
// its classifier's contract declares (value = coalesce(the instance's set value,
// the contract default)), plus any ad-hoc property the instance sets directly that
// the contract does not declare. An instance with no classifier (a productless
// component, a one-off system) has only the ad-hoc set. The instance must be within
// the read scope; an out-of-scope instance is its non-disclosing not-found.
//
// The two arms are one UNION so the merge stays in SQL: the contract arm is a left
// join from the contract table, the ad-hoc arm is the instance's values with no
// matching contract row.
func (p *PG) EffectiveProperties(ctx context.Context, ownerKind, ownerID string, read scope.Set) ([]EffectiveProperty, error) {
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

	// The ad-hoc arm alone when the owner kind has no classifier to inherit from.
	adHoc := fmt.Sprintf(`
		select pv.property_name, pr.display_name, pr.data_type, false as required,
		       null::jsonb as default_value,
		       pv.value as set_value,
		       pv.value as effective_value,
		       true as is_set,
		       false as from_contract,
		       pv.id as value_id
		from inst
		join property_value pv on pv.%[1]s = inst.name
		     and pv.instance = '' and pv.provenance = 'declared'
		join property pr on pr.name = pv.property_name`, oc.arcCol)

	var q string
	if oc.contractTable == "" {
		q = fmt.Sprintf(`with inst as (select name from %s where name = $1) %s order by 1`,
			oc.instanceTable, adHoc)
	} else {
		q = fmt.Sprintf(`
		with inst as (
			select name, %[2]s as classifier from %[1]s where name = $1
		)
		-- The contract arm: what the instance's classifier declares, resolved
		-- against the instance's own value.
		select c.property_name, pr.display_name, pr.data_type, c.required,
		       c.default_value,
		       pv.value as set_value,
		       coalesce(pv.value, c.default_value) as effective_value,
		       (pv.id is not null) as is_set,
		       true as from_contract,
		       pv.id as value_id
		from inst
		join %[3]s c on c.%[4]s = inst.classifier
		join property pr on pr.name = c.property_name
		left join property_value pv
		       on pv.%[5]s = inst.name
		      and pv.property_name = c.property_name
		      and pv.instance = ''
		      and pv.provenance = 'declared'
		union all
		-- The ad-hoc arm: values set directly on the instance for properties the
		-- contract does not declare (every value on a classifier-less instance).
		%[6]s
		where not exists (
			select 1 from %[3]s c
			where c.%[4]s = inst.classifier and c.property_name = pv.property_name
		)
		order by 1`,
			oc.instanceTable, oc.classifierCol, oc.contractTable, oc.contractKeyCol, oc.arcCol, adHoc)
	}

	rows, err := p.pool.Query(ctx, q, ownerID)
	if err != nil {
		return nil, fmt.Errorf("storage: effective properties %s/%s: %w", ownerKind, ownerID, err)
	}
	defer rows.Close()

	var out []EffectiveProperty
	for rows.Next() {
		var (
			e             EffectiveProperty
			def, set, val []byte
			displayName   *string // NULL when unset
			valueID       *string // NULL when the property is unset
		)
		if err := rows.Scan(&e.PropertyName, &displayName, &e.DataType, &e.Required,
			&def, &set, &val, &e.IsSet, &e.FromContract, &valueID); err != nil {
			return nil, fmt.Errorf("storage: scan effective property: %w", err)
		}
		if displayName != nil {
			e.DisplayName = *displayName
		}
		e.DefaultValue = copyRaw(def)
		e.SetValue = copyRaw(set)
		e.Value = copyRaw(val)
		if valueID != nil {
			e.ValueID = *valueID
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ownerInScope reports whether the named owner exists and falls within the given
// scope, for any owner kind on the arc. An absent owner is that kind's not-found
// sentinel (nothing to disclose); an existing but out-of-scope owner returns
// inScope=false so each caller picks its own sentinel. A node is estate-wide (not
// scope-tree scoped, like a principal), so it is in scope once it exists.
func (p *PG) ownerInScope(ctx context.Context, q querier, ownerKind, ownerID string, s scope.Set) (bool, error) {
	switch ownerKind {
	case "component":
		c, err := scopedByName(ctx, q, componentConfig, ownerID)
		if err != nil {
			return false, err
		}
		return inScopeTree(ctx, q, componentTable, c.ID, s)
	case "system":
		sys, err := scopedByName(ctx, q, systemConfig, ownerID)
		if err != nil {
			return false, err
		}
		return inScopeTree(ctx, q, systemTable, sys.ID, s)
	case "location":
		l, err := scopedByName(ctx, q, locationConfig, ownerID)
		if err != nil {
			return false, err
		}
		return inScopeTree(ctx, q, locationTable, l.ID, s)
	case "node":
		// A node is not a scope tree, so existence is the whole check.
		var exists bool
		if err := q.QueryRow(ctx, `select exists (select 1 from node where name = $1)`, ownerID).Scan(&exists); err != nil {
			return false, fmt.Errorf("storage: resolve node %q: %w", ownerID, err)
		}
		if !exists {
			return false, ErrNodeNotFound
		}
		return true, nil
	}
	return false, ErrUnknownOwnerKind
}

// guardOwnerScope confirms the owner exists and is reachable within the caller's
// scope before a value is written to it, so a caller cannot set a value on an
// instance it cannot reach. Every arc is scope-checked, not just the component one.
func (p *PG) guardOwnerScope(ctx context.Context, q querier, ownerKind, ownerID string, write scope.Set) error {
	oc, ok := ownerContracts[ownerKind]
	if !ok {
		return ErrUnknownOwnerKind
	}
	inScope, err := p.ownerInScope(ctx, q, ownerKind, ownerID, write)
	if err != nil {
		return err
	}
	if !inScope {
		return oc.notFound
	}
	return nil
}

// mapPropertyValueWriteErr turns a foreign-key violation on the value write into a
// caller-meaningful sentinel: the owner or the property does not exist.
func mapPropertyValueWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign_key_violation
		return ErrPropertyRefNotFound
	}
	return fmt.Errorf("storage: set property value: %w", err)
}

// componentIDResolved resolves a component by name to its id and reports whether it
// falls within the given scope. An absent component is always ErrComponentNotFound
// (nothing to disclose); an existing but out-of-scope component is returned with
// inScope=false so each caller picks its own sentinel: the write path forbids, the
// non-disclosing read path 404s. It runs on any querier so it works standalone or
// inside a transaction.
func (p *PG) componentIDResolved(ctx context.Context, q querier, name string, s scope.Set) (id string, inScope bool, err error) {
	c, err := scopedByName(ctx, q, componentConfig, name)
	if err != nil {
		return "", false, err // ErrComponentNotFound when absent
	}
	in, err := inScopeTree(ctx, q, componentTable, c.ID, s)
	if err != nil {
		return "", false, err
	}
	return c.ID, in, nil
}

// copyRaw returns a private copy of a jsonb column, or nil for a SQL NULL, so the
// value does not alias pgx's row buffer.
func copyRaw(b []byte) json.RawMessage {
	if b == nil {
		return nil
	}
	return append(json.RawMessage(nil), b...)
}
