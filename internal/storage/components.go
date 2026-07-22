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
	// PrimarySystem is the name of the component's default system, and SystemCount
	// how many it belongs to in total. Both are derived from system_member rather
	// than stored: a component can be in several systems, so there is no single
	// pointer to keep. The name rather than an id, because a name is the address
	// the API speaks.
	PrimarySystem   *string
	PrimarySystemID *string
	SystemCount     int
	// ParentName and LocationName are how the API addresses this component's
	// placement. The *ID fields beside them are internal: a uuid is identity, never
	// a reference that leaves the process.
	ParentName   *string
	LocationName *string
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

// ComponentPatch is the update input: nil fields unchanged. ProductName follows
// the house three-state convention, nil unchanged and an explicit empty string
// CLEARS it (leaving a component that carries only its own capability facts).
// Reparent and relocate are deferred.
type ComponentPatch struct {
	Name        *string
	DisplayName *string
	ProductName *string
}

// --- component CRUD (read/delete via the generic helpers) --------------------

const componentCols = `id, name, coalesce(display_name, ''), parent_id,
	-- The primary membership, both forms: the name for display and the id as the
	-- canonical handle. The arc points at the primary key, so the join is by id.
	(select s.name from system s join system_member m on m.system_id = s.id
	  where m.component_id = component.id and m.is_primary) as primary_system,
	(select m.system_id from system_member m where m.component_id = component.id and m.is_primary) as primary_system_id,
	(select count(*) from system_member m where m.component_id = component.id) as system_count,
	location_id, product_id,
	-- The names the API addresses these by. The ids stay for the scope walks and
	-- tree joins, which are internal; a name is what leaves the process.
	(select p.name from component p where p.id = component.parent_id) as parent_name,
	(select l.name from location l where l.id = component.location_id) as location_name,
	created_at, updated_at`

func scanComponent(row pgx.Row) (*Component, error) {
	var c Component
	if err := row.Scan(&c.ID, &c.Name, &c.DisplayName, &c.ParentID, &c.PrimarySystem, &c.PrimarySystemID, &c.SystemCount,
		&c.LocationID, &c.ProductID, &c.ParentName, &c.LocationName, &c.CreatedAt, &c.UpdatedAt); err != nil {
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

	// A system named at create becomes a MEMBERSHIP rather than a column on the
	// component: the relation lives in system_member, and this is simply the first
	// one. Resolved here so an unknown name is a 422 before anything is written.
	if spec.SystemName != nil {
		if _, err := scopedByName(ctx, tx, systemConfig, *spec.SystemName); err != nil {
			return nil, err // ErrSystemNotFound -> 422
		}
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
		insert into component (name, display_name, parent_id, location_id, product_id)
		values ($1, $2, $3, $4, $5)
		returning `+componentCols,
		spec.Name, nullize(spec.DisplayName), parentID, locationID, productID))
	if err != nil {
		return nil, mapComponentWriteErr(err)
	}
	// The membership after the row exists, since it references the component by
	// name. Re-read so the returned component carries the primary it just gained.
	if spec.SystemName != nil {
		if err := addMemberTx(ctx, tx, *spec.SystemName, spec.Name); err != nil {
			return nil, err
		}
		if c, err = scanComponent(tx.QueryRow(ctx,
			`select `+componentCols+` from component where id = $1`, c.ID)); err != nil {
			return nil, fmt.Errorf("storage: re-read component after membership: %w", err)
		}
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
// in-transaction audit, recomputing health when the product moved. Reparent and
// relocate are deferred.
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
			-- product_id takes the house three states: a nil field is unchanged, an
			-- explicit empty string clears it, anything else is the new product (whose
			-- id is its name, so it goes in as given; an unknown one comes back from
			-- the FK as the named ErrProductNotFound).
			product_id   = case
				when $4::text is null then product_id
				when $4 = '' then null
				else $4
			end,
			updated_at   = now()
		where id = $1
		returning `+componentCols,
		before.ID, patch.Name, patch.DisplayName, patch.ProductName))
	if err != nil {
		return nil, mapComponentWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "component", after.ID, before, after); err != nil {
		return nil, err
	}
	// The product supplies the component's default capabilities, so swapping it can
	// make an assignee stop satisfying the role it fills (or start). Detected against
	// the before-image, and recomputed under the name the row carries after the
	// patch, which is what every capability and assignment lookup keys on.
	if !sameOptional(before.ProductID, after.ProductID) {
		if err := p.RecomputeHealth(ctx, tx, after.Name); err != nil {
			return nil, err
		}
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
			case "component_location_id_fkey":
				return ErrLocationNotFound
			case "component_product_id_fkey":
				return ErrProductNotFound
			}
		}
	}
	return fmt.Errorf("storage: component write: %w", err)
}
