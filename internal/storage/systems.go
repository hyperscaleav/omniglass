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

// --- system CRUD -------------------------------------------------------------

const systemCols = `id, name, coalesce(display_name, ''), system_type, parent_id, location_id, created_at, updated_at`

func scanSystem(row pgx.Row) (*System, error) {
	var s System
	if err := row.Scan(&s.ID, &s.Name, &s.DisplayName, &s.SystemType, &s.ParentID, &s.LocationID, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}

// ListSystems returns the systems in the caller's read scope, ordered by name,
// using the shared scoped-tree primitive.
func (p *PG) ListSystems(ctx context.Context, read scope.Set) ([]System, error) {
	if read.Empty() {
		return nil, nil
	}
	sql := scopedListSQL(systemTable, systemCols, read.All)
	var (
		rows pgx.Rows
		err  error
	)
	if read.All {
		rows, err = p.pool.Query(ctx, sql)
	} else {
		rows, err = p.pool.Query(ctx, sql, read.IDs)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: list systems: %w", err)
	}
	defer rows.Close()
	var out []System
	for rows.Next() {
		s, err := scanSystem(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan system: %w", err)
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// GetSystem resolves a system by name within the caller's read scope; absent or
// out-of-scope is the same non-disclosing ErrSystemNotFound.
func (p *PG) GetSystem(ctx context.Context, name string, read scope.Set) (*System, error) {
	s, err := p.systemByName(ctx, p.pool, name)
	if err != nil {
		return nil, err
	}
	in, err := inScopeTree(ctx, p.pool, systemTable, s.ID, read)
	if err != nil {
		return nil, err
	}
	if !in {
		return nil, ErrSystemNotFound
	}
	return s, nil
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
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete system: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := p.resolveSystemForAction(ctx, tx, name, read, action)
	if err != nil {
		return err
	}
	var childCount int
	if err := tx.QueryRow(ctx, `select count(*) from system where parent_id = $1`, before.ID).Scan(&childCount); err != nil {
		return fmt.Errorf("storage: count child systems: %w", err)
	}
	if childCount > 0 {
		return ErrSystemOccupied
	}
	if _, err := tx.Exec(ctx, `delete from system where id = $1`, before.ID); err != nil {
		return mapSystemWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "system", before.ID, before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete system: %w", err)
	}
	return nil
}

func (p *PG) resolveSystemForAction(ctx context.Context, q querier, name string, read, action scope.Set) (*System, error) {
	s, err := p.systemByName(ctx, q, name)
	if err != nil {
		return nil, err
	}
	readable, err := inScopeTree(ctx, q, systemTable, s.ID, read)
	if err != nil {
		return nil, err
	}
	if !readable {
		return nil, ErrSystemNotFound
	}
	actionable, err := inScopeTree(ctx, q, systemTable, s.ID, action)
	if err != nil {
		return nil, err
	}
	if !actionable {
		return nil, ErrSystemForbidden
	}
	return s, nil
}

func (p *PG) systemByName(ctx context.Context, q querier, name string) (*System, error) {
	s, err := scanSystem(q.QueryRow(ctx, `select `+systemCols+` from system where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSystemNotFound
	} else if err != nil {
		return nil, fmt.Errorf("storage: load system %q: %w", name, err)
	}
	return s, nil
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
