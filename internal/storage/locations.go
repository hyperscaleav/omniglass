package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
	ErrLocationNotFound    = errors.New("storage: location not found")
	ErrLocationForbidden   = errors.New("storage: action not permitted on this location")
	ErrLocationOccupied    = errors.New("storage: location has child locations")
	ErrLocationExists      = errors.New("storage: location name already exists")
	ErrParentNotFound      = errors.New("storage: parent location not found")
	ErrUnknownType         = errors.New("storage: unknown location_type")
	ErrPlacementNotAllowed = errors.New("storage: placement not allowed for this location_type")
	ErrLocationCycle       = errors.New("storage: cannot move a location under itself or a descendant")
	ErrReservedTypeID      = errors.New("storage: \"root\" is a reserved location_type id")
)

// RootPlacement is the reserved allowed_parent_types member meaning "may sit at
// the top, no parent." It is not a real location_type id: CreateLocationType
// refuses it (ErrReservedTypeID), so a real type can never collide with the
// sentinel.
const RootPlacement = "root"

// PlacementError is a location placement violation: childType (a location_type
// id) may not be placed under a parent of ParentType (a location_type id, or
// "" for a root placement, no parent). It wraps ErrPlacementNotAllowed via
// Unwrap, so errors.Is still matches generically; errors.As extracts the two
// type names for the API's 422 message.
type PlacementError struct {
	ChildType  string
	ParentType string // "" for a rejected root placement
}

func (e *PlacementError) Error() string {
	if e.ParentType == "" {
		return fmt.Sprintf("%s is not allowed at root", e.ChildType)
	}
	return fmt.Sprintf("%s is not allowed under %s", e.ChildType, e.ParentType)
}

func (e *PlacementError) Unwrap() error { return ErrPlacementNotAllowed }

// validatePlacement enforces a location_type's allowed_parent_types against a
// candidate placement: parentType is the parent location's type, or nil for a
// root placement (no parent). An empty (or unset) allowed set is
// unconstrained. CreateLocation and the reparent path in UpdateLocation both
// call this before writing. A childType that does not exist in the registry is
// left to the insert's FK check (ErrUnknownType), not this validator.
func (p *PG) validatePlacement(ctx context.Context, q querier, childType string, parentType *string) error {
	var allowed []string
	err := q.QueryRow(ctx, `select allowed_parent_types from location_type where id = $1`, childType).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("storage: load allowed_parent_types for %q: %w", childType, err)
	}
	if len(allowed) == 0 {
		return nil
	}
	if parentType == nil {
		if slices.Contains(allowed, RootPlacement) {
			return nil
		}
		return &PlacementError{ChildType: childType}
	}
	if slices.Contains(allowed, *parentType) {
		return nil
	}
	return &PlacementError{ChildType: childType, ParentType: *parentType}
}

// locationIsDescendant reports whether candidateID is targetID or a descendant
// of it (self-inclusive): the cycle guard for a reparent, which must not move a
// location under itself or one of its own children.
func (p *PG) locationIsDescendant(ctx context.Context, q querier, targetID, candidateID string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
		with recursive sub(id) as (
			select id from location where id = $1
			union all
			select l.id from location l join sub on l.parent_id = sub.id
		) cycle id set is_cycle using path
		select exists(select 1 from sub where id = $2)`,
		targetID, candidateID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("storage: descendant check: %w", err)
	}
	return ok, nil
}

// Location is a place in the estate tree: name-addressable (name is globally
// unique), classified by location_type, and nested under an optional parent.
type Location struct {
	ID           string
	Name         string
	DisplayName  string
	LocationType string
	ParentID     *string
	// The name the API addresses the parent by; ParentID above is internal.
	ParentName *string
	CreatedAt  time.Time
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

// LocationPatch is the update input: nil fields are left unchanged. Name, when
// set, renames the location. ParentName, when set, re-parents the location (a
// tree move) to the named parent, cycle-guarded and placement-validated exactly
// like create; a move to root (no parent) is not supported via patch this slice.
type LocationPatch struct {
	Name         *string
	DisplayName  *string
	LocationType *string
	ParentName   *string
}

// LocationType is a registry row classifying a location: a stable id, the
// official flag, a display_name, an icon (a glyph key the console renders as
// the leading glyph on every location of this type), and AllowedParentTypes,
// the placement constraint: a set of location_type ids and/or RootPlacement
// this type may be placed under. An empty set is unconstrained. It is the
// only shape-definer for a location, which has no template. The registry
// lists alphabetically by display_name; there is no ordering field.
type LocationType struct {
	ID                 string
	Official           bool
	DisplayName        string
	Icon               string
	AllowedParentTypes []string
}

// UpsertLocationType installs or updates a location type by id, the boot-seed
// phase's write. Idempotent: re-seeding the same id updates it in place.
// SeedLocationType inserts a shipped example location type only when it is
// absent. Like a standard, a location type is content an operator shapes to their
// organization, so re-seeding must never reassert over an edit. Deliberately not
// UpsertLocationType, whose ON CONFLICT DO UPDATE is the authoritative behavior
// the canonical catalogs want.
func (p *PG) SeedLocationType(ctx context.Context, lt LocationType) error {
	_, err := p.pool.Exec(ctx, `
		insert into location_type (id, official, display_name, icon, allowed_parent_types)
		values ($1, $2, $3, $4, $5)
		on conflict (id) do nothing`,
		lt.ID, lt.Official, lt.DisplayName, lt.Icon, normalizeAllowedParentTypes(lt.AllowedParentTypes))
	if err != nil {
		return fmt.Errorf("storage: seed location_type %q: %w", lt.ID, err)
	}
	return nil
}

func (p *PG) UpsertLocationType(ctx context.Context, lt LocationType) error {
	_, err := p.pool.Exec(ctx, `
		insert into location_type (id, official, display_name, icon, allowed_parent_types)
		values ($1, $2, $3, $4, $5)
		on conflict (id) do update
			set official             = excluded.official,
			    display_name         = excluded.display_name,
			    icon                 = excluded.icon,
			    allowed_parent_types = excluded.allowed_parent_types`,
		lt.ID, lt.Official, lt.DisplayName, lt.Icon, normalizeAllowedParentTypes(lt.AllowedParentTypes))
	if err != nil {
		return fmt.Errorf("storage: upsert location_type %q: %w", lt.ID, err)
	}
	return nil
}

// normalizeAllowedParentTypes returns a non-nil slice so a nil set writes and
// reads back as an empty text[], not SQL null (the column is not null, and
// "empty" is a meaningful, first-class state: unconstrained).
func normalizeAllowedParentTypes(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ListLocationTypes returns every location type, ordered alphabetically by
// display_name then id, for the registry view and validation.
func (p *PG) ListLocationTypes(ctx context.Context) ([]LocationType, error) {
	rows, err := p.pool.Query(ctx,
		`select id, official, display_name, icon, allowed_parent_types from location_type order by display_name, id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list location_types: %w", err)
	}
	defer rows.Close()
	var out []LocationType
	for rows.Next() {
		var lt LocationType
		if err := rows.Scan(&lt.ID, &lt.Official, &lt.DisplayName, &lt.Icon, &lt.AllowedParentTypes); err != nil {
			return nil, fmt.Errorf("storage: scan location_type: %w", err)
		}
		lt.AllowedParentTypes = normalizeAllowedParentTypes(lt.AllowedParentTypes)
		out = append(out, lt)
	}
	return out, rows.Err()
}

// LocationTypePatch carries the mutable fields of a location_type update; a nil
// field is left unchanged. AllowedParentTypes is a pointer to a slice so a
// caller can distinguish "leave unchanged" (nil) from "replace with this set"
// (a non-nil slice, including an empty one, which clears it back to
// unconstrained).
type LocationTypePatch struct {
	DisplayName        *string
	Icon               *string
	AllowedParentTypes *[]string
}

// CreateLocationType inserts a custom (official=false) location_type and audits
// it. A duplicate id (including a seed-owned official id) is ErrTypeExists;
// "root" (the allowed_parent_types sentinel) is ErrReservedTypeID.
func (p *PG) CreateLocationType(ctx context.Context, actorID string, lt LocationType) (*LocationType, error) {
	if lt.ID == RootPlacement {
		return nil, ErrReservedTypeID
	}
	lt.Official = false
	lt.AllowedParentTypes = normalizeAllowedParentTypes(lt.AllowedParentTypes)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create location_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`insert into location_type (id, official, display_name, icon, allowed_parent_types) values ($1, false, $2, $3, $4)`,
		lt.ID, lt.DisplayName, lt.Icon, lt.AllowedParentTypes); err != nil {
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

// UpdateLocationType patches a custom location_type's display_name, icon, or
// allowed_parent_types (nil fields unchanged) and audits it. Official rows are
// read-only (ErrTypeOfficial); an unknown id is ErrTypeNotFound.
func (p *PG) UpdateLocationType(ctx context.Context, actorID, id string, patch LocationTypePatch) (*LocationType, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update location_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "location_type", id); err != nil {
		return nil, err
	}
	var allowed *[]string
	if patch.AllowedParentTypes != nil {
		v := normalizeAllowedParentTypes(*patch.AllowedParentTypes)
		allowed = &v
	}
	var lt LocationType
	if err := tx.QueryRow(ctx, `
		update location_type set
			display_name         = coalesce($2, display_name),
			icon                 = coalesce($3, icon),
			allowed_parent_types = coalesce($4, allowed_parent_types)
		where id = $1
		returning id, official, display_name, icon, allowed_parent_types`,
		id, patch.DisplayName, patch.Icon, allowed).
		Scan(&lt.ID, &lt.Official, &lt.DisplayName, &lt.Icon, &lt.AllowedParentTypes); err != nil {
		return nil, fmt.Errorf("storage: update location_type %q: %w", id, err)
	}
	lt.AllowedParentTypes = normalizeAllowedParentTypes(lt.AllowedParentTypes)
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
const locationCols = `id, name, coalesce(display_name, ''), location_type, parent_id,
	(select p.name from location p where p.id = location.parent_id) as parent_name,
	created_at, updated_at`

func scanLocation(row pgx.Row) (*Location, error) {
	var l Location
	if err := row.Scan(&l.ID, &l.Name, &l.DisplayName, &l.LocationType, &l.ParentID, &l.ParentName,
		&l.CreatedAt, &l.UpdatedAt); err != nil {
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
// The child type's allowed_parent_types is enforced against the resolved parent
// (or root) before the insert.
func (p *PG) CreateLocation(ctx context.Context, actorID string, spec LocationSpec, create scope.Set) (*Location, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create location: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := ValidateEntityName(spec.Name); err != nil {
		return nil, err
	}

	var parentID *string
	var parentType *string
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
		parentType = &parent.LocationType
	}

	if err := p.validatePlacement(ctx, tx, spec.LocationType, parentType); err != nil {
		return nil, err
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
// outside the action scope is ErrLocationForbidden (403). When ParentName is
// set, the move is cycle-guarded (ErrLocationCycle) and placement-validated
// against the resolved (possibly also-patched) location_type, exactly like
// create. The old and new shapes are audited in the same transaction.
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
	if patch.Name != nil {
		if err := ValidateEntityName(*patch.Name); err != nil {
			return nil, err
		}
	}

	parentID := before.ParentID
	if patch.ParentName != nil {
		newParent, err := p.locationByName(ctx, tx, *patch.ParentName)
		if errors.Is(err, ErrLocationNotFound) {
			return nil, ErrParentNotFound
		} else if err != nil {
			return nil, err
		}
		in, err := p.inScope(ctx, tx, newParent.ID, action)
		if err != nil {
			return nil, err
		}
		if !in {
			return nil, ErrLocationForbidden
		}
		finalType := before.LocationType
		if patch.LocationType != nil {
			finalType = *patch.LocationType
		}
		// Placement is checked before the cycle guard: a rejected placement
		// (a type mismatch) is reported as PlacementError even when the target
		// parent also happens to be a descendant, so the caller sees the more
		// specific, actionable reason. The cycle guard remains the last-resort
		// structural check, catching moves an unconstrained (or otherwise
		// type-compatible) placement would otherwise let through.
		if err := p.validatePlacement(ctx, tx, finalType, &newParent.LocationType); err != nil {
			return nil, err
		}
		desc, err := p.locationIsDescendant(ctx, tx, before.ID, newParent.ID)
		if err != nil {
			return nil, err
		}
		if desc {
			return nil, ErrLocationCycle
		}
		parentID = &newParent.ID
	}

	after, err := scanLocation(tx.QueryRow(ctx, `
		update location set
			name          = coalesce($2, name),
			display_name  = coalesce($3, display_name),
			location_type = coalesce($4, location_type),
			parent_id     = $5,
			updated_at    = now()
		where id = $1
		returning `+locationCols,
		before.ID, patch.Name, patch.DisplayName, patch.LocationType, parentID))
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

// locationNameByID resolves a location id back to its name. Placements hold the
// id while the estate-address records (health among them) hold the name, so a
// before-image's location has to be translated before the location it points at
// can be acted on.
func locationNameByID(ctx context.Context, q querier, id string) (string, error) {
	var name string
	if err := q.QueryRow(ctx, `select name from location where id = $1`, id).Scan(&name); err != nil {
		return "", fmt.Errorf("storage: location name for %q: %w", id, err)
	}
	return name, nil
}

// LocationNameTaken reports whether a location with this name exists. Scope-blind
// by design: the name unique constraint is global, so availability must be a
// global fact to match it (a scope-aware answer would false-positive on a name
// held outside the caller's scope). Gated at the API by location:update.
func (p *PG) LocationNameTaken(ctx context.Context, name string) (bool, error) {
	var exists bool
	if err := p.pool.QueryRow(ctx, `select exists(select 1 from location where name = $1)`, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("storage: location name taken: %w", err)
	}
	return exists, nil
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
