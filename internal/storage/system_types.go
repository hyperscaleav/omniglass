package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrParentSystemTypeNotFound is a system_type write naming a parent id that
// does not exist: the system_type counterpart of ErrParentTypeNotFound.
var ErrParentSystemTypeNotFound = errors.New("storage: parent system_type not found")

// ErrRootSystemTypeNeedsStem is the refusal for a root type (ParentID nil) with
// no stem, on both the paths that can produce one: CreateSystemType, and (since
// #716) an update clearing the stem back to inherited. The same structural guard
// ErrRootComponentTypeNeedsStem is for components. A root has no ancestor to
// inherit from, so resolveSystemTypeFacts's walk stops immediately with
// nothing and every stemless descendant resolves to "". Enforced at write
// time rather than left for the name rules that will read the column (#657)
// to discover.
var ErrRootSystemTypeNeedsStem = errors.New("storage: a root system_type (no parent) must have a stem; there is no ancestor to inherit one from")

// maxSystemTypeDepth caps the parent walk resolveSystemTypeFacts runs, the
// same defensive backstop maxComponentTypeDepth is for the component tree: the
// tree cannot form a cycle through normal writes (a row can only reference a
// parent that already exists), so this guards corrupted data, not a real depth
// any seeded or operator-built tree approaches.
const maxSystemTypeDepth = 32

// SystemType is a registry row in the coarse taxonomy of what kind of space a
// system is (a boardroom, a classroom, a video wall): a stable id, the official
// flag, a display_name, and an optional parent for a tree of arbitrary depth.
// It is the system-side counterpart of ComponentType, and it does NOT replace
// standard: a standard is the blueprint a system conforms to, this is what kind
// of space the system is.
//
// Stem, Icon, and Abbrev are nullable: null means "inherit from parent",
// resolved by ResolveSystemTypeFacts walking ParentID in Go (no DB logic) until
// it finds the first non-null value. The registry lists alphabetically by
// display_name; there is no ordering field.
//
// No DefaultTags. ComponentType carries one because a product's instances start
// from its type's tag set; a system's effective tags come from the platform,
// its location, and its own system tree (the cascade), never from a classifier,
// so the column would be a field with no reader.
type SystemType struct {
	ID          uuid.UUID
	Name        string
	DisplayName string
	Stem        *string
	Icon        *string
	Abbrev      *string
	// LabelRule is this node's label template (#682), nullable on the same
	// inherit-from-parent rule Stem/Icon/Abbrev follow: null defers to the
	// nearest ancestor that sets one, and then to the global tier.
	LabelRule *string
	Official  bool
	ParentID  *uuid.UUID
}

// SystemTypePatch carries the mutable fields of a system_type update; a nil
// field is left unchanged. No ParentID: as with component_type, a custom type's
// placement in the tree is fixed at create, because a correct reparent needs a
// cycle guard that has no consumer yet.
type SystemTypePatch struct {
	DisplayName *string
	Stem        *string
	Icon        *string
	Abbrev      *string
	LabelRule   *string
}

const systemTypeCols = `id, name, display_name, stem, icon, abbrev, label_rule, official, parent_id`

func scanSystemType(row pgx.Row) (*SystemType, error) {
	var st SystemType
	if err := row.Scan(&st.ID, &st.Name, &st.DisplayName, &st.Stem, &st.Icon, &st.Abbrev, &st.LabelRule, &st.Official, &st.ParentID); err != nil {
		return nil, err
	}
	return &st, nil
}

// mapSystemTypeWriteErr translates Postgres constraint violations on a
// system_type write into the registry sentinels: a duplicate name is
// ErrTypeExists, and the only foreign key the row carries is its parent, so an
// FK violation is ErrParentSystemTypeNotFound.
func mapSystemTypeWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrTypeExists
		case "23503": // foreign_key_violation
			return ErrParentSystemTypeNotFound
		}
	}
	return fmt.Errorf("storage: system_type write: %w", err)
}

// UpsertSystemType installs or updates a system_type by name, the boot-seed
// phase's write. Authoritative (ON CONFLICT DO UPDATE) like component_type's,
// not seed-if-absent like location_type's example content: the coarse taxonomy
// is shipped reference data an estate reads, not content it owns. Idempotent:
// re-seeding the same name updates it in place, id stable.
func (p *PG) UpsertSystemType(ctx context.Context, st SystemType) error {
	_, err := p.pool.Exec(ctx, `
		insert into system_type (name, display_name, stem, icon, abbrev, label_rule, official, parent_id)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
		on conflict (name) do update
			set display_name = excluded.display_name,
			    stem         = excluded.stem,
			    icon         = excluded.icon,
			    abbrev       = excluded.abbrev,
			    label_rule   = excluded.label_rule,
			    official     = excluded.official,
			    parent_id    = excluded.parent_id,
			    updated_at   = now()`,
		st.Name, st.DisplayName, st.Stem, st.Icon, st.Abbrev, nilIfEmpty(st.LabelRule), st.Official, st.ParentID)
	if err != nil {
		return fmt.Errorf("storage: upsert system_type %q: %w", st.Name, mapSystemTypeWriteErr(err))
	}
	return nil
}

// ListSystemTypes returns every system_type, ordered alphabetically by
// display_name, with unlabelled rows last and the name breaking ties (#613; see
// ListVendors for why the ordering is spelled nullif(...) nulls last).
func (p *PG) ListSystemTypes(ctx context.Context) ([]SystemType, error) {
	rows, err := p.pool.Query(ctx, `select `+systemTypeCols+` from system_type order by nullif(display_name, '') nulls last, name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list system_types: %w", err)
	}
	defer rows.Close()
	var out []SystemType
	for rows.Next() {
		st, err := scanSystemType(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan system_type: %w", err)
		}
		out = append(out, *st)
	}
	return out, rows.Err()
}

// GetSystemType resolves one system_type by name or id. An unknown ref is
// ErrTypeNotFound.
func (p *PG) GetSystemType(ctx context.Context, ref string) (*SystemType, error) {
	st, err := scanSystemType(p.pool.QueryRow(ctx, `select `+systemTypeCols+` from system_type where `+registryRefCol(ref)+` = $1`, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get system_type %q: %w", ref, err)
	}
	return st, nil
}

// CreateSystemType inserts a custom (official=false) system_type and audits it.
// A duplicate name is ErrTypeExists; a ParentID naming no row is
// ErrParentSystemTypeNotFound.
//
// Stem is validated by the same rule as Name (validateEntityName, not the
// table-dispatched ValidateName: a stem is not itself a system_type's name, it
// is a name-shaped fragment), for the reason CreateComponentType's own doc
// comment sets out: #657's name rules mint it straight into a generated system
// name with no re-check downstream, so a stem with a dot or a space would
// produce a name the address parser and every rendering surface disagree
// about. A ROOT type additionally requires a non-nil stem
// (ErrRootSystemTypeNeedsStem): malformed and absent are two failure modes of
// the same invariant, that a resolved stem is a valid, non-empty name fragment.
func (p *PG) CreateSystemType(ctx context.Context, actorID string, st SystemType) (*SystemType, error) {
	if err := ValidateName("system_type", st.Name); err != nil {
		return nil, err
	}
	if st.Stem != nil {
		if err := validateEntityName(*st.Stem); err != nil {
			return nil, err
		}
	} else if st.ParentID == nil {
		return nil, ErrRootSystemTypeNeedsStem
	}
	if err := validateLabelRule(st.LabelRule); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create system_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	created, err := scanSystemType(tx.QueryRow(ctx, `
		insert into system_type (name, display_name, stem, icon, abbrev, label_rule, official, parent_id)
		values ($1, $2, $3, $4, $5, $6, false, $7)
		returning `+systemTypeCols,
		st.Name, st.DisplayName, st.Stem, st.Icon, st.Abbrev, nilIfEmpty(st.LabelRule), st.ParentID))
	if err != nil {
		return nil, mapSystemTypeWriteErr(err)
	}
	resID := created.ID.String()
	if err := writeAuditRes(ctx, tx, actorID, "create", "system_type", resID, nil, created); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create system_type: %w", err)
	}
	return created, nil
}

// UpdateSystemType patches a custom system_type's display_name, stem, icon, or
// abbrev (nil fields unchanged) and audits it. Official rows are read-only
// (ErrTypeOfficial); an unknown ref is ErrTypeNotFound. Stem is validated
// exactly as CreateSystemType validates it: a bad stem is reachable from an
// existing, previously valid row, not just a fresh create.
//
// Stem, icon and abbrev honour the three-state string sentinel (#716), exactly
// as UpdateComponentType's do: an omitted field is unchanged, an explicit ""
// clears the column back to NULL so the inheritance walk resumes at the nearest
// ancestor, and a value sets. Clearing a ROOT's stem is
// ErrRootSystemTypeNeedsStem.
func (p *PG) UpdateSystemType(ctx context.Context, actorID, ref string, patch SystemTypePatch) (*SystemType, error) {
	// The empty string is the CLEAR, not a stem, so it skips the name rule
	// rather than being refused by it (#716); every other value is validated
	// exactly as before.
	if patch.Stem != nil && *patch.Stem != "" {
		if err := validateEntityName(*patch.Stem); err != nil {
			return nil, err
		}
	}
	if err := validateLabelRule(patch.LabelRule); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update system_type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "system_type", ref); err != nil {
		return nil, err
	}
	// A root has nothing to inherit a stem from, so the clear is refused there
	// exactly as CreateSystemType refuses a stemless root. The parent is read
	// only when the patch actually clears, so an ordinary edit pays nothing.
	if patch.Stem != nil && *patch.Stem == "" {
		var parentID *uuid.UUID
		if err := tx.QueryRow(ctx, `select parent_id from system_type where `+registryRefCol(ref)+` = $1`, ref).Scan(&parentID); err != nil {
			return nil, fmt.Errorf("storage: load system_type %q parent: %w", ref, err)
		}
		if clearsARootStem(patch.Stem, parentID) {
			return nil, ErrRootSystemTypeNeedsStem
		}
	}
	before, err := registryAuditImage(ctx, tx, "system_type", ref)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTypeNotFound
		}
		return nil, fmt.Errorf("storage: audit image system_type %q: %w", ref, err)
	}
	st, err := scanSystemType(tx.QueryRow(ctx, `
		update system_type set
			display_name = coalesce($2, display_name),
			-- The four columns whose NULL MEANS "this node declares no fact of
			-- its own, walk to the nearest ancestor that does", so each has a
			-- CLEAR state coalesce cannot express: an omitted field is
			-- unchanged, an explicit '' clears to NULL, a value sets. See
			-- UpdateComponentType, which spells the identical rule on the
			-- identical four columns (#716).
			stem         = case when $3::text is null then stem       else nullif($3::text, '') end,
			icon         = case when $4::text is null then icon       else nullif($4::text, '') end,
			abbrev       = case when $5::text is null then abbrev     else nullif($5::text, '') end,
			label_rule   = case when $6::text is null then label_rule else nullif($6::text, '') end,
			updated_at   = now()
		where `+registryRefCol(ref)+` = $1
		returning `+systemTypeCols,
		ref, patch.DisplayName, patch.Stem, patch.Icon, patch.Abbrev, patch.LabelRule))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTypeNotFound
		}
		return nil, fmt.Errorf("storage: update system_type %q: %w", ref, err)
	}
	after, err := registryAuditImage(ctx, tx, "system_type", ref)
	if err != nil {
		return nil, fmt.Errorf("storage: audit image system_type %q: %w", ref, err)
	}
	resID := st.ID.String()
	if err := writeAuditRes(ctx, tx, actorID, "update", "system_type", resID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update system_type: %w", err)
	}
	return st, nil
}

// DeleteSystemType removes a custom system_type, refusing an official row
// (ErrTypeOfficial), a row still parenting another system_type, and a row still
// classifying a system (both ErrTypeInUse). Both foreign keys also carry ON
// DELETE RESTRICT, so these pre-counts turn a raw FK error into the clean
// sentinel the API maps to a 409.
func (p *PG) DeleteSystemType(ctx context.Context, actorID, ref string) error {
	return deleteTypeRow(ctx, p, "system_type", "system_type", actorID, ref,
		typeRef{table: "system_type", col: "parent_id"},
		typeRef{table: "system", col: "system_type_id"})
}

// systemTypeWalkRow is the minimal per-row projection resolveSystemTypeFacts
// needs for one step of the parent walk.
type systemTypeWalkRow struct {
	parentID           *uuid.UUID
	stem, icon, abbrev *string
	labelRule          *string
}

func loadSystemTypeWalk(ctx context.Context, q querier, id uuid.UUID) (*systemTypeWalkRow, error) {
	var row systemTypeWalkRow
	err := q.QueryRow(ctx, `select parent_id, stem, icon, abbrev, label_rule from system_type where id = $1`, id).
		Scan(&row.parentID, &row.stem, &row.icon, &row.abbrev, &row.labelRule)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: load system_type %q: %w", id, err)
	}
	return &row, nil
}

// ResolveSystemTypeFacts walks parent_id from id toward the root, resolving
// stem, icon, and abbrev to the first non-null value found (the type itself,
// then its ancestors). No DB logic: the walk is one row-fetch at a time here in
// Go, not a recursive query.
//
// It runs on p.pool, so it is only correct for a caller with nothing else open.
// A caller resolving facts inside its own transaction calls
// resolveSystemTypeFacts directly with its own querier instead, exactly as
// ResolveTypeFacts and resolveTypeFacts split for components.
// The label rule the walk also resolves (#682) is deliberately NOT returned
// here: it is a storage-side input to the label generator, never a wire field,
// and the Gateway's only callers of this are the console-facing facts.
func (p *PG) ResolveSystemTypeFacts(ctx context.Context, id uuid.UUID) (stem, icon, abbrev string, err error) {
	stem, icon, abbrev, _, err = resolveSystemTypeFacts(ctx, p.pool, id)
	return stem, icon, abbrev, err
}

// resolveSystemTypeFacts is ResolveSystemTypeFacts's walk, taking a querier so
// it can run inside a caller's transaction.
//
// labelRule (#682) inherits by the identical first-non-null rule the other
// three follow, and that is the answer to "a type's rule is set and its
// parent's is not, which applies": the same one ADR-0095 gives for facts. A
// rule is a fact of the node that declares it, so a child that declares none
// inherits its ancestor's, and a child that declares one overrides for itself
// and its own descendants only.
func resolveSystemTypeFacts(ctx context.Context, q querier, id uuid.UUID) (stem, icon, abbrev, labelRule string, err error) {
	cur := id
	var gotStem, gotIcon, gotAbbrev, gotRule bool
	for range maxSystemTypeDepth {
		row, rerr := loadSystemTypeWalk(ctx, q, cur)
		if rerr != nil {
			return "", "", "", "", rerr
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
		if !gotRule && row.labelRule != nil {
			labelRule, gotRule = *row.labelRule, true
		}
		if row.parentID == nil || (gotStem && gotIcon && gotAbbrev && gotRule) {
			break
		}
		cur = *row.parentID
	}
	return stem, icon, abbrev, labelRule, nil
}
