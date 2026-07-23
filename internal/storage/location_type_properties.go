package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// LocationTypeProperty is one line of a location type's declared-property contract: the
// location type exposes this property, optionally with a default, optionally required.
// The property catalog owns data_type and validation, so neither is repeated
// here; a contract row only names the property and how the location type presents it.
type LocationTypeProperty struct {
	ID             string
	LocationTypeID string
	PropertyName   string
	PropertyTypeID string
	DefaultValue   json.RawMessage // nil when the contract sets no default
	Required       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// LocationTypePropertySpec is the write payload for one contract line. The property
// is addressed by name, so a spec is complete on its own: there is no separate
// create and update shape (a set is an upsert on the location type/property pair).
type LocationTypePropertySpec struct {
	PropertyName string
	DefaultValue json.RawMessage
	Required     bool
}

const locationTypePropertyCols = `id, location_type_id, (select pr.name from property_type pr where pr.id = location_type_property.property_type_id) as property_name, location_type_property.property_type_id as property_type_id, default_value, required, created_at, updated_at`

func scanLocationTypeProperty(row pgx.Row) (*LocationTypeProperty, error) {
	var (
		pp  LocationTypeProperty
		def []byte // NULL when the contract sets no default
	)
	if err := row.Scan(&pp.ID, &pp.LocationTypeID, &pp.PropertyName, &pp.PropertyTypeID, &def, &pp.Required, &pp.CreatedAt, &pp.UpdatedAt); err != nil {
		return nil, err
	}
	pp.DefaultValue = copyRaw(def)
	return &pp, nil
}

// mapLocationTypePropertyWriteErr translates Postgres constraint violations on a
// contract write into sentinels the caller already handles. The two foreign keys
// are told apart by constraint name so the fault names the side that is missing:
// an unknown location type is ErrTypeNotFound (what the official guard would have
// returned), an unknown property is the catalog's ErrPropertyNotFound.
func mapLocationTypePropertyWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrTypeExists
		case "23503": // foreign_key_violation
			if pgErr.ConstraintName == "location_type_property_property_id_fkey" {
				return ErrPropertyTypeNotFound
			}
			return ErrTypeNotFound
		}
	}
	return fmt.Errorf("storage: location type property write: %w", err)
}

// upsertLocationTypePropertyRow writes one contract line, keyed by the unique
// (location_type_id, property_name): the first write inserts, a later one revises the
// default and the required flag in place. Shared by the audited operator path
// and the boot-seed path so both keep the same semantics. It runs on any
// querier, so the seed path needs no transaction for its single statement.
func upsertLocationTypePropertyRow(ctx context.Context, q querier, locationTypeID string, spec LocationTypePropertySpec) (*LocationTypeProperty, error) {
	if err := requireProperty(ctx, q, spec.PropertyName); err != nil {
		return nil, err
	}
	pp, err := scanLocationTypeProperty(q.QueryRow(ctx, `
		insert into location_type_property (location_type_id, property_type_id, default_value, required)
		values ((select id from location_type where `+registryRefCol(locationTypeID)+` = $1), (select id from property_type where name = $2), $3, $4)
		on conflict (location_type_id, property_type_id) do update
			set default_value = excluded.default_value,
			    required      = excluded.required,
			    updated_at    = now()
		returning `+locationTypePropertyCols,
		locationTypeID, spec.PropertyName, []byte(spec.DefaultValue), spec.Required))
	if err != nil {
		return nil, mapLocationTypePropertyWriteErr(err)
	}
	return pp, nil
}

// ListLocationTypeProperties returns a location type's declared-property contract, ordered
// by property name. A location type that declares nothing lists empty; an unknown
// location type is indistinguishable from one with an empty contract, since the read
// side has nothing to disclose.
func (p *PG) ListLocationTypeProperties(ctx context.Context, locationTypeID string) ([]LocationTypeProperty, error) {
	rows, err := p.pool.Query(ctx, `select `+locationTypePropertyCols+` from location_type_property where location_type_id = (select id from location_type where `+registryRefCol(locationTypeID)+` = $1) order by (select pr.name from property_type pr where pr.id = location_type_property.property_type_id)`, locationTypeID)
	if err != nil {
		return nil, fmt.Errorf("storage: list location type properties %q: %w", locationTypeID, err)
	}
	defer rows.Close()
	out := []LocationTypeProperty{}
	for rows.Next() {
		pp, err := scanLocationTypeProperty(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan location type property: %w", err)
		}
		out = append(out, *pp)
	}
	return out, rows.Err()
}

// SetLocationTypeProperty declares a property on a location type (or revises the
// declaration), audited. Official location types carry seed-owned contracts and are
// read-only (ErrTypeOfficial); an unknown location type is ErrTypeNotFound and an
// unknown property is ErrPropertyNotFound. The audit names the location type as the
// resource because the contract belongs to it, with the verb reflecting whether
// this write added the line or revised one already there.
func (p *PG) SetLocationTypeProperty(ctx context.Context, actorID, locationTypeID string, spec LocationTypePropertySpec) (*LocationTypeProperty, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin set location type property: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "location_type", locationTypeID); err != nil {
		return nil, err
	}
	// The before-image decides create vs update and gives the audit its old side.
	var before any
	prior, err := scanLocationTypeProperty(tx.QueryRow(ctx,
		`select `+locationTypePropertyCols+` from location_type_property where location_type_id = (select id from location_type where `+registryRefCol(locationTypeID)+` = $1) and property_type_id = (select id from property_type where name = $2)`,
		locationTypeID, spec.PropertyName))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("storage: load location type property %q/%q: %w", locationTypeID, spec.PropertyName, err)
	default:
		before = prior
	}

	pp, err := upsertLocationTypePropertyRow(ctx, tx, locationTypeID, spec)
	if err != nil {
		return nil, err
	}
	verb := "create"
	if before != nil {
		verb = "update"
	}
	if err := writeAuditRes(ctx, tx, actorID, verb, "location_type", locationTypeID, before, pp); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit set location type property: %w", err)
	}
	return pp, nil
}

// DeleteLocationTypeProperty withdraws one property from a location type's contract,
// audited as an update to the location type. Official location types are read-only
// (ErrTypeOfficial); an undeclared property is ErrTypeNotFound.
func (p *PG) DeleteLocationTypeProperty(ctx context.Context, actorID, locationTypeID, propertyName string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete location type property: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "location_type", locationTypeID); err != nil {
		return err
	}
	// Delete and capture the before-image in one statement, so the audit records
	// the withdrawn declaration and a missing row is caught without a second read.
	before, err := scanLocationTypeProperty(tx.QueryRow(ctx, `
		delete from location_type_property
		where location_type_id = (select id from location_type where `+registryRefCol(locationTypeID)+` = $1) and property_type_id = (select id from property_type where name = $2)
		returning `+locationTypePropertyCols, locationTypeID, propertyName))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTypeNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: delete location type property %q/%q: %w", locationTypeID, propertyName, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "location_type", locationTypeID, before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete location type property: %w", err)
	}
	return nil
}

// UpsertLocationTypeProperty installs one contract line for the boot-seed phase.
// Idempotent, and deliberately unguarded and unaudited: the seed owns the
// official location types' contracts, so the official read-only rule (which protects
// them from operators) does not apply to the writer that ships them.
func (p *PG) UpsertLocationTypeProperty(ctx context.Context, locationTypeID string, spec LocationTypePropertySpec) error {
	_, err := upsertLocationTypePropertyRow(ctx, p.pool, locationTypeID, spec)
	return err
}
