package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
// of that type carries it. The literal a component sets lives in field_value (a
// later slice); this is the schema row.
type FieldDefinition struct {
	ID            string
	ComponentType string
	Name          string
	DataType      string          // string | int | float | bool | json
	DefaultValue  json.RawMessage // nil when the field has no default
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// FieldDefinitionSpec is the create payload.
type FieldDefinitionSpec struct {
	ComponentType string
	Name          string
	DataType      string
	DefaultValue  json.RawMessage
}

const fieldDefinitionCols = `id, component_type, name, data_type, default_value, created_at, updated_at`

func scanFieldDefinition(row pgx.Row) (*FieldDefinition, error) {
	var fd FieldDefinition
	if err := row.Scan(&fd.ID, &fd.ComponentType, &fd.Name, &fd.DataType, &fd.DefaultValue, &fd.CreatedAt, &fd.UpdatedAt); err != nil {
		return nil, err
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
		insert into field_definition (component_type, name, data_type, default_value)
		values ($1, $2, $3, $4)
		returning `+fieldDefinitionCols,
		spec.ComponentType, spec.Name, spec.DataType, []byte(spec.DefaultValue)))
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
func (p *PG) UpdateFieldDefinition(ctx context.Context, actorID, id, dataType string, def json.RawMessage) (*FieldDefinition, error) {
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
		set data_type = $2, default_value = $3, updated_at = now()
		where id = $1
		returning `+fieldDefinitionCols, id, dataType, []byte(def)))
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
