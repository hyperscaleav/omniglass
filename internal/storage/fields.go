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

// Field-definition sentinel errors. A field_definition is the schema half of the
// field primitive: a typed field declared on a component_type, flat and unscoped
// like the type registries (no owner arc, no ABAC scope), so a duplicate name is
// a request fault rather than a scope fault. An unknown component_type reuses
// the component tier's ErrUnknownComponentType (components.go): same FK, same
// fault.
var (
	ErrFieldDefinitionNotFound = errors.New("storage: field definition not found")
	ErrFieldDefinitionConflict = errors.New("storage: field definition already exists for this type and name")
	ErrInvalidValue            = errors.New("storage: value does not match the field's data_type")
)

// FieldDefinition is a typed field declared on a component_type; every component
// of that type carries it. The literal a component sets lives in field_value;
// this is the schema row.
type FieldDefinition struct {
	ID            string
	ComponentType string
	Name          string
	DisplayName   string          // optional human label; empty when unset (falls back to Name in the UI)
	DataType      string          // string | int | float | bool | json
	DefaultValue  json.RawMessage // nil when the field has no default
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// FieldDefinitionSpec is the create payload.
type FieldDefinitionSpec struct {
	ComponentType string
	Name          string
	DisplayName   string
	DataType      string
	DefaultValue  json.RawMessage
}

// nilIfEmpty maps an empty string to a SQL NULL, so an unset display_name stores
// NULL (not ""), keeping "unset" and "" indistinguishable at the UI fallback.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

const fieldDefinitionCols = `id, component_type, name, display_name, data_type, default_value, created_at, updated_at`

func scanFieldDefinition(row pgx.Row) (*FieldDefinition, error) {
	var (
		fd          FieldDefinition
		displayName *string // NULL when unset
	)
	if err := row.Scan(&fd.ID, &fd.ComponentType, &fd.Name, &displayName, &fd.DataType, &fd.DefaultValue, &fd.CreatedAt, &fd.UpdatedAt); err != nil {
		return nil, err
	}
	if displayName != nil {
		fd.DisplayName = *displayName
	}
	return &fd, nil
}

// ListFieldDefinitions returns every field definition (the admin directory),
// ordered by owning type then name.
func (p *PG) ListFieldDefinitions(ctx context.Context) ([]FieldDefinition, error) {
	rows, err := p.pool.Query(ctx, `select `+fieldDefinitionCols+` from field_definition order by component_type, name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list field definitions: %w", err)
	}
	defer rows.Close()
	var out []FieldDefinition
	for rows.Next() {
		fd, err := scanFieldDefinition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *fd)
	}
	return out, rows.Err()
}

// CreateFieldDefinition declares a new field on a component_type. A default,
// when present, must satisfy the declared data_type: this reuses the variable
// primitive's pure validator (same scalar set for slice 0), so "the value's
// shape matches its type" is defined once. An unknown component_type is the
// FK-backed ErrUnknownComponentType; a duplicate (component_type, name) is
// ErrFieldDefinitionConflict.
func (p *PG) CreateFieldDefinition(ctx context.Context, actorID string, spec FieldDefinitionSpec) (*FieldDefinition, error) {
	if len(spec.DefaultValue) > 0 {
		if err := variable.ValidateValue(variable.ValueType(spec.DataType), spec.DefaultValue); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidValue, err)
		}
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create field definition: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fd, err := scanFieldDefinition(tx.QueryRow(ctx, `
		insert into field_definition (component_type, name, display_name, data_type, default_value)
		values ($1, $2, $3, $4, $5)
		returning `+fieldDefinitionCols,
		spec.ComponentType, spec.Name, nilIfEmpty(spec.DisplayName), spec.DataType, []byte(spec.DefaultValue)))
	if err != nil {
		return nil, mapFieldDefinitionWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "field_definition", fd.ID, nil, auditFieldDefinition(fd)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create field definition: %w", err)
	}
	return fd, nil
}

// UpdateFieldDefinition patches a field definition's data_type and default
// value, revalidating the default against the new data_type. component_type and
// name are fixed at creation (renaming or reparenting a field is a later
// slice). An unknown id is ErrFieldDefinitionNotFound.
func (p *PG) UpdateFieldDefinition(ctx context.Context, actorID, id, dataType, displayName string, def json.RawMessage) (*FieldDefinition, error) {
	if len(def) > 0 {
		if err := variable.ValidateValue(variable.ValueType(dataType), def); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidValue, err)
		}
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update field definition: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fd, err := scanFieldDefinition(tx.QueryRow(ctx, `
		update field_definition
		set data_type = $2, display_name = $3, default_value = $4, updated_at = now()
		where id = $1
		returning `+fieldDefinitionCols, id, dataType, nilIfEmpty(displayName), []byte(def)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFieldDefinitionNotFound
	}
	if err != nil {
		return nil, mapFieldDefinitionWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "field_definition", fd.ID, nil, auditFieldDefinition(fd)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update field definition: %w", err)
	}
	return fd, nil
}

// DeleteFieldDefinition removes a field definition by id, audited. The
// before-image is captured first so the audit records the deleted state; an
// unknown id is ErrFieldDefinitionNotFound.
func (p *PG) DeleteFieldDefinition(ctx context.Context, actorID, id string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete field definition: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := scanFieldDefinition(tx.QueryRow(ctx, `select `+fieldDefinitionCols+` from field_definition where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFieldDefinitionNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: get field definition %q: %w", id, err)
	}
	if _, err := tx.Exec(ctx, `delete from field_definition where id = $1`, id); err != nil {
		return fmt.Errorf("storage: delete field definition %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "field_definition", id, auditFieldDefinition(before), nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete field definition: %w", err)
	}
	return nil
}

// auditFieldDefinition is the audit projection: the definition's metadata only.
// The default_value's contents are not projected, keeping the audit compact and
// value-agnostic (mirroring auditVariable).
func auditFieldDefinition(fd *FieldDefinition) map[string]any {
	return map[string]any{
		"id":             fd.ID,
		"component_type": fd.ComponentType,
		"name":           fd.Name,
		"display_name":   fd.DisplayName,
		"data_type":      fd.DataType,
	}
}

// mapFieldDefinitionWriteErr translates Postgres constraint violations into the
// field-definition sentinels: a duplicate (component_type, name) and an unknown
// component_type FK are request faults the API reports as 409 and 400.
func mapFieldDefinitionWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrFieldDefinitionConflict
		case "23503": // foreign_key_violation
			return ErrUnknownComponentType
		}
	}
	return fmt.Errorf("storage: field definition write: %w", err)
}

// --- field_value: the value half ---------------------------------------------

// Field-value sentinel errors. A field_value is the variable table narrowed to a
// single owner (the component): the field must be defined on that component's own
// type (else ErrFieldNotApplicable), and a component may set one literal per field
// (a duplicate is ErrFieldValueConflict). An unknown or out-of-read-scope value id
// is the non-disclosing ErrFieldValueNotFound; a mismatched value reuses the
// definition tier's ErrInvalidValue.
var (
	ErrFieldValueNotFound = errors.New("storage: field value not found")
	ErrFieldValueConflict = errors.New("storage: field value already set for this field and component")
	ErrFieldNotApplicable = errors.New("storage: field is not defined for this component's type")
)

// FieldValue is a literal a component sets for a field defined on its type. Unlike
// a variable it has no owner arc and no cascade: the sole owner is the component.
type FieldValue struct {
	ID          string
	FieldID     string
	ComponentID string
	Value       json.RawMessage
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// EffectiveField is one field resolved for a component: the set value if present,
// otherwise the definition's default. Value is coalesce(SetValue, DefaultValue);
// SetValue is nil when the component has not overridden the field.
type EffectiveField struct {
	FieldID      string
	Name         string
	DisplayName  string // optional human label; empty when unset
	DataType     string
	DefaultValue json.RawMessage
	SetValue     json.RawMessage // nil when the component has not overridden it
	Value        json.RawMessage // coalesce(SetValue, DefaultValue)
	IsSet        bool
	ValueID      string // the field_value id when set; empty when the field is unset (the id the surface deletes to clear the override)
}

const fieldValueCols = `id, field_id, component_id, value, created_at, updated_at`

func scanFieldValue(row pgx.Row) (*FieldValue, error) {
	var (
		fv    FieldValue
		value []byte
	)
	if err := row.Scan(&fv.ID, &fv.FieldID, &fv.ComponentID, &value, &fv.CreatedAt, &fv.UpdatedAt); err != nil {
		return nil, err
	}
	fv.Value = copyRaw(value)
	return &fv, nil
}

// CreateFieldValue sets a literal for (componentName, fieldName). It resolves the
// component within the create scope, requires the field to be defined on that
// component's own type (else ErrFieldNotApplicable), validates the value against
// the definition's data_type, then inserts the row plus its audit in one
// transaction. A second value for the same field and component is
// ErrFieldValueConflict.
func (p *PG) CreateFieldValue(ctx context.Context, actorID, componentName, fieldName string, value json.RawMessage, create scope.Set) (*FieldValue, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create field value: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	compID, inScope, err := p.componentIDResolved(ctx, tx, componentName, create)
	if err != nil {
		return nil, err // ErrComponentNotFound when the component is absent
	}
	if !inScope {
		// The component exists but is outside the create scope: forbid (403), not the
		// non-disclosing 404 the read path returns.
		return nil, ErrComponentForbidden
	}
	// The field must be defined on this component's own type.
	var fieldID, dataType string
	err = tx.QueryRow(ctx, `
		select fd.id, fd.data_type
		from field_definition fd
		join component c on c.component_type = fd.component_type
		where c.id = $1 and fd.name = $2`, compID, fieldName).Scan(&fieldID, &dataType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFieldNotApplicable
	}
	if err != nil {
		return nil, fmt.Errorf("storage: load field definition: %w", err)
	}
	if err := variable.ValidateValue(variable.ValueType(dataType), value); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidValue, err)
	}
	fv, err := scanFieldValue(tx.QueryRow(ctx, `
		insert into field_value (field_id, component_id, value)
		values ($1, $2, $3)
		returning `+fieldValueCols, fieldID, compID, []byte(value)))
	if err != nil {
		return nil, mapFieldValueWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "field_value", fv.ID, nil, auditFieldValue(fv)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create field value: %w", err)
	}
	return fv, nil
}

// UpdateFieldValue replaces a field value's literal, revalidating it against the
// field's fixed data_type, audited. The owning component must be within the action
// scope; an unknown id or one out of read scope is the non-disclosing
// ErrFieldValueNotFound, readable-but-not-actionable is ErrComponentForbidden.
func (p *PG) UpdateFieldValue(ctx context.Context, actorID, id string, value json.RawMessage, read, action scope.Set) (*FieldValue, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update field value: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := p.fieldValueRowForAction(ctx, tx, id, read, action)
	if err != nil {
		return nil, err
	}
	if err := variable.ValidateValue(variable.ValueType(row.dataType), value); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidValue, err)
	}
	fv, err := scanFieldValue(tx.QueryRow(ctx, `
		update field_value set value = $2, updated_at = now()
		where id = $1
		returning `+fieldValueCols, id, []byte(value)))
	if err != nil {
		return nil, mapFieldValueWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "field_value", fv.ID, nil, auditFieldValue(fv)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update field value: %w", err)
	}
	return fv, nil
}

// DeleteFieldValue removes a field value by id, audited. The owning component must
// be within the action scope; an unknown id or one out of read scope is the
// non-disclosing ErrFieldValueNotFound. The before-image is captured first so the
// audit records the cleared literal (metadata only).
func (p *PG) DeleteFieldValue(ctx context.Context, actorID, id string, read, action scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete field value: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := p.fieldValueRowForAction(ctx, tx, id, read, action)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from field_value where id = $1`, row.id); err != nil {
		return fmt.Errorf("storage: delete field value: %w", err)
	}
	before := &FieldValue{ID: row.id, FieldID: row.fieldID, ComponentID: row.componentID, Value: row.value}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "field_value", row.id, auditFieldValue(before), nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete field value: %w", err)
	}
	return nil
}

// EffectiveFields returns every field declared on a component's type resolved
// against that component: the set literal when present, otherwise the
// definition's default (Value = coalesce(set, default), IsSet marking the
// override). The component must be within the read scope; an out-of-scope
// component is the non-disclosing ErrComponentNotFound.
func (p *PG) EffectiveFields(ctx context.Context, componentName string, read scope.Set) ([]EffectiveField, error) {
	compID, inScope, err := p.componentIDResolved(ctx, p.pool, componentName, read)
	if err != nil {
		return nil, err
	}
	if !inScope {
		// The read path is non-disclosing: an out-of-scope component is not-found, not
		// forbidden, so a reader cannot probe for components it cannot see.
		return nil, ErrComponentNotFound
	}
	rows, err := p.pool.Query(ctx, `
		select fd.id, fd.name, fd.display_name, fd.data_type, fd.default_value,
		       fv.value as set_value,
		       coalesce(fv.value, fd.default_value) as effective_value,
		       (fv.id is not null) as is_set,
		       fv.id as value_id
		from component c
		join field_definition fd on fd.component_type = c.component_type
		left join field_value fv on fv.field_id = fd.id and fv.component_id = c.id
		where c.id = $1
		order by fd.name`, compID)
	if err != nil {
		return nil, fmt.Errorf("storage: effective fields: %w", err)
	}
	defer rows.Close()
	var out []EffectiveField
	for rows.Next() {
		var (
			e             EffectiveField
			def, set, val []byte
			displayName   *string // NULL when unset
			valueID       *string // NULL when the field is unset
		)
		if err := rows.Scan(&e.FieldID, &e.Name, &displayName, &e.DataType, &def, &set, &val, &e.IsSet, &valueID); err != nil {
			return nil, fmt.Errorf("storage: scan effective field: %w", err)
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

// --- field_value helpers -----------------------------------------------------

// componentIDResolved resolves a component by name to its id and reports whether it
// falls within the given scope. An absent component is always ErrComponentNotFound
// (nothing to disclose); an existing but out-of-scope component is returned with
// inScope=false so each caller picks its own sentinel. This mirrors how the variable
// create path (resolveVariableOwner) separates an absent owner from an out-of-scope
// one: the create path forbids (403), the non-disclosing read path 404s. It runs on
// any querier so it works standalone or inside a transaction.
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

// fieldValueRow is the raw field value plus its field's data_type, used by the
// action-scoped read paths (update, delete) before the mutation.
type fieldValueRow struct {
	id          string
	fieldID     string
	componentID string
	dataType    string
	value       json.RawMessage
}

// fieldValueRowForAction fetches a field value by id and enforces the
// read-then-action scope split on its owning component: read (component in read
// scope, else the non-disclosing not-found) then action (component in action
// scope, else forbidden).
func (p *PG) fieldValueRowForAction(ctx context.Context, q querier, id string, read, action scope.Set) (fieldValueRow, error) {
	var (
		row   fieldValueRow
		value []byte
	)
	err := q.QueryRow(ctx, `
		select fv.id, fv.field_id, fv.component_id, fd.data_type, fv.value
		from field_value fv
		join field_definition fd on fd.id = fv.field_id
		where fv.id = $1`, id).
		Scan(&row.id, &row.fieldID, &row.componentID, &row.dataType, &value)
	if errors.Is(err, pgx.ErrNoRows) {
		return fieldValueRow{}, ErrFieldValueNotFound
	}
	if err != nil {
		return fieldValueRow{}, fmt.Errorf("storage: get field value: %w", err)
	}
	row.value = copyRaw(value)
	readable, err := inScopeTree(ctx, q, componentTable, row.componentID, read)
	if err != nil {
		return fieldValueRow{}, err
	}
	if !readable {
		return fieldValueRow{}, ErrFieldValueNotFound
	}
	actionable, err := inScopeTree(ctx, q, componentTable, row.componentID, action)
	if err != nil {
		return fieldValueRow{}, err
	}
	if !actionable {
		return fieldValueRow{}, ErrComponentForbidden
	}
	return row, nil
}

// auditFieldValue is the audit projection: the value's metadata only (id, field,
// component). The literal itself is never projected, keeping the audit compact and
// value-agnostic (mirroring auditFieldDefinition / auditVariable).
func auditFieldValue(fv *FieldValue) map[string]any {
	return map[string]any{
		"id":           fv.ID,
		"field_id":     fv.FieldID,
		"component_id": fv.ComponentID,
	}
}

// mapFieldValueWriteErr translates a duplicate (field_id, component_id) into
// ErrFieldValueConflict; anything else is an opaque write error.
func mapFieldValueWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrFieldValueConflict
	}
	return fmt.Errorf("storage: field value write: %w", err)
}

// copyRaw returns a private copy of a jsonb column, or nil for a SQL NULL, so the
// value does not alias pgx's row buffer.
func copyRaw(b []byte) json.RawMessage {
	if b == nil {
		return nil
	}
	return append(json.RawMessage(nil), b...)
}
