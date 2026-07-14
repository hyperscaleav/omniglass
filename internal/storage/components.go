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

// Component-layer sentinel errors, mirroring the location/system sets.
var (
	ErrComponentNotFound       = errors.New("storage: component not found")
	ErrComponentForbidden      = errors.New("storage: action not permitted on this component")
	ErrComponentOccupied       = errors.New("storage: component has child components")
	ErrComponentExists         = errors.New("storage: component name already exists")
	ErrParentComponentNotFound = errors.New("storage: parent component not found")
	ErrUnknownComponentType    = errors.New("storage: unknown component_type")
)

// ComponentType is a registry row classifying a component. The registry lists
// alphabetically by display_name; there is no ordering field.
type ComponentType struct {
	ID          string
	Official    bool
	DisplayName string
}

// Component is a leaf of the estate: name-addressable, classified by
// component_type, nestable via parent_id, belonging to a primary system and
// located at a location.
type Component struct {
	ID            string
	Name          string
	DisplayName   string
	ComponentType string
	ParentID      *string
	SystemID      *string
	LocationID    *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ComponentSpec is the create input. ParentName nil makes a root component;
// SystemName / LocationName optionally bind it to a system and place it.
type ComponentSpec struct {
	Name          string
	DisplayName   string
	ComponentType string
	ParentName    *string
	SystemName    *string
	LocationName  *string
}

// ComponentPatch is the update input: nil fields unchanged. Reparent, rebind,
// and relocate are deferred.
type ComponentPatch struct {
	DisplayName   *string
	ComponentType *string
}

// --- component_type registry -------------------------------------------------

func (p *PG) UpsertComponentType(ctx context.Context, ct ComponentType) error {
	_, err := p.pool.Exec(ctx, `
		insert into component_type (id, official, display_name)
		values ($1, $2, $3)
		on conflict (id) do update
			set official = excluded.official, display_name = excluded.display_name`,
		ct.ID, ct.Official, ct.DisplayName)
	if err != nil {
		return fmt.Errorf("storage: upsert component_type %q: %w", ct.ID, err)
	}
	return nil
}

func (p *PG) ListComponentTypes(ctx context.Context) ([]ComponentType, error) {
	rows, err := p.pool.Query(ctx, `select id, official, display_name from component_type order by display_name, id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list component_types: %w", err)
	}
	defer rows.Close()
	var out []ComponentType
	for rows.Next() {
		var ct ComponentType
		if err := rows.Scan(&ct.ID, &ct.Official, &ct.DisplayName); err != nil {
			return nil, fmt.Errorf("storage: scan component_type: %w", err)
		}
		out = append(out, ct)
	}
	return out, rows.Err()
}

// ComponentTypePatch carries the mutable fields of a component_type update; a nil
// field is left unchanged.
type ComponentTypePatch struct {
	DisplayName *string
}

// CreateComponentType inserts a custom (official=false) component_type and audits
// it. A duplicate id is ErrTypeExists.
func (p *PG) CreateComponentType(ctx context.Context, actorID string, ct ComponentType) (*ComponentType, error) {
	ct.Official = false
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create component_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`insert into component_type (id, official, display_name) values ($1, false, $2)`,
		ct.ID, ct.DisplayName); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrTypeExists
		}
		return nil, fmt.Errorf("storage: insert component_type %q: %w", ct.ID, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "component_type", ct.ID, nil, ct); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create component_type: %w", err)
	}
	return &ct, nil
}

// UpdateComponentType patches a custom component_type's display_name (nil
// unchanged) and audits it. Official rows are read-only; an unknown id is
// ErrTypeNotFound.
func (p *PG) UpdateComponentType(ctx context.Context, actorID, id string, patch ComponentTypePatch) (*ComponentType, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update component_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "component_type", id); err != nil {
		return nil, err
	}
	var ct ComponentType
	if err := tx.QueryRow(ctx, `
		update component_type set
			display_name = coalesce($2, display_name)
		where id = $1
		returning id, official, display_name`,
		id, patch.DisplayName).
		Scan(&ct.ID, &ct.Official, &ct.DisplayName); err != nil {
		return nil, fmt.Errorf("storage: update component_type %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "component_type", id, nil, ct); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update component_type: %w", err)
	}
	return &ct, nil
}

// DeleteComponentType removes a custom component_type, refusing an official row
// and a row still referenced by a component.
func (p *PG) DeleteComponentType(ctx context.Context, actorID, id string) error {
	return deleteTypeRow(ctx, p, "component_type", "component_type", typeRef{table: "component", col: "component_type"}, actorID, id)
}

// --- component CRUD (read/delete via the generic helpers) --------------------

const componentCols = `id, name, coalesce(display_name, ''), component_type, parent_id, system_id, location_id, created_at, updated_at`

func scanComponent(row pgx.Row) (*Component, error) {
	var c Component
	if err := row.Scan(&c.ID, &c.Name, &c.DisplayName, &c.ComponentType, &c.ParentID, &c.SystemID, &c.LocationID, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

var componentConfig = scopedConfig[Component]{
	table: componentTable, cols: componentCols, resource: "component",
	scan: scanComponent, idOf: func(c *Component) string { return c.ID },
	notFound: ErrComponentNotFound, forbidden: ErrComponentForbidden, occupied: ErrComponentOccupied,
}

func (p *PG) ListComponents(ctx context.Context, read scope.Set) ([]Component, error) {
	return scopedList(ctx, p, componentConfig, read)
}

func (p *PG) GetComponent(ctx context.Context, name string, read scope.Set) (*Component, error) {
	return scopedGet(ctx, p, componentConfig, name, read)
}

func (p *PG) DeleteComponent(ctx context.Context, actorID, name string, read, action scope.Set) error {
	return scopedDelete(ctx, p, componentConfig, actorID, name, read, action)
}

// CreateComponent inserts a component under an optional parent, bound to an
// optional system and location, writing the audit row in the same transaction.
// A root component requires an all create scope; a child requires the parent in
// the create scope.
func (p *PG) CreateComponent(ctx context.Context, actorID string, spec ComponentSpec, create scope.Set) (*Component, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create component: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var parentID *string
	if spec.ParentName == nil {
		if !create.All {
			return nil, ErrComponentForbidden
		}
	} else {
		parent, err := scopedByName(ctx, tx, componentConfig, *spec.ParentName)
		if errors.Is(err, ErrComponentNotFound) {
			return nil, ErrParentComponentNotFound
		} else if err != nil {
			return nil, err
		}
		in, err := inScopeTree(ctx, tx, componentTable, parent.ID, create)
		if err != nil {
			return nil, err
		}
		if !in {
			return nil, ErrComponentForbidden
		}
		parentID = &parent.ID
	}

	var systemID *string
	if spec.SystemName != nil {
		s, err := scopedByName(ctx, tx, systemConfig, *spec.SystemName)
		if err != nil {
			return nil, err // ErrSystemNotFound -> 422
		}
		systemID = &s.ID
	}
	var locationID *string
	if spec.LocationName != nil {
		loc, err := scopedByName(ctx, tx, locationConfig, *spec.LocationName)
		if err != nil {
			return nil, err // ErrLocationNotFound -> 422
		}
		locationID = &loc.ID
	}

	c, err := scanComponent(tx.QueryRow(ctx, `
		insert into component (name, display_name, component_type, parent_id, system_id, location_id)
		values ($1, $2, $3, $4, $5, $6)
		returning `+componentCols,
		spec.Name, nullize(spec.DisplayName), spec.ComponentType, parentID, systemID, locationID))
	if err != nil {
		return nil, mapComponentWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "component", c.ID, nil, c); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create component: %w", err)
	}
	return c, nil
}

// UpdateComponent patches a component by name with the three-way scope split and
// in-transaction audit. Reparent, rebind, and relocate are deferred.
func (p *PG) UpdateComponent(ctx context.Context, actorID, name string, patch ComponentPatch, read, action scope.Set) (*Component, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update component: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := resolveScoped(ctx, tx, componentConfig, name, read, action)
	if err != nil {
		return nil, err
	}
	after, err := scanComponent(tx.QueryRow(ctx, `
		update component set
			display_name   = coalesce($2, display_name),
			component_type = coalesce($3, component_type),
			updated_at     = now()
		where id = $1
		returning `+componentCols,
		before.ID, patch.DisplayName, patch.ComponentType))
	if err != nil {
		return nil, mapComponentWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "component", after.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update component: %w", err)
	}
	return after, nil
}

func mapComponentWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return ErrComponentExists
		case "23503":
			switch pgErr.ConstraintName {
			case "component_component_type_fkey":
				return ErrUnknownComponentType
			case "component_system_id_fkey":
				return ErrSystemNotFound
			case "component_location_id_fkey":
				return ErrLocationNotFound
			}
		}
	}
	return fmt.Errorf("storage: component write: %w", err)
}
