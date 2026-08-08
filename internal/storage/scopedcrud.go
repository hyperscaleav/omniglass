package storage

import (
	"context"
	"errors"
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

// refPolicy is resolveRef's policy axis: what to report when a bare-name
// reference matches at least one row, but none of the matches fall inside
// the given scope.
type refPolicy int

const (
	// refPolicyHide reports the SAME non-disclosing notFound whether the
	// reference is absent or merely out of scope: the read-path posture
	// (architect ruling 2, #627), where a caller must never learn that an
	// out-of-scope row exists at all, not even via a different status.
	refPolicyHide refPolicy = iota
	// refPolicyForbid preserves a write path's pre-existing notFound-versus-
	// forbidden split: absent anywhere is cfg.notFound, present only outside
	// scope is cfg.forbidden. This is not a new disclosure the way a read's
	// uuid would be (the caller supplied the reference itself, in the same
	// request), and several routes' tested contracts already distinguish
	// the two (interfaces_scope_test.go:82).
	refPolicyForbid
)

// resolveRef is the single primitive behind every bare-name/uuid resolve
// against a scoped tree entity. It used to be three near-identical
// functions (scopedByName, scopedByNameInScope, resolveScopedRef), which is
// exactly how architect ruling 2 (#627, "scope decides before ambiguity
// does") landed on two of them and missed the rest, and how this task's own
// review-response round then chased nine more call sites one at a time:
// three copies means the next ruling gets applied two or three times.
// scopedByName's old scope-blind, existence-only behavior is not a separate
// code path here, it is what this degenerates to when set.All is true:
// inScopeTree's own short-circuit (scopetree.go) treats every row as in
// scope, so refPolicyHide and refPolicyForbid become indistinguishable and
// every match is a candidate, which is scopedByName's exact contract.
//
// resource names the resource the caller's set was actually RESOLVED for
// (e.g. what a.scopeFor(ctx, "component", "read") was called with at the
// API layer), which is not always cfg.resource: interface and task cascade
// through the component tier, and secret/variable/field/telemetry cascade
// through all three tree tiers (scope.Covers, mirroring Resolve's own
// applicableKinds). Unless set.All, resource must Cover cfg.resource: a
// scope resolved for one tier checked against a DIFFERENT tier's table is
// comparing incompatible id spaces (inScopeTree walks the target's own
// ancestor chain, so the ids can never match), which silently denies every
// non-all caller on a read/write path or leaks a candidate estate-wide on
// an advisory one, rather than being caught. This exact mismatch shipped
// twice in this task's own history (CreateComponent's system/location
// binds, ResolveTags' forSystem filter) before this guard existed; it is
// deliberately a panic, not a returned error, because a tier mismatch is a
// caller bug to fix in code, not a runtime condition to route around.
//
// What this guard is NOT: proof that every call site's resource label is
// correct today. Each one passes a label its own author already reasoned to
// match cfg (a hardcoded literal beside a hardcoded config, or ownerKind
// pulled from inside the switch arm that already selected that same
// config), so resource == cfg.resource (or Covers admits it) by
// construction at every current site; the guard cannot fire on any input
// the current code produces, and a green suite running with it live proves
// nothing beyond that. It is forward insurance for the NEXT call site that
// copies a pattern without updating the label, not a validator of this
// round's own hand-traced re-derivation (that trace's real check was
// behavioral: the full test suite exercising actual read/write outcomes).
// scope.Covers also documents a blind spot this guard inherits: within the
// secret/variable/field/telemetry family it cannot tell a right tier from a
// wrong one, only a right family from a wrong one.
func resolveRef[T any](ctx context.Context, q querier, cfg scopedConfig[T], ref, resource string, set scope.Set, policy refPolicy) (*T, error) {
	if !set.All && !scope.Covers(resource, cfg.resource) {
		panic(fmt.Sprintf("storage: resolveRef: a scope resolved for %q cannot be checked against %q (resolving %q)", resource, cfg.resource, ref))
	}
	matches, err := loadByRef(ctx, q, cfg, ref)
	if err != nil {
		return nil, err
	}
	if policy == refPolicyForbid && len(matches) == 0 {
		return nil, cfg.notFound
	}
	inScope := make([]*T, 0, len(matches))
	for _, v := range matches {
		ok, err := inScopeTree(ctx, q, cfg.table, cfg.idOf(v), set)
		if err != nil {
			return nil, err
		}
		if ok {
			inScope = append(inScope, v)
		}
	}
	if policy == refPolicyForbid && len(inScope) == 0 {
		return nil, cfg.forbidden
	}
	return resolveMatches(cfg, ref, inScope)
}

// scopedByName resolves an entity by REFERENCE, which is either its uuid or
// its name, with NO scope check: callers that need one layer it on
// afterward (a create's placement resolve, for example, checks the resolved
// row against a create/action scope of its own right after), or have no
// caller scope that could ever apply (a cross-tier existence-only bind, or
// the three *NameTaken advisories, which are intentionally blind to the
// caller's own grant by design). A thin wrapper over resolveRef with an All
// set: every row is a candidate, matching scopedByName's original contract
// exactly (see resolveRef's doc for why this is a degenerate case, not a
// separate implementation to keep in sync).
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
	return resolveRef(ctx, q, cfg, ref, cfg.resource, scope.Set{All: true}, refPolicyHide)
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
// resource is the resource read was resolved for (see resolveRef).
func scopedByNameInScope[T any](ctx context.Context, q querier, cfg scopedConfig[T], ref, resource string, read scope.Set) (*T, error) {
	return resolveRef(ctx, q, cfg, ref, resource, read, refPolicyHide)
}

// withoutCandidates redacts an *ErrAmbiguousName's Candidates, for a
// resolution path that has no caller scope to filter by at all (the three
// *NameTaken advisories, which intentionally resolve scope-blind so
// availability matches the placement bucket asked about rather than the
// caller's own grant, ADR the checkName routes document; and a cross-tier
// existence-only bind, whose caller scope is the wrong tier to apply here at
// all). The reference is still refused as ambiguous; the response just
// never names uuids the caller might have no scope to read, since nothing
// here can tell which ones do.
func withoutCandidates(err error) error {
	var ambig *ErrAmbiguousName
	if errors.As(err, &ambig) {
		return &ErrAmbiguousName{Kind: ambig.Kind, Ref: ambig.Ref}
	}
	return err
}

// resolveScopedRef resolves a create- or action-time placement or owner
// reference: ambiguity is judged within the given scope, same ordering as
// scopedByNameInScope (ruling 2, #627), but this preserves the
// notFound-versus-forbidden split a write already makes, which
// scopedByNameInScope's single non-disclosing notFound would otherwise
// collapse. A reference matching no row anywhere is cfg.notFound; one
// matching a row only outside the given scope is cfg.forbidden, not
// notFound, because the caller supplied this reference itself in the same
// request (it is not a new disclosure the way a read's uuid would be, and
// several routes' tested contracts already distinguish "no such row" from
// "that row is not yours to use here"). A genuine collision within scope is
// ErrAmbiguousName, whose Candidates never name a row outside the given
// scope, same guarantee as scopedByNameInScope. resource is the resource set
// was resolved for (see resolveRef).
func resolveScopedRef[T any](ctx context.Context, q querier, cfg scopedConfig[T], ref, resource string, set scope.Set) (*T, error) {
	return resolveRef(ctx, q, cfg, ref, resource, set, refPolicyForbid)
}

// scopedGet resolves an entity by name within the caller's read scope; absent,
// out of scope, or ambiguous only outside the caller's scope is the same
// non-disclosing notFound (scopedByNameInScope narrows to read scope first).
// read is always resolved for cfg's own resource: scopedGet is one
// entity-generic helper called once per tree entity with its own matching
// scope, never a cross-tier reference, so no mismatch can occur here.
func scopedGet[T any](ctx context.Context, p *PG, cfg scopedConfig[T], name string, read scope.Set) (*T, error) {
	return scopedByNameInScope(ctx, p.pool, cfg, name, cfg.resource, read)
}

// resolveScoped loads an entity by name and enforces the read-then-action scope
// split: out of read scope (or ambiguous only outside it) is notFound
// (non-disclosing), readable but out of action scope is forbidden. Same
// same-tier guarantee as scopedGet.
func resolveScoped[T any](ctx context.Context, q querier, cfg scopedConfig[T], name string, read, action scope.Set) (*T, error) {
	v, err := scopedByNameInScope(ctx, q, cfg, name, cfg.resource, read)
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
