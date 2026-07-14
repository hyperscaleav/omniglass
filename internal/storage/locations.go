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
// official flag, a display_name, and an icon (a glyph key the console renders as
// the leading glyph on every location of this type). It is the only
// shape-definer for a location, which has no template. The registry lists
// alphabetically by display_name; there is no ordering field.
type LocationType struct {
	ID          string
	Official    bool
	DisplayName string
	Icon        string
}

// UpsertLocationType installs or updates a location type by id, the boot-seed
// phase's write. Idempotent: re-seeding the same id updates it in place.
func (p *PG) UpsertLocationType(ctx context.Context, lt LocationType) error {
	_, err := p.pool.Exec(ctx, `
		insert into location_type (id, official, display_name, icon)
		values ($1, $2, $3, $4)
		on conflict (id) do update
			set official     = excluded.official,
			    display_name = excluded.display_name,
			    icon         = excluded.icon`,
		lt.ID, lt.Official, lt.DisplayName, lt.Icon)
	if err != nil {
		return fmt.Errorf("storage: upsert location_type %q: %w", lt.ID, err)
	}
	return nil
}

// ListLocationTypes returns every location type, ordered alphabetically by
// display_name then id, for the registry view and validation.
func (p *PG) ListLocationTypes(ctx context.Context) ([]LocationType, error) {
	rows, err := p.pool.Query(ctx,
		`select id, official, display_name, icon from location_type order by display_name, id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list location_types: %w", err)
	}
	defer rows.Close()
	var out []LocationType
	for rows.Next() {
		var lt LocationType
		if err := rows.Scan(&lt.ID, &lt.Official, &lt.DisplayName, &lt.Icon); err != nil {
			return nil, fmt.Errorf("storage: scan location_type: %w", err)
		}
		out = append(out, lt)
	}
	return out, rows.Err()
}

// LocationTypePatch carries the mutable fields of a location_type update; a nil
// field is left unchanged.
type LocationTypePatch struct {
	DisplayName *string
	Icon        *string
}

// CreateLocationType inserts a custom (official=false) location_type and audits
// it. A duplicate id (including a seed-owned official id) is ErrTypeExists.
func (p *PG) CreateLocationType(ctx context.Context, actorID string, lt LocationType) (*LocationType, error) {
	lt.Official = false
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create location_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`insert into location_type (id, official, display_name, icon) values ($1, false, $2, $3)`,
		lt.ID, lt.DisplayName, lt.Icon); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrTypeExists
		}
		return nil, fmt.Errorf("storage: insert location_type %q: %w", lt.ID, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "location_type", lt.ID, nil, lt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create location_type: %w", err)
	}
	return &lt, nil
}

// UpdateLocationType patches a custom location_type's display_name or icon
// (nil fields unchanged) and audits it. Official rows are read-only (ErrTypeOfficial);
// an unknown id is ErrTypeNotFound.
func (p *PG) UpdateLocationType(ctx context.Context, actorID, id string, patch LocationTypePatch) (*LocationType, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update location_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "location_type", id); err != nil {
		return nil, err
	}
	var lt LocationType
	if err := tx.QueryRow(ctx, `
		update location_type set
			display_name = coalesce($2, display_name),
			icon         = coalesce($3, icon)
		where id = $1
		returning id, official, display_name, icon`,
		id, patch.DisplayName, patch.Icon).
		Scan(&lt.ID, &lt.Official, &lt.DisplayName, &lt.Icon); err != nil {
		return nil, fmt.Errorf("storage: update location_type %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "location_type", id, nil, lt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update location_type: %w", err)
	}
	return &lt, nil
}

// DeleteLocationType removes a custom location_type, refusing an official row and
// a row still referenced by a location.
func (p *PG) DeleteLocationType(ctx context.Context, actorID, id string) error {
	return deleteTypeRow(ctx, p, "location_type", "location_type", typeRef{table: "location", col: "location_type"}, actorID, id)
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

// locationConfig drives the generic scoped-CRUD helpers for the location tree.
var locationConfig = scopedConfig[Location]{
	table: locationTable, cols: locationCols, resource: "location",
	scan: scanLocation, idOf: func(l *Location) string { return l.ID },
	notFound: ErrLocationNotFound, forbidden: ErrLocationForbidden, occupied: ErrLocationOccupied,
}

// ListLocations returns the locations in the caller's read scope, ordered by
// name (the shared scoped-tree read path).
func (p *PG) ListLocations(ctx context.Context, read scope.Set) ([]Location, error) {
	return scopedList(ctx, p, locationConfig, read)
}

// GetLocation resolves a location by name within the caller's read scope; absent
// or out of scope is the same non-disclosing ErrLocationNotFound.
func (p *PG) GetLocation(ctx context.Context, name string, read scope.Set) (*Location, error) {
	return scopedGet(ctx, p, locationConfig, name, read)
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
	return scopedDelete(ctx, p, locationConfig, actorID, name, read, action)
}

// resolveForAction enforces the read-then-action scope split for a location
// (the shared helper); Create/Update use it.
func (p *PG) resolveForAction(ctx context.Context, q querier, name string, read, action scope.Set) (*Location, error) {
	return resolveScoped(ctx, q, locationConfig, name, read, action)
}

// locationByName loads a single location by its unique name (no scope check),
// reused by the system/component located-at resolution.
func (p *PG) locationByName(ctx context.Context, q querier, name string) (*Location, error) {
	return scopedByName(ctx, q, locationConfig, name)
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
	// actor_username / real_actor_username denormalize the actor's label at write
	// time, so the row still names its actor after that principal is purged (the
	// foreign keys go null, the text remains). principal_label(null) is null.
	if _, err := tx.Exec(ctx, `
		insert into audit_log (actor_principal_id, real_actor_principal_id, actor_username, real_actor_username, verb, resource, resource_id, old, new)
		values ($1, $2, principal_label($1), principal_label($2), $3, $4, $5, $6, $7)`,
		nullize(actorID), nullize(realActorFrom(ctx)), verb, resource, resourceID, oldJSON, newJSON); err != nil {
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
