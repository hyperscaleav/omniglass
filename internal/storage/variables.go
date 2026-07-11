package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/variable"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Variable-layer sentinel errors, mapped by the API to status: the non-disclosing
// 404, the readable-but-not-actionable 403, and the request faults (422).
var (
	ErrVariableNotFound      = errors.New("storage: variable not found")
	ErrVariableForbidden     = errors.New("storage: action not permitted on this variable")
	ErrVariableExists        = errors.New("storage: variable name already exists at this scope")
	ErrVariableOwnerNotFound = errors.New("storage: variable owner not found")
	ErrVariableValueInvalid  = errors.New("storage: variable value invalid for its type")
	ErrUnknownValueType      = errors.New("storage: unknown value_type")
)

// Variable is one cascaded, plaintext value: its name (the cascade key), its
// declared value_type, its owner on the exclusive arc, and its jsonb value. Unlike
// a secret, the value is shown in the clear (no encryption, no masking).
type Variable struct {
	ID        string
	Name      string
	ValueType string          // string | int | float | bool | json
	OwnerKind string          // global | component | system | location
	OwnerID   *string         // the owning entity id; nil for the global singleton
	OwnerName string          // the owning entity's name (empty for global), for display
	Value     json.RawMessage // the jsonb value, typed by ValueType
	CreatedAt time.Time
	UpdatedAt time.Time
}

// VariableSpec is the create input. OwnerName is the owning entity's name
// (resolved to its id), nil for a global variable. Value is the jsonb value,
// validated against ValueType before any write.
type VariableSpec struct {
	Name      string
	ValueType string
	OwnerKind string
	OwnerName *string
	Value     json.RawMessage
}

// ResolvedVariable is one entry in a component's effective-variables cascade: the
// winning-or-shadowed variable, the owner it comes from, and where that owner sits
// in the chain. Band orders the tiers (0 global, 1 location, 2 system, 3
// component) and Depth is the distance up that tier's tree; the highest band then
// lowest depth wins. Winner marks the resolved value; the shadowed entries come
// back too so the surface can teach the override.
type ResolvedVariable struct {
	ID        string
	Name      string
	ValueType string
	OwnerKind string
	OwnerID   *string
	OwnerName string
	Band      int
	Depth     int
	Winner    bool
	Value     json.RawMessage
}

const variableCols = `id, name, value_type, owner_kind, component_id, system_id, location_id, value, created_at, updated_at`

// CreateVariable writes a new variable at its owner scope. The value_type and the
// value are validated (app-level typing), the owner is resolved and scope-checked
// (a global variable needs an all create scope; a scoped one needs its owner
// within the create scope), and the row plus its audit are written in one
// transaction.
func (p *PG) CreateVariable(ctx context.Context, actorID string, spec VariableSpec, create scope.Set) (*Variable, error) {
	vt, err := variable.ParseValueType(spec.ValueType)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnknownValueType, err)
	}
	if err := variable.ValidateValue(vt, spec.Value); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVariableValueInvalid, err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create variable: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	ownerID, err := p.resolveVariableOwner(ctx, tx, spec.OwnerKind, spec.OwnerName, create)
	if err != nil {
		return nil, err
	}

	compID, sysID, locID := arcColumns(spec.OwnerKind, ownerID)
	v, err := scanVariableRow(tx.QueryRow(ctx, `
		insert into variable (name, value_type, owner_kind, component_id, system_id, location_id, value)
		values ($1, $2, $3, $4, $5, $6, $7)
		returning `+variableCols,
		spec.Name, spec.ValueType, spec.OwnerKind, compID, sysID, locID, []byte(spec.Value)))
	if err != nil {
		return nil, mapVariableWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "variable", v.ID, nil, auditVariable(v)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create variable: %w", err)
	}
	v.OwnerName = ownerNameOf(spec.OwnerName)
	return v, nil
}

// ListVariables returns every variable (the admin directory). Requires an
// all-scope read: a variable is owned across three trees plus a global tier, so
// slice-1 lists it only for the all-scope operator; the scoped, per-component view
// is ResolveVariables. A non-all read is ErrVariableForbidden.
func (p *PG) ListVariables(ctx context.Context, read scope.Set) ([]Variable, error) {
	if !read.All {
		return nil, ErrVariableForbidden
	}
	rows, err := p.pool.Query(ctx, `
		select `+variableColsQualified("v")+`,
		       coalesce(c.name, sy.name, l.name, '') as owner_name
		from variable v
		left join component c on v.component_id = c.id
		left join system    sy on v.system_id   = sy.id
		left join location  l on v.location_id  = l.id
		order by v.name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list variables: %w", err)
	}
	defer rows.Close()
	var out []Variable
	for rows.Next() {
		v, name, err := scanVariableListRow(rows)
		if err != nil {
			return nil, err
		}
		v.OwnerName = name
		out = append(out, *v)
	}
	return out, rows.Err()
}

// UpdateVariable replaces a variable's value, validated against its fixed
// value_type, audited. Name, type, and owner are fixed at creation. Requires the
// owner within the action scope; an unknown or out-of-scope id is
// ErrVariableNotFound.
func (p *PG) UpdateVariable(ctx context.Context, actorID, id string, value json.RawMessage, read, action scope.Set) (*Variable, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update variable: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := p.variableRowForAction(ctx, tx, id, read, action)
	if err != nil {
		return nil, err
	}
	vt, err := variable.ParseValueType(row.valueType)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnknownValueType, err)
	}
	if err := variable.ValidateValue(vt, value); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVariableValueInvalid, err)
	}
	v, err := scanVariableRow(tx.QueryRow(ctx, `
		update variable set value = $2, updated_at = now()
		where id = $1
		returning `+variableCols, id, []byte(value)))
	if err != nil {
		return nil, mapVariableWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "variable", v.ID, nil, auditVariable(v)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update variable: %w", err)
	}
	return v, nil
}

// DeleteVariable removes a variable by id, audited. The owner must be within the
// action scope (all for a global variable); an unknown id or one out of read scope
// is the non-disclosing ErrVariableNotFound.
func (p *PG) DeleteVariable(ctx context.Context, actorID, id string, read, action scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete variable: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := p.variableRowForAction(ctx, tx, id, read, action)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from variable where id = $1`, row.id); err != nil {
		return fmt.Errorf("storage: delete variable: %w", err)
	}
	before := &Variable{ID: row.id, Name: row.name, ValueType: row.valueType, OwnerKind: row.ownerKind, OwnerID: row.ownerID, Value: row.value}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "variable", row.id, auditVariable(before), nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete variable: %w", err)
	}
	return nil
}

// ResolveVariables returns the effective variables for a component: every variable
// that resolves onto it down the structural cascade (global -> location tree ->
// system tree -> component tree, deepest and most-specific winning), with the
// shadowed candidates included so the surface can teach the override. The
// component must be within the read scope; an out-of-scope component is the
// non-disclosing ErrComponentNotFound.
func (p *PG) ResolveVariables(ctx context.Context, componentID string, read scope.Set) ([]ResolvedVariable, error) {
	in, err := inScopeTree(ctx, p.pool, componentTable, componentID, read)
	if err != nil {
		return nil, err
	}
	if !in {
		return nil, ErrComponentNotFound
	}
	rows, err := p.pool.Query(ctx, resolveVariablesSQL, componentID)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve variables: %w", err)
	}
	defer rows.Close()
	var out []ResolvedVariable
	for rows.Next() {
		var (
			r         ResolvedVariable
			ownerID   *string
			ownerName string
			rnk       int
			value     []byte
		)
		if err := rows.Scan(&r.ID, &r.Name, &r.ValueType, &r.OwnerKind, &ownerID,
			&r.Band, &r.Depth, &rnk, &ownerName, &value); err != nil {
			return nil, fmt.Errorf("storage: scan resolved variable: %w", err)
		}
		r.OwnerID = ownerID
		r.OwnerName = ownerName
		r.Winner = rnk == 1
		r.Value = append(json.RawMessage(nil), value...)
		out = append(out, r)
	}
	return out, rows.Err()
}

// resolveVariablesSQL walks the three owner trees up from a component, tags each
// owner with its cascade band and depth, joins the variables owned at those
// scopes, and ranks per name (highest band, then nearest depth wins). It returns
// the winner and every shadowed candidate, each with its owner's display name. The
// CYCLE guards protect against a corrupted parent edge.
const resolveVariablesSQL = `
with recursive
target as (
    select id, system_id, location_id from component where id = $1
),
comp_chain(id, depth) as (
    select id, 0 from component where id = $1
    union all
    select c.parent_id, cc.depth + 1
    from component c join comp_chain cc on c.id = cc.id
    where c.parent_id is not null
) cycle id set comp_cyc using comp_path,
sys_chain(id, depth) as (
    select system_id, 0 from target where system_id is not null
    union all
    select s.parent_id, sc.depth + 1
    from system s join sys_chain sc on s.id = sc.id
    where s.parent_id is not null
) cycle id set sys_cyc using sys_path,
loc_chain(id, depth) as (
    select location_id, 0 from target where location_id is not null
    union all
    select l.parent_id, lc.depth + 1
    from location l join loc_chain lc on l.id = lc.id
    where l.parent_id is not null
) cycle id set loc_cyc using loc_path,
owners(owner_kind, owner_id, band, depth) as (
                select 'global',    null::uuid, 0, 0
    union all   select 'location',  id,         1, depth from loc_chain
    union all   select 'system',    id,         2, depth from sys_chain
    union all   select 'component', id,         3, depth from comp_chain
),
ranked as (
    select v.id, v.name, v.value_type, v.owner_kind, o.owner_id, o.band, o.depth, v.value,
           row_number() over (partition by v.name order by o.band desc, o.depth asc) as rnk
    from variable v
    join owners o
      on o.owner_kind = v.owner_kind
     and o.owner_id is not distinct from coalesce(v.component_id, v.system_id, v.location_id)
)
select r.id, r.name, r.value_type, r.owner_kind, r.owner_id, r.band, r.depth, r.rnk,
       coalesce(c.name, sy.name, l.name, '') as owner_name,
       r.value
from ranked r
left join component c on r.owner_kind = 'component' and c.id = r.owner_id
left join system    sy on r.owner_kind = 'system'   and sy.id = r.owner_id
left join location  l on r.owner_kind = 'location'  and l.id = r.owner_id
order by r.name, r.band desc, r.depth asc`

// --- helpers -----------------------------------------------------------------

// resolveVariableOwner turns an owner kind + optional name into the owning id,
// enforcing the create scope: a global variable needs an all create scope; a
// scoped one resolves its owner in the matching tree and requires it within the
// create scope. Returns a nil id for a global owner.
func (p *PG) resolveVariableOwner(ctx context.Context, q querier, kind string, name *string, create scope.Set) (*string, error) {
	if kind == "global" {
		if !create.All {
			return nil, ErrVariableForbidden
		}
		return nil, nil
	}
	if name == nil {
		return nil, ErrVariableOwnerNotFound
	}
	var (
		id  string
		err error
	)
	switch kind {
	case "component":
		var c *Component
		c, err = scopedByName(ctx, q, componentConfig, *name)
		if c != nil {
			id = c.ID
		}
	case "system":
		var s *System
		s, err = scopedByName(ctx, q, systemConfig, *name)
		if s != nil {
			id = s.ID
		}
	case "location":
		var l *Location
		l, err = scopedByName(ctx, q, locationConfig, *name)
		if l != nil {
			id = l.ID
		}
	default:
		return nil, ErrVariableOwnerNotFound
	}
	if err != nil {
		if errors.Is(err, ErrComponentNotFound) || errors.Is(err, ErrSystemNotFound) || errors.Is(err, ErrLocationNotFound) {
			return nil, ErrVariableOwnerNotFound
		}
		return nil, err
	}
	tbl, _ := scopeKindTable(kind)
	inScope, err := inScopeTree(ctx, q, tbl, id, create)
	if err != nil {
		return nil, err
	}
	if !inScope {
		return nil, ErrVariableForbidden
	}
	return &id, nil
}

// variableRow is the raw scanned variable used by the action-scoped read paths
// (update, delete) before the mutation.
type variableRow struct {
	id        string
	name      string
	valueType string
	ownerKind string
	ownerID   *string
	value     json.RawMessage
}

// variableRowForAction fetches a variable by id and enforces the read-then-action
// scope split on its owner: read (owner in read scope, else the non-disclosing
// not-found) then the action (owner in action scope, else forbidden). A global
// variable needs the all scope on each leg.
func (p *PG) variableRowForAction(ctx context.Context, q querier, id string, read, action scope.Set) (variableRow, error) {
	var (
		row            variableRow
		comp, sys, loc *string
		value          []byte
	)
	err := q.QueryRow(ctx, `
		select id, name, value_type, owner_kind, component_id, system_id, location_id, value
		from variable where id = $1`, id).
		Scan(&row.id, &row.name, &row.valueType, &row.ownerKind, &comp, &sys, &loc, &value)
	if errors.Is(err, pgx.ErrNoRows) {
		return variableRow{}, ErrVariableNotFound
	}
	if err != nil {
		return variableRow{}, fmt.Errorf("storage: get variable: %w", err)
	}
	row.ownerID = firstNonNil(comp, sys, loc)
	row.value = append(json.RawMessage(nil), value...)
	if ok, err := p.variableOwnerInScope(ctx, q, row.ownerKind, row.ownerID, read); err != nil {
		return variableRow{}, err
	} else if !ok {
		return variableRow{}, ErrVariableNotFound
	}
	if ok, err := p.variableOwnerInScope(ctx, q, row.ownerKind, row.ownerID, action); err != nil {
		return variableRow{}, err
	} else if !ok {
		return variableRow{}, ErrVariableForbidden
	}
	return row, nil
}

// variableOwnerInScope reports whether a variable's owner falls within a scope
// set: a global variable needs the all scope; a scoped one defers to the owner
// tree.
func (p *PG) variableOwnerInScope(ctx context.Context, q querier, kind string, id *string, set scope.Set) (bool, error) {
	if kind == "global" {
		return set.All, nil
	}
	if id == nil {
		return false, nil
	}
	tbl, ok := scopeKindTable(kind)
	if !ok {
		return false, nil
	}
	return inScopeTree(ctx, q, tbl, *id, set)
}

// scanVariableRow scans a CREATE/UPDATE returning-row into a Variable.
func scanVariableRow(row pgx.Row) (*Variable, error) {
	var (
		v              Variable
		comp, sys, loc *string
		value          []byte
	)
	if err := row.Scan(&v.ID, &v.Name, &v.ValueType, &v.OwnerKind, &comp, &sys, &loc, &value, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	v.OwnerID = firstNonNil(comp, sys, loc)
	v.Value = append(json.RawMessage(nil), value...)
	return &v, nil
}

func scanVariableListRow(row pgx.Row) (*Variable, string, error) {
	var (
		v              Variable
		comp, sys, loc *string
		value          []byte
		ownerName      string
	)
	if err := row.Scan(&v.ID, &v.Name, &v.ValueType, &v.OwnerKind, &comp, &sys, &loc, &value, &v.CreatedAt, &v.UpdatedAt, &ownerName); err != nil {
		return nil, "", err
	}
	v.OwnerID = firstNonNil(comp, sys, loc)
	v.Value = append(json.RawMessage(nil), value...)
	return &v, ownerName, nil
}

func variableColsQualified(alias string) string {
	return alias + ".id, " + alias + ".name, " + alias + ".value_type, " + alias + ".owner_kind, " +
		alias + ".component_id, " + alias + ".system_id, " + alias + ".location_id, " +
		alias + ".value, " + alias + ".created_at, " + alias + ".updated_at"
}

// auditVariable is the audit projection: metadata plus the (plaintext) value type.
// The value itself is not projected, keeping the audit compact and value-agnostic.
func auditVariable(v *Variable) map[string]any {
	return map[string]any{
		"name":       v.Name,
		"value_type": v.ValueType,
		"owner_kind": v.OwnerKind,
		"owner_id":   v.OwnerID,
	}
}

func mapVariableWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrVariableExists
	}
	return fmt.Errorf("storage: variable write: %w", err)
}
