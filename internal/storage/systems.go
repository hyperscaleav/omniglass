package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// System-layer sentinel errors, mirroring the location set: the non-disclosing
// 404, the readable-but-not-actionable 403, and the request faults.
var (
	ErrSystemNotFound         = errors.New("storage: system not found")
	ErrSystemForbidden        = errors.New("storage: action not permitted on this system")
	ErrSystemOccupied         = errors.New("storage: system has child systems")
	ErrSystemExists           = errors.New("storage: system name already exists")
	ErrParentSystemNotFound   = errors.New("storage: parent system not found")
	ErrSystemCycle            = errors.New("storage: cannot move a system under itself or a descendant")
	ErrUnknownStandard        = errors.New("storage: unknown standard")
	ErrParentStandardNotFound = errors.New("storage: parent standard not found")
	// ErrUnknownSystemType is a create or patch naming a system_type that
	// resolves to nothing. Deliberately separate from ErrUnknownStandard: the
	// two fields answer different questions (what kind of space this is versus
	// which blueprint it is built to), so a refusal that named the wrong one
	// would send an operator to the wrong picker.
	ErrUnknownSystemType = errors.New("storage: unknown system_type")

	// ErrSystemBindForbidden is a component create naming a system the caller may
	// READ but not UPDATE. It is separate from ErrSystemForbidden because it is
	// about a reference in a create's BODY rather than about the row the request
	// addresses, and the two want different words: the generic one becomes a bare
	// "forbidden" on the component route, where this one names the scope the bind
	// resolves in and the recovery that needs no grant at all.
	//
	// It is deliberately NOT ErrSystemNotFound. That refusal is the right answer
	// for a system the caller cannot see (it must not learn the row exists), and
	// the wrong one here, where the caller can GET the row and would read "system
	// not found" as a platform defect instead of as the missing grant it is.
	ErrSystemBindForbidden = errors.New("storage: the system named on this create is outside the caller's system:update scope")

	// ErrSystemExistsUnderParent / ErrSystemExistsInLocation / ErrSystemExistsUnplaced
	// name which placement bucket a 23505 collided in, mirroring the component
	// set: #627 scopes name uniqueness to placement, not the whole fleet. Each
	// wraps ErrSystemExists via %w, so errors.Is(err, ErrSystemExists) still
	// matches any of them generically.
	ErrSystemExistsUnderParent = fmt.Errorf("storage: a system with this name already exists under this parent: %w", ErrSystemExists)
	ErrSystemExistsInLocation  = fmt.Errorf("storage: a system with this name already exists at this location: %w", ErrSystemExists)
	ErrSystemExistsUnplaced    = fmt.Errorf("storage: an unplaced system with this name already exists: %w", ErrSystemExists)
)

// Standard is the blueprint a system conforms to (huddle room, classroom,
// auditorium): the system-side counterpart of product for a component. Beyond
// the registry shape (id, official, label) it carries an optional parent
// standard, so a variant inherits from a base one exactly as
// product.parent_product_id does. The registry lists alphabetically by
// label; there is no ordering field.
type Standard struct {
	// ID is the uuid primary key and Name the renameable name (ADR-0062).
	ID                 string
	Name               string
	Official           bool
	Label              string
	ParentStandardID   *string
	ParentStandardName *string
	// LabelRule is this standard's label template (#682), the most specific
	// tier a system's label resolves through: null defers to the system_type
	// chain, and then to the global tier.
	LabelRule *string
	// Map is the standard's room-layout declaration (#791, ADR-0128), the raw
	// jsonb value ({aspect, positions:[{role, position, x, y}]}); nil when the
	// standard declares none. Display data: nothing queries into it.
	Map json.RawMessage
}

// StandardMap is the parsed shape of Standard.Map, used for validation on
// write and by any Go reader; the wire carries the raw value.
type StandardMap struct {
	// Aspect is width over height of the room's drawing space.
	Aspect float64 `json:"aspect"`
	// Positions place each role position in normalized space: x and y in
	// [0, 1], position 1-based (matching position_labels).
	Positions []StandardMapPosition `json:"positions"`
}

type StandardMapPosition struct {
	Role     string  `json:"role"`
	Position int     `json:"position"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
}

// ErrStandardMapInvalid is a map declaration that fails validation: a
// non-positive aspect, a coordinate outside [0, 1], a position below 1, or a
// duplicate (role, position) pair.
var ErrStandardMapInvalid = errors.New("storage: standard map invalid")

// validateStandardMap parses and checks a map declaration. A JSON null is
// valid and means "clear the declaration".
func validateStandardMap(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var m StandardMap
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("%w: %v", ErrStandardMapInvalid, err)
	}
	if m.Aspect <= 0 {
		return fmt.Errorf("%w: aspect must be positive", ErrStandardMapInvalid)
	}
	seen := map[string]bool{}
	for _, pos := range m.Positions {
		if pos.Role == "" || pos.Position < 1 || pos.X < 0 || pos.X > 1 || pos.Y < 0 || pos.Y > 1 {
			return fmt.Errorf("%w: position %s/%d out of bounds", ErrStandardMapInvalid, pos.Role, pos.Position)
		}
		key := fmt.Sprintf("%s/%d", pos.Role, pos.Position)
		if seen[key] {
			return fmt.Errorf("%w: duplicate position %s", ErrStandardMapInvalid, key)
		}
		seen[key] = true
	}
	return nil
}

// System is a composition of components (the service tree): name-addressable,
// nestable via parent_id, optionally located at a location, and optionally
// conforming to a standard. StandardID is nil for a one-off system, mirroring
// component.product_id: a system that matches no blueprint carries only its own
// ad-hoc values.
//
// SystemTypeID/SystemTypeName are the coarse classifier (a boardroom, a
// classroom, a video wall), the system-side counterpart of a product's
// component_type and a different question from the standard: the type says what
// kind of space this is, the standard says which blueprint it is built to. Both
// forms are carried for the same reason the standard pair is, the uuid being
// the stable handle and the name the one an operator reads. Nullable, and
// nullable it stays until the floor slice.
type System struct {
	ID    string
	Name  string
	Label string
	// NameGenerated is the NAME's pen (#686), the same shape and polarity
	// component's has carried since #627: true means the platform minted this
	// name from the system_type's stem and a later act on the row (a move, a
	// reclassify, :resetName) re-mints it; false means an operator typed it
	// and only :resetName hands it back. False for every row that existed
	// before the generator reached this tier.
	NameGenerated bool
	// Ordinal is the number the generator allocated for this row's name
	// (#686, the system-tier twin of #681's component column). nil means the
	// platform owns no number here: an operator typed the name, or renamed the
	// row and took the pen (:rename nulls this in the same statement it clears
	// NameGenerated). The two always agree, which the recompute-and-compare
	// invariant test pins.
	//
	// A SUPPRESSED first name stores 1 while carrying no digits (ADR-0101):
	// "boardroom" is ordinal 1 and "boardroom-2" is ordinal 2. That the two can
	// differ is exactly why the number is a column of its own rather than
	// something a reader picks back out of the name.
	//
	// A storage fact, not a wire field: a label rule reads it server-side, and
	// the console learns who holds the pen from NameGenerated.
	Ordinal *int
	// LabelGenerated is the label's pen (#682), mirroring
	// name_generated's polarity: true means the platform owns the field and a
	// write re-renders it from the resolved rule. Setting the label by hand
	// clears it; clearing the field returns it.
	LabelGenerated bool
	StandardID     *string
	StandardName   *string
	SystemTypeID   *string
	SystemTypeName *string
	ParentID       *string
	LocationID     *string
	// MemberCount is how many components are bound into this system. It comes from
	// system_member, not from any pointer on the component: membership is the
	// relation that says what is in a system, and reading it from anywhere else is
	// how a fully staffed system came to report zero components.
	MemberCount int
	// The names the API addresses placement by; the ids above are internal.
	ParentName   *string
	LocationName *string
	// Path, PathSegments, and Renders are the dotted address and its two
	// display-only compact forms (#627 Task 15), attached by attachSystemPaths
	// after every GET or LIST fetch; see Component's own Path field for the
	// full reasoning (write paths leave this zero-value).
	Path         string
	PathSegments []string
	Renders      Renders
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// SystemSpec is the create input. ParentName nil makes a root system (which only
// an all-scoped create grant may place); LocationName optionally places it at a
// location; StandardID optionally names the blueprint it conforms to and
// SystemTypeID the coarse kind of space it is, each by name or uuid.
type SystemSpec struct {
	Name         string
	Label        string
	StandardID   *string
	SystemTypeID *string
	ParentName   *string
	LocationName *string
	// ExpectedName is the create form's precondition (#702): the name the form
	// previewed and locked its name field on. Nil is no precondition. See
	// ComponentSpec.ExpectedName for why it is the name and not the ordinal, and
	// why it is still not the name field.
	ExpectedName *string
}

// SystemPatch is the update input: nil fields unchanged. StandardID follows the
// house three-state convention, nil unchanged and an explicit empty string
// CLEARS it (converting a classified system to a one-off).
//
// There is deliberately no Name here. A rename is RenameSystem, its own act under
// its own permission, because it breaks the references an operator stored outside
// this system. There is deliberately no LocationName or ParentName here either
// (#627 Task 13): a placement change is its own act, MoveSystem, gated by
// system:move rather than system:update; see that function's doc comment.
// SystemTypeID follows the same three-state convention StandardID does, and for
// the same reason: reclassifying a room and un-classifying it are different
// acts, and coalesce alone cannot tell "omitted" from "clear".
type SystemPatch struct {
	Label        *string
	StandardID   *string
	SystemTypeID *string
}

// SystemMove is the :move input: nil fields unchanged, an explicit empty string
// clears (lifting a placed system out of its location, or making a nested
// system a root), a name sets. ParentName is cycle-guarded and scope-injected:
// the new parent must be inside the caller's move scope and must not be the
// system itself or one of its own descendants. Clearing ParentName to root
// requires an all-scoped move grant (see MoveSystem's doc comment for why).
type SystemMove struct {
	LocationName *string
	ParentName   *string
}

// --- standard registry -------------------------------------------------------

// parent_standard_id stores a uuid; the parent's handle is projected beside it,
// as the fleet arcs do.
const standardCols = `id, name, official, coalesce(label, ''), parent_standard_id,
	(select p.name from standard p where p.id = standard.parent_standard_id) as parent_handle, label_rule, map`

func scanStandard(row pgx.Row) (*Standard, error) {
	var st Standard
	var mapRaw []byte
	if err := row.Scan(&st.ID, &st.Name, &st.Official, &st.Label, &st.ParentStandardID, &st.ParentStandardName, &st.LabelRule, &mapRaw); err != nil {
		return nil, err
	}
	if len(mapRaw) > 0 && string(mapRaw) != "null" {
		st.Map = json.RawMessage(mapRaw)
	}
	return &st, nil
}

// mapStandardWriteErr translates Postgres constraint violations on a standard
// write into the registry sentinels: a duplicate id is ErrTypeExists, and the
// only foreign key a standard carries is its parent, so an FK violation is
// ErrParentStandardNotFound rather than an opaque 500.
func mapStandardWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrTypeExists
		case "23503": // foreign_key_violation
			return ErrParentStandardNotFound
		}
	}
	return fmt.Errorf("storage: standard write: %w", err)
}

// UpsertStandard installs or updates a standard by id, the boot-seed phase's
// write. Idempotent: re-seeding the same id updates it in place.
// SeedStandard inserts a shipped example standard only when it is absent. A
// standard is operator-owned content: it is forked from an in-code template and
// then belongs to the fleet, so re-seeding must never reassert over an edit the
// operator made. This is deliberately not UpsertStandard, whose ON CONFLICT DO
// UPDATE is the authoritative behavior the canonical catalogs want.
func (p *PG) SeedStandard(ctx context.Context, st Standard) error {
	_, err := p.pool.Exec(ctx, `
		insert into standard (name, official, label, parent_standard_id, label_rule)
		values ($1, $2, $3, (select id from standard where name = nullif($4, '')), $5)
		on conflict (name) do nothing`,
		st.Name, st.Official, labelOrNull(st.Label), st.ParentStandardID, nilIfEmpty(st.LabelRule))
	if err != nil {
		return fmt.Errorf("storage: seed standard %q: %w", st.Name, err)
	}
	return nil
}

func (p *PG) UpsertStandard(ctx context.Context, st Standard) error {
	_, err := p.pool.Exec(ctx, `
		insert into standard (name, official, label, parent_standard_id, label_rule)
		values ($1, $2, $3, (select id from standard where name = nullif($4, '')), $5)
		on conflict (name) do update
			set official           = excluded.official,
			    label       = excluded.label,
			    parent_standard_id = excluded.parent_standard_id,
			    label_rule         = excluded.label_rule,
			    updated_at         = now()`,
		st.Name, st.Official, labelOrNull(st.Label), st.ParentStandardID, nilIfEmpty(st.LabelRule))
	if err != nil {
		return fmt.Errorf("storage: upsert standard %q: %w", st.ID, err)
	}
	return nil
}

// ListStandards returns every standard, ordered alphabetically by label,
// with unlabelled rows last and the name breaking ties (#613; see ListVendors for
// why `nulls last` is written out).
func (p *PG) ListStandards(ctx context.Context) ([]Standard, error) {
	rows, err := p.pool.Query(ctx, `select `+standardCols+` from standard order by label nulls last, name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list standards: %w", err)
	}
	defer rows.Close()
	out := []Standard{}
	for rows.Next() {
		st, err := scanStandard(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan standard: %w", err)
		}
		out = append(out, *st)
	}
	return out, rows.Err()
}

// GetStandard resolves one standard by id. An unknown id is ErrTypeNotFound.
func (p *PG) GetStandard(ctx context.Context, id string) (*Standard, error) {
	st, err := scanStandard(p.pool.QueryRow(ctx, `select `+standardCols+` from standard where `+registryRefCol(id)+` = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get standard %q: %w", id, err)
	}
	return st, nil
}

// StandardPatch carries the mutable fields of a standard update; a nil field is
// left unchanged.
type StandardPatch struct {
	Label            *string
	ParentStandardID *string
	LabelRule        *string
	// Map: nil leaves the declaration untouched; a JSON null clears it; any
	// other value replaces it (validated, ErrStandardMapInvalid).
	Map *json.RawMessage
}

// CreateStandard inserts a custom (official=false) standard and audits it. A
// duplicate id is ErrTypeExists; an unknown parent is ErrParentStandardNotFound.
func (p *PG) CreateStandard(ctx context.Context, actorID string, st Standard) (*Standard, error) {
	if err := ValidateName("standard", st.Name); err != nil {
		return nil, err
	}
	if err := validateLabelRule(st.LabelRule); err != nil {
		return nil, err
	}
	st.Official = false
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create standard: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if st.ParentStandardID != nil && *st.ParentStandardID != "" {
		var known bool
		if err := tx.QueryRow(ctx, `select true from standard where `+registryRefCol(*st.ParentStandardID)+` = $1`, *st.ParentStandardID).Scan(&known); err != nil {
			return nil, ErrParentStandardNotFound
		}
	}
	created, err := scanStandard(tx.QueryRow(ctx, `
		insert into standard (name, official, label, parent_standard_id, label_rule)
		values ($1, false, $2, (select id from standard where name = $3 or id::text = $3), $4)
		returning `+standardCols,
		st.Name, labelOrNull(st.Label), st.ParentStandardID, nilIfEmpty(st.LabelRule)))
	if err != nil {
		return nil, mapStandardWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "standard", created.ID, nil, created); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create standard: %w", err)
	}
	return created, nil
}

// UpdateStandard patches a custom standard's label or parent (nil fields
// unchanged) and audits it. Official rows are read-only (ErrTypeOfficial); an
// unknown id is ErrTypeNotFound; an unknown parent is ErrParentStandardNotFound.
func (p *PG) UpdateStandard(ctx context.Context, actorID, id string, patch StandardPatch) (*Standard, error) {
	if err := validateLabelRule(patch.LabelRule); err != nil {
		return nil, err
	}
	if patch.Map != nil {
		if err := validateStandardMap(*patch.Map); err != nil {
			return nil, err
		}
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update standard: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "standard", id); err != nil {
		return nil, err
	}
	before, err := registryAuditImage(ctx, tx, "standard", id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTypeNotFound
		}
		return nil, fmt.Errorf("storage: audit image standard %q: %w", id, err)
	}
	setLabel, labelVal := labelPatch(patch.Label)
	st, err := scanStandard(tx.QueryRow(ctx, `
		update standard set
			label       = case when $5::boolean then $2 else label end,
			parent_standard_id = coalesce((select id from standard where name = $3 or id::text = $3), parent_standard_id),
			-- An explicit empty string CLEARS the rule; see UpdateComponentType's
			-- own label_rule CASE for why this is not a coalesce.
			label_rule         = case
				when $4::text is null then label_rule
				when $4 = '' then null
				else $4::text
			end,
			-- The map patch mirrors label_rule's three-way CASE: absent leaves
			-- it, JSON null clears it, a value replaces it.
			map                = case
				when $6::boolean then nullif($7::jsonb, 'null'::jsonb)
				else map
			end,
			updated_at         = now()
		where `+registryRefCol(id)+` = $1
		returning `+standardCols,
		id, labelVal, patch.ParentStandardID, patch.LabelRule, setLabel, patch.Map != nil, mapPatchValue(patch.Map)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTypeNotFound
		}
		return nil, mapStandardWriteErr(err)
	}
	after, err := registryAuditImage(ctx, tx, "standard", id)
	if err != nil {
		return nil, fmt.Errorf("storage: audit image standard %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "standard", st.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update standard: %w", err)
	}
	return st, nil
}

// DeleteStandard removes a custom standard, refusing an official row and a row
// still referenced by a system. Child standards are not a refusal: the parent FK
// is ON DELETE SET NULL, so a variant survives its base as a standalone.
func (p *PG) DeleteStandard(ctx context.Context, actorID, id string) error {
	return deleteTypeRow(ctx, p, "standard", "standard", actorID, id, typeRef{table: "system", col: "standard_id"})
}

// --- system CRUD -------------------------------------------------------------

// The system_type handle is a live subselect, not a stored copy: the row is
// pinned by uuid, so renaming the registry row moves the projected name with it
// and can never orphan the classification.
const systemCols = `id, name, coalesce(label, ''), name_generated, ordinal, label_generated, standard_id,
	(select st.name from standard st where st.id = system.standard_id) as standard_handle,
	system_type_id,
	(select ty.name from system_type ty where ty.id = system.system_type_id) as system_type_handle,
	parent_id, location_id,
	(select count(*) from system_member m where m.system_id = system.id) as member_count,
	(select p.name from system p where p.id = system.parent_id) as parent_name,
	(select l.name from location l where l.id = system.location_id) as location_name,
	created_at, updated_at`

func scanSystem(row pgx.Row) (*System, error) {
	var s System
	if err := row.Scan(&s.ID, &s.Name, &s.Label, &s.NameGenerated, &s.Ordinal, &s.LabelGenerated, &s.StandardID, &s.StandardName,
		&s.SystemTypeID, &s.SystemTypeName, &s.ParentID, &s.LocationID,
		&s.MemberCount, &s.ParentName, &s.LocationName, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}

// attachSystemPaths fills every s's Path/.PathSegments/.Renders (#627 Task
// 15), in one batch walk however long the page (#643). A system's address
// has the identical shape a component's does (its own plane root's location,
// never a location derived from anything else), but it deliberately passes
// RenderBare nil and "" even now that BOTH halves of the substitution exist:
// system_type carries an abbrev (ADR-0096) and #686 gives a generated system a
// stored ordinal.
//
// The reason is D3's suppression (ADR-0101). RenderBare stamps
// "<abbrev><ordinal>" whenever it is given both, with no shape check, which is
// what makes #654's guarantee structural for a component. A suppressed system
// name carries no digits while owning ordinal 1, so wiring this would render
// "brd1" for a room whose name is "boardroom", putting a number on a physical
// label that appears nowhere in the entity's name: the exact defect the ordinal
// column's own migration cites as the thing NOT to do. Making it correct means
// teaching the render which mint produced the name, which is a rendering
// decision this slice does not need and should not guess at. full is unused
// here (nothing to gate: the walk is the whole cost); it exists so this
// matches scopedConfig.attachPaths' shared signature, which
// attachComponentPaths' own full does need (review finding 3,
// task-15-review.md #2).
func attachSystemPaths(ctx context.Context, q querier, ss []*System, full bool) error {
	_ = full
	if len(ss) == 0 {
		return nil
	}
	ids := make([]string, 0, len(ss))
	for _, s := range ss {
		ids = append(ids, s.ID)
	}
	paths, err := PathsOf(ctx, q, systemTable, ids)
	if err != nil {
		return err
	}
	for _, s := range ss {
		segs := paths[s.ID]
		s.PathSegments = segs
		s.Path = strings.Join(segs, ".")
		s.Renders = Renders{Dash: RenderDash(segs), Bare: RenderBare(segs, nil, "")}
	}
	return nil
}

// systemConfig drives the generic scoped-CRUD helpers for the system tree.
//
// beforeDelete releases the memberships. The database would take them anyway
// (ON DELETE CASCADE), which is exactly the problem: a membership disappearing
// moves a component's default and stales the label that reads its type, and
// neither consequence can be attached to a cascade the gateway never sees.
// Releasing them explicitly, one step ahead of the row's own delete, makes this
// the same act RemoveMember performs and subject to the same rules.
//
// afterDelete records the room's recovery. A location's verdict is the rollup of
// the systems in it, so deleting the system that was dragging it down improves it,
// and that improvement is an edge an operator has to be able to read back. The
// system's own rows go with it (cascade), but the location's history is the
// location's, and it has to say when it got better.
var systemConfig = scopedConfig[System]{
	table: systemTable, cols: systemCols, resource: "system",
	scan: scanSystem, idOf: func(s *System) string { return s.ID },
	notFound: ErrSystemNotFound, forbidden: ErrSystemForbidden, occupied: ErrSystemOccupied,
	attachPaths: attachSystemPaths,
	beforeDelete: func(ctx context.Context, p *PG, tx pgx.Tx, before *System) error {
		return p.releaseSystemMembers(ctx, tx, before.ID)
	},
	afterDelete: func(ctx context.Context, p *PG, q txQuerier, before *System) error {
		if before.LocationID == nil {
			return nil // placed nowhere: its removal rolls up to nothing
		}
		// The id is already in hand (before.LocationID); recordHealth binds
		// it directly (see ownerRef), so no lookup is needed at all, not even
		// for the name: this used to fetch one solely to populate a field
		// nothing downstream reads.
		return p.recomputeChain(ctx, q, nil, nil, []ownerRef{{ID: *before.LocationID}})
	},
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
//
// spec.Name empty is not an error (#686): it is the operator handing the pen to
// the platform, which mints the system_type chain's stem plus the lowest free
// ordinal in this placement, suppressing the ordinal on the first of its stem
// in the bucket (ADR-0101). An unclassified system cannot be named that way
// (ErrSystemTypeRequiredForName): the stem lives on the registry row.
//
// locationRead is the PLACEMENT's scope, the same one the draft render takes
// for the same reference (#700): create says who may write a system, this says
// which locations it may be placed at, because the label this stamps reads the
// location's own. See resolvePlacementRef.
func (p *PG) CreateSystem(ctx context.Context, actorID string, spec SystemSpec, create, locationRead scope.Set) (*System, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create system: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// An operator-typed name is validated now, before any other resolve, so a
	// bad slug fails fast (the existing behavior). A generated name is
	// validated too, but later, inside generateName itself, the one place every
	// generation call site funnels through: it is not knowable until the type
	// and the placement resolve below, and an invariant a comment asserts
	// rather than a callee enforces is exactly the shape that let "-1" through
	// review the first time.
	if spec.Name != "" {
		if err := ValidateName("system", spec.Name); err != nil {
			return nil, err
		}
	}

	var parentID *string
	if spec.ParentName == nil {
		if !create.All {
			return nil, ErrSystemForbidden
		}
	} else {
		// resolveScopedRef, not systemByName-then-inScopeTree: ruling 2
		// (#627) requires ambiguity judged inside create, not fleet-wide.
		// A parent that exists only outside create scope stays
		// ErrSystemForbidden (preserved, not collapsed into not-found).
		parent, err := resolveScopedRef(ctx, tx, systemConfig, *spec.ParentName, "system", create)
		if errors.Is(err, ErrSystemNotFound) {
			return nil, ErrParentSystemNotFound
		} else if err != nil {
			return nil, err
		}
		parentID = &parent.ID
	}

	// Resolve the optional located-at location by name to its id, within the
	// caller's location:read scope (#700). The bind is CROSS-tier, so create
	// (resolved for "system") could never match a location's own ancestor chain,
	// but the label stamped below reads that location's LABEL, so the set that
	// decides is the caller's read scope on the location tier. Out of scope is
	// the same non-disclosing ErrLocationNotFound an absent one gives, which is
	// what :renderLabel already answers for the identical reference.
	var locationID *string
	if spec.LocationName != nil {
		loc, err := resolvePlacementRef(ctx, tx, locationConfig, *spec.LocationName, locationRead)
		if err != nil {
			return nil, err // ErrLocationNotFound -> mapped to 422 by the API
		}
		locationID = &loc.ID
	}

	// standard is a catalog, not a scoped tree: resolve by id (a standard's id is
	// its name) with a plain lookup. An unknown id is ErrUnknownStandard -> 422;
	// the FK below is the belt-and-suspenders.
	var standardID *string
	if spec.StandardID != nil {
		var sid string
		err := tx.QueryRow(ctx, `select id from standard where `+registryRefCol(*spec.StandardID)+` = $1`, *spec.StandardID).Scan(&sid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnknownStandard
		} else if err != nil {
			return nil, fmt.Errorf("storage: resolve standard %q: %w", *spec.StandardID, err)
		}
		standardID = &sid
	}

	// system_type is a registry on the same footing: resolved by name or uuid,
	// an unknown ref refused here as ErrUnknownSystemType (-> 422) rather than
	// left to surface as an opaque foreign-key error at insert time.
	var systemTypeID *string
	if spec.SystemTypeID != nil {
		var tid string
		err := tx.QueryRow(ctx, `select id from system_type where `+registryRefCol(*spec.SystemTypeID)+` = $1`, *spec.SystemTypeID).Scan(&tid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUnknownSystemType
		} else if err != nil {
			return nil, fmt.Errorf("storage: resolve system_type %q: %w", *spec.SystemTypeID, err)
		}
		systemTypeID = &tid
	}

	// name empty means "generate one" (#686): resolved now, after the type and
	// the placement are both known, since the stem comes from the system_type
	// chain and the ordinal from siblings in this exact bucket.
	name := spec.Name
	generated := name == ""
	// nil unless the platform picked the name: an operator-typed name has no
	// ordinal the platform owns, and the column says so by being absent.
	var ordinal *int
	if generated {
		genName, n, err := generateNameForSystemType(ctx, tx, systemTypeID, parentID, locationID, nil)
		if err != nil {
			return nil, err
		}
		name, ordinal = genName, &n
	}
	// The form's precondition (#702), checked under the lock that allocated the
	// number and before the insert. See CreateComponent's own call.
	if err := confirmDraftedName(spec.ExpectedName, ordinal, name); err != nil {
		return nil, err
	}

	// An empty label hands the LABEL's pen to the platform (#682); the
	// row is inserted with no label and stamped once its classification is
	// resolvable, which is now.
	s, err := scanSystem(tx.QueryRow(ctx, `
		insert into system (name, label, standard_id, system_type_id, parent_id, location_id, name_generated, ordinal, label_generated)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		returning `+systemCols,
		name, labelOrNull(spec.Label), standardID, systemTypeID, parentID, locationID, generated, ordinal, strings.TrimSpace(spec.Label) == ""))
	if err != nil {
		return nil, mapSystemWriteErr(err)
	}
	if s, err = p.stampSystemLabel(ctx, tx, s); err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "system", s.ID, nil, s); err != nil {
		return nil, err
	}
	// A system inherits its standard's roles the instant it exists, and nobody is
	// assigned yet, so it is usually born impaired. Recording the opening verdict is
	// what gives its history a defined beginning: without it the first edge would be
	// whatever a later write happened to notice.
	//
	// s.ID, not s.Name: the row this transaction just inserted is passed by
	// its own id, never re-resolved by name. Under scoped name uniqueness
	// (#627) the name this system was just given may already be shared by
	// another system elsewhere, and recomputeSystems' own name-to-id
	// resolution would then refuse the ambiguous reference (ErrAmbiguousName)
	// for a create that has nothing ambiguous about it: the row it means is
	// the one it is holding.
	if err := p.recomputeSystems(ctx, tx, s.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create system: %w", err)
	}
	return s, nil
}

// UpdateSystem patches a system by name with the three-way scope split and
// systemIsDescendant reports whether candidateID is targetID or a descendant of
// it: the cycle guard for a reparent, which must not move a system under itself or
// one of its own children. Mirrors componentIsDescendant on the system tree.
func (p *PG) systemIsDescendant(ctx context.Context, q querier, targetID, candidateID string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
		with recursive sub(id) as (
			select id from system where id = $1
			union all
			select s.id from system s join sub on s.parent_id = sub.id
		) cycle id set is_cycle using path
		select exists(select 1 from sub where id = $2)`,
		targetID, candidateID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("storage: system descendant check: %w", err)
	}
	return ok, nil
}

// UpdateSystem patches a system by name with the three-way scope split and
// in-transaction audit, recomputing health when the standard moved (it swaps
// the whole inherited role set, so the verdict can flip in either direction).
//
// Placement (a relocate or a reparent) is NOT here (#627 Task 13): it is its
// own act, MoveSystem, gated by system:move. See that function's doc comment,
// including for why a relocate keeps recomputing health there.
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
	var standardPatchID *string
	if patch.StandardID != nil && *patch.StandardID != "" {
		var sid string
		if err := tx.QueryRow(ctx, `select id from standard where `+registryRefCol(*patch.StandardID)+` = $1`, *patch.StandardID).Scan(&sid); err != nil {
			return nil, ErrUnknownStandard
		}
		standardPatchID = &sid
	}
	var systemTypePatchID *string
	if patch.SystemTypeID != nil && *patch.SystemTypeID != "" {
		var tid string
		if err := tx.QueryRow(ctx, `select id from system_type where `+registryRefCol(*patch.SystemTypeID)+` = $1`, *patch.SystemTypeID).Scan(&tid); err != nil {
			return nil, ErrUnknownSystemType
		}
		systemTypePatchID = &tid
	}
	// A reclassify that leaves the system still platform-named (#686) may no
	// longer fit its old stem: a "boardroom" reclassified as a classroom needs
	// the classroom stem, not the one it was minted from. before.NameGenerated
	// is the read of record; an operator-typed name (RenameSystem clears the
	// flag forever) is never touched by a reclassify. Computed before the write
	// so the name, the ordinal and the classification all land in one UPDATE
	// and one audit image.
	//
	// Un-classifying (an explicit empty string) reaches the generator with no
	// type and is refused there, by the generator rather than by a branch here:
	// a platform-owned name has no source once the type is gone, and silently
	// keeping the name while quietly handing back the pen would be a change of
	// ownership nobody asked for. :rename claims the name first, and then the
	// unclassify goes through.
	//
	// The guard is the STEM the mint reads actually moving, not the field being
	// present in the patch and not the classification id changing either, and
	// both halves of that are load-bearing rather than tidy. The console sends
	// system_type_id on every save (web/src/pages/Systems.tsx keys it always, so
	// an unclassify can clear), so a presence test re-mints on an edit to the
	// label alone; and an id test re-mints on a reclassify between two
	// types that inherit ONE stem from a shared ancestor, which the mint cannot
	// tell apart because it never reads the id (#706). With a lower ordinal
	// freed in the meantime by a rename, either re-mint MOVES THE NAME:
	// "boardroom-2" becomes "boardroom" under system:update, with no rename
	// asked for and possibly no system:rename grant held. The mint reads the
	// stem and the bucket, and neither of those patches changes either, which is
	// the derivation ADR-0101 records. systemTypeStemMoved is the system-tier
	// twin of the component tier's productStemMoved, so both tiers now spell one
	// rule over their own path to a stem.
	var (
		namePatch    *string
		ordinalPatch *int
	)
	if patch.SystemTypeID != nil && before.NameGenerated {
		moved, err := systemTypeStemMoved(ctx, tx, before.SystemTypeID, systemTypePatchID)
		if err != nil {
			return nil, err
		}
		if moved {
			newName, newOrdinal, err := generateNameForSystemType(ctx, tx, systemTypePatchID, before.ParentID, before.LocationID, &before.ID)
			if err != nil {
				return nil, err
			}
			namePatch, ordinalPatch = &newName, &newOrdinal
		}
	}
	setLabel, labelVal := labelPatch(patch.Label)
	after, err := scanSystem(tx.QueryRow(ctx, `
		update system set
			name    = coalesce($8, name),
			ordinal = coalesce($9::integer, ordinal),
			-- Two parameters rather than one overloaded: $10 says whether the
			-- caller named the field, $2 is the value, and an empty or
			-- whitespace-only value is already SQL NULL by the time it gets here
			-- (labelOrNull, ADR-0118). Typing a label takes the pen ($7),
			-- clearing it hands the pen back (#682).
			label = case when $10::boolean then $2 else label end,
			label_generated = $7,
			-- standard_id follows the house patch convention: a nil field is left
			-- unchanged, and a provided empty string CLEARS the column, which is how a
			-- classified system is converted back to a one-off. coalesce alone cannot
			-- express the difference between "omitted" and "clear".
			standard_id  = case
				when $3::text is null then standard_id
				when $3 = '' then null
				else $4::uuid
			end,
			-- system_type_id, same three states: omitted leaves the classification
			-- alone, "" un-classifies the system (legal while the column is
			-- nullable), a ref reclassifies it.
			system_type_id = case
				when $5::text is null then system_type_id
				when $5 = '' then null
				else $6::uuid
			end,
			updated_at   = now()
		where id = $1
		returning `+systemCols,
		before.ID, labelVal, patch.StandardID, standardPatchID, patch.SystemTypeID, systemTypePatchID,
		labelPen(before.LabelGenerated, patch.Label), namePatch, ordinalPatch, setLabel))
	if err != nil {
		return nil, mapSystemWriteErr(err)
	}
	// A reclassify changes the standard and system_type facts a rule reads, and
	// clearing the label hands the pen back; both are settled by now, so the
	// stamp runs before the audit image is taken (#682).
	if after, err = p.stampSystemLabel(ctx, tx, after); err != nil {
		return nil, err
	}
	// A system's TYPE is a fact its member components read (.SystemTypeLabel,
	// #685), so a reclassify stales every one of them. Bounded by the system's
	// own membership rather than by the fleet, which is why it cascades here
	// instead of waiting for the recompute verb. The label lock is taken before
	// any health lock below, the fixed order that keeps the two from
	// deadlocking (see label_recompute.go).
	if !sameOptional(before.SystemTypeID, after.SystemTypeID) {
		if err := p.cascadeSystemMemberLabels(ctx, tx, after.ID); err != nil {
			return nil, err
		}
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "system", after.ID, before, after); err != nil {
		return nil, err
	}
	// Detected against the before-image the update already loaded rather than
	// against the patch (a patch that sets a field to what it already held
	// changes nothing).
	if !sameOptional(before.StandardID, after.StandardID) {
		// after.ID, not after.Name: see CreateSystem's comment on the same
		// shape. A rename in the same patch (not possible today, but the
		// principle holds) would make this doubly wrong if it read the name.
		if err := p.recomputeMovedSystem(ctx, tx, after.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update system: %w", err)
	}
	return after, nil
}

// RenameSystem moves a system's name, scoped exactly as UpdateSystem is (the same
// read-then-action split, so an unreadable target is a non-disclosing not-found and
// a readable-but-unactionable one is forbidden).
//
// Separate from the patch because a rename is a separate act: it breaks the
// references an operator stored outside this system. Nothing inside breaks (every
// arc holds the uuid), which is also why the audit row keys on that uuid rather
// than on the name it just moved.
//
// Health is not recomputed: its records address their owner by id, so the history
// follows the row without being touched, and no verdict depends on a name.
//
// name_generated is always cleared to false here, whether or not it was already
// (#686, the rule component's rename has followed since #627): an operator who
// types a specific name is claiming the pen, and a rename is the one act whose
// entire point is an operator-chosen name. A later move or reclassify then
// leaves this name alone; :resetName is how an operator hands the pen back.
//
// ordinal goes to NULL in the same statement. The platform allocated a number
// for a name that no longer exists, and there is no number it owns for the one
// the operator chose, whatever digits that name happens to contain.
func (p *PG) RenameSystem(ctx context.Context, actorID, name, newName string, read, action scope.Set) (*System, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin rename system: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := p.resolveSystemForAction(ctx, tx, name, read, action)
	if err != nil {
		return nil, err
	}
	if err := ValidateName("system", newName); err != nil {
		return nil, err
	}
	after, err := scanSystem(tx.QueryRow(ctx,
		`update system set name = $2, name_generated = false, ordinal = null, updated_at = now() where id = $1 returning `+systemCols,
		before.ID, newName))
	if err != nil {
		return nil, mapSystemWriteErr(err)
	}
	// .Name is a fact a rule can read, so a platform-owned label follows the
	// rename (#682), and so is the ordinal this write just cleared (#686).
	if after, err = p.stampSystemLabel(ctx, tx, after); err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "rename", "system", after.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit rename system: %w", err)
	}
	return after, nil
}

// ResetSystemName hands the pen back to the platform (#686), the system-tier
// twin of ResetComponentName: it regenerates the name from the system's CURRENT
// type and placement, the exact rule a nameless create applies, and marks the
// result name_generated again, recording the ordinal it was minted from. It
// does not matter whether the name was already platform-owned or an operator
// had typed one: either way the answer is a fresh mint. Scoped exactly as
// RenameSystem (the same read-then-action split), because it is the same act
// from the permission's point of view: it changes the name, gated by
// system:rename, no new token.
//
// An unclassified system is refused (ErrSystemTypeRequiredForName -> 422)
// rather than left half-reset: there is no stem to mint from, and a reset that
// silently kept the operator's name while claiming the pen would be a lie about
// who owns the field.
//
// The audit verb is "reset", distinct from "rename": the row records what
// actually happened (the platform picked the name, not the caller), the same
// reasoning that gave move its own verb apart from update.
func (p *PG) ResetSystemName(ctx context.Context, actorID, name string, read, action scope.Set) (*System, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin reset system name: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := p.resolveSystemForAction(ctx, tx, name, read, action)
	if err != nil {
		return nil, err
	}
	newName, newOrdinal, err := generateNameForSystemType(ctx, tx, before.SystemTypeID, before.ParentID, before.LocationID, &before.ID)
	if err != nil {
		return nil, err
	}
	after, err := scanSystem(tx.QueryRow(ctx,
		`update system set name = $2, name_generated = true, ordinal = $3, updated_at = now() where id = $1 returning `+systemCols,
		before.ID, newName, newOrdinal))
	if err != nil {
		return nil, mapSystemWriteErr(err)
	}
	// Same pair as a rename, in the other direction: a fresh name and a freshly
	// allocated ordinal, both facts a rule reads, so a platform-owned label
	// follows (#682).
	if after, err = p.stampSystemLabel(ctx, tx, after); err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "reset", "system", after.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit reset system name: %w", err)
	}
	return after, nil
}

// MoveSystem relocates and/or reparents a system by name, the same three-way
// scope split as UpdateSystem, in its own transaction with its own DISTINCT
// audit verb ("move", not "update"). Its own act, not a PATCH field (#627 Task
// 13), for the reason the ADR states: a PATCH that cleared parent_id lifted a
// row out of every subtree scope with no check, while creating the same root
// requires an all-scoped grant. Clearing ParentName to root here now requires
// action.All (mirroring CreateSystem's own root check), the guard the old
// UpdateSystem never had.
//
// The cycle guard and the resolveScopedRef ambiguity-inside-scope ordering
// (ruling 2, #627) both carry over unchanged from the old UpdateSystem
// reparent branch: dropping them here, rather than moving them, would delete
// the only entry point for a system reparent and make the guard unreachable.
//
// Health: a reparent (moving within the system tree) does not move health, as
// before (the rollup runs system -> location, not through the system tree). A
// relocate (a location change) DOES still recompute, both ends, exactly as
// UpdateSystem's old combined patch did: TestHealthMovesOnRelocation is the
// proof this is load-bearing, not incidental, so "a placement move never
// recomputes health" (true for every component move, and for a system
// reparent) does NOT generalize to a system's own location field, which the
// health rollup depends on directly.
func (p *PG) MoveSystem(ctx context.Context, actorID, name string, move SystemMove, read, action, locationRead scope.Set) (*System, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin move system: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := p.resolveSystemForAction(ctx, tx, name, read, action)
	if err != nil {
		return nil, err
	}
	// The location arrives as a name and the column holds an id, so the three-state
	// value is resolved before the statement: nil stays nil (unchanged), "" stays ""
	// (clear), and a named location becomes its id.
	locationPatch := move.LocationName
	if move.LocationName != nil && *move.LocationName != "" {
		// resolvePlacementRef, exactly as the create binds it (#700): the
		// destination is cross-tier, so action (resolved for "system") can never
		// match it, and the stamp below re-renders this system's label from the
		// DESTINATION's, so a relocate hands back a location's label the same
		// way a create into it does. The caller's location:read scope is what
		// decides whether it may.
		loc, err := resolvePlacementRef(ctx, tx, locationConfig, *move.LocationName, locationRead)
		if err != nil {
			return nil, err // ErrLocationNotFound -> mapped to 422 by the API
		}
		locationPatch = &loc.ID
	}
	// The parent is a reparent within the system tree: resolve within the
	// caller's action scope (resolveScopedRef, ruling 2), which both
	// requires the new parent inside that scope and judges ambiguity inside
	// it too, and cycle-guard against moving the system under itself or a
	// descendant. nil leaves the parent untouched.
	parentPatch := move.ParentName
	if move.ParentName != nil {
		if *move.ParentName == "" {
			// Clearing to root: the same authorization CreateSystem already
			// enforces for a root system. The old UpdateSystem patch let ANY
			// action-scoped (not necessarily all-scoped) caller clear parent_id
			// to NULL, lifting the system out of every subtree scope with no
			// check at all; this closes that gap.
			if !action.All {
				return nil, ErrSystemForbidden
			}
		} else {
			parent, err := resolveScopedRef(ctx, tx, systemConfig, *move.ParentName, "system", action)
			if errors.Is(err, ErrSystemNotFound) {
				return nil, ErrParentSystemNotFound
			} else if err != nil {
				return nil, err
			}
			descendant, err := p.systemIsDescendant(ctx, tx, before.ID, parent.ID)
			if err != nil {
				return nil, err
			}
			if descendant {
				return nil, ErrSystemCycle
			}
			parentPatch = &parent.ID
		}
	}
	// A still-platform-owned name (#686) is scoped to the OLD placement's
	// siblings; a move changes that bucket, so the ordinal (and with it whether
	// the ordinal appears at all, since the first of a stem in a bucket is
	// bare) may no longer be what a fresh generate would pick. The type does
	// not change on a move, so in the common case only the ordinal shifts, but
	// the stem is re-resolved from before.SystemTypeID's chain anyway: one
	// rule, no special case. An operator-typed name is left exactly where the
	// operator put it.
	//
	// effLocationID/effParentID decode the same three-state patches the UPDATE's
	// own CASE expressions apply (nil unchanged, "" clear, else set), computed
	// here in Go because the generator needs the DESTINATION bucket to scan
	// siblings in, not the placement the row reads right now.
	//
	// The guard is the BUCKET actually moving, not merely the row being
	// platform-named, and it is the reclassify defect ADR-0101 records wearing
	// the other verb's clothes: a :move that re-states the placement the system
	// already has (the console's shape, and this verb's own documented no-op
	// when neither field is supplied) changes no input to the mint, and
	// re-minting anyway MOVES the name whenever a lower ordinal was freed in the
	// meantime, with no rename requested and possibly no system:rename grant
	// held. The comparison is on the bucket rather than on the two pointers
	// because a parent WINS over a location: relocating a parented system leaves
	// it in the same parent bucket, so its name must not move either.
	var (
		namePatch    *string
		ordinalPatch *int
	)
	if before.NameGenerated {
		effLocationID := before.LocationID
		if locationPatch != nil {
			if *locationPatch == "" {
				effLocationID = nil
			} else {
				v := *locationPatch
				effLocationID = &v
			}
		}
		effParentID := before.ParentID
		if parentPatch != nil {
			if *parentPatch == "" {
				effParentID = nil
			} else {
				v := *parentPatch
				effParentID = &v
			}
		}
		from := systemNameScope(before.ParentID, before.LocationID)
		to := systemNameScope(effParentID, effLocationID)
		if from.bucket != to.bucket {
			newName, newOrdinal, err := generateNameForSystemType(ctx, tx, before.SystemTypeID, effParentID, effLocationID, &before.ID)
			if err != nil {
				return nil, err
			}
			namePatch, ordinalPatch = &newName, &newOrdinal
		}
	}
	after, err := scanSystem(tx.QueryRow(ctx, `
		update system set
			name        = coalesce($4, name),
			-- The number the new name was minted from, recorded with it: a move
			-- changes the bucket, so the old bucket's ordinal is no more true of
			-- the row than the old name was.
			ordinal     = coalesce($5::integer, ordinal),
			location_id = case
				when $2::text is null then location_id
				when $2 = '' then null
				else $2::uuid
			end,
			parent_id   = case
				when $3::text is null then parent_id
				when $3 = '' then null
				else $3::uuid
			end,
			updated_at  = now()
		where id = $1
		returning `+systemCols,
		before.ID, locationPatch, parentPatch, namePatch, ordinalPatch))
	if err != nil {
		return nil, mapSystemWriteErr(err)
	}
	// A relocate changes .LocationLabel, which a rule reads (#685). This was
	// the one write path on these three tiers that stamped no label at all,
	// correctly so while a system's map carried nothing a move could touch; the
	// placement key is what made it a write path.
	if after, err = p.stampSystemLabel(ctx, tx, after); err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "move", "system", after.ID, before, after); err != nil {
		return nil, err
	}
	// The location moves the system's contribution from one rollup to another,
	// so BOTH are recomputed: the one it left may have just improved. See the
	// doc comment above for why this survives the "a move never recomputes
	// health" invariant that holds everywhere else.
	if movedLocation := !sameOptional(before.LocationID, after.LocationID); movedLocation {
		var left []string
		if before.LocationID != nil {
			// The id is already in hand; no lookup needed at all (see
			// systemConfig.afterDelete for the same shape).
			left = append(left, *before.LocationID)
		}
		if err := p.recomputeMovedSystem(ctx, tx, after.ID, left...); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit move system: %w", err)
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

// SystemNameTaken reports whether name is already used within the placement a
// create would actually land in (#627: the unique constraint is scoped to
// placement, not global). parentRef wins over locationRef, mirroring
// CreateSystem's own placement resolution: a parent makes it a child
// (system_parent_name_key), no parent but a location makes it a room-level
// system (system_location_name_key), and neither is the unplaced/root bucket
// (system_orphan_name_key). Gated at the API by system:update.
func (p *PG) SystemNameTaken(ctx context.Context, name string, parentRef, locationRef *string) (bool, error) {
	var exists bool
	switch {
	case parentRef != nil && *parentRef != "":
		parent, err := scopedByName(ctx, p.pool, systemConfig, *parentRef)
		if err != nil {
			// withoutCandidates: this advisory has no caller scope to filter
			// an ambiguous parentRef by (intentionally scope-blind, see
			// ComponentNameTaken's comment), so a refusal here must never
			// name a row the caller might not be able to read.
			return false, withoutCandidates(err)
		}
		if err := p.pool.QueryRow(ctx, `select exists(select 1 from system where parent_id = $1 and name = $2)`, parent.ID, name).Scan(&exists); err != nil {
			return false, fmt.Errorf("storage: system name taken: %w", err)
		}
	case locationRef != nil && *locationRef != "":
		loc, err := scopedByName(ctx, p.pool, locationConfig, *locationRef)
		if err != nil {
			return false, withoutCandidates(err) // see the parentRef case above
		}
		if err := p.pool.QueryRow(ctx, `select exists(select 1 from system where parent_id is null and location_id = $1 and name = $2)`, loc.ID, name).Scan(&exists); err != nil {
			return false, fmt.Errorf("storage: system name taken: %w", err)
		}
	default:
		if err := p.pool.QueryRow(ctx, `select exists(select 1 from system where parent_id is null and location_id is null and name = $1)`, name).Scan(&exists); err != nil {
			return false, fmt.Errorf("storage: system name taken: %w", err)
		}
	}
	return exists, nil
}

func mapSystemWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			switch pgErr.ConstraintName {
			case idxSystemParentName:
				return ErrSystemExistsUnderParent
			case idxSystemLocationName:
				return ErrSystemExistsInLocation
			case idxSystemOrphanName:
				return ErrSystemExistsUnplaced
			}
			return ErrSystemExists
		case "23503":
			switch pgErr.ConstraintName {
			// The standard FK keeps its original constraint name through the
			// system_type -> standard_id column rename (Postgres renames the column,
			// not the constraint), so both names are the same reference; a future
			// schema squash would emit the standard_id one.
			case "system_system_type_fkey", "system_standard_id_fkey":
				return ErrUnknownStandard
			// NOT the same constraint as the two above, despite how it reads:
			// those name the STANDARD foreign key (system_type was the old
			// column name for standard_id, ADR-0048), while this one names the
			// system_type registry that came back as its own table (ADR-0096).
			// The _id_ makes them distinct strings, which is why the reuse is
			// safe at the schema level; it is only the prose that collides.
			case "system_system_type_id_fkey":
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

// mapPatchValue renders the map patch for the SQL CASE: nil becomes SQL null
// (the CASE ignores it anyway), a value passes through as its raw JSON.
func mapPatchValue(m *json.RawMessage) any {
	if m == nil {
		return nil
	}
	return string(*m)
}
