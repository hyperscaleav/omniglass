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

// StandardProperty is one line of a standard's declared-property contract: the
// standard exposes this property, optionally with a default, optionally required.
// The property catalog owns data_type and validation, so neither is repeated
// here; a contract row only names the property and how the standard presents it.
type StandardProperty struct {
	ID           string
	StandardID   string
	StandardName string
	PropertyName string
	DefaultValue json.RawMessage // nil when the contract sets no default
	Required     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// StandardPropertySpec is the write payload for one contract line. The property
// is addressed by name, so a spec is complete on its own: there is no separate
// create and update shape (a set is an upsert on the standard/property pair).
type StandardPropertySpec struct {
	PropertyName string
	DefaultValue json.RawMessage
	Required     bool
}

const standardPropertyCols = `id, standard_id,
	(select s.name from standard s where s.id = standard_property.standard_id) as standard_handle, property_name, default_value, required, created_at, updated_at`

func scanStandardProperty(row pgx.Row) (*StandardProperty, error) {
	var (
		pp  StandardProperty
		def []byte // NULL when the contract sets no default
	)
	if err := row.Scan(&pp.ID, &pp.StandardID, &pp.StandardName, &pp.PropertyName, &def, &pp.Required, &pp.CreatedAt, &pp.UpdatedAt); err != nil {
		return nil, err
	}
	pp.DefaultValue = copyRaw(def)
	return &pp, nil
}

// mapStandardPropertyWriteErr translates Postgres constraint violations on a
// contract write into sentinels the caller already handles. The two foreign keys
// are told apart by constraint name so the fault names the side that is missing:
// an unknown standard is ErrTypeNotFound (what the official guard would have
// returned), an unknown property is the catalog's ErrPropertyNotFound.
func mapStandardPropertyWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrTypeExists
		case "23503": // foreign_key_violation
			if pgErr.ConstraintName == "standard_property_property_name_fkey" {
				return ErrPropertyNotFound
			}
			return ErrTypeNotFound
		}
	}
	return fmt.Errorf("storage: standard property write: %w", err)
}

// upsertStandardPropertyRow writes one contract line, keyed by the unique
// (standard_id, property_name): the first write inserts, a later one revises the
// default and the required flag in place. Shared by the audited operator path
// and the boot-seed path so both keep the same semantics. It runs on any
// querier, so the seed path needs no transaction for its single statement.
func upsertStandardPropertyRow(ctx context.Context, q querier, standardID string, spec StandardPropertySpec) (*StandardProperty, error) {
	var known bool
	if err := q.QueryRow(ctx, `select true from standard where `+registryRefCol("standard", standardID)+` = $1`, standardID).Scan(&known); err != nil {
		return nil, ErrTypeNotFound
	}
	pp, err := scanStandardProperty(q.QueryRow(ctx, `
		insert into standard_property (standard_id, property_name, default_value, required)
		values ((select id from standard where `+registryRefCol("standard", standardID)+` = $1), $2, $3, $4)
		on conflict (standard_id, property_name) do update
			set default_value = excluded.default_value,
			    required      = excluded.required,
			    updated_at    = now()
		returning `+standardPropertyCols,
		standardID, spec.PropertyName, []byte(spec.DefaultValue), spec.Required))
	if err != nil {
		return nil, mapStandardPropertyWriteErr(err)
	}
	return pp, nil
}

// ListStandardProperties returns a standard's declared-property contract, ordered
// by property name. A standard that declares nothing lists empty; an unknown
// standard is indistinguishable from one with an empty contract, since the read
// side has nothing to disclose.
func (p *PG) ListStandardProperties(ctx context.Context, standardID string) ([]StandardProperty, error) {
	rows, err := p.pool.Query(ctx, `select `+standardPropertyCols+` from standard_property where standard_id = (select id from standard where `+registryRefCol("standard", standardID)+` = $1) order by property_name`, standardID)
	if err != nil {
		return nil, fmt.Errorf("storage: list standard properties %q: %w", standardID, err)
	}
	defer rows.Close()
	out := []StandardProperty{}
	for rows.Next() {
		pp, err := scanStandardProperty(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan standard property: %w", err)
		}
		out = append(out, *pp)
	}
	return out, rows.Err()
}

// SetStandardProperty declares a property on a standard (or revises the
// declaration), audited. Official standards carry seed-owned contracts and are
// read-only (ErrTypeOfficial); an unknown standard is ErrTypeNotFound and an
// unknown property is ErrPropertyNotFound. The audit names the standard as the
// resource because the contract belongs to it, with the verb reflecting whether
// this write added the line or revised one already there.
func (p *PG) SetStandardProperty(ctx context.Context, actorID, standardID string, spec StandardPropertySpec) (*StandardProperty, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin set standard property: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "standard", standardID); err != nil {
		return nil, err
	}
	// The before-image decides create vs update and gives the audit its old side.
	var before any
	prior, err := scanStandardProperty(tx.QueryRow(ctx,
		`select `+standardPropertyCols+` from standard_property where standard_id = (select id from standard where `+registryRefCol("standard", standardID)+` = $1) and property_name = $2`,
		standardID, spec.PropertyName))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("storage: load standard property %q/%q: %w", standardID, spec.PropertyName, err)
	default:
		before = prior
	}

	pp, err := upsertStandardPropertyRow(ctx, tx, standardID, spec)
	if err != nil {
		return nil, err
	}
	verb := "create"
	if before != nil {
		verb = "update"
	}
	if err := writeAuditRes(ctx, tx, actorID, verb, "standard", standardID, before, pp); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit set standard property: %w", err)
	}
	return pp, nil
}

// DeleteStandardProperty withdraws one property from a standard's contract,
// audited as an update to the standard. Official standards are read-only
// (ErrTypeOfficial); an undeclared property is ErrTypeNotFound.
func (p *PG) DeleteStandardProperty(ctx context.Context, actorID, standardID, propertyName string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete standard property: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "standard", standardID); err != nil {
		return err
	}
	// Delete and capture the before-image in one statement, so the audit records
	// the withdrawn declaration and a missing row is caught without a second read.
	before, err := scanStandardProperty(tx.QueryRow(ctx, `
		delete from standard_property
		where standard_id = (select id from standard where `+registryRefCol("standard", standardID)+` = $1) and property_name = $2
		returning `+standardPropertyCols, standardID, propertyName))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTypeNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: delete standard property %q/%q: %w", standardID, propertyName, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "standard", standardID, before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete standard property: %w", err)
	}
	return nil
}

// UpsertStandardProperty installs one contract line for the boot-seed phase.
// Idempotent, and deliberately unguarded and unaudited: the seed owns the
// official standards' contracts, so the official read-only rule (which protects
// them from operators) does not apply to the writer that ships them.
func (p *PG) UpsertStandardProperty(ctx context.Context, standardID string, spec StandardPropertySpec) error {
	_, err := upsertStandardPropertyRow(ctx, p.pool, standardID, spec)
	return err
}
