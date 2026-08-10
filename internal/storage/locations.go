package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
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

	// ErrLocationTypeNoNameRule is :resetName on a location today (#686). The
	// pen and both verbs spread to this tier so the shape is the same one on
	// all three trees, but a location has nothing to regenerate FROM:
	// location_type carries no stem, unlike component_type and system_type, so
	// there is no mint. The seam is #687's nullable per-type name rule, which
	// is also the reason this is a typed refusal naming the missing fact rather
	// than a silent no-op: a no-op would report success for a name that never
	// changed and would make the verb untestable the day the rule lands.
	ErrLocationTypeNoNameRule = errors.New("storage: this location_type has no name rule, so the platform cannot generate a name for it")

	// ErrLocationExistsUnderParent / ErrLocationExistsAtRoot name which
	// placement bucket a 23505 collided in (#627 scopes name uniqueness to
	// placement: unique under a given parent, or unique among roots, but not
	// across both at once). Each wraps ErrLocationExists via %w, so
	// errors.Is(err, ErrLocationExists) still matches either generically.
	ErrLocationExistsUnderParent = fmt.Errorf("storage: a location with this name already exists under this parent: %w", ErrLocationExists)
	ErrLocationExistsAtRoot      = fmt.Errorf("storage: a root location with this name already exists: %w", ErrLocationExists)
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
	err := q.QueryRow(ctx, `select allowed_parent_types from location_type where `+registryRefCol(childType)+` = $1`, childType).Scan(&allowed)
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
	ID          string
	Name        string
	DisplayName string
	// NameGenerated is the NAME's pen (#686); see System's own field for the
	// polarity. False on every location today and written only false: a
	// location cannot generate a name until its type carries a name rule
	// (#687), so this tier has the pen and the verbs without a generator yet.
	// It ships now rather than with the generator because the pen is what makes
	// a later generated name safe: :rename freezes it false, so a row an
	// operator named before #687 lands can never be re-minted afterwards.
	NameGenerated bool
	// DisplayNameGenerated is the label's pen (#682); see System's own field
	// for the polarity and what a write does with it.
	DisplayNameGenerated bool
	LocationType         string
	LocationTypeID       string
	ParentID             *string
	// The name the API addresses the parent by; ParentID above is internal.
	ParentName *string
	// Path, PathSegments, and Renders are the dotted address (no accessor: a
	// location's own address IS its location-tree ancestor chain) and its two
	// display-only compact forms (#627 Task 15), attached by
	// attachLocationPaths after every GET or LIST fetch; see Component's own
	// Path field for the full reasoning (write paths leave this zero-value).
	Path         string
	PathSegments []string
	Renders      Renders
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

// LocationPatch is the update input: nil fields are left unchanged.
//
// There is deliberately no Name here. A rename is RenameLocation, its own act under
// its own permission, because it breaks the references an operator stored outside
// this system. There is deliberately no ParentName here either (#627 Task 13): a
// reparent is its own act, MoveLocation, gated by location:move rather than
// location:update; see that function's doc comment for why.
type LocationPatch struct {
	DisplayName  *string
	LocationType *string
}

// LocationMove is the :move input: ParentName nil is a no-op (only a
// direct-gateway caller would pass one; the API requires it, since a location's
// :move carries no other field). A named parent re-parents the location (a tree
// move), cycle-guarded and placement-validated exactly like create. An explicit
// empty string is refused (ErrParentNotFound, 422): MoveLocation does NOT gain a
// clear-to-root capability locations have never had. UpdateLocation's old
// ParentName patch never had a "" branch either (it always resolved the parent
// unconditionally, so an empty string already 422'd before this split), so this
// is a straight carry of existing behavior into its own act, not a new
// restriction; see the ADR for why the asymmetry with component/system (which
// DO gain a guarded clear-to-root under :move) is deliberate rather than closed.
type LocationMove struct {
	ParentName *string
}

// LocationType is a registry row classifying a location: a stable id, the
// official flag, a display_name, an icon (a glyph key the console renders as
// the leading glyph on every location of this type), and AllowedParentTypes,
// the placement constraint: a set of location_type ids and/or RootPlacement
// this type may be placed under. An empty set is unconstrained. It is the
// only shape-definer for a location, which has no template. The registry
// lists alphabetically by display_name; there is no ordering field.
type LocationType struct {
	// ID is the uuid primary key, Name the renameable slug handle (ADR-0062). The
	// allowed_parent_types array still holds slugs, a within-registry reference by
	// name that this epic leaves as-is.
	ID                 string
	Name               string
	Official           bool
	DisplayName        string
	Icon               string
	AllowedParentTypes []string
	// LabelRule is this type's label template (#682), nullable: null defers to
	// the global tier. A location_type is flat (no parent link), so there is
	// nothing to inherit from and the two tiers are the whole ladder.
	LabelRule *string
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
		insert into location_type (name, official, display_name, icon, allowed_parent_types, label_rule)
		values ($1, $2, $3, $4, $5, $6)
		on conflict (name) do nothing`,
		lt.Name, lt.Official, lt.DisplayName, lt.Icon, normalizeAllowedParentTypes(lt.AllowedParentTypes), nilIfEmptyRule(lt.LabelRule))
	if err != nil {
		return fmt.Errorf("storage: seed location_type %q: %w", lt.Name, err)
	}
	return nil
}

func (p *PG) UpsertLocationType(ctx context.Context, lt LocationType) error {
	_, err := p.pool.Exec(ctx, `
		insert into location_type (name, official, display_name, icon, allowed_parent_types, label_rule)
		values ($1, $2, $3, $4, $5, $6)
		on conflict (name) do update
			set official             = excluded.official,
			    display_name         = excluded.display_name,
			    icon                 = excluded.icon,
			    allowed_parent_types = excluded.allowed_parent_types,
			    label_rule           = excluded.label_rule`,
		lt.Name, lt.Official, lt.DisplayName, lt.Icon, normalizeAllowedParentTypes(lt.AllowedParentTypes), nilIfEmptyRule(lt.LabelRule))
	if err != nil {
		return fmt.Errorf("storage: upsert location_type %q: %w", lt.Name, err)
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
		`select id, name, official, display_name, icon, allowed_parent_types, label_rule from location_type order by display_name, name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list location_types: %w", err)
	}
	defer rows.Close()
	var out []LocationType
	for rows.Next() {
		var lt LocationType
		if err := rows.Scan(&lt.ID, &lt.Name, &lt.Official, &lt.DisplayName, &lt.Icon, &lt.AllowedParentTypes, &lt.LabelRule); err != nil {
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
	LabelRule          *string
}

// CreateLocationType inserts a custom (official=false) location_type and audits
// it. A duplicate id (including a seed-owned official id) is ErrTypeExists;
// "root" (the allowed_parent_types sentinel) is ErrReservedTypeID.
func (p *PG) CreateLocationType(ctx context.Context, actorID string, lt LocationType) (*LocationType, error) {
	if lt.Name == RootPlacement {
		return nil, ErrReservedTypeID
	}
	if err := ValidateName("location_type", lt.Name); err != nil {
		return nil, err
	}
	if err := validateLabelRule(lt.LabelRule); err != nil {
		return nil, err
	}
	lt.Official = false
	lt.AllowedParentTypes = normalizeAllowedParentTypes(lt.AllowedParentTypes)
	lt.LabelRule = nilIfEmptyRule(lt.LabelRule)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create location_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`insert into location_type (name, official, display_name, icon, allowed_parent_types, label_rule) values ($1, false, $2, $3, $4, $5) returning id`,
		lt.Name, lt.DisplayName, lt.Icon, lt.AllowedParentTypes, lt.LabelRule); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrTypeExists
		}
		return nil, fmt.Errorf("storage: insert location_type %q: %w", lt.Name, err)
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
	if err := validateLabelRule(patch.LabelRule); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update location_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "location_type", id); err != nil {
		return nil, err
	}
	before, err := registryAuditImage(ctx, tx, "location_type", id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTypeNotFound
		}
		return nil, fmt.Errorf("storage: audit image location_type %q: %w", id, err)
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
			allowed_parent_types = coalesce($4, allowed_parent_types),
			-- An explicit empty string CLEARS the rule; see UpdateComponentType's
			-- own label_rule CASE for why this is not a coalesce.
			label_rule           = case
				when $5::text is null then label_rule
				when $5 = '' then null
				else $5::text
			end
		where `+registryRefCol(id)+` = $1
		returning id, name, official, display_name, icon, allowed_parent_types, label_rule`,
		id, patch.DisplayName, patch.Icon, allowed, patch.LabelRule).
		Scan(&lt.ID, &lt.Name, &lt.Official, &lt.DisplayName, &lt.Icon, &lt.AllowedParentTypes, &lt.LabelRule); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTypeNotFound
		}
		return nil, fmt.Errorf("storage: update location_type %q: %w", id, err)
	}
	lt.AllowedParentTypes = normalizeAllowedParentTypes(lt.AllowedParentTypes)
	after, err := registryAuditImage(ctx, tx, "location_type", id)
	if err != nil {
		return nil, fmt.Errorf("storage: audit image location_type %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "location_type", lt.ID, before, after); err != nil {
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
	return deleteTypeRow(ctx, p, "location_type", "location_type", actorID, id, typeRef{table: "location", col: "location_type"})
}

// locationCols is the column list every location read scans, in struct order.
const locationCols = `id, name, coalesce(display_name, ''), name_generated, display_name_generated,
	(select t.name from location_type t where t.id = location.location_type) as location_type, location.location_type as location_type_id, parent_id,
	(select p.name from location p where p.id = location.parent_id) as parent_name,
	created_at, updated_at`

func scanLocation(row pgx.Row) (*Location, error) {
	var l Location
	if err := row.Scan(&l.ID, &l.Name, &l.DisplayName, &l.NameGenerated, &l.DisplayNameGenerated, &l.LocationType, &l.LocationTypeID, &l.ParentID, &l.ParentName,
		&l.CreatedAt, &l.UpdatedAt); err != nil {
		return nil, err
	}
	return &l, nil
}

// attachLocationPaths fills every l's Path/.PathSegments/.Renders (#627 Task
// 15), in one batch walk however long the page (#643, and one query rather
// than the accessor planes' three: a location's address is its own ancestor
// chain, with no plane root to cross to). A location has no accessor, no
// type-level abbreviation (location_type carries no abbrev column the way
// component_type does), and no generated name to have allocated an ordinal for
// (#657 slices 6 and 7 bring both), so RenderBare always gets nil and ""
// here. full is unused
// (see attachSystemPaths' own doc comment for why the parameter exists
// anyway).
func attachLocationPaths(ctx context.Context, q querier, ls []*Location, full bool) error {
	_ = full
	if len(ls) == 0 {
		return nil
	}
	ids := make([]string, 0, len(ls))
	for _, l := range ls {
		ids = append(ids, l.ID)
	}
	paths, err := PathsOf(ctx, q, locationTable, ids)
	if err != nil {
		return err
	}
	for _, l := range ls {
		segs := paths[l.ID]
		l.PathSegments = segs
		l.Path = strings.Join(segs, ".")
		l.Renders = Renders{Dash: RenderDash(segs), Bare: RenderBare(segs, nil, "")}
	}
	return nil
}

// locationConfig drives the generic scoped-CRUD helpers for the location tree.
var locationConfig = scopedConfig[Location]{
	table: locationTable, cols: locationCols, resource: "location",
	scan: scanLocation, idOf: func(l *Location) string { return l.ID },
	notFound: ErrLocationNotFound, forbidden: ErrLocationForbidden, occupied: ErrLocationOccupied,
	attachPaths: attachLocationPaths,
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

	if err := ValidateName("location", spec.Name); err != nil {
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
		// resolveScopedRef, not locationByName-then-inScope: ruling 2 (#627)
		// requires ambiguity judged inside create, not estate-wide. A parent
		// that exists only outside create scope stays ErrLocationForbidden
		// (preserved, not collapsed into not-found).
		parent, err := resolveScopedRef(ctx, tx, locationConfig, *spec.ParentName, "location", create)
		if errors.Is(err, ErrLocationNotFound) {
			return nil, ErrParentNotFound
		} else if err != nil {
			return nil, err
		}
		parentID = &parent.ID
		parentType = &parent.LocationType
	}

	if err := p.validatePlacement(ctx, tx, spec.LocationType, parentType); err != nil {
		return nil, err
	}

	// An empty display_name hands the LABEL's pen to the platform (#682); the
	// row is inserted with no label and stamped below.
	l, err := scanLocation(tx.QueryRow(ctx, `
		insert into location (name, display_name, location_type, parent_id, display_name_generated)
		values ($1, $2, (select id from location_type where `+registryRefCol(spec.LocationType)+` = $3), $4, $5)
		returning `+locationCols,
		spec.Name, nullize(spec.DisplayName), spec.LocationType, parentID, spec.DisplayName == ""))
	if err != nil {
		return nil, mapLocationWriteErr(err)
	}
	if l, err = p.stampLocationLabel(ctx, tx, l); err != nil {
		return nil, err
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
//
// Placement (a reparent) is NOT here (#627 Task 13): it is its own act,
// MoveLocation, gated by location:move. See that function's doc comment.
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

	// A nil type patch keeps the ref column on name: the subselect gets NULL,
	// resolves nothing, and coalesce keeps the current value, same as before.
	typeRefCol := "name"
	if patch.LocationType != nil {
		typeRefCol = registryRefCol(*patch.LocationType)
	}
	after, err := scanLocation(tx.QueryRow(ctx, `
		update location set
			-- Three-state, not a coalesce: a value is the operator typing a
			-- label and taking the pen ($4), an explicit empty string clears it
			-- and hands the pen back (#682).
			display_name  = case
				when $2::text is null then display_name
				when $2 = '' then null
				else $2::text
			end,
			display_name_generated = $4,
			location_type = coalesce((select id from location_type where `+typeRefCol+` = $3), location_type),
			updated_at    = now()
		where id = $1
		returning `+locationCols,
		before.ID, patch.DisplayName, patch.LocationType,
		labelPen(before.DisplayNameGenerated, patch.DisplayName)))
	if err != nil {
		return nil, mapLocationWriteErr(err)
	}
	// A reclassify changes the location_type facts a rule reads, and clearing
	// the label hands the pen back; both are settled by now (#682).
	if after, err = p.stampLocationLabel(ctx, tx, after); err != nil {
		return nil, err
	}
	// What a component or system reads for .LocationLabel is this location's
	// read ladder, so the cascade fires on the LADDER moving rather than on the
	// column moving: clearing a typed label hands the pen back and the ladder
	// falls through to the name, which is a different string for every row
	// below even though the column ends up empty. Compared after the stamp,
	// because the stamp is what settles the column (#685).
	if locationReadLabel(before) != locationReadLabel(after) {
		if err := p.cascadeLocationLabels(ctx, tx, after.ID); err != nil {
			return nil, err
		}
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "location", after.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update location: %w", err)
	}
	return after, nil
}

// MoveLocation re-parents a location addressed by name, the same three-way scope
// split as UpdateLocation, in its own transaction with its own DISTINCT audit
// verb ("move", not "update"). Its own act, not a PATCH field (#627 Task 13),
// for the reason the ADR states on component and system: a PATCH that cleared
// parent_id used to lift a row out of every subtree scope with no check, while
// creating the same root requires an all-scoped grant. A location's own
// UpdateLocation never had that hole (its ParentName patch always resolved the
// new parent unconditionally, so it could only ever move the location to
// somewhere that resolved, never clear it), so this split closes no bug here;
// it exists so placement is one authorization act across all three tiers, the
// same custom method with the same permission shape, rather than the odd one
// out. Placement is checked before the cycle guard: a rejected placement (a
// type mismatch) is reported as PlacementError even when the target parent also
// happens to be a descendant, so the caller sees the more specific, actionable
// reason.
//
// MoveLocation does NOT gain a clear-to-root capability: an explicit empty
// ParentName resolves nothing (ErrParentNotFound, 422), the same 422 an empty
// string already produced under the old UpdateLocation patch. Component and
// system DO gain a guarded clear-to-root under their own :move (see
// MoveComponent); this is the one deliberate asymmetry the ADR documents rather
// than closes, because adding clear-to-root to locations is a product capability
// nobody has asked for, not a security fix.
func (p *PG) MoveLocation(ctx context.Context, actorID, name string, move LocationMove, read, action scope.Set) (*Location, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin move location: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := p.resolveForAction(ctx, tx, name, read, action)
	if err != nil {
		return nil, err
	}

	parentID := before.ParentID
	if move.ParentName != nil {
		// resolveScopedRef, not locationByName-then-inScope: ruling 2
		// (#627), ambiguity judged inside action rather than estate-wide.
		newParent, err := resolveScopedRef(ctx, tx, locationConfig, *move.ParentName, "location", action)
		if errors.Is(err, ErrLocationNotFound) {
			return nil, ErrParentNotFound
		} else if err != nil {
			return nil, err
		}
		if err := p.validatePlacement(ctx, tx, before.LocationType, &newParent.LocationType); err != nil {
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
		update location set parent_id = $2, updated_at = now() where id = $1 returning `+locationCols,
		before.ID, parentID))
	if err != nil {
		return nil, mapLocationWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "move", "location", after.ID, before, after); err != nil {
		return nil, err
	}
	// A reparent recomputes both ancestor chains (#642), the one exception to
	// ":move never recomputes health" on this tier, and the same exception a
	// system's relocate already carries one level down. locationVerdict rolls
	// up recursively through the location tree (locationsOver walks a system's
	// location upward, locationVerdict folds every system in a location's own
	// subtree downward), so a location with placed descendants moving to a new
	// parent really does change what its old and new ancestors' rollups read:
	// the chain it left has lost that whole subtree's contribution and the
	// chain it joined has gained it. A reparent that changes nothing (a nil
	// ParentName, the documented no-op) recomputes nothing, the same guard
	// MoveSystem's relocate applies.
	if reparented := !sameOptional(before.ParentID, after.ParentID); reparented {
		if err := p.recomputeMovedLocation(ctx, tx, ownerRef{ID: after.ID, Name: after.Name}, before.ParentID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit move location: %w", err)
	}
	return after, nil
}

// RenameLocation moves a location's name, scoped exactly as UpdateLocation is (the
// same read-then-action split, so an unreadable target is a non-disclosing
// not-found and a readable-but-unactionable one is forbidden).
//
// Separate from the patch because a rename is a separate act: it breaks the
// references an operator stored outside this system. Nothing inside breaks, because
// every arc that points at a location (a placement, a tag binding, a variable, a
// secret) stores the uuid, which is also why the audit row keys on that uuid rather
// than on the name it just moved.
//
// Placement is not revalidated: a name is not part of the placement rule, which
// reads location_type against the parent's.
func (p *PG) RenameLocation(ctx context.Context, actorID, name, newName string, read, action scope.Set) (*Location, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin rename location: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := p.resolveForAction(ctx, tx, name, read, action)
	if err != nil {
		return nil, err
	}
	if err := ValidateName("location", newName); err != nil {
		return nil, err
	}
	// name_generated is cleared here whether or not it was already set (#686),
	// the same freeze a component's and a system's rename applies: an operator
	// who types a name is claiming the pen for good, and :resetName is the only
	// way back. It writes false today over false, because nothing generates a
	// location name yet; it is here so the row an operator names TODAY is still
	// protected the day #687 gives its type a name rule.
	after, err := scanLocation(tx.QueryRow(ctx,
		`update location set name = $2, name_generated = false, updated_at = now() where id = $1 returning `+locationCols,
		before.ID, newName))
	if err != nil {
		return nil, mapLocationWriteErr(err)
	}
	// .Name is a fact a rule can read, so a platform-owned label follows the
	// rename (#682).
	if after, err = p.stampLocationLabel(ctx, tx, after); err != nil {
		return nil, err
	}
	// And so does everything placed here, when the rename moved what this
	// location READS as: an unlabelled location reads as its name, which is the
	// state every shipped estate is in (#685).
	if locationReadLabel(before) != locationReadLabel(after) {
		if err := p.cascadeLocationLabels(ctx, tx, after.ID); err != nil {
			return nil, err
		}
	}
	if err := writeAuditRes(ctx, tx, actorID, "rename", "location", after.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit rename location: %w", err)
	}
	return after, nil
}

// ResetLocationName is the location tier's half of the pen (#686), and today it
// always refuses: ErrLocationTypeNoNameRule (a 422 naming the missing fact).
//
// The verb exists on all three trees so the contract is one shape rather than
// two, and it refuses HERE, in the gateway, rather than being left off the API:
// a location has no stem source at all (location_type carries none, unlike
// component_type and system_type), so there is no mint for it to run and no
// honest success to report. The alternative, returning the row unchanged, would
// report a reset that did not happen and would leave nothing for #687's first
// test to flip.
//
// It resolves the target first, under the same read-then-action split
// RenameLocation uses, so an operator learns "this type cannot generate a name"
// only about a location they can actually see and rename. An unreadable one is
// still the non-disclosing 404 it would be for any other act.
func (p *PG) ResetLocationName(ctx context.Context, actorID, name string, read, action scope.Set) (*Location, error) {
	if _, err := p.resolveForAction(ctx, p.pool, name, read, action); err != nil {
		return nil, err
	}
	return nil, ErrLocationTypeNoNameRule
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

// LocationNameTaken reports whether name is already used within the placement
// a create would actually land in (#627: the unique constraint is scoped to
// placement, not global). A parentRef makes it a child location
// (location_parent_name_key); no parentRef (nil or "") is the root bucket
// (location_root_name_key), the only bucket a location has since it carries no
// located-at column of its own. Gated at the API by location:update.
func (p *PG) LocationNameTaken(ctx context.Context, name string, parentRef *string) (bool, error) {
	var exists bool
	if parentRef != nil && *parentRef != "" {
		parent, err := scopedByName(ctx, p.pool, locationConfig, *parentRef)
		if err != nil {
			// withoutCandidates: this advisory has no caller scope to filter
			// an ambiguous parentRef by (intentionally scope-blind, see
			// ComponentNameTaken's comment), so a refusal here must never
			// name a row the caller might not be able to read.
			return false, withoutCandidates(err)
		}
		if err := p.pool.QueryRow(ctx, `select exists(select 1 from location where parent_id = $1 and name = $2)`, parent.ID, name).Scan(&exists); err != nil {
			return false, fmt.Errorf("storage: location name taken: %w", err)
		}
		return exists, nil
	}
	if err := p.pool.QueryRow(ctx, `select exists(select 1 from location where parent_id is null and name = $1)`, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("storage: location name taken: %w", err)
	}
	return exists, nil
}

// querier is the read surface shared by *pgxpool.Pool and pgx.Tx, so scope and
// lookup helpers run either standalone or inside a transaction. Query sits
// beside QueryRow because scopedByName needs it to detect a second matching row
// (an ambiguous bare name, #627) rather than silently taking the first the way
// QueryRow's single-row contract would.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
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
			switch pgErr.ConstraintName {
			case idxLocationParentName:
				return ErrLocationExistsUnderParent
			case idxLocationRootName:
				return ErrLocationExistsAtRoot
			}
			return ErrLocationExists
		case "23502": // not-null: an unknown location_type name resolved to null
			if pgErr.ColumnName == "location_type" {
				return ErrUnknownType
			}
		case "23503": // foreign_key_violation
			if pgErr.ConstraintName == "location_location_type_fkey" {
				return ErrUnknownType
			}
		}
	}
	return fmt.Errorf("storage: location write: %w", err)
}
