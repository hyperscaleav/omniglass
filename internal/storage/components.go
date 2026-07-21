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
	ErrProductNotFound         = errors.New("storage: product not found")
)

// Component is a leaf of the estate: name-addressable, nestable via parent_id,
// belonging to a primary system and located at a location. Its shape (the
// properties it declares) comes from the product it is an instance of.
type Component struct {
	ID          string
	Name        string
	DisplayName string
	ParentID    *string
	SystemID    *string
	LocationID  *string
	ProductID   *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ComponentSpec is the create input. ParentName nil makes a root component;
// SystemName / LocationName optionally bind it to a system and place it;
// ProductName optionally names the product (catalog SKU) it is an instance of.
type ComponentSpec struct {
	Name         string
	DisplayName  string
	ParentName   *string
	SystemName   *string
	LocationName *string
	ProductName  *string
}

// ComponentPatch is the update input: nil fields unchanged. Reparent, rebind,
// and relocate are deferred.
type ComponentPatch struct {
	Name        *string
	DisplayName *string
}

// --- component CRUD (read/delete via the generic helpers) --------------------

const componentCols = `id, name, coalesce(display_name, ''), parent_id, system_id, location_id, product_id, created_at, updated_at`

func scanComponent(row pgx.Row) (*Component, error) {
	var c Component
	if err := row.Scan(&c.ID, &c.Name, &c.DisplayName, &c.ParentID, &c.SystemID, &c.LocationID, &c.ProductID, &c.CreatedAt, &c.UpdatedAt); err != nil {
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

// ComponentNameTaken reports whether a component with this name exists.
// Scope-blind by design: the name unique constraint is global, so availability
// must be a global fact to match it (a scope-aware answer would false-positive
// on a name held outside the caller's scope). Gated at the API by
// component:update.
func (p *PG) ComponentNameTaken(ctx context.Context, name string) (bool, error) {
	var exists bool
	if err := p.pool.QueryRow(ctx, `select exists(select 1 from component where name = $1)`, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("storage: component name taken: %w", err)
	}
	return exists, nil
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

	if err := ValidateEntityName(spec.Name); err != nil {
		return nil, err
	}

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

	// product is a catalog, not a scoped tree: resolve by id (product id is its
	// pk/name) with a plain lookup, not scopedByName. An unknown id is
	// ErrProductNotFound -> 422 (the FK below is the belt-and-suspenders).
	var productID *string
	if spec.ProductName != nil {
		var pid string
		err := tx.QueryRow(ctx, `select id from product where id = $1`, *spec.ProductName).Scan(&pid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProductNotFound
		} else if err != nil {
			return nil, fmt.Errorf("storage: resolve product %q: %w", *spec.ProductName, err)
		}
		productID = &pid
	}

	c, err := scanComponent(tx.QueryRow(ctx, `
		insert into component (name, display_name, parent_id, system_id, location_id, product_id)
		values ($1, $2, $3, $4, $5, $6)
		returning `+componentCols,
		spec.Name, nullize(spec.DisplayName), parentID, systemID, locationID, productID))
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
	if patch.Name != nil {
		if err := ValidateEntityName(*patch.Name); err != nil {
			return nil, err
		}
	}
	after, err := scanComponent(tx.QueryRow(ctx, `
		update component set
			name         = coalesce($2, name),
			display_name = coalesce($3, display_name),
			updated_at   = now()
		where id = $1
		returning `+componentCols,
		before.ID, patch.Name, patch.DisplayName))
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

// ComponentInterface is one placement-bound connection owned by a component: the
// unit the reachability panel iterates. Params is the raw endpoint jsonb (target,
// port, and settings); NodeName is the probing node (nullable). The verdict,
// layer signals, and history are read separately per (component, key, interface).
type ComponentInterface struct {
	Name     string
	Type     string
	NodeName string
	Params   []byte
}

// ListComponentInterfaces returns a component's interfaces ordered by name, the
// rows the reachability read composes over. It is not scope-injected: the caller
// gates on the component being in read scope (GetComponent) first, then reads its
// interfaces by the verified name.
func (p *PG) ListComponentInterfaces(ctx context.Context, componentName string) ([]ComponentInterface, error) {
	rows, err := p.pool.Query(ctx, `
		select name, type, coalesce(node_name, ''), params
		from interface
		where component = $1
		order by name asc`, componentName)
	if err != nil {
		return nil, fmt.Errorf("storage: list interfaces for %s: %w", componentName, err)
	}
	defer rows.Close()
	var out []ComponentInterface
	for rows.Next() {
		var it ComponentInterface
		if err := rows.Scan(&it.Name, &it.Type, &it.NodeName, &it.Params); err != nil {
			return nil, fmt.Errorf("storage: scan interface for %s: %w", componentName, err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate interfaces for %s: %w", componentName, err)
	}
	return out, nil
}

func mapComponentWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return ErrComponentExists
		case "23503":
			switch pgErr.ConstraintName {
			case "component_system_id_fkey":
				return ErrSystemNotFound
			case "component_location_id_fkey":
				return ErrLocationNotFound
			case "component_product_id_fkey":
				return ErrProductNotFound
			}
		}
	}
	return fmt.Errorf("storage: component write: %w", err)
}
