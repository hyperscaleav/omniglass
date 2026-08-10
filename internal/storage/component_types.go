package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrParentTypeNotFound is a component_type write naming a parent id that does
// not exist: the component_type counterpart of ErrParentStandardNotFound.
var ErrParentTypeNotFound = errors.New("storage: parent component_type not found")

// ErrRootComponentTypeNeedsStem is CreateComponentType's refusal for a root
// type (ParentID nil) with no stem. A root has no ancestor to inherit one
// from, so ResolveTypeFacts's walk stops immediately with nothing: every
// descendant that does not set its own stem would resolve to "", and #627
// Task 14's name generator refuses to mint a name from that (see
// ErrComponentTypeNoStem, internal/storage/namegen.go). Enforced here,
// structurally, at write time, rather than trusting the generator's own
// guard to be the only thing standing between a data-entry mistake and a
// broken address grammar.
var ErrRootComponentTypeNeedsStem = errors.New("storage: a root component_type (no parent) must have a stem; there is no ancestor to inherit one from")

// maxComponentTypeDepth caps the parent walk ResolveTypeFacts and TypeIsWithin
// run. The tree cannot form a cycle through normal writes (a row can only
// reference a parent that already exists, so a parent always predates its
// child), so this is a defensive backstop against corrupted data, not a real
// depth limit any seeded or operator-built tree approaches.
const maxComponentTypeDepth = 32

// ComponentType is a registry row in the hierarchical taxonomy a product is
// classified by (mic, camera, wireless-mic under mic): a stable id, the
// official flag, a display_name, and an optional parent for a tree of
// arbitrary depth. Stem, Icon, and Abbrev are nullable: null means "inherit
// from parent," resolved by ResolveTypeFacts walking ParentID in Go (no DB
// logic) until it finds the first non-null value. DefaultTags follows the
// same first-non-empty-wins rule. The registry lists alphabetically by
// display_name; there is no ordering field.
//
// Official and Forked are the two halves of the origin an operator reads
// (#655, ADR-0095): official alone is "shipped", neither is "yours", and both
// together is "yours, overriding shipped". Forked is derived at read time from
// the presence of a registry_shadow row, never stored on the registry itself.
type ComponentType struct {
	ID          uuid.UUID
	Name        string
	DisplayName string
	Stem        *string
	Icon        *string
	Abbrev      *string
	DefaultTags []string
	Official    bool
	ParentID    *uuid.UUID
	Forked      bool
}

// ComponentTypePatch carries the mutable fields of a component_type update; a
// nil field is left unchanged.
type ComponentTypePatch struct {
	DisplayName *string
	Stem        *string
	Icon        *string
	Abbrev      *string
	DefaultTags *[]string
}

const componentTypeCols = `id, name, display_name, stem, icon, abbrev, default_tags, official, parent_id`

// componentTypeRegistry is this registry's key in registry_shadow, the first
// adopter of the fork primitive (registryshadow.go).
const componentTypeRegistry = "component_type"

// componentTypeResolved selects a component_type together with its operator
// shadow, the read every caller outside the seed goes through. The join is a
// left join on the row's own uuid, so an unforked row reads exactly as before
// and a forked one carries the image scanComponentTypeResolved overlays.
const componentTypeResolved = `
	select ct.id, ct.name, ct.display_name, ct.stem, ct.icon, ct.abbrev, ct.default_tags, ct.official, ct.parent_id, s.image
	from component_type ct
	left join registry_shadow s on s.registry = '` + componentTypeRegistry + `' and s.row_id = ct.id`

func scanComponentType(row pgx.Row) (*ComponentType, error) {
	var ct ComponentType
	if err := row.Scan(&ct.ID, &ct.Name, &ct.DisplayName, &ct.Stem, &ct.Icon, &ct.Abbrev, &ct.DefaultTags, &ct.Official, &ct.ParentID); err != nil {
		return nil, err
	}
	return &ct, nil
}

// scanComponentTypeResolved scans a componentTypeResolved row and applies the
// operator's shadow over the official values, so no caller above the gateway
// ever sees which of the two it got.
func scanComponentTypeResolved(row pgx.Row) (*ComponentType, error) {
	var ct ComponentType
	var image []byte
	if err := row.Scan(&ct.ID, &ct.Name, &ct.DisplayName, &ct.Stem, &ct.Icon, &ct.Abbrev, &ct.DefaultTags, &ct.Official, &ct.ParentID, &image); err != nil {
		return nil, err
	}
	resolved, err := applyComponentTypeImage(ct, image)
	if err != nil {
		return nil, err
	}
	resolved.Forked = len(image) > 0
	return &resolved, nil
}

// componentTypeShadowImage is the mutable-column projection a fork captures:
// the whole set, every time (ADR-0095 decision 2), each nullable column written
// as an explicit null rather than dropped, because null is a value on this
// registry ("inherit from the nearest ancestor that sets one") and a dropped
// key would re-inherit the official row's on the next read.
//
// It deliberately omits id, name, official and parent_id. Those are structure,
// and structure is official space: a shadow restates a row's facts, it never
// moves the row in the tree, renames it, or changes where it came from. That
// omission is what makes the inheritance walk resolvable per node.
func componentTypeShadowImage(ct ComponentType) ([]byte, error) {
	image, err := json.Marshal(map[string]any{
		"display_name": ct.DisplayName,
		"stem":         ct.Stem,
		"icon":         ct.Icon,
		"abbrev":       ct.Abbrev,
		"default_tags": normalizeComponentTypeTags(ct.DefaultTags),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: marshal component_type shadow: %w", err)
	}
	return image, nil
}

// applyComponentTypeImage overlays a shadow image onto the official row. An
// empty image is the unforked case and returns the row untouched; a key the
// image does not carry (a column added to the registry after the fork was
// taken) falls back to the official value; a key it carries as null clears the
// column, which on stem/icon/abbrev means "inherit from the parent" and is a
// real answer, not an absence.
func applyComponentTypeImage(ct ComponentType, image []byte) (ComponentType, error) {
	fields, err := shadowFields(image)
	if err != nil {
		return ct, err
	}
	if fields == nil {
		return ct, nil
	}
	for _, f := range []struct {
		key string
		dst any
	}{
		{"display_name", &ct.DisplayName},
		{"stem", &ct.Stem},
		{"icon", &ct.Icon},
		{"abbrev", &ct.Abbrev},
		{"default_tags", &ct.DefaultTags},
	} {
		if err := shadowField(fields, f.key, f.dst); err != nil {
			return ct, err
		}
	}
	ct.DefaultTags = normalizeComponentTypeTags(ct.DefaultTags)
	return ct, nil
}

// normalizeComponentTypeTags returns a non-nil slice so a nil set writes and
// reads back as an empty text[], not SQL null (default_tags is not null).
func normalizeComponentTypeTags(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// mapComponentTypeWriteErr translates Postgres constraint violations on a
// component_type write into the registry sentinels: a duplicate name is
// ErrTypeExists, and an unknown parent_id is ErrParentTypeNotFound.
func mapComponentTypeWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrTypeExists
		case "23503": // foreign_key_violation
			return ErrParentTypeNotFound
		}
	}
	return fmt.Errorf("storage: component_type write: %w", err)
}

// UpsertComponentType installs or updates a component_type by name, the
// boot-seed phase's write. Idempotent: re-seeding the same name updates it in
// place, id stable.
func (p *PG) UpsertComponentType(ctx context.Context, ct ComponentType) error {
	_, err := p.pool.Exec(ctx, `
		insert into component_type (name, display_name, stem, icon, abbrev, default_tags, official, parent_id)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
		on conflict (name) do update
			set display_name = excluded.display_name,
			    stem         = excluded.stem,
			    icon         = excluded.icon,
			    abbrev       = excluded.abbrev,
			    default_tags = excluded.default_tags,
			    official     = excluded.official,
			    parent_id    = excluded.parent_id,
			    updated_at   = now()`,
		ct.Name, ct.DisplayName, ct.Stem, ct.Icon, ct.Abbrev, normalizeComponentTypeTags(ct.DefaultTags), ct.Official, ct.ParentID)
	if err != nil {
		return fmt.Errorf("storage: upsert component_type %q: %w", ct.Name, mapComponentTypeWriteErr(err))
	}
	return nil
}

// ListComponentTypes returns every component_type, ordered alphabetically by
// display_name then name, each row resolved over its operator shadow. A forked
// row is one row here, not two: the shadow is an overlay on the row it names,
// never a second entry in the registry.
//
// The ORDER BY reads the resolved display_name (the shadow's when there is
// one) so a rename through a fork sorts where an operator expects it, and it
// stays in SQL rather than moving to a Go sort so the collation the registry
// has always ordered by is the collation it still orders by.
func (p *PG) ListComponentTypes(ctx context.Context) ([]ComponentType, error) {
	rows, err := p.pool.Query(ctx, componentTypeResolved+`
		order by coalesce(s.image->>'display_name', ct.display_name), ct.name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list component_types: %w", err)
	}
	defer rows.Close()
	var out []ComponentType
	for rows.Next() {
		ct, err := scanComponentTypeResolved(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan component_type: %w", err)
		}
		out = append(out, *ct)
	}
	return out, rows.Err()
}

// GetComponentType resolves one component_type by name or id, over its
// operator shadow. An unknown ref is ErrTypeNotFound.
//
// Both ref forms address the same logical row whether it is forked or not: the
// shadow shares the official row's uuid and does not touch its name, so an
// operator asks for "ceiling-mic" (or for the uuid a product's classification
// holds) and gets theirs if they have one, the shipped row otherwise. The
// namespace is not part of the address and never reaches a caller.
func (p *PG) GetComponentType(ctx context.Context, ref string) (*ComponentType, error) {
	ct, err := scanComponentTypeResolved(p.pool.QueryRow(ctx, componentTypeResolved+` where ct.`+registryRefCol(ref)+` = $1`, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get component_type %q: %w", ref, err)
	}
	return ct, nil
}

// CreateComponentType inserts a custom (official=false) component_type and
// audits it. A duplicate name is ErrTypeExists; a ParentID naming no row is
// ErrParentTypeNotFound.
//
// Stem is validated by the same rule as Name (validateEntityName, not the
// table-dispatched ValidateName: a stem is not itself a component_type's
// name, it is a name-shaped fragment). Before #627 Task 14 an odd stem was
// inert data, read back and displayed, never consumed. Task 14's generator
// is what turns it into a live input: it is minted straight into
// "<stem>-<n>" component names with no re-check downstream, so a stem with a
// dot would produce a name the Task 12 address parser splits into segments,
// and a space or an uppercase letter would produce a name that violates the
// one grammar every addressing and rendering surface assumes. Guarding it
// here, at the one place it is written, closes that instead of trusting
// every future reader of the column to re-derive the same rule.
//
// A ROOT type (ParentID nil) additionally requires a non-nil Stem
// (ErrRootComponentTypeNeedsStem): a root has no ancestor to inherit one
// from, so a nil stem here is not "inherit," it is "nothing," and the walk
// would resolve every stemless descendant to "". Malformed and absent are
// two distinct failure modes of the same invariant (a resolved stem must be
// a valid, non-empty name fragment); this closes the absent half, the
// validateEntityName call above closes the malformed half.
func (p *PG) CreateComponentType(ctx context.Context, actorID string, ct ComponentType) (*ComponentType, error) {
	if err := ValidateName("component_type", ct.Name); err != nil {
		return nil, err
	}
	if ct.Stem != nil {
		if err := validateEntityName(*ct.Stem); err != nil {
			return nil, err
		}
	} else if ct.ParentID == nil {
		return nil, ErrRootComponentTypeNeedsStem
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create component_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	created, err := scanComponentType(tx.QueryRow(ctx, `
		insert into component_type (name, display_name, stem, icon, abbrev, default_tags, official, parent_id)
		values ($1, $2, $3, $4, $5, $6, false, $7)
		returning `+componentTypeCols,
		ct.Name, ct.DisplayName, ct.Stem, ct.Icon, ct.Abbrev, normalizeComponentTypeTags(ct.DefaultTags), ct.ParentID))
	if err != nil {
		return nil, mapComponentTypeWriteErr(err)
	}
	resID := created.ID.String()
	if err := writeAuditRes(ctx, tx, actorID, "create", "component_type", resID, nil, created); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create component_type: %w", err)
	}
	return created, nil
}

// UpdateComponentType patches a component_type's display_name, stem, icon,
// abbrev, or default_tags (nil fields unchanged) and audits it. An unknown ref
// is ErrTypeNotFound.
//
// An operator's own row is written in place, as it always was. A SHIPPED
// (official) row is never written: the patch forks it instead (#655,
// ADR-0095), storing the operator's whole version of the row as a shadow that
// every read resolves over the official one. The official row is byte-
// identical afterwards, so the next release can improve it, and
// RestoreComponentType discards the shadow to get those improvements back.
//
// Stem is validated exactly as CreateComponentType validates it (see that
// doc comment): a bad stem here is reachable from an existing, previously
// valid row, not just a fresh create.
func (p *PG) UpdateComponentType(ctx context.Context, actorID, ref string, patch ComponentTypePatch) (*ComponentType, error) {
	if patch.Stem != nil {
		if err := validateEntityName(*patch.Stem); err != nil {
			return nil, err
		}
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update component_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := scanComponentTypeResolved(tx.QueryRow(ctx, componentTypeResolved+` where ct.`+registryRefCol(ref)+` = $1`, ref))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTypeNotFound
		}
		return nil, fmt.Errorf("storage: load component_type %q: %w", ref, err)
	}
	if current.Official {
		return p.forkComponentType(ctx, tx, actorID, *current, patch)
	}

	before, err := registryAuditImage(ctx, tx, "component_type", ref)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTypeNotFound
		}
		return nil, fmt.Errorf("storage: audit image component_type %q: %w", ref, err)
	}
	var tags any
	if patch.DefaultTags != nil {
		tags = normalizeComponentTypeTags(*patch.DefaultTags)
	}
	ct, err := scanComponentType(tx.QueryRow(ctx, `
		update component_type set
			display_name = coalesce($2, display_name),
			stem         = coalesce($3, stem),
			icon         = coalesce($4, icon),
			abbrev       = coalesce($5, abbrev),
			default_tags = coalesce($6, default_tags),
			updated_at   = now()
		where `+registryRefCol(ref)+` = $1
		returning `+componentTypeCols,
		ref, patch.DisplayName, patch.Stem, patch.Icon, patch.Abbrev, tags))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTypeNotFound
		}
		return nil, fmt.Errorf("storage: update component_type %q: %w", ref, err)
	}
	after, err := registryAuditImage(ctx, tx, "component_type", ref)
	if err != nil {
		return nil, fmt.Errorf("storage: audit image component_type %q: %w", ref, err)
	}
	resID := ct.ID.String()
	if err := writeAuditRes(ctx, tx, actorID, "update", "component_type", resID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update component_type: %w", err)
	}
	return ct, nil
}

// forkComponentType is UpdateComponentType's shipped-row leg: it applies the
// patch to the row as the operator currently reads it (the shipped values, or
// their own earlier fork of them) and stores the result as a shadow, leaving
// the official row untouched. Commits the caller's transaction.
//
// The audit records the operator's version before and after, keyed on the
// shipped row's uuid, because that uuid is the only address this row has ever
// had and the only one a reviewer reading the trail will recognise. A first
// fork has no "before" shadow, so it records the shipped values it forked
// from: the diff an operator wants is "what changed about this row", not
// "which storage row moved".
func (p *PG) forkComponentType(ctx context.Context, tx pgx.Tx, actorID string, current ComponentType, patch ComponentTypePatch) (*ComponentType, error) {
	before, err := componentTypeShadowImage(current)
	if err != nil {
		return nil, err
	}
	forked := current
	if patch.DisplayName != nil {
		forked.DisplayName = *patch.DisplayName
	}
	if patch.Stem != nil {
		forked.Stem = patch.Stem
	}
	if patch.Icon != nil {
		forked.Icon = patch.Icon
	}
	if patch.Abbrev != nil {
		forked.Abbrev = patch.Abbrev
	}
	if patch.DefaultTags != nil {
		forked.DefaultTags = normalizeComponentTypeTags(*patch.DefaultTags)
	}
	after, err := componentTypeShadowImage(forked)
	if err != nil {
		return nil, err
	}
	if err := putShadowImage(ctx, tx, componentTypeRegistry, current.ID, after); err != nil {
		return nil, err
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", componentTypeRegistry, current.ID.String(),
		json.RawMessage(before), json.RawMessage(after)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit fork component_type: %w", err)
	}
	forked.Forked = true
	return &forked, nil
}

// RestoreComponentType is restore-to-defaults: it deletes the operator's
// shadow so reads return the shipped row again, and the row picks up whatever
// later releases have improved about it. ErrTypeNotForked when the row carries
// no shadow (there is nothing to undo, and saying so beats a silent success);
// ErrTypeNotFound when the ref names nothing.
//
// This is the only delete an operator can aim at a shipped row, and it does
// not touch it: DeleteComponentType still refuses an official row, forked or
// not. Restoring an operator's OWN row is meaningless (there are no defaults
// behind it) and is ErrTypeNotForked too.
func (p *PG) RestoreComponentType(ctx context.Context, actorID, ref string) (*ComponentType, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin restore component_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := scanComponentTypeResolved(tx.QueryRow(ctx, componentTypeResolved+` where ct.`+registryRefCol(ref)+` = $1`, ref))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTypeNotFound
		}
		return nil, fmt.Errorf("storage: load component_type %q: %w", ref, err)
	}
	if !current.Forked {
		return nil, ErrTypeNotForked
	}
	before, err := componentTypeShadowImage(*current)
	if err != nil {
		return nil, err
	}
	dropped, err := dropShadowImage(ctx, tx, componentTypeRegistry, current.ID)
	if err != nil {
		return nil, err
	}
	if !dropped {
		return nil, ErrTypeNotForked
	}
	shipped, err := scanComponentTypeResolved(tx.QueryRow(ctx, componentTypeResolved+` where ct.id = $1`, current.ID))
	if err != nil {
		return nil, fmt.Errorf("storage: reload component_type %q: %w", ref, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "restore", componentTypeRegistry, current.ID.String(),
		json.RawMessage(before), nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit restore component_type: %w", err)
	}
	return shipped, nil
}

// DeleteComponentType removes a custom component_type, refusing an official
// row (ErrTypeOfficial) and a row still classifying a child component_type
// (ErrTypeInUse; parent_id also carries ON DELETE RESTRICT, so this pre-count
// turns the raw FK error into the clean sentinel, same as the other trees).
//
// A shipped row stays undeletable whether or not an operator has forked it:
// the fork is an overlay, not ownership. RestoreComponentType is the delete
// that shipped row admits, and it removes the shadow, not the row.
func (p *PG) DeleteComponentType(ctx context.Context, actorID, ref string) error {
	return deleteTypeRow(ctx, p, "component_type", "component_type", typeRef{table: "component_type", col: "parent_id"}, actorID, ref)
}

// componentTypeWalkRow is the minimal per-row projection ResolveTypeFacts and
// TypeIsWithin need for one step of the parent walk.
type componentTypeWalkRow struct {
	parentID           *uuid.UUID
	stem, icon, abbrev *string
	tags               []string
}

// loadComponentTypeWalk reads one node of the walk with its operator shadow
// applied to the FACTS and never to the STRUCTURE (#655, ADR-0095 decision 3).
// parent_id comes from the official row on purpose: a shadow restates what a
// node says, it does not move the node, so the chain a walk follows is the
// same chain whether nodes along it are forked or not.
func loadComponentTypeWalk(ctx context.Context, q querier, id uuid.UUID) (*componentTypeWalkRow, error) {
	var row componentTypeWalkRow
	var image []byte
	err := q.QueryRow(ctx, `
		select ct.parent_id, ct.stem, ct.icon, ct.abbrev, ct.default_tags, s.image
		from component_type ct
		left join registry_shadow s on s.registry = '`+componentTypeRegistry+`' and s.row_id = ct.id
		where ct.id = $1`, id).
		Scan(&row.parentID, &row.stem, &row.icon, &row.abbrev, &row.tags, &image)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: load component_type %q: %w", id, err)
	}
	resolved, err := applyComponentTypeImage(ComponentType{
		Stem: row.stem, Icon: row.icon, Abbrev: row.abbrev, DefaultTags: row.tags,
	}, image)
	if err != nil {
		return nil, err
	}
	row.stem, row.icon, row.abbrev, row.tags = resolved.Stem, resolved.Icon, resolved.Abbrev, resolved.DefaultTags
	return &row, nil
}

// ResolveTypeFacts walks parent_id from id toward the root, resolving stem,
// icon, and abbrev to the first non-null value found (the type itself, then
// its ancestors), and default_tags to the first non-empty set. No DB logic:
// the walk is one row-fetch at a time here in Go, not a recursive query.
//
// The walk crosses the fork boundary PER NODE (#655, ADR-0095 decision 3):
// each node contributes its effective facts, the operator's when that node is
// forked and the shipped ones otherwise, and the chain itself is always the
// official one. So forking an ancestor reaches every descendant that does not
// override the field, and forking a leaf does not cut it off from the
// ancestors it inherits from. There is no such thing as a forked chain, only
// forked nodes.
//
// It runs on p.pool, so it is only correct for a caller with nothing else
// open. A caller resolving facts inside its own transaction (the name
// generator, #627 Task 14, recomputing a stem after a :move or a product
// reclassify) calls resolveTypeFacts directly with its own querier instead:
// this method's pool read would otherwise see the pre-transaction committed
// state, not the write still in flight.
func (p *PG) ResolveTypeFacts(ctx context.Context, id uuid.UUID) (stem, icon, abbrev string, tags []string, err error) {
	return resolveTypeFacts(ctx, p.pool, id)
}

// resolveTypeFacts is ResolveTypeFacts's walk, taking a querier so it can run
// inside a caller's transaction (loadComponentTypeWalk already does).
func resolveTypeFacts(ctx context.Context, q querier, id uuid.UUID) (stem, icon, abbrev string, tags []string, err error) {
	cur := id
	var gotStem, gotIcon, gotAbbrev, gotTags bool
	for range maxComponentTypeDepth {
		row, rerr := loadComponentTypeWalk(ctx, q, cur)
		if rerr != nil {
			return "", "", "", nil, rerr
		}
		if !gotStem && row.stem != nil {
			stem, gotStem = *row.stem, true
		}
		if !gotIcon && row.icon != nil {
			icon, gotIcon = *row.icon, true
		}
		if !gotAbbrev && row.abbrev != nil {
			abbrev, gotAbbrev = *row.abbrev, true
		}
		if !gotTags && len(row.tags) > 0 {
			tags, gotTags = row.tags, true
		}
		if row.parentID == nil || (gotStem && gotIcon && gotAbbrev && gotTags) {
			break
		}
		cur = *row.parentID
	}
	return stem, icon, abbrev, normalizeComponentTypeTags(tags), nil
}

// TypeIsWithin reports whether id is ancestor itself or a descendant of it,
// walking parent_id toward the root: self-inclusive, matching the estate
// tree's other descendant checks (e.g. locationIsDescendant).
func (p *PG) TypeIsWithin(ctx context.Context, id, ancestor uuid.UUID) (bool, error) {
	cur := id
	for range maxComponentTypeDepth {
		if cur == ancestor {
			return true, nil
		}
		row, err := loadComponentTypeWalk(ctx, p.pool, cur)
		if err != nil {
			return false, err
		}
		if row.parentID == nil {
			return false, nil
		}
		cur = *row.parentID
	}
	return false, nil
}
