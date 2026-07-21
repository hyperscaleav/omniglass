package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// System-layer sentinel errors, mirroring the location set: the non-disclosing
// 404, the readable-but-not-actionable 403, and the request faults.
var (
	ErrSystemNotFound         = errors.New("storage: system not found")
	ErrSystemForbidden        = errors.New("storage: action not permitted on this system")
	ErrSystemOccupied         = errors.New("storage: system has child systems")
	ErrSystemExists           = errors.New("storage: system name already exists")
	ErrParentSystemNotFound   = errors.New("storage: parent system not found")
	ErrUnknownStandard        = errors.New("storage: unknown standard")
	ErrParentStandardNotFound = errors.New("storage: parent standard not found")
)

// Standard is the blueprint a system conforms to (huddle room, classroom,
// auditorium): the system-side counterpart of product for a component. Beyond
// the registry shape (id, official, display_name) it carries an optional parent
// standard, so a variant inherits from a base one exactly as
// product.parent_product_id does. The registry lists alphabetically by
// display_name; there is no ordering field.
type Standard struct {
	ID               string
	Official         bool
	DisplayName      string
	ParentStandardID *string
}

// System is a composition of components (the service tree): name-addressable,
// nestable via parent_id, optionally located at a location, and optionally
// conforming to a standard. StandardID is nil for a one-off system, mirroring
// component.product_id: a system that matches no blueprint carries only its own
// ad-hoc values.
type System struct {
	ID          string
	Name        string
	DisplayName string
	StandardID  *string
	ParentID    *string
	LocationID  *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SystemSpec is the create input. ParentName nil makes a root system (which only
// an all-scoped create grant may place); LocationName optionally places it at a
// location; StandardID optionally names the blueprint it conforms to.
type SystemSpec struct {
	Name         string
	DisplayName  string
	StandardID   *string
	ParentName   *string
	LocationName *string
}

// SystemPatch is the update input: nil fields unchanged. Reparenting and
// changing located-at are deferred to a later slice.
type SystemPatch struct {
	Name        *string
	DisplayName *string
	StandardID  *string
}

// --- standard registry -------------------------------------------------------

const standardCols = `id, official, display_name, parent_standard_id`

func scanStandard(row pgx.Row) (*Standard, error) {
	var st Standard
	if err := row.Scan(&st.ID, &st.Official, &st.DisplayName, &st.ParentStandardID); err != nil {
		return nil, err
	}
	return &st, nil
}

// mapStandardWriteErr translates Postgres constraint violations on a standard
// write into the registry sentinels: a duplicate id is ErrTypeExists, and the
// only foreign key a standard carries is its parent, so an FK violation is
// ErrParentStandardNotFound rather than an opaque 500.
func mapStandardWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrTypeExists
		case "23503": // foreign_key_violation
			return ErrParentStandardNotFound
		}
	}
	return fmt.Errorf("storage: standard write: %w", err)
}

// UpsertStandard installs or updates a standard by id, the boot-seed phase's
// write. Idempotent: re-seeding the same id updates it in place.
// SeedStandard inserts a shipped example standard only when it is absent. A
// standard is operator-owned content: it is forked from an in-code template and
// then belongs to the estate, so re-seeding must never reassert over an edit the
// operator made. This is deliberately not UpsertStandard, whose ON CONFLICT DO
// UPDATE is the authoritative behavior the canonical catalogs want.
func (p *PG) SeedStandard(ctx context.Context, st Standard) error {
	_, err := p.pool.Exec(ctx, `
		insert into standard (id, official, display_name, parent_standard_id)
		values ($1, $2, $3, $4)
		on conflict (id) do nothing`,
		st.ID, st.Official, st.DisplayName, st.ParentStandardID)
	if err != nil {
		return fmt.Errorf("storage: seed standard %q: %w", st.ID, err)
	}
	return nil
}

func (p *PG) UpsertStandard(ctx context.Context, st Standard) error {
	_, err := p.pool.Exec(ctx, `
		insert into standard (id, official, display_name, parent_standard_id)
		values ($1, $2, $3, $4)
		on conflict (id) do update
			set official           = excluded.official,
			    display_name       = excluded.display_name,
			    parent_standard_id = excluded.parent_standard_id,
			    updated_at         = now()`,
		st.ID, st.Official, st.DisplayName, st.ParentStandardID)
	if err != nil {
		return fmt.Errorf("storage: upsert standard %q: %w", st.ID, err)
	}
	return nil
}

// ListStandards returns every standard, ordered alphabetically by display_name
// then id.
func (p *PG) ListStandards(ctx context.Context) ([]Standard, error) {
	rows, err := p.pool.Query(ctx, `select `+standardCols+` from standard order by display_name, id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list standards: %w", err)
	}
	defer rows.Close()
	out := []Standard{}
	for rows.Next() {
		st, err := scanStandard(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan standard: %w", err)
		}
		out = append(out, *st)
	}
	return out, rows.Err()
}

// GetStandard resolves one standard by id. An unknown id is ErrTypeNotFound.
func (p *PG) GetStandard(ctx context.Context, id string) (*Standard, error) {
	st, err := scanStandard(p.pool.QueryRow(ctx, `select `+standardCols+` from standard where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get standard %q: %w", id, err)
	}
	return st, nil
}

// StandardPatch carries the mutable fields of a standard update; a nil field is
// left unchanged.
type StandardPatch struct {
	DisplayName      *string
	ParentStandardID *string
}

// CreateStandard inserts a custom (official=false) standard and audits it. A
// duplicate id is ErrTypeExists; an unknown parent is ErrParentStandardNotFound.
func (p *PG) CreateStandard(ctx context.Context, actorID string, st Standard) (*Standard, error) {
	st.Official = false
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create standard: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	created, err := scanStandard(tx.QueryRow(ctx, `
		insert into standard (id, official, display_name, parent_standard_id)
		values ($1, false, $2, $3)
		returning `+standardCols,
		st.ID, st.DisplayName, st.ParentStandardID))
	if err != nil {
		return nil, mapStandardWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "standard", created.ID, nil, created); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create standard: %w", err)
	}
	return created, nil
}

// UpdateStandard patches a custom standard's display_name or parent (nil fields
// unchanged) and audits it. Official rows are read-only (ErrTypeOfficial); an
// unknown id is ErrTypeNotFound; an unknown parent is ErrParentStandardNotFound.
func (p *PG) UpdateStandard(ctx context.Context, actorID, id string, patch StandardPatch) (*Standard, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update standard: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "standard", id); err != nil {
		return nil, err
	}
	st, err := scanStandard(tx.QueryRow(ctx, `
		update standard set
			display_name       = coalesce($2, display_name),
			parent_standard_id = coalesce($3, parent_standard_id),
			updated_at         = now()
		where id = $1
		returning `+standardCols,
		id, patch.DisplayName, patch.ParentStandardID))
	if err != nil {
		return nil, mapStandardWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "standard", id, nil, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update standard: %w", err)
	}
	return st, nil
}

// DeleteStandard removes a custom standard, refusing an official row and a row
// still referenced by a system. Child standards are not a refusal: the parent FK
// is ON DELETE SET NULL, so a variant survives its base as a standalone.
func (p *PG) DeleteStandard(ctx context.Context, actorID, id string) error {
	return deleteTypeRow(ctx, p, "standard", "standard", typeRef{table: "system", col: "standard_id"}, actorID, id)
}

// --- system CRUD -------------------------------------------------------------

const systemCols = `id, name, coalesce(display_name, ''), standard_id, parent_id, location_id, created_at, updated_at`

func scanSystem(row pgx.Row) (*System, error) {
	var s System
	if err := row.Scan(&s.ID, &s.Name, &s.DisplayName, &s.StandardID, &s.ParentID, &s.LocationID, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}

// systemConfig drives the generic scoped-CRUD helpers for the system tree.
var systemConfig = scopedConfig[System]{
	table: systemTable, cols: systemCols, resource: "system",
	scan: scanSystem, idOf: func(s *System) string { return s.ID },
	notFound: ErrSystemNotFound, forbidden: ErrSystemForbidden, occupied: ErrSystemOccupied,
}

// ListSystems returns the systems in the caller's read scope (shared read path).
func (p *PG) ListSystems(ctx context.Context, read scope.Set) ([]System, error) {
	return scopedList(ctx, p, systemConfig, read)
}

// GetSystem resolves a system by name within the caller's read scope; absent or
// out of scope is the same non-disclosing ErrSystemNotFound.
func (p *PG) GetSystem(ctx context.Context, name string, read scope.Set) (*System, error) {
	return scopedGet(ctx, p, systemConfig, name, read)
}

// CreateSystem inserts a system under an optional parent and optional location,
// writing the audit row in the same transaction. A root system requires an all
// create scope; a child requires the parent within the create scope.
func (p *PG) CreateSystem(ctx context.Context, actorID string, spec SystemSpec, create scope.Set) (*System, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create system: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := ValidateEntityName(spec.Name); err != nil {
		return nil, err
	}

	var parentID *string
	if spec.ParentName == nil {
		if !create.All {
			return nil, ErrSystemForbidden
		}
	} else {
		parent, err := p.systemByName(ctx, tx, *spec.ParentName)
		if errors.Is(err, ErrSystemNotFound) {
			return nil, ErrParentSystemNotFound
		} else if err != nil {
			return nil, err
		}
		in, err := inScopeTree(ctx, tx, systemTable, parent.ID, create)
		if err != nil {
			return nil, err
		}
		if !in {
			return nil, ErrSystemForbidden
		}
		parentID = &parent.ID
	}

	// Resolve the optional located-at location by name to its id.
	var locationID *string
	if spec.LocationName != nil {
		loc, err := p.locationByName(ctx, tx, *spec.LocationName)
		if err != nil {
			return nil, err // ErrLocationNotFound -> mapped to 422 by the API
		}
		locationID = &loc.ID
	}

	// standard is a catalog, not a scoped tree: resolve by id (a standard's id is
	// its name) with a plain lookup. An unknown id is ErrUnknownStandard -> 422;
	// the FK below is the belt-and-suspenders.
	var standardID *string
	if spec.StandardID != nil {
		var sid string
		err := tx.QueryRow(ctx, `select id from standard where id = $1`, *spec.StandardID).Scan(&sid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnknownStandard
		} else if err != nil {
			return nil, fmt.Errorf("storage: resolve standard %q: %w", *spec.StandardID, err)
		}
		standardID = &sid
	}

	s, err := scanSystem(tx.QueryRow(ctx, `
		insert into system (name, display_name, standard_id, parent_id, location_id)
		values ($1, $2, $3, $4, $5)
		returning `+systemCols,
		spec.Name, nullize(spec.DisplayName), standardID, parentID, locationID))
	if err != nil {
		return nil, mapSystemWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "system", s.ID, nil, s); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create system: %w", err)
	}
	return s, nil
}

// UpdateSystem patches a system by name with the three-way scope split and
// in-transaction audit. Reparent and located-at changes are deferred.
func (p *PG) UpdateSystem(ctx context.Context, actorID, name string, patch SystemPatch, read, action scope.Set) (*System, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update system: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := p.resolveSystemForAction(ctx, tx, name, read, action)
	if err != nil {
		return nil, err
	}
	if patch.Name != nil {
		if err := ValidateEntityName(*patch.Name); err != nil {
			return nil, err
		}
	}
	after, err := scanSystem(tx.QueryRow(ctx, `
		update system set
			name         = coalesce($2, name),
			display_name = coalesce($3, display_name),
			-- standard_id follows the house patch convention: a nil field is left
			-- unchanged, and a provided empty string CLEARS the column, which is how a
			-- classified system is converted back to a one-off. coalesce alone cannot
			-- express the difference between "omitted" and "clear".
			standard_id  = case
				when $4::text is null then standard_id
				when $4 = '' then null
				else $4
			end,
			updated_at   = now()
		where id = $1
		returning `+systemCols,
		before.ID, patch.Name, patch.DisplayName, patch.StandardID))
	if err != nil {
		return nil, mapSystemWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "system", after.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update system: %w", err)
	}
	return after, nil
}

// DeleteSystem removes a system, same three-way split, refused while it has
// child systems (the occupancy rule; member-component re-home is deferred).
func (p *PG) DeleteSystem(ctx context.Context, actorID, name string, read, action scope.Set) error {
	return scopedDelete(ctx, p, systemConfig, actorID, name, read, action)
}

func (p *PG) resolveSystemForAction(ctx context.Context, q querier, name string, read, action scope.Set) (*System, error) {
	return resolveScoped(ctx, q, systemConfig, name, read, action)
}

func (p *PG) systemByName(ctx context.Context, q querier, name string) (*System, error) {
	return scopedByName(ctx, q, systemConfig, name)
}

// SystemNameTaken reports whether a system with this name exists. Scope-blind
// by design: the name unique constraint is global, so availability must be a
// global fact to match it (a scope-aware answer would false-positive on a name
// held outside the caller's scope). Gated at the API by system:update.
func (p *PG) SystemNameTaken(ctx context.Context, name string) (bool, error) {
	var exists bool
	if err := p.pool.QueryRow(ctx, `select exists(select 1 from system where name = $1)`, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("storage: system name taken: %w", err)
	}
	return exists, nil
}

func mapSystemWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return ErrSystemExists
		case "23503":
			switch pgErr.ConstraintName {
			// The standard FK keeps its original constraint name through the
			// system_type -> standard_id column rename (Postgres renames the column,
			// not the constraint), so both names are the same reference; a future
			// schema squash would emit the standard_id one.
			case "system_system_type_fkey", "system_standard_id_fkey":
				return ErrUnknownStandard
			case "system_location_id_fkey":
				// The located-at location was removed between resolve and insert
				// (a race); report it like the resolve-time miss (422).
				return ErrLocationNotFound
			}
		}
	}
	return fmt.Errorf("storage: system write: %w", err)
}
