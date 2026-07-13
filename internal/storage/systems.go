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
	ErrSystemNotFound       = errors.New("storage: system not found")
	ErrSystemForbidden      = errors.New("storage: action not permitted on this system")
	ErrSystemOccupied       = errors.New("storage: system has child systems")
	ErrSystemExists         = errors.New("storage: system name already exists")
	ErrParentSystemNotFound = errors.New("storage: parent system not found")
	ErrUnknownSystemType    = errors.New("storage: unknown system_type")
)

// SystemType is a registry row classifying a system (id, official, display_name,
// rank), the shape-definer, mirroring location_type.
type SystemType struct {
	ID          string
	Official    bool
	DisplayName string
	Rank        int
}

// System is a composition of components (the service tree): name-addressable,
// classified by system_type, nestable via parent_id, and optionally located at
// a location.
type System struct {
	ID          string
	Name        string
	DisplayName string
	SystemType  string
	ParentID    *string
	LocationID  *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SystemSpec is the create input. ParentName nil makes a root system (which only
// an all-scoped create grant may place); LocationName optionally places it at a
// location.
type SystemSpec struct {
	Name         string
	DisplayName  string
	SystemType   string
	ParentName   *string
	LocationName *string
}

// SystemPatch is the update input: nil fields unchanged. Reparenting and
// changing located-at are deferred to a later slice.
type SystemPatch struct {
	DisplayName *string
	SystemType  *string
}

// --- system_type registry ---------------------------------------------------

func (p *PG) UpsertSystemType(ctx context.Context, st SystemType) error {
	_, err := p.pool.Exec(ctx, `
		insert into system_type (id, official, display_name, rank)
		values ($1, $2, $3, $4)
		on conflict (id) do update
			set official = excluded.official, display_name = excluded.display_name, rank = excluded.rank`,
		st.ID, st.Official, st.DisplayName, st.Rank)
	if err != nil {
		return fmt.Errorf("storage: upsert system_type %q: %w", st.ID, err)
	}
	return nil
}

func (p *PG) ListSystemTypes(ctx context.Context) ([]SystemType, error) {
	rows, err := p.pool.Query(ctx, `select id, official, display_name, rank from system_type order by rank, id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list system_types: %w", err)
	}
	defer rows.Close()
	var out []SystemType
	for rows.Next() {
		var st SystemType
		if err := rows.Scan(&st.ID, &st.Official, &st.DisplayName, &st.Rank); err != nil {
			return nil, fmt.Errorf("storage: scan system_type: %w", err)
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// SystemTypePatch carries the mutable fields of a system_type update; a nil field
// is left unchanged.
type SystemTypePatch struct {
	DisplayName *string
	Rank        *int
}

// CreateSystemType inserts a custom (official=false) system_type and audits it. A
// duplicate id is ErrTypeExists.
func (p *PG) CreateSystemType(ctx context.Context, actorID string, st SystemType) (*SystemType, error) {
	st.Official = false
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create system_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`insert into system_type (id, official, display_name, rank) values ($1, false, $2, $3)`,
		st.ID, st.DisplayName, st.Rank); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrTypeExists
		}
		return nil, fmt.Errorf("storage: insert system_type %q: %w", st.ID, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "system_type", st.ID, nil, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create system_type: %w", err)
	}
	return &st, nil
}

// UpdateSystemType patches a custom system_type (nil fields unchanged) and audits
// it. Official rows are read-only; an unknown id is ErrTypeNotFound.
func (p *PG) UpdateSystemType(ctx context.Context, actorID, id string, patch SystemTypePatch) (*SystemType, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update system_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "system_type", id); err != nil {
		return nil, err
	}
	var st SystemType
	if err := tx.QueryRow(ctx, `
		update system_type set
			display_name = coalesce($2, display_name),
			rank         = coalesce($3, rank)
		where id = $1
		returning id, official, display_name, rank`,
		id, patch.DisplayName, patch.Rank).
		Scan(&st.ID, &st.Official, &st.DisplayName, &st.Rank); err != nil {
		return nil, fmt.Errorf("storage: update system_type %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "system_type", id, nil, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update system_type: %w", err)
	}
	return &st, nil
}

// DeleteSystemType removes a custom system_type, refusing an official row and a
// row still referenced by a system.
func (p *PG) DeleteSystemType(ctx context.Context, actorID, id string) error {
	return deleteTypeRow(ctx, p, "system_type", "system_type", typeRef{table: "system", col: "system_type"}, actorID, id)
}

// --- system CRUD -------------------------------------------------------------

const systemCols = `id, name, coalesce(display_name, ''), system_type, parent_id, location_id, created_at, updated_at`

func scanSystem(row pgx.Row) (*System, error) {
	var s System
	if err := row.Scan(&s.ID, &s.Name, &s.DisplayName, &s.SystemType, &s.ParentID, &s.LocationID, &s.CreatedAt, &s.UpdatedAt); err != nil {
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

	s, err := scanSystem(tx.QueryRow(ctx, `
		insert into system (name, display_name, system_type, parent_id, location_id)
		values ($1, $2, $3, $4, $5)
		returning `+systemCols,
		spec.Name, nullize(spec.DisplayName), spec.SystemType, parentID, locationID))
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
	after, err := scanSystem(tx.QueryRow(ctx, `
		update system set
			display_name = coalesce($2, display_name),
			system_type  = coalesce($3, system_type),
			updated_at   = now()
		where id = $1
		returning `+systemCols,
		before.ID, patch.DisplayName, patch.SystemType))
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

func mapSystemWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return ErrSystemExists
		case "23503":
			switch pgErr.ConstraintName {
			case "system_system_type_fkey":
				return ErrUnknownSystemType
			case "system_location_id_fkey":
				// The located-at location was removed between resolve and insert
				// (a race); report it like the resolve-time miss (422).
				return ErrLocationNotFound
			}
		}
	}
	return fmt.Errorf("storage: system write: %w", err)
}
