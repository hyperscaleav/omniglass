package storage

import (
	"context"
	"errors"
	"fmt"

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
	Label       string
	DataType    string
	Validation  []byte
	Description string
}

// PropertyTypePatch carries the mutable fields of a property update; a nil field is
// unchanged. DataType is fixed at create (a property's type must not shift under its
// consumers). Validation replaces wholesale when non-nil.
type PropertyTypePatch struct {
	Label       *string
	Description *string
	Validation  []byte
}

const propertyCols = `id, name, label, data_type, validation, fusion_policy, description, official`

func scanPropertyType(row pgx.Row) (*PropertyType, error) {
	var prop PropertyType
	if err := row.Scan(&prop.ID, &prop.Name, &prop.Label, &prop.DataType, &prop.Validation, &prop.FusionPolicy, &prop.Description, &prop.Official); err != nil {
		return nil, err
	}
	return &prop, nil
}

// GetPropertyType returns one property by name. The registry is estate-wide reference
// data, so there is no scope injection.
func (p *PG) GetPropertyType(ctx context.Context, name string) (*PropertyType, error) {
	if err := RejectAddressForm("property_type", name); err != nil {
		return nil, err
	}
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
	if err := RejectAddressForm("property_type", name); err != nil {
		return err
	}
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
	if err := ValidateName("property_type", spec.Name); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPropertyTypeInvalid, err)
	}
	if err := checkSchemaFragment(spec.Validation, "validation"); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPropertyTypeInvalid, err)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create property: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Cross-registry uniqueness, the mirror of the check in CreateEventType: the
	// three ingest registries (property, metric, event) share one resolution
	// namespace, and a name in two of them resolves to nothing at ingest, so refuse
	// it here where the operator can still choose a different one.
	var clash bool
	if err := tx.QueryRow(ctx,
		`select exists (select 1 from event_type where name = $1)
		     or exists (select 1 from metric_type where name = $1)`, spec.Name).Scan(&clash); err != nil {
		return nil, fmt.Errorf("storage: check registry clash for %q: %w", spec.Name, err)
	}
	if clash {
		return nil, ErrPropertyTypeExists
	}

	// The insert returns the generated id so the audit row can key on the primary
	// key, which survives a later rename, rather than on the name.
	var ptID string
	if err := tx.QueryRow(ctx,
		`insert into property_type (name, label, data_type, validation, description, official)
		 values ($1, $2, $3, $4, $5, false)
		 returning id`,
		spec.Name, spec.Label, spec.DataType, spec.Validation, spec.Description).Scan(&ptID); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrPropertyTypeExists
		}
		return nil, fmt.Errorf("storage: insert property %q: %w", spec.Name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "property_type", ptID, nil, spec); err != nil {
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
	if err := checkSchemaFragment(patch.Validation, "validation"); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPropertyTypeInvalid, err)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update property: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardPropertyMutable(ctx, tx, name); err != nil {
		return nil, err
	}
	before, err := registryAuditImage(ctx, tx, "property_type", name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPropertyTypeNotFound
		}
		return nil, fmt.Errorf("storage: audit image property_type %q: %w", name, err)
	}
	if _, err := tx.Exec(ctx, `
		update property_type set
			label = coalesce($2, label),
			description  = coalesce($3, description),
			validation   = coalesce($4, validation)
		where name = $1`,
		name, patch.Label, patch.Description, patch.Validation); err != nil {
		return nil, fmt.Errorf("storage: update property %q: %w", name, err)
	}
	prop, err := scanPropertyType(tx.QueryRow(ctx, `select `+propertyCols+` from property_type where name = $1`, name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPropertyTypeNotFound
		}
		return nil, fmt.Errorf("storage: reload property %q: %w", name, err)
	}
	after, err := registryAuditImage(ctx, tx, "property_type", name)
	if err != nil {
		return nil, fmt.Errorf("storage: audit image property_type %q: %w", name, err)
	}
	// A property type is addressed by name, but the audit row keys on the uuid the
	// before-image already carries: a rename must not orphan the trail.
	if err := writeAuditRes(ctx, tx, actorID, "update", "property_type", auditImageID(before), before, after); err != nil {
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
	before, err := registryAuditImage(ctx, tx, "property_type", name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPropertyTypeNotFound
		}
		return fmt.Errorf("storage: audit image property_type %q: %w", name, err)
	}
	if _, err := tx.Exec(ctx, `delete from property_type where name = $1`, name); err != nil {
		return fmt.Errorf("storage: delete property %q: %w", name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "property_type", auditImageID(before), before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete property: %w", err)
	}
	return nil
}
