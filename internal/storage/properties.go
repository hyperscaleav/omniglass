package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/key"
	"github.com/jackc/pgx/v5"
)

// Sentinels for property CRUD. Official (seed-owned) properties are read-only.
var (
	ErrPropertyTypeNotFound = errors.New("storage: property not found")
	ErrPropertyTypeExists   = errors.New("storage: property name already exists")
	ErrPropertyTypeOfficial = errors.New("storage: official property is read-only")
	ErrPropertyTypeInvalid  = errors.New("storage: property is invalid")
)

// PropertyTypeSpec is the create input for a property.
type PropertyTypeSpec struct {
	Name        string
	DisplayName string
	Kind        *string
	DataType    string
	Unit        *string
	Precision   *int
	Validation  []byte
	Description string
}

// PropertyTypePatch carries the mutable fields of a property update; a nil field is
// unchanged. DataType and Kind are fixed at create (a property's type must not shift
// under its consumers). Validation replaces wholesale when non-nil.
type PropertyTypePatch struct {
	DisplayName *string
	Description *string
	Unit        *string
	Validation  []byte
}

const propertyCols = `id, name, coalesce(display_name, ''), kind, data_type, unit, precision, validation, fusion_policy, description, official`

func scanPropertyType(row pgx.Row) (*PropertyType, error) {
	var prop PropertyType
	if err := row.Scan(&prop.ID, &prop.Name, &prop.DisplayName, &prop.Kind, &prop.DataType, &prop.Unit, &prop.Precision, &prop.Validation, &prop.FusionPolicy, &prop.Description, &prop.Official); err != nil {
		return nil, err
	}
	return &prop, nil
}

// GetPropertyType returns one property by name. The registry is estate-wide reference
// data, so there is no scope injection.
func (p *PG) GetPropertyType(ctx context.Context, name string) (*PropertyType, error) {
	prop, err := scanPropertyType(p.pool.QueryRow(ctx, `select `+propertyCols+` from property_type where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPropertyTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get property %q: %w", name, err)
	}
	return prop, nil
}

// guardPropertyMutable loads a property's official flag by name: ErrPropertyTypeNotFound
// if absent, ErrPropertyTypeOfficial if seed-owned. Update and delete call it first.
func guardPropertyMutable(ctx context.Context, q querier, name string) error {
	var official bool
	err := q.QueryRow(ctx, `select official from property_type where name = $1`, name).Scan(&official)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPropertyTypeNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: load property %q: %w", name, err)
	}
	if official {
		return ErrPropertyTypeOfficial
	}
	return nil
}

// CreatePropertyType inserts a custom (official=false) property and audits it. The name
// must be a valid canonical key and the validation must be well-formed JSON. A
// duplicate name is ErrPropertyTypeExists.
func (p *PG) CreatePropertyType(ctx context.Context, actorID string, spec PropertyTypeSpec) (*PropertyType, error) {
	if err := key.ValidateKey(spec.Name); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPropertyTypeInvalid, err)
	}
	if len(spec.Validation) > 0 && !json.Valid(spec.Validation) {
		return nil, fmt.Errorf("%w: validation is not valid JSON", ErrPropertyTypeInvalid)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create property: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`insert into property_type (name, display_name, kind, data_type, unit, precision, validation, description, official)
		 values ($1, $2, $3, $4, $5, $6, $7, $8, false)`,
		spec.Name, spec.DisplayName, spec.Kind, spec.DataType, spec.Unit, spec.Precision, spec.Validation, spec.Description); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrPropertyTypeExists
		}
		return nil, fmt.Errorf("storage: insert property %q: %w", spec.Name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "property", spec.Name, nil, spec); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create property: %w", err)
	}
	return p.GetPropertyType(ctx, spec.Name)
}

// UpdatePropertyType patches a custom property's mutable fields (nil unchanged) and audits
// it. Official properties are read-only; an unknown name is ErrPropertyTypeNotFound.
func (p *PG) UpdatePropertyType(ctx context.Context, actorID, name string, patch PropertyTypePatch) (*PropertyType, error) {
	if len(patch.Validation) > 0 && !json.Valid(patch.Validation) {
		return nil, fmt.Errorf("%w: validation is not valid JSON", ErrPropertyTypeInvalid)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update property: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardPropertyMutable(ctx, tx, name); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		update property_type set
			display_name = coalesce($2, display_name),
			description  = coalesce($3, description),
			unit         = coalesce($4, unit),
			validation   = coalesce($5, validation)
		where name = $1`,
		name, patch.DisplayName, patch.Description, patch.Unit, patch.Validation); err != nil {
		return nil, fmt.Errorf("storage: update property %q: %w", name, err)
	}
	prop, err := scanPropertyType(tx.QueryRow(ctx, `select `+propertyCols+` from property_type where name = $1`, name))
	if err != nil {
		return nil, fmt.Errorf("storage: reload property %q: %w", name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "property", name, nil, prop); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update property: %w", err)
	}
	return prop, nil
}

// DeletePropertyType removes a custom property and audits it. Official properties are
// read-only.
func (p *PG) DeletePropertyType(ctx context.Context, actorID, name string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete property: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardPropertyMutable(ctx, tx, name); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from property_type where name = $1`, name); err != nil {
		return fmt.Errorf("storage: delete property %q: %w", name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "property", name, map[string]string{"name": name}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete property: %w", err)
	}
	return nil
}
