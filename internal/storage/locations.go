package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Location-layer sentinel errors. The API maps them to status: ErrLocationNotFound
// is the non-disclosing 404 (absent, or outside the caller's read scope, which are
// indistinguishable by design); ErrLocationForbidden is the 403 for a target the
// caller can read but not act on; the rest are request faults.
var (
	ErrLocationNotFound  = errors.New("storage: location not found")
	ErrLocationForbidden = errors.New("storage: action not permitted on this location")
	ErrLocationOccupied  = errors.New("storage: location has child locations")
	ErrLocationExists    = errors.New("storage: location name already exists")
	ErrParentNotFound    = errors.New("storage: parent location not found")
	ErrUnknownType       = errors.New("storage: unknown location_type")
)

// Location is a place in the estate tree: name-addressable (name is globally
// unique), classified by location_type, and nested under an optional parent.
type Location struct {
	ID           string
	Name         string
	DisplayName  string
	LocationType string
	ParentID     *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// LocationSpec is the create input. ParentName nil makes a root location, which
// only an all-scoped create grant may place.
type LocationSpec struct {
	Name         string
	DisplayName  string
	LocationType string
	ParentName   *string
}

// LocationPatch is the update input: nil fields are left unchanged. Renaming and
// reparenting (a tree move) are deferred to a later slice.
type LocationPatch struct {
	DisplayName  *string
	LocationType *string
}

// LocationType is a registry row classifying a location: a stable id, the
// official flag, a display_name, and a rank (ordering plus a soft hierarchy
// signal, not a nesting constraint). It is the only shape-definer for a
// location, which has no template.
type LocationType struct {
	ID          string
	Official    bool
	DisplayName string
	Rank        int
}

// UpsertLocationType installs or updates a location type by id, the boot-seed
// phase's write. Idempotent: re-seeding the same id updates it in place.
func (p *PG) UpsertLocationType(ctx context.Context, lt LocationType) error {
	_, err := p.pool.Exec(ctx, `
		insert into location_type (id, official, display_name, rank)
		values ($1, $2, $3, $4)
		on conflict (id) do update
			set official     = excluded.official,
			    display_name = excluded.display_name,
			    rank         = excluded.rank`,
		lt.ID, lt.Official, lt.DisplayName, lt.Rank)
	if err != nil {
		return fmt.Errorf("storage: upsert location_type %q: %w", lt.ID, err)
	}
	return nil
}

// ListLocationTypes returns every location type, ordered by rank then id, for
// the registry view and validation.
func (p *PG) ListLocationTypes(ctx context.Context) ([]LocationType, error) {
	rows, err := p.pool.Query(ctx,
		`select id, official, display_name, rank from location_type order by rank, id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list location_types: %w", err)
	}
	defer rows.Close()
	var out []LocationType
	for rows.Next() {
		var lt LocationType
		if err := rows.Scan(&lt.ID, &lt.Official, &lt.DisplayName, &lt.Rank); err != nil {
			return nil, fmt.Errorf("storage: scan location_type: %w", err)
		}
		out = append(out, lt)
	}
	return out, rows.Err()
}

// locationCols is the column list every location read scans, in struct order.
const locationCols = `id, name, coalesce(display_name, ''), location_type, parent_id, created_at, updated_at`

func scanLocation(row pgx.Row) (*Location, error) {
	var l Location
	if err := row.Scan(&l.ID, &l.Name, &l.DisplayName, &l.LocationType, &l.ParentID, &l.CreatedAt, &l.UpdatedAt); err != nil {
		return nil, err
	}
	return &l, nil
}

// ListLocations returns the locations in the caller's read scope, ordered by
// name. An all scope returns every row; a rooted scope expands each root to its
// subtree (the recursive descendant walk) and filters to it; an empty scope
// returns nothing.
func (p *PG) ListLocations(ctx context.Context, read scope.Set) ([]Location, error) {
	if read.Empty() {
		return nil, nil
	}
	// The scoped-tree primitive builds the subtree-filtered list query; only the
	// columns and scan are location-specific.
	sql := scopedListSQL(locationTable, locationCols, read.All)
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
		return nil, fmt.Errorf("storage: list locations: %w", err)
	}
	defer rows.Close()
	var out []Location
	for rows.Next() {
		l, err := scanLocation(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan location: %w", err)
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

// GetLocation resolves a location by name within the caller's read scope. A name
// that does not exist, or exists outside the read scope, returns the same
// ErrLocationNotFound: the 404 is non-disclosing.
func (p *PG) GetLocation(ctx context.Context, name string, read scope.Set) (*Location, error) {
	l, err := p.locationByName(ctx, p.pool, name)
	if err != nil {
		return nil, err
	}
	in, err := p.inScope(ctx, p.pool, l.ID, read)
	if err != nil {
		return nil, err
	}
	if !in {
		return nil, ErrLocationNotFound
	}
	return l, nil
}

// CreateLocation inserts a location under an optional parent and writes the audit
// row in the same transaction. A root location (no parent) requires an all create
// scope; a child requires the parent to be within the create scope. The new
// row's owner is itself, so create scope is evaluated on the parent's placement.
func (p *PG) CreateLocation(ctx context.Context, actorID string, spec LocationSpec, create scope.Set) (*Location, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create location: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var parentID *string
	if spec.ParentName == nil {
		// A root location is only placeable by an all-scoped create grant.
		if !create.All {
			return nil, ErrLocationForbidden
		}
	} else {
		parent, err := p.locationByName(ctx, tx, *spec.ParentName)
		if errors.Is(err, ErrLocationNotFound) {
			return nil, ErrParentNotFound
		} else if err != nil {
			return nil, err
		}
		in, err := p.inScope(ctx, tx, parent.ID, create)
		if err != nil {
			return nil, err
		}
		if !in {
			return nil, ErrLocationForbidden
		}
		parentID = &parent.ID
	}

	l, err := scanLocation(tx.QueryRow(ctx, `
		insert into location (name, display_name, location_type, parent_id)
		values ($1, $2, $3, $4)
		returning `+locationCols,
		spec.Name, nullize(spec.DisplayName), spec.LocationType, parentID))
	if err != nil {
		return nil, mapLocationWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "location", l.ID, nil, l); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create location: %w", err)
	}
	return l, nil
}

// UpdateLocation applies a patch to a location addressed by name, enforcing the
// three-way split: outside read scope is ErrLocationNotFound (404), readable but
// outside the action scope is ErrLocationForbidden (403). The old and new shapes
// are audited in the same transaction.
func (p *PG) UpdateLocation(ctx context.Context, actorID, name string, patch LocationPatch, read, action scope.Set) (*Location, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update location: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := p.resolveForAction(ctx, tx, name, read, action)
	if err != nil {
		return nil, err
	}

	after, err := scanLocation(tx.QueryRow(ctx, `
		update location set
			display_name  = coalesce($2, display_name),
			location_type = coalesce($3, location_type),
			updated_at    = now()
		where id = $1
		returning `+locationCols,
		before.ID, patch.DisplayName, patch.LocationType))
	if err != nil {
		return nil, mapLocationWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "location", after.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update location: %w", err)
	}
	return after, nil
}

// DeleteLocation removes a location addressed by name, with the same three-way
// scope split as update, and refuses while the location still has child
// locations (the "occupied" rule, for the structural children this slice knows
// about; placed systems and components join the check when they land).
func (p *PG) DeleteLocation(ctx context.Context, actorID, name string, read, action scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete location: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := p.resolveForAction(ctx, tx, name, read, action)
	if err != nil {
		return err
	}

	var childCount int
	if err := tx.QueryRow(ctx, `select count(*) from location where parent_id = $1`, before.ID).Scan(&childCount); err != nil {
		return fmt.Errorf("storage: count children: %w", err)
	}
	if childCount > 0 {
		return ErrLocationOccupied
	}

	if _, err := tx.Exec(ctx, `delete from location where id = $1`, before.ID); err != nil {
		return mapLocationWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "location", before.ID, before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete location: %w", err)
	}
	return nil
}

// resolveForAction loads a location by name and enforces the read-then-action
// scope split, returning the row when the caller may act on it.
func (p *PG) resolveForAction(ctx context.Context, q querier, name string, read, action scope.Set) (*Location, error) {
	l, err := p.locationByName(ctx, q, name)
	if err != nil {
		return nil, err
	}
	readable, err := p.inScope(ctx, q, l.ID, read)
	if err != nil {
		return nil, err
	}
	if !readable {
		return nil, ErrLocationNotFound // non-disclosing
	}
	actionable, err := p.inScope(ctx, q, l.ID, action)
	if err != nil {
		return nil, err
	}
	if !actionable {
		return nil, ErrLocationForbidden
	}
	return l, nil
}

// locationByName loads a single location by its unique name, ErrLocationNotFound
// if absent. It applies no scope: callers layer the scope check on top.
func (p *PG) locationByName(ctx context.Context, q querier, name string) (*Location, error) {
	l, err := scanLocation(q.QueryRow(ctx, `select `+locationCols+` from location where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLocationNotFound
	} else if err != nil {
		return nil, fmt.Errorf("storage: load location %q: %w", name, err)
	}
	return l, nil
}

// inScope reports whether a target location falls within a resolved scope,
// delegating to the shared scoped-tree walk.
func (p *PG) inScope(ctx context.Context, q querier, targetID string, set scope.Set) (bool, error) {
	return inScopeTree(ctx, q, locationTable, targetID, set)
}

// querier is the read surface shared by *pgxpool.Pool and pgx.Tx, so scope and
// lookup helpers run either standalone or inside a transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// writeAuditRes records one write in the audit_log, in the caller's
// transaction, for the named resource. A nil old or new marshals to a SQL NULL
// (a create has no old, a delete no new). Shared by every entity gateway.
func writeAuditRes(ctx context.Context, tx pgx.Tx, actorID, verb, resource, resourceID string, old, new any) error {
	oldJSON, err := auditJSON(old)
	if err != nil {
		return err
	}
	newJSON, err := auditJSON(new)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into audit_log (actor_principal_id, verb, resource, resource_id, old, new)
		values ($1, $2, $3, $4, $5, $6)`,
		nullize(actorID), verb, resource, resourceID, oldJSON, newJSON); err != nil {
		return fmt.Errorf("storage: write audit: %w", err)
	}
	return nil
}

func auditJSON(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("storage: marshal audit: %w", err)
	}
	return b, nil
}

// mapLocationWriteErr translates Postgres constraint violations into the
// location sentinels: a unique-name clash and an unknown location_type FK are
// request faults the API reports as 409 and 400.
func mapLocationWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrLocationExists
		case "23503": // foreign_key_violation
			if pgErr.ConstraintName == "location_location_type_fkey" {
				return ErrUnknownType
			}
		}
	}
	return fmt.Errorf("storage: location write: %w", err)
}
