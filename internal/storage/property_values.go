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

// Property is one declared-value series row (#591): a declared value is a row
// in the property series like any other, distinguished by provenance='declared',
// and its current value is the latest row per (type, owner arc, instance,
// provenance). It carries the same owner exclusive-arc as metric and event:
// OwnerKind picks the arc, OwnerID is the estate address (the owner's name).
type Property struct {
	ID               string
	OwnerKind        string
	OwnerID          string
	PropertyTypeName string
	PropertyTypeID   string
	Instance         string
	Provenance       string
	Value            json.RawMessage
	TS               time.Time
}

// EffectiveProperty is one property resolved for a component: the set value when
// present, otherwise the product contract's default (Value = coalesce(set,
// default), IsSet marking the override). FromContract distinguishes a property the
// component's product declares from an ad-hoc one set directly on the component
// (including every property on a productless component), so the surface can show
// the contract and the off-contract additions differently.
type EffectiveProperty struct {
	PropertyTypeName string
	PropertyTypeID   string
	Label            string // optional human label; empty when unset
	DataType         string
	Required         bool // from the product contract; always false for an ad-hoc property
	DefaultValue     json.RawMessage
	SetValue         json.RawMessage // nil when the component has not set it
	Value            json.RawMessage // coalesce(SetValue, DefaultValue)
	IsSet            bool
	FromContract     bool
	ValueID          string // the property id when set; empty when unset (what the surface clears)
}

// Property-value sentinels. Clearing a value the owner never set is the explicit
// ErrPropertyNotFound; a write naming an owner or property that does not exist
// trips the arc or property FK and surfaces as ErrPropertyRefNotFound (a request
// fault the API reports as 422, not a server error).
var (
	ErrPropertyNotFound    = errors.New("storage: property value not found")
	ErrPropertyRefNotFound = errors.New("storage: property value references a missing owner or property")
)

// declaredProvenance is the provenance of an operator assertion, the only one
// the declared-value writes below produce.
const declaredProvenance = "declared"

// declaredCurrentExpr resolves a declared series row to its current jsonb
// value: the value column carries the exact jsonb (bool and json round-trip),
// and a JSON null is the unset tombstone, resolving to SQL NULL (no value).
const declaredCurrentExpr = `nullif(value, 'null'::jsonb)`

// declaredSeriesCols is the read shape of one declared series row.
const declaredSeriesCols = `id::text, owner_kind, (select pr.name from property_type pr where pr.id = property.property_type_id) as property_type_name, property.property_type_id as property_type_id, instance, provenance, value, ts`

// scanProperty reads a row into a Property. OwnerID is not in the column
// list (it lives in whichever arc column the owner kind selects), so the caller
// stamps it from the address it queried by.
func scanProperty(row pgx.Row) (*Property, error) {
	var (
		pv    Property
		value []byte
	)
	if err := row.Scan(&pv.ID, &pv.OwnerKind, &pv.PropertyTypeName, &pv.PropertyTypeID, &pv.Instance, &pv.Provenance, &value, &pv.TS); err != nil {
		return nil, err
	}
	pv.Value = copyRaw(value)
	return &pv, nil
}

// SetProperty declares a value for (owner, property, instance) by APPENDING a
// series row, so every edit is history (#591). The save stays idempotent the
// way the observed lane's transition guard is: re-declaring the value already
// current appends nothing and returns the current row. The write and its audit
// are one transaction.
//
// The owner is resolved with the read-then-action split (#736): out of the read
// scope is that kind's non-disclosing not-found, and readable but out of the
// action scope is its forbidden. One resolve, not the two this used to make
// (a single-set owner guard for the check and ownerArcValueInScope for the arc),
// because resolveTargetID returns the id the arc stores.
func (p *PG) SetProperty(ctx context.Context, actorID, ownerKind, ownerID, propertyName, instance string, value json.RawMessage, read, action scope.Set) (*Property, error) {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	if len(value) == 0 {
		// The tombstone (ClearProperty) is the only no-value declared row; an
		// explicit null set would silently read back as unset.
		return nil, fmt.Errorf("storage: set property value %s/%s: a declared value requires a value", ownerID, propertyName)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin set property value: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// One scoped resolve, and its result IS the arc value: no second, scope-blind
	// re-resolve of the same reference can follow it (ruling 2, #627), because
	// there is no second resolve at all.
	arc, err := p.resolveTargetID(ctx, tx, ownerKind, ownerID, read, action)
	if err != nil {
		return nil, err
	}

	// cur is the series' current value (its latest row); the insert appends only
	// when the declared value actually changes. value is the exact jsonb
	// (ADR-0038's structured value: bool and json round-trip without loss).
	sql := fmt.Sprintf(`
		with cur as (
			select `+declaredCurrentExpr+` as value
			from property
			where owner_kind = $1 and %s = $2
			  and property_type_id = (select id from property_type where name = $3)
			  and instance = $4 and provenance = '`+declaredProvenance+`'
			order by ts desc, id desc
			limit 1
		)
		insert into property (owner_kind, %s, property_type_id, instance, provenance, value)
		select $1, $2, (select id from property_type where name = $3), $4, '`+declaredProvenance+`', $5::jsonb
		where not exists (select 1 from cur where cur.value = $5::jsonb)
		returning `+declaredSeriesCols, col, col)
	pv, err := scanProperty(tx.QueryRow(ctx, sql, ownerKind, arc, propertyName, instance, []byte(value)))
	if errors.Is(err, pgx.ErrNoRows) {
		// The value already current: nothing appended, nothing to audit. Return
		// the current row so the caller still holds its id.
		pv, err = p.currentDeclared(ctx, tx, col, ownerKind, arc, propertyName, instance)
		if err != nil {
			return nil, err
		}
		pv.OwnerID = ownerID
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("storage: commit set property value: %w", err)
		}
		return pv, nil
	}
	if err != nil {
		return nil, mapPropertyWriteErr(err)
	}
	pv.OwnerID = ownerID
	if err := writeAuditRes(ctx, tx, actorID, "update", "property", pv.ID, nil, pv); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit set property value: %w", err)
	}
	return pv, nil
}

// currentDeclared reads the current declared series row for one series, the
// read half of SetProperty's append-only-on-change guard.
func (p *PG) currentDeclared(ctx context.Context, q querier, col, ownerKind, arc, propertyName, instance string) (*Property, error) {
	sql := fmt.Sprintf(`
		select `+declaredSeriesCols+`
		from property
		where owner_kind = $1 and %s = $2
		  and property_type_id = (select id from property_type where name = $3)
		  and instance = $4 and provenance = '`+declaredProvenance+`'
		order by ts desc, id desc
		limit 1`, col)
	pv, err := scanProperty(q.QueryRow(ctx, sql, ownerKind, arc, propertyName, instance))
	if err != nil {
		return nil, fmt.Errorf("storage: read current declared value %s: %w", propertyName, err)
	}
	return pv, nil
}

// ClearProperty unsets a declared value by appending a tombstone row (a
// declared row whose value is JSON null), so the unset is itself an edit
// in the series' history and nothing is deleted. Clearing a property with no
// current value (never set, or already tombstoned) stays the explicit
// ErrPropertyNotFound miss. Scope-split and audited like the set (#736).
func (p *PG) ClearProperty(ctx context.Context, actorID, ownerKind, ownerID, propertyName, instance string, read, action scope.Set) error {
	col, err := ownerColumn(ownerKind)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin clear property value: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// One scoped resolve, and its result IS the arc value: see SetProperty.
	arc, err := p.resolveTargetID(ctx, tx, ownerKind, ownerID, read, action)
	if err != nil {
		return err
	}

	// The tombstone appends only when the series has a current value, so an
	// unset-of-unset is a miss rather than a growing pile of tombstones.
	sql := fmt.Sprintf(`
		with cur as (
			select `+declaredCurrentExpr+` as value
			from property
			where owner_kind = $1 and %s = $2
			  and property_type_id = (select id from property_type where name = $3)
			  and instance = $4 and provenance = '`+declaredProvenance+`'
			order by ts desc, id desc
			limit 1
		)
		insert into property (owner_kind, %s, property_type_id, instance, provenance, value)
		select $1, $2, (select id from property_type where name = $3), $4, '`+declaredProvenance+`', 'null'::jsonb
		from cur
		where cur.value is not null
		returning id::text`, col, col)
	var tombstoneID string
	if err := tx.QueryRow(ctx, sql, ownerKind, arc, propertyName, instance).Scan(&tombstoneID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPropertyNotFound
		}
		return fmt.Errorf("storage: clear property value %s/%s: %w", ownerID, propertyName, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "property", tombstoneID, nil, nil); err != nil {
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
	arcCol         string // the property arc column for this owner kind
	// arcMatch is the instance column the arc points AT: the primary key for the
	// three estate kinds, and still the name for a node until the collection tier
	// converts. Naming it here keeps the query shape identical across kinds.
	arcMatch string
	notFound error
	// metricContractTable is where the classifier declares its metrics, the #587
	// sibling of contractTable keyed by the same contractKeyCol; "" = no metric
	// contract. The metric sample table carries the same arc columns as the
	// property series, so arcCol serves both resolutions.
	metricContractTable string
}

var ownerContracts = map[string]ownerContract{
	"component": {"component", "product_id", "product_property", "product_id", "component_id", "id", ErrComponentNotFound, "product_metric"},
	"system":    {"system", "standard_id", "standard_property", "standard_id", "system_id", "id", ErrSystemNotFound, "standard_metric"},
	"location":  {"location", "location_type", "location_type_property", "location_type_id", "location_id", "id", ErrLocationNotFound, "location_type_metric"},
	// A node has the arc but no classifier, so it resolves ad-hoc values only. Its
	// arc points at principal_id, the node's primary key.
	"node": {"node", "", "", "", "node_id", "principal_id", ErrNodeNotFound, ""},
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
	// Resolved once here via ownerArcValueInScope, ambiguity-safe within
	// read (ruling 2, #627): the inst CTEs below used to look the instance
	// row up by name (`where name = $1`), which is a CTE that can hold more
	// than one row once a name is no longer unique, not a scalar subquery,
	// so it never raised SQLSTATE 21000 -- it silently joined properties
	// from every same-named row into one "effective properties" answer
	// instead, a worse failure than an error. Binding the resolved id
	// directly (matched on oc.arcMatch, the same primary-key-equivalent
	// column ownerArcValueInScope itself resolves against) makes inst
	// exactly one row again. The scope-blind ownerArcValue would still leak
	// an out-of-scope row's uuid here even though ownerInScope just passed:
	// that first check narrowing to scope does not help if this next
	// statement re-resolves the same bare name scope-blind.
	arc, err := p.ownerArcValueInScope(ctx, p.pool, ownerKind, ownerID, read)
	if err != nil {
		return nil, err
	}

	// The current declared value per property: the latest series row of each
	// (property, instance='') declared series on this owner's arc, a tombstone
	// (JSON-null value) resolving to no value. Shared by both arms.
	declared := fmt.Sprintf(`
		declared as (
			select distinct on (s.property_type_id)
			       s.property_type_id, s.id,
			       `+declaredCurrentExpr+` as value
			from property s
			join inst on s.%[1]s = inst.arc
			where s.instance = '' and s.provenance = 'declared'
			order by s.property_type_id, s.ts desc, s.id desc
		)`, oc.arcCol)

	var q string
	if oc.contractTable == "" {
		// The ad-hoc arm alone when the owner kind has no classifier to inherit
		// from: everything the instance declares, minus tombstoned series.
		q = fmt.Sprintf(`
		with inst as (select %[3]s as arc from %[1]s where %[3]s = $1), %[2]s
		select pr.name as property_type_name, pr.id as property_type_id, pr.label, pr.data_type, false as required,
		       null::jsonb as default_value,
		       d.value as set_value,
		       d.value as effective_value,
		       true as is_set,
		       false as from_contract,
		       d.id::text as value_id
		from declared d
		join property_type pr on pr.id = d.property_type_id
		where d.value is not null
		order by 1`,
			oc.instanceTable, declared, oc.arcMatch)
	} else {
		q = fmt.Sprintf(`
		with inst as (
			select %[6]s as arc, %[2]s as classifier from %[1]s where %[6]s = $1
		), %[5]s
		-- The contract arm: what the instance's classifier declares, resolved
		-- against the instance's own current declared value.
		select pr.name as property_type_name, pr.id as property_type_id, pr.label, pr.data_type, c.required,
		       c.default_value,
		       d.value as set_value,
		       coalesce(d.value, c.default_value) as effective_value,
		       (d.value is not null) as is_set,
		       true as from_contract,
		       case when d.value is not null then d.id::text end as value_id
		from inst
		join %[3]s c on c.%[4]s = inst.classifier
		join property_type pr on pr.id = c.property_type_id
		left join declared d on d.property_type_id = c.property_type_id
		union all
		-- The ad-hoc arm: values set directly on the instance for properties the
		-- contract does not declare.
		select pr.name, pr.id, pr.label, pr.data_type, false,
		       null::jsonb,
		       d.value, d.value, true, false, d.id::text
		from inst
		cross join declared d
		join property_type pr on pr.id = d.property_type_id
		where d.value is not null
		  and not exists (
			select 1 from %[3]s c
			where c.%[4]s = inst.classifier and c.property_type_id = d.property_type_id
		)
		order by 1`,
			oc.instanceTable, oc.classifierCol, oc.contractTable, oc.contractKeyCol, declared, oc.arcMatch)
	}

	rows, err := p.pool.Query(ctx, q, arc)
	if err != nil {
		return nil, fmt.Errorf("storage: effective properties %s/%s: %w", ownerKind, ownerID, err)
	}
	defer rows.Close()

	var out []EffectiveProperty
	for rows.Next() {
		var (
			e             EffectiveProperty
			def, set, val []byte
			label         *string // NULL when unset
			valueID       *string // NULL when the property is unset
		)
		if err := rows.Scan(&e.PropertyTypeName, &e.PropertyTypeID, &label, &e.DataType, &e.Required,
			&def, &set, &val, &e.IsSet, &e.FromContract, &valueID); err != nil {
			return nil, fmt.Errorf("storage: scan effective property: %w", err)
		}
		if label != nil {
			e.Label = *label
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
// ownerArcValue resolves an owner reference to the value its arc column stores.
// For every kind that is the entity's primary key: a uuid for the three estate
// kinds, principal_id for a node, so a rename never touches the arc.
//
// The reference itself may be either form, since scopedByName resolves a uuid or
// a name; this is only about what gets written.
func (p *PG) ownerArcValue(ctx context.Context, q querier, ownerKind, ownerRef string) (string, error) {
	switch ownerKind {
	case "component":
		c, err := scopedByName(ctx, q, componentConfig, ownerRef)
		if err != nil {
			return "", err
		}
		return c.ID, nil
	case "system":
		sys, err := scopedByName(ctx, q, systemConfig, ownerRef)
		if err != nil {
			return "", err
		}
		return sys.ID, nil
	case "location":
		l, err := scopedByName(ctx, q, locationConfig, ownerRef)
		if err != nil {
			return "", err
		}
		return l.ID, nil
	case "node":
		col := "name"
		if isUUID(ownerRef) {
			col = "principal_id"
		}
		var pid string
		if err := q.QueryRow(ctx, `select principal_id from node where `+col+` = $1`, ownerRef).Scan(&pid); err != nil {
			return "", ErrNodeNotFound
		}
		return pid, nil
	}
	return ownerRef, nil
}

// ownerArcValueInScope is ownerArcValue's scope-aware twin: ambiguity is
// judged within s, not estate-wide (ruling 2, #627). Every caller that has
// already scope-checked the same (ownerKind, ownerRef) via ownerInScope must
// resolve the arc value through THIS,
// not the scope-blind ownerArcValue: ownerInScope narrowing to scope first
// does not help if the very next statement re-resolves the same bare name
// scope-blind, which is exactly how EffectiveProperties, EffectiveMetrics,
// SetProperty, ClearProperty, IssueCommand, and CommandSettlement each
// leaked an out-of-scope row's uuid on a name unique to the caller's own
// scope but ambiguous estate-wide, even after ownerInScope's own fix.
// Nodes have no scope tree (node_name_key stays a plain global unique
// constraint, so a bare node name is never ambiguous); this delegates to the
// scope-blind resolve for that one kind.
//
// Four of those six no longer resolve a name at all, which is the stronger
// answer to the same problem: the property writes take the id resolveTargetID
// returned (#736), and the two command methods take the id the route resolved
// (#749). A path with one resolve cannot have a second one that disagrees.
func (p *PG) ownerArcValueInScope(ctx context.Context, q querier, ownerKind, ownerRef string, s scope.Set) (string, error) {
	switch ownerKind {
	case "component":
		c, err := scopedByNameInScope(ctx, q, componentConfig, ownerRef, ownerKind, s)
		if err != nil {
			return "", err
		}
		return c.ID, nil
	case "system":
		sys, err := scopedByNameInScope(ctx, q, systemConfig, ownerRef, ownerKind, s)
		if err != nil {
			return "", err
		}
		return sys.ID, nil
	case "location":
		l, err := scopedByNameInScope(ctx, q, locationConfig, ownerRef, ownerKind, s)
		if err != nil {
			return "", err
		}
		return l.ID, nil
	case "node":
		return p.ownerArcValue(ctx, q, ownerKind, ownerRef)
	}
	return "", ErrUnknownOwnerKind
}

// isOwnerNotFound reports whether err is one of the owner-kind not-found
// sentinels ownerArcValue can return. Several read paths that build SQL on the
// owner arc (current_values.go's latestValue, settlement.go's settleCheck and
// latestMetricValue) fold this back into their old "nothing yet" result: an
// unknown owner's name used to resolve an inline subquery to no match,
// silently, and a caller that never had to handle a hard not-found error here
// must not start seeing one just because the resolution moved from a
// same-query subquery to an up-front resolve.
func isOwnerNotFound(err error) bool {
	return errors.Is(err, ErrComponentNotFound) || errors.Is(err, ErrSystemNotFound) ||
		errors.Is(err, ErrLocationNotFound) || errors.Is(err, ErrNodeNotFound)
}

// The three tree kinds resolve through scopedByNameInScope, not
// scopedByName-then-inScopeTree: architect ruling 2 (#627, "scope decides
// before ambiguity does") requires ambiguity to be judged inside s, not
// estate-wide, because ownerInScope's own read scope s is right here to
// filter with. errAsNotFound preserves the exact external contract (false,
// nil for "exists but outside s", matching the pre-#627 shape every caller
// already converts to its own not-found sentinel) while still letting a
// genuine in-scope collision surface as *ErrAmbiguousName rather than being
// swallowed into a false negative.
func errAsNotFound(err, notFound error) (bool, error) {
	if errors.Is(err, notFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (p *PG) ownerInScope(ctx context.Context, q querier, ownerKind, ownerID string, s scope.Set) (bool, error) {
	switch ownerKind {
	case "component":
		_, err := scopedByNameInScope(ctx, q, componentConfig, ownerID, ownerKind, s)
		return errAsNotFound(err, ErrComponentNotFound)
	case "system":
		_, err := scopedByNameInScope(ctx, q, systemConfig, ownerID, ownerKind, s)
		return errAsNotFound(err, ErrSystemNotFound)
	case "location":
		_, err := scopedByNameInScope(ctx, q, locationConfig, ownerID, ownerKind, s)
		return errAsNotFound(err, ErrLocationNotFound)
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

// guardOwnerScope is GONE, and its absence is the point (#736, #749). It
// confirmed an owner against ONE set and answered that kind's not-found, which is
// the shape both of the arcs it guarded have now left behind: a write on an owner
// resolves through resolveTargetID's read-then-action split (the property writes),
// or is handed an id its route already resolved that way (the command pair). A
// single-set guard on an owner is what made a readable-but-not-writable row
// answer "not found", so re-introducing one is a decision to make, not a
// convenience to reach for.

// mapPropertyWriteErr turns a foreign-key violation on the value write into a
// caller-meaningful sentinel: the owner or the property does not exist.
func mapPropertyWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23503" || // foreign_key_violation
		// an unknown property name resolved to null on the not-null arc
		(pgErr.Code == "23502" && pgErr.ColumnName == "property_type_id")) {
		return ErrPropertyRefNotFound
	}
	return fmt.Errorf("storage: set property value: %w", err)
}

// componentIDResolved resolves a component by name to its id and reports whether
// it falls within the given scope, judging ambiguity inside s (ruling 2, #627):
// absent, or present but entirely outside s, is the same (id="", inScope=false,
// err=nil), matching ownerInScope's shape; a genuine collision within s is
// *ErrAmbiguousName. It runs on any querier so it works standalone or inside a
// transaction. Unreached today (no caller); kept consistent with ownerInScope
// so the next caller does not have to rediscover the scope-blind version's
// disclosure bug.
func (p *PG) componentIDResolved(ctx context.Context, q querier, name string, s scope.Set) (id string, inScope bool, err error) {
	c, err := scopedByNameInScope(ctx, q, componentConfig, name, "component", s)
	if errors.Is(err, ErrComponentNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return c.ID, true, nil
}

// copyRaw returns a private copy of a jsonb column, or nil for a SQL NULL, so the
// value does not alias pgx's row buffer.
func copyRaw(b []byte) json.RawMessage {
	if b == nil {
		return nil
	}
	return append(json.RawMessage(nil), b...)
}
