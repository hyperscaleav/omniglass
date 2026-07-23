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
	ErrPropertyNotFound = errors.New("storage: property not found")
	ErrPropertyExists   = errors.New("storage: property name already exists")
	ErrPropertyOfficial = errors.New("storage: official property is read-only")
	ErrPropertyInvalid  = errors.New("storage: property is invalid")
)

// PropertySpec is the create input for a property.
type PropertySpec struct {
	Name        string
	DisplayName string
	Kind        *string
	DataType    string
	Unit        *string
	Precision   *int
	Validation  []byte
	Description string
}

// PropertyPatch carries the mutable fields of a property update; a nil field is
// unchanged. DataType and Kind are fixed at create (a property's type must not shift
// under its consumers). Validation replaces wholesale when non-nil.
type PropertyPatch struct {
	DisplayName *string
	Description *string
	Unit        *string
	Validation  []byte
}

const propertyCols = `id, name, coalesce(display_name, ''), kind, data_type, unit, precision, validation, fusion_policy, description, official`

func scanProperty(row pgx.Row) (*Property, error) {
	var prop Property
	if err := row.Scan(&prop.ID, &prop.Name, &prop.DisplayName, &prop.Kind, &prop.DataType, &prop.Unit, &prop.Precision, &prop.Validation, &prop.FusionPolicy, &prop.Description, &prop.Official); err != nil {
		return nil, err
	}
	return &prop, nil
}

// GetProperty returns one property by name. The registry is estate-wide reference
// data, so there is no scope injection.
func (p *PG) GetProperty(ctx context.Context, name string) (*Property, error) {
	prop, err := scanProperty(p.pool.QueryRow(ctx, `select `+propertyCols+` from property where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPropertyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get property %q: %w", name, err)
	}
	return prop, nil
}

// guardPropertyMutable loads a property's official flag by name: ErrPropertyNotFound
// if absent, ErrPropertyOfficial if seed-owned. Update and delete call it first.
func guardPropertyMutable(ctx context.Context, q querier, name string) error {
	var official bool
	err := q.QueryRow(ctx, `select official from property where name = $1`, name).Scan(&official)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPropertyNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: load property %q: %w", name, err)
	}
	if official {
		return ErrPropertyOfficial
	}
	return nil
}

// CreateProperty inserts a custom (official=false) property and audits it. The name
// must be a valid canonical key and the validation must be well-formed JSON. A
// duplicate name is ErrPropertyExists.
func (p *PG) CreateProperty(ctx context.Context, actorID string, spec PropertySpec) (*Property, error) {
	if err := key.ValidateKey(spec.Name); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPropertyInvalid, err)
	}
	if len(spec.Validation) > 0 && !json.Valid(spec.Validation) {
		return nil, fmt.Errorf("%w: validation is not valid JSON", ErrPropertyInvalid)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create property: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`insert into property (name, display_name, kind, data_type, unit, precision, validation, description, official)
		 values ($1, $2, $3, $4, $5, $6, $7, $8, false)`,
		spec.Name, spec.DisplayName, spec.Kind, spec.DataType, spec.Unit, spec.Precision, spec.Validation, spec.Description); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrPropertyExists
		}
		return nil, fmt.Errorf("storage: insert property %q: %w", spec.Name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "property", spec.Name, nil, spec); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create property: %w", err)
	}
	return p.GetProperty(ctx, spec.Name)
}

// UpdateProperty patches a custom property's mutable fields (nil unchanged) and audits
// it. Official properties are read-only; an unknown name is ErrPropertyNotFound.
func (p *PG) UpdateProperty(ctx context.Context, actorID, name string, patch PropertyPatch) (*Property, error) {
	if len(patch.Validation) > 0 && !json.Valid(patch.Validation) {
		return nil, fmt.Errorf("%w: validation is not valid JSON", ErrPropertyInvalid)
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
		update property set
			display_name = coalesce($2, display_name),
			description  = coalesce($3, description),
			unit         = coalesce($4, unit),
			validation   = coalesce($5, validation)
		where name = $1`,
		name, patch.DisplayName, patch.Description, patch.Unit, patch.Validation); err != nil {
		return nil, fmt.Errorf("storage: update property %q: %w", name, err)
	}
	prop, err := scanProperty(tx.QueryRow(ctx, `select `+propertyCols+` from property where name = $1`, name))
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

// DeleteProperty removes a custom property and audits it. Official properties are
// read-only.
func (p *PG) DeleteProperty(ctx context.Context, actorID, name string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete property: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardPropertyMutable(ctx, tx, name); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from property where name = $1`, name); err != nil {
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
