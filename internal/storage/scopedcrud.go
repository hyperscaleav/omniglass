package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
)

// ErrAmbiguousName is what a bare-name reference resolves to once more than one
// row shares that name (#627 relaxes name uniqueness from global to scoped): the
// reference is refused rather than one row being picked and the other hidden.
// Kind is the entity table ("component", "system", "location"); Ref is the
// reference as the caller sent it; Candidates is every matching row's id, in
// the order loadByRef's ORDER BY resolved them. Through scopedByNameInScope
// (the path a plain GET takes) Candidates never names a row the caller could
// not otherwise read: the id list is built AFTER the scope filter, not before.
type ErrAmbiguousName struct {
	Kind       string
	Ref        string
	Candidates []string
}

func (e *ErrAmbiguousName) Error() string {
	return fmt.Sprintf("storage: %q is ambiguous for %s (matches %s)", e.Ref, e.Kind, strings.Join(e.Candidates, ", "))
}

// The partial-unique index names the #627 migration creates, one per placement
// bucket per tree entity. Postgres reports the violated index's name as
// pgconn.PgError.ConstraintName on a 23505, so each entity's write mapper
// switches on these to report which specific bucket collided, rather than
// folding every placement into one generic duplicate sentinel. Declared once
// here rather than as string literals in three files.
const (
	idxLocationParentName = "location_parent_name_key"
	idxLocationRootName   = "location_root_name_key"

	idxComponentParentName   = "component_parent_name_key"
	idxComponentLocationName = "component_location_name_key"
	idxComponentOrphanName   = "component_orphan_name_key"

	idxSystemParentName   = "system_parent_name_key"
	idxSystemLocationName = "system_location_name_key"
	idxSystemOrphanName   = "system_orphan_name_key"
)

// The generic scoped-CRUD helpers: the read, resolve, and delete paths that are
// identical for every scoped tree entity (location, system, component), given
// the entity's table, columns, scan, and sentinels. Each entity keeps only its
// own create/update (which differ by the foreign keys they resolve). Go methods
// cannot take type parameters, so these are package functions over *PG.
//
// scopedConfig is the per-entity knob set.
type scopedConfig[T any] struct {
	table     scopeTable                // the tree table
	cols      string                    // the select column list, in scan order
	resource  string                    // the audit resource label
	scan      func(pgx.Row) (*T, error) // row -> entity
	idOf      func(*T) string           // entity -> id
	notFound  error                     // 404 sentinel (absent or out of read scope)
	forbidden error                     // 403 sentinel (readable, out of action scope)
	occupied  error                     // 409 sentinel (delete refused: has children)
	// afterDelete runs inside the delete's transaction, after the row is gone and
	// before the commit, receiving the entity as it was. It exists for the ripples
	// a delete causes elsewhere: removing a degraded system improves the health of
	// the location it sat in, and that improvement is an edge worth recording. In
	// the transaction so the ripple cannot commit apart from the delete that caused
	// it. Optional; nil for entities whose removal ripples nowhere.
	afterDelete func(ctx context.Context, p *PG, q txQuerier, before *T) error
}

// sameOptional reports whether two optional columns hold the same value, absence
// included. It is how an update path tells a field that MOVED from one that was
// merely written again with the value it already had, which is what keeps a patch
// from firing a recompute (and a transition) over nothing.
func sameOptional(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// scopedList returns the entities in the caller's read scope, ordered by name,
// via the scoped-tree subtree filter.
func scopedList[T any](ctx context.Context, p *PG, cfg scopedConfig[T], read scope.Set) ([]T, error) {
	if read.Empty() {
		return nil, nil
	}
	sql := scopedListSQL(cfg.table, cfg.cols, read.All)
	var (
		rows pgx.Rows
		err  error
	)
	if read.All {
		rows, err = p.pool.Query(ctx, sql)
	} else {
		roots := uuidRoots(read.IDs)
		selfIDs := uuidRoots(read.SelfIDs)
		if len(roots) == 0 && len(selfIDs) == 0 {
			return nil, nil // every scope root is malformed: nothing is in scope
		}
		rows, err = p.pool.Query(ctx, sql, roots, selfIDs)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: list %s: %w", cfg.table, err)
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := cfg.scan(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan %s: %w", cfg.table, err)
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// loadByRef runs the reference lookup shared by every scopedByName variant: a
// well-formed uuid matches on id (a primary key, never ambiguous), anything else
// matches on name (possibly more than one row, #627 scopes name uniqueness to
// placement rather than holding it global). Ordered by id so a caller that later
// caps the result with a LIMIT gets a stable "first" row.
func loadByRef[T any](ctx context.Context, q querier, cfg scopedConfig[T], ref string) ([]*T, error) {
	col := "name"
	if isUUID(ref) {
		col = "id"
	}
	rows, err := q.Query(ctx, `select `+cfg.cols+` from `+string(cfg.table)+` where `+col+` = $1 order by id`, ref)
	if err != nil {
		return nil, fmt.Errorf("storage: load %s %q: %w", cfg.table, ref, err)
	}
	defer rows.Close()

	var matches []*T
	for rows.Next() {
		v, err := cfg.scan(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan %s %q: %w", cfg.table, ref, err)
		}
		matches = append(matches, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: load %s %q: %w", cfg.table, ref, err)
	}
	return matches, nil
}

// resolveMatches turns a candidate row set into the single-row-or-refuse
// contract every scopedByName variant shares: zero is the entity's notFound
// sentinel, exactly one is the resolved row, and two or more is
// ErrAmbiguousName, refusing the reference rather than silently taking
// whichever row sorted first and hiding the rest.
func resolveMatches[T any](cfg scopedConfig[T], ref string, matches []*T) (*T, error) {
	switch len(matches) {
	case 0:
		return nil, cfg.notFound
	case 1:
		return matches[0], nil
	default:
		candidates := make([]string, len(matches))
		for i, v := range matches {
			candidates[i] = cfg.idOf(v)
		}
		return nil, &ErrAmbiguousName{Kind: string(cfg.table), Ref: ref, Candidates: candidates}
	}
}

// scopedByName resolves an entity by REFERENCE, which is either its uuid or its
// name, with NO scope check: callers that need one layer it on afterward (a
// create's placement resolve, for example, checks the resolved row against a
// create/action scope of its own right after). The uuid is the canonical handle
// and the name is the friendly alias, and a caller uses whichever it holds: a
// script that just created something has the id, a human at a CLI has the name.
//
// The uuid is tried first, and that ordering is only unambiguous because a name
// can never be uuid-shaped (ValidateName refuses the form). Without that
// rule the same reference would resolve differently depending on which entity
// happened to exist, making the answer a property of the data rather than of the
// request.
//
// A well-formed uuid that matches nothing is an ordinary not-found rather than a
// fallback to a name lookup that would also miss: falling through would turn one
// clear miss into two and report the second.
//
// Ambiguity here is estate-wide: every row sharing the name, in or out of any
// scope. That is the right answer for the callers above (a create's placement
// reference is not itself a caller-scoped read), and the wrong one for a plain
// GET, which is why scopedGet and resolveScoped do NOT call this directly; see
// scopedByNameInScope.
func scopedByName[T any](ctx context.Context, q querier, cfg scopedConfig[T], ref string) (*T, error) {
	matches, err := loadByRef(ctx, q, cfg, ref)
	if err != nil {
		return nil, err
	}
	return resolveMatches(cfg, ref, matches)
}

// scopedByNameInScope resolves a reference the same way scopedByName does, but
// narrows the candidate rows to the caller's read scope BEFORE deciding whether
// the reference is ambiguous, rather than after (architect ruling, #627: "scope
// decides before ambiguity does"). Two consequences follow. First, a name that
// is ambiguous estate-wide but unique within the caller's own scope resolves
// cleanly: an operator scoped to room-b, where exactly one "display-1" exists,
// is not refused just because room-a holds an unrelated same-named row they
// cannot even read. Second, an ErrAmbiguousName's Candidates list can never
// name an id the caller is not allowed to read: disclosing that a row exists
// out of scope, even only as a uuid in a 409, is the same leak the
// non-disclosing 404 exists to prevent. A uuid reference still narrows to at
// most one row (a primary key), so this only ever changes the bare-name path.
func scopedByNameInScope[T any](ctx context.Context, q querier, cfg scopedConfig[T], ref string, read scope.Set) (*T, error) {
	matches, err := loadByRef(ctx, q, cfg, ref)
	if err != nil {
		return nil, err
	}
	inScope := make([]*T, 0, len(matches))
	for _, v := range matches {
		ok, err := inScopeTree(ctx, q, cfg.table, cfg.idOf(v), read)
		if err != nil {
			return nil, err
		}
		if ok {
			inScope = append(inScope, v)
		}
	}
	return resolveMatches(cfg, ref, inScope)
}

// scopedGet resolves an entity by name within the caller's read scope; absent,
// out of scope, or ambiguous only outside the caller's scope is the same
// non-disclosing notFound (scopedByNameInScope narrows to read scope first).
func scopedGet[T any](ctx context.Context, p *PG, cfg scopedConfig[T], name string, read scope.Set) (*T, error) {
	return scopedByNameInScope(ctx, p.pool, cfg, name, read)
}

// resolveScoped loads an entity by name and enforces the read-then-action scope
// split: out of read scope (or ambiguous only outside it) is notFound
// (non-disclosing), readable but out of action scope is forbidden.
func resolveScoped[T any](ctx context.Context, q querier, cfg scopedConfig[T], name string, read, action scope.Set) (*T, error) {
	v, err := scopedByNameInScope(ctx, q, cfg, name, read)
	if err != nil {
		return nil, err
	}
	actionable, err := inScopeTree(ctx, q, cfg.table, cfg.idOf(v), action)
	if err != nil {
		return nil, err
	}
	if !actionable {
		return nil, cfg.forbidden
	}
	return v, nil
}

// scopedDelete removes an entity by name with the read/action split, refuses
// while it has child rows (occupancy), and writes the audit row in the same
// transaction.
func scopedDelete[T any](ctx context.Context, p *PG, cfg scopedConfig[T], actorID, name string, read, action scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete %s: %w", cfg.table, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := resolveScoped(ctx, tx, cfg, name, read, action)
	if err != nil {
		return err
	}
	// The caller addressed the entity by uuid or by name; everything below keys on
	// the row's own primary key, the audit row included.
	beforeID := cfg.idOf(before)
	var childCount int
	if err := tx.QueryRow(ctx, `select count(*) from `+string(cfg.table)+` where parent_id = $1`, beforeID).Scan(&childCount); err != nil {
		return fmt.Errorf("storage: count %s children: %w", cfg.table, err)
	}
	if childCount > 0 {
		return cfg.occupied
	}
	if _, err := tx.Exec(ctx, `delete from `+string(cfg.table)+` where id = $1`, beforeID); err != nil {
		// A row that something else still references is refused, like a row with
		// children, and must reach the caller as a conflict rather than an opaque
		// server error. It is a distinct sentinel from occupied: the child count
		// above proved there are no children, so a restrict FK from anywhere else
		// (a component staffing a system role, say) landed here, and this path
		// cannot tell which one. Reporting "has children" would be false.
		if isReferencedViolation(err) {
			return ErrReferenced
		}
		return fmt.Errorf("storage: delete %s: %w", cfg.table, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", cfg.resource, beforeID, before, nil); err != nil {
		return err
	}
	if cfg.afterDelete != nil {
		if err := cfg.afterDelete(ctx, p, tx, before); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete %s: %w", cfg.table, err)
	}
	return nil
}
