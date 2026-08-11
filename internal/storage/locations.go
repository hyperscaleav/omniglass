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

	// ErrLocationTypeNoNameRule is asking the platform to name a location whose
	// location_type carries no name rule (#687): a nameless create, or
	// :resetName. It is a refusal by POLICY, not a gap. Of the shipped place
	// vocabulary only a floor is genuinely auto-nameable; a building's or a
	// room's name is ground truth an operator holds ("17c" is a fact, not a
	// default), so generating one would be the platform guessing. Naming the
	// missing fact beats a silent no-op, which would report a reset that never
	// happened.
	//
	// It refused unconditionally on this tier for one slice (#686 shipped the
	// pen and the verbs ahead of the generator, so a location an operator named
	// then is already frozen now). What changed here is only which types it
	// applies to.
	//
	// The message names the WAY OUT, not only the missing fact, because the
	// path an operator meets it on most is neither of the two above: `floor` is
	// the one shipped type carrying a rule, so RECLASSIFYING a platform-named
	// floor as a room, a building or a campus (the routine misclassification
	// fix) lands here, and there "name it yourself" is advice with nowhere to
	// take it, since the patch that reclassifies carries no name field. The
	// system tier's twin (ErrSystemTypeRequiredForName) names its escape for the
	// same reason.
	ErrLocationTypeNoNameRule = errors.New("storage: this location_type has no name rule, so the platform cannot generate a name for it; supply a name on create, or :rename the location to claim its name before reclassifying it to this type")

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
	// polarity. True only when the row's location_type carried a name rule at
	// the moment the platform minted the name (#687). Every location that
	// predates that rule holds the pen, since :rename froze it false a slice
	// before the generator existed.
	NameGenerated bool
	// Ordinal is the number the generator allocated for this row's name (#687),
	// null for a name an operator typed or renamed; see System's own field. It
	// is the number itself, never the digits in the name: a positional
	// location's name IS its ordinal, and a stemmed one with a suppressed first
	// ordinal ("wing", ordinal 1) has no digits in its name at all.
	Ordinal *int
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
//
// Name is optional (#687), exactly as a component's and a system's are: empty
// asks the platform to mint one from the location_type's name rule, and a type
// carrying no rule refuses (ErrLocationTypeNoNameRule) rather than inventing a
// name for a place whose real one an operator holds.
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
	// NameRule is this type's opt-in to GENERATING the names of the locations
	// it classifies (#687), nullable: null means an operator names every one of
	// them, which is where three of the four shipped types stay. Unlike
	// LabelRule it is a declaration rather than a template, and unlike
	// LabelRule it has no tier above it to defer to: a name either comes from
	// this rule or from the operator (ADR-0102).
	NameRule *NameRule
}

// UpsertLocationType installs or updates a location type by id, the boot-seed
// phase's write. Idempotent: re-seeding the same id updates it in place.
// SeedLocationType inserts a shipped example location type only when it is
// absent. Like a standard, a location type is content an operator shapes to their
// organization, so re-seeding must never reassert over an edit. Deliberately not
// UpsertLocationType, whose ON CONFLICT DO UPDATE is the authoritative behavior
// the canonical catalogs want.
func (p *PG) SeedLocationType(ctx context.Context, lt LocationType) error {
	rule, err := nameRuleJSON(lt.NameRule)
	if err != nil {
		return fmt.Errorf("storage: seed location_type %q: %w", lt.Name, err)
	}
	if _, err := p.pool.Exec(ctx, `
		insert into location_type (name, official, display_name, icon, allowed_parent_types, label_rule, name_rule)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (name) do nothing`,
		lt.Name, lt.Official, lt.DisplayName, lt.Icon, normalizeAllowedParentTypes(lt.AllowedParentTypes), nilIfEmptyRule(lt.LabelRule), rule); err != nil {
		return fmt.Errorf("storage: seed location_type %q: %w", lt.Name, err)
	}
	return nil
}

func (p *PG) UpsertLocationType(ctx context.Context, lt LocationType) error {
	rule, err := nameRuleJSON(lt.NameRule)
	if err != nil {
		return fmt.Errorf("storage: upsert location_type %q: %w", lt.Name, err)
	}
	if _, err := p.pool.Exec(ctx, `
		insert into location_type (name, official, display_name, icon, allowed_parent_types, label_rule, name_rule)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (name) do update
			set official             = excluded.official,
			    display_name         = excluded.display_name,
			    icon                 = excluded.icon,
			    allowed_parent_types = excluded.allowed_parent_types,
			    label_rule           = excluded.label_rule,
			    name_rule            = excluded.name_rule`,
		lt.Name, lt.Official, lt.DisplayName, lt.Icon, normalizeAllowedParentTypes(lt.AllowedParentTypes), nilIfEmptyRule(lt.LabelRule), rule); err != nil {
		return fmt.Errorf("storage: upsert location_type %q: %w", lt.Name, err)
	}
	return nil
}

// nameRuleJSON encodes a name rule for its jsonb column, refusing one that
// cannot mint a legal name BEFORE it is stored (ErrInvalidNameRule, a 422).
// Every write path funnels through it, the boot seed included, so a shipped
// rule is held to the same bar an operator's is: a stored rule that cannot mint
// would surface as a failed create on a path that supplied nothing wrong.
//
// nil is no rule, written as SQL null: the opt-out, and the one spelling of it.
func nameRuleJSON(r *NameRule) (any, error) {
	if r == nil {
		return nil, nil
	}
	if err := validateNameRule(r); err != nil {
		return nil, err
	}
	b, err := json.Marshal(r.normalized())
	if err != nil {
		return nil, fmt.Errorf("storage: encode name rule: %w", err)
	}
	return b, nil
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
		`select `+locationTypeCols+` from location_type order by display_name, name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list location_types: %w", err)
	}
	defer rows.Close()
	var out []LocationType
	for rows.Next() {
		lt, err := scanLocationType(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan location_type: %w", err)
		}
		out = append(out, *lt)
	}
	return out, rows.Err()
}

// locationTypeCols is the column list every location_type read scans, in struct
// order, and scanLocationType is the one decode of it. They exist because
// name_rule arrived as the second column needing more than a straight scan (the
// first being allowed_parent_types' nil-to-empty normalization), and two call
// sites decoding jsonb by hand is how a list and a write path start disagreeing
// about what an absent rule reads as.
const locationTypeCols = `id, name, official, display_name, icon, allowed_parent_types, label_rule, name_rule`

func scanLocationType(row pgx.Row) (*LocationType, error) {
	var lt LocationType
	var rule []byte
	if err := row.Scan(&lt.ID, &lt.Name, &lt.Official, &lt.DisplayName, &lt.Icon, &lt.AllowedParentTypes, &lt.LabelRule, &rule); err != nil {
		return nil, err
	}
	lt.AllowedParentTypes = normalizeAllowedParentTypes(lt.AllowedParentTypes)
	if len(rule) > 0 {
		var r NameRule
		if err := json.Unmarshal(rule, &r); err != nil {
			return nil, fmt.Errorf("decode name rule: %w", err)
		}
		r = r.normalized()
		lt.NameRule = &r
	}
	return &lt, nil
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
	// NameRule sets the type's name rule; nil leaves it alone. There is no
	// spelling that CLEARS it back to operator-named, the same limit
	// component_type.stem's patch already carries (its wire field is
	// minLength 1, so an inherited stem cannot be cleared either). Adding one
	// means a three-state on an object field, which JSON cannot give a Go
	// struct: an omitted key and an explicit null both decode to nil.
	NameRule *NameRule
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
	// Refused here, at rule-EDIT time, which is the whole point of a rule that
	// is a declaration: a rule that cannot mint a legal name never reaches a
	// column some later create would have executed it from (ADR-0102).
	rule, err := nameRuleJSON(lt.NameRule)
	if err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create location_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Scanned rather than Exec'd, which this insert's own `returning id` always
	// meant to do and never did: the id is minted by the database, so a caller
	// that discarded it handed the operator back a row with an EMPTY id (the
	// field the wire schema calls the stable handle that survives a rename) and
	// wrote an audit row keyed on the empty string, which commits because
	// resource_id is text. The static audit-key guard could not see it, since it
	// reads the expression at the call site and `lt.ID` is exactly the shape it
	// wants. CreateComponentType has always scanned its own RETURNING; this is
	// the same statement finally doing so, and it is also what makes the
	// normalized name_rule come back from the column rather than from the input.
	created, err := scanLocationType(tx.QueryRow(ctx,
		`insert into location_type (name, official, display_name, icon, allowed_parent_types, label_rule, name_rule)
		 values ($1, false, $2, $3, $4, $5, $6)
		 returning `+locationTypeCols,
		lt.Name, lt.DisplayName, lt.Icon, lt.AllowedParentTypes, lt.LabelRule, rule))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrTypeExists
		}
		return nil, fmt.Errorf("storage: insert location_type %q: %w", lt.Name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "location_type", created.ID, nil, created); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create location_type: %w", err)
	}
	return created, nil
}

// UpdateLocationType patches a custom location_type's display_name, icon, or
// allowed_parent_types (nil fields unchanged) and audits it. Official rows are
// read-only (ErrTypeOfficial); an unknown id is ErrTypeNotFound.
func (p *PG) UpdateLocationType(ctx context.Context, actorID, id string, patch LocationTypePatch) (*LocationType, error) {
	if err := validateLabelRule(patch.LabelRule); err != nil {
		return nil, err
	}
	rule, err := nameRuleJSON(patch.NameRule)
	if err != nil {
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
	lt, err := scanLocationType(tx.QueryRow(ctx, `
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
			end,
			-- A plain coalesce, unlike label_rule above: a name rule has no
			-- "clear" spelling on the wire (see LocationTypePatch.NameRule), so
			-- there is no third state for a CASE to tell apart.
			name_rule            = coalesce($6::jsonb, name_rule)
		where `+registryRefCol(id)+` = $1
		returning `+locationTypeCols,
		id, patch.DisplayName, patch.Icon, allowed, patch.LabelRule, rule))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTypeNotFound
		}
		return nil, fmt.Errorf("storage: update location_type %q: %w", id, err)
	}
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
	return lt, nil
}

// DeleteLocationType removes a custom location_type, refusing an official row and
// a row still referenced by a location.
func (p *PG) DeleteLocationType(ctx context.Context, actorID, id string) error {
	return deleteTypeRow(ctx, p, "location_type", "location_type", actorID, id, typeRef{table: "location", col: "location_type"})
}

// locationCols is the column list every location read scans, in struct order.
const locationCols = `id, name, coalesce(display_name, ''), name_generated, ordinal, display_name_generated,
	(select t.name from location_type t where t.id = location.location_type) as location_type, location.location_type as location_type_id, parent_id,
	(select p.name from location p where p.id = location.parent_id) as parent_name,
	created_at, updated_at`

func scanLocation(row pgx.Row) (*Location, error) {
	var l Location
	if err := row.Scan(&l.ID, &l.Name, &l.DisplayName, &l.NameGenerated, &l.Ordinal, &l.DisplayNameGenerated, &l.LocationType, &l.LocationTypeID, &l.ParentID, &l.ParentName,
		&l.CreatedAt, &l.UpdatedAt); err != nil {
		return nil, err
	}
	return &l, nil
}

// attachLocationPaths fills every l's Path/.PathSegments/.Renders (#627 Task
// 15), in one batch walk however long the page (#643, and one query rather
// than the accessor planes' three: a location's address is its own ancestor
// chain, with no plane root to cross to). A location has no accessor and no
// type-level abbreviation: location_type carries no abbrev column the way
// component_type does, so RenderBare gets nil and "" here and substitutes
// nothing. It now HAS an ordinal to pass (#687), and it is deliberately not
// passed: the render substitutes when it is given an abbrev and an ordinal
// together, so without the abbrev half there is nothing to compact, and a
// positional location's name already IS its ordinal. That is the same ruling
// ADR-0101 records for a system's bare render, reached for a different reason.
// full is unused
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

	// An operator-typed name is validated now, before any other resolve, so a
	// bad slug fails fast (the existing behavior). A generated one is validated
	// too, but later, inside generateName, the one place every generation site
	// funnels through: it is not knowable until the type and the parent below
	// resolve. See CreateSystem's own comment on the same split.
	if spec.Name != "" {
		if err := ValidateName("location", spec.Name); err != nil {
			return nil, err
		}
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

	// An empty name means "generate one" (#687): resolved now, after the type
	// and the parent are both known, since the mint comes from the type's name
	// rule and the ordinal from siblings in this exact bucket.
	name := spec.Name
	generated := name == ""
	// nil unless the platform picked the name: an operator-typed name has no
	// ordinal the platform owns, and the column says so by being absent.
	var ordinal *int
	if generated {
		genName, n, err := generateNameForLocationType(ctx, tx, spec.LocationType, parentID, nil)
		if err != nil {
			return nil, err
		}
		name, ordinal = genName, &n
	}

	// An empty display_name hands the LABEL's pen to the platform (#682); the
	// row is inserted with no label and stamped below.
	l, err := scanLocation(tx.QueryRow(ctx, `
		insert into location (name, display_name, location_type, parent_id, name_generated, ordinal, display_name_generated)
		values ($1, $2, (select id from location_type where `+registryRefCol(spec.LocationType)+` = $3), $4, $5, $6, $7)
		returning `+locationCols,
		name, nullize(spec.DisplayName), spec.LocationType, parentID, generated, ordinal, spec.DisplayName == ""))
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
	// A reclassify that leaves the location still platform-named (#687) may no
	// longer fit its old type's rule: a floor reclassified as a wing needs the
	// wing rule, not the one it was minted from. before.NameGenerated is the
	// read of record, so an operator-typed name (RenameLocation clears the flag
	// forever) is never touched by a reclassify.
	//
	// The guard is the classification CHANGING, not the field being present,
	// and that distinction is load-bearing rather than tidy: the console sends
	// location_type on every save (web/src/pages/Locations.tsx), so a presence
	// test would re-mint on an edit to the display name alone, and with a lower
	// ordinal freed in the meantime by a rename that re-mint MOVES the name
	// under location:update with no rename asked for. That is the defect review
	// caught on the system tier one slice ago (ADR-0101); it is not repeated
	// here.
	var (
		namePatch    *string
		ordinalPatch *int
	)
	//
	// The cheap half of the comparison runs first and skips the resolve
	// entirely: a patch whose ref is the name or the uuid the row already
	// carries changes nothing, and that is what the console sends on every
	// ordinary save, so the common path costs no round trip at all. A ref that
	// LOOKS different still resolves, because a uuid and its name address one
	// row and only the id comparison can tell them apart.
	typePatchID, err := resolveLocationTypeID(ctx, tx, restatedType(before, patch.LocationType))
	if err != nil {
		return nil, err
	}
	if typePatchID != nil && before.NameGenerated && *typePatchID != before.LocationTypeID {
		newName, newOrdinal, err := generateNameForLocationType(ctx, tx, *typePatchID, before.ParentID, &before.ID)
		if err != nil {
			return nil, err
		}
		namePatch, ordinalPatch = &newName, &newOrdinal
	}
	after, err := scanLocation(tx.QueryRow(ctx, `
		update location set
			name    = coalesce($5, name),
			ordinal = coalesce($6::integer, ordinal),
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
		labelPen(before.DisplayNameGenerated, patch.DisplayName), namePatch, ordinalPatch))
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

// restatedType returns nil for a type ref that re-states what the row already
// carries, so the caller can skip resolving it. It compares against whichever
// form the ref is written in, the pair every registry reference is addressed by
// (ADR-0062): a name against the row's name, a uuid against the row's id.
//
// It is a fast path, not a correctness one, and only in the safe direction: a
// ref it lets through is resolved and compared by id anyway, so the two written
// forms of one row still settle as unchanged. What it buys is the console's
// every-save shape, which restates the type the row already has, no longer
// costing a round trip to learn that.
func restatedType(before *Location, ref *string) *string {
	if ref == nil || *ref == "" {
		return nil
	}
	if *ref == before.LocationType || *ref == before.LocationTypeID {
		return nil
	}
	return ref
}

// resolveLocationTypeID resolves a location_type patch field (a name or a uuid)
// to the row's uuid, so a caller can ask whether the classification actually
// CHANGED before acting on it.
//
// A ref that names no row resolves to nil rather than to an error, which looks
// like swallowing one and is not: the UPDATE below keeps the current type for
// an unresolved ref (its coalesce over a NULL subselect), and that has been
// this path's behavior since it was written. Refusing it is a defensible change
// and is not this slice's; making the two disagree, so a nameless ref refused
// here but was accepted there, would be a third behavior nobody chose.
func resolveLocationTypeID(ctx context.Context, q querier, ref *string) (*string, error) {
	if ref == nil || *ref == "" {
		return nil, nil
	}
	var id string
	err := q.QueryRow(ctx, `select id from location_type where `+registryRefCol(*ref)+` = $1`, *ref).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: resolve location_type %q: %w", *ref, err)
	}
	return &id, nil
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

	// A reparent changes the placement BUCKET, which is one of the mint's two
	// inputs, so a still-platform-owned name is re-minted in the destination
	// (#687). The other input, the type's rule, is unchanged by a move, so the
	// rule is simply re-read for the same type.
	//
	// Guarded on the bucket actually moving, not merely on the row being
	// platform-named: a no-op move (a nil ParentName, this verb's documented
	// no-op) changes no input to the mint, and re-minting anyway would move the
	// NAME whenever a lower ordinal had been freed in the meantime. That is the
	// reclassify defect ADR-0101 records wearing the other verb's clothes, and
	// MoveSystem now carries the same guard: it was left wide when this arm was
	// written, on the belief that narrowing it would move an existing expected
	// value, which was false (systems gained generated names one slice earlier
	// on the same unmerged branch, so every value at risk was that branch's own).
	// The two arms spell the comparison differently because the two kinds do
	// not have the same number of buckets: a location has TWO and the parent is
	// the whole of the distinction, so comparing the parent IS comparing the
	// bucket, while a system has three with a parent winning over a location,
	// so its arm compares the nameScope bucket the two placements resolve to.
	var (
		namePatch    *string
		ordinalPatch *int
	)
	if before.NameGenerated && !sameOptional(before.ParentID, parentID) {
		newName, newOrdinal, err := generateNameForLocationType(ctx, tx, before.LocationTypeID, parentID, &before.ID)
		if err != nil {
			return nil, err
		}
		namePatch, ordinalPatch = &newName, &newOrdinal
	}
	after, err := scanLocation(tx.QueryRow(ctx, `
		update location set
			parent_id = $2,
			name      = coalesce($3, name),
			-- The number the new name was minted from, recorded with it: a move
			-- changes the bucket, so the old bucket's ordinal is no more true of
			-- the row than the old name was.
			ordinal   = coalesce($4::integer, ordinal),
			updated_at = now()
		where id = $1
		returning `+locationCols,
		before.ID, parentID, namePatch, ordinalPatch))
	if err != nil {
		return nil, mapLocationWriteErr(err)
	}
	// A re-minted name is a fact a label rule reads (.Name), so a
	// platform-owned label follows it (#682), and so does everything placed
	// here when the rename moved what this location READS as (#685). The same
	// two steps in the same order RenameLocation runs, for the same reason: a
	// move that re-mints IS a rename, it is just not the operator's.
	if before.Name != after.Name {
		if after, err = p.stampLocationLabel(ctx, tx, after); err != nil {
			return nil, err
		}
		if locationReadLabel(before) != locationReadLabel(after) {
			if err := p.cascadeLocationLabels(ctx, tx, after.ID); err != nil {
				return nil, err
			}
		}
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
	// way back. The ordinal goes with it (#687): the platform owns no number for
	// a name it did not pick, and absent is how that is written down.
	after, err := scanLocation(tx.QueryRow(ctx,
		`update location set name = $2, name_generated = false, ordinal = null, updated_at = now() where id = $1 returning `+locationCols,
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

// ResetLocationName hands the pen back to the platform (#687), the location
// tier's twin of ResetSystemName: it regenerates the name from the location's
// CURRENT type rule and parent, the exact rule a nameless create applies, and
// marks the result name_generated again, recording the ordinal it was minted
// from. It does not matter whether the name was already platform-owned or an
// operator had typed one: either way the answer is a fresh mint. Scoped exactly
// as RenameLocation (the same read-then-action split), because it is the same
// act from the permission's point of view: it changes the name, gated by
// location:rename, no new token.
//
// A type with no name rule is refused (ErrLocationTypeNoNameRule -> 422) rather
// than left half-reset, which is what this verb did for EVERY type for one
// slice (#686 shipped the pen and the verbs ahead of the generator). The
// refusal is not a gap: a building's name is ground truth an operator holds, so
// a reset that invented one would be worse than declining, and a reset that
// silently kept the operator's name while claiming the pen would be a lie about
// who owns the field.
//
// The target resolves first, so an operator learns "this type cannot generate a
// name" only about a location they can actually see and rename; an unreadable
// one is still the non-disclosing 404 it would be for any other act.
//
// The audit verb is "reset", distinct from "rename": the row records what
// actually happened (the platform picked the name, not the caller), the same
// reasoning that gave move its own verb apart from update.
func (p *PG) ResetLocationName(ctx context.Context, actorID, name string, read, action scope.Set) (*Location, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin reset location name: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := p.resolveForAction(ctx, tx, name, read, action)
	if err != nil {
		return nil, err
	}
	newName, newOrdinal, err := generateNameForLocationType(ctx, tx, before.LocationTypeID, before.ParentID, &before.ID)
	if err != nil {
		return nil, err
	}
	after, err := scanLocation(tx.QueryRow(ctx,
		`update location set name = $2, name_generated = true, ordinal = $3, updated_at = now() where id = $1 returning `+locationCols,
		before.ID, newName, newOrdinal))
	if err != nil {
		return nil, mapLocationWriteErr(err)
	}
	// The same pair a rename settles, in the other direction: a fresh name is a
	// fact a rule reads, so a platform-owned label follows it, and so does
	// everything placed here when the read ladder moved (#682, #685).
	if after, err = p.stampLocationLabel(ctx, tx, after); err != nil {
		return nil, err
	}
	if locationReadLabel(before) != locationReadLabel(after) {
		if err := p.cascadeLocationLabels(ctx, tx, after.ID); err != nil {
			return nil, err
		}
	}
	if err := writeAuditRes(ctx, tx, actorID, "reset", "location", after.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit reset location name: %w", err)
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
