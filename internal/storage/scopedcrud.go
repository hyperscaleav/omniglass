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
// well-formed uuid matches on id (a primary key, never ambiguous), a dotted
// address (#627 Task 12) resolves structurally to a uuid via resolvePath and
// then matches on id the same way, and anything else matches on name
// (possibly more than one row, #627 Task 10/11 scopes name uniqueness to
// placement rather than holding it global). Ordered by id so a caller that
// later caps the result with a LIMIT gets a stable "first" row.
func loadByRef[T any](ctx context.Context, q querier, cfg scopedConfig[T], ref string) ([]*T, error) {
	col := "name"
	switch {
	case isUUID(ref):
		col = "id"
	default:
		addr, err := ParseAddress(ref)
		if err != nil {
			return nil, err
		}
		if addr != nil {
			resolved, err := resolvePath(ctx, q, cfg, addr, ref)
			if err != nil {
				return nil, err
			}
			ref = resolved
			col = "id"
		}
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

// ErrPathNotFound is returned when a dotted address (#627 Task 12) fails to
// resolve structurally: some segment along the walk does not exist, or the
// address's terminal plane does not match the entity being resolved (a
// location address handed to the component resolver, say). Both collapse to
// this one sentinel rather than two, because which of them happened is not a
// distinction worth leaking.
//
// Unwrap returns cfg.notFound (ErrComponentNotFound, ErrSystemNotFound, or
// ErrLocationNotFound), the SAME sentinel the bare-name and uuid forms
// produce for an absent or out-of-scope row. This is load-bearing, not
// decoration: loadByRef calls ParseAddress on every non-uuid ref
// unconditionally, so a dotted value reaching a request BODY field (a
// create's parent/location/system, say, none of which carry a pattern tag)
// gets path resolution whether the feature intended it there or not. A
// caller resolving that reference (components.go's parent-resolve, for
// example) already does `if errors.Is(err, ErrComponentNotFound) { return
// ErrParentComponentNotFound }` to convert a generic miss into a
// field-specific 422; without Unwrap that fold silently stopped firing for
// every dotted body reference, producing a 404 that names the entity being
// PATCHED rather than the reference that failed to resolve. With it, the
// same thirteen existing folds (components.go, systems.go, locations.go,
// secrets.go, variables.go, role_declarations.go, nodes.go) run unchanged
// for all three reference forms: name, uuid, and address. mapRefErr's own
// *ErrPathNotFound case (matched via errors.As on the concrete type, which
// does not consult Unwrap) still fires first and wins for the path-PARAMETER
// case, where nothing upstream substitutes a more specific sentinel, so the
// direct non-disclosing 404 is unchanged there.
type ErrPathNotFound struct {
	Kind string
	Ref  string
	// notFound is cfg.notFound, carried through so a caller's existing
	// errors.Is(err, cfg.notFound) still fires. Unexported: nothing
	// constructs this sentinel outside resolvePath, and Unwrap is the
	// contract, not the field.
	notFound error
}

func (e *ErrPathNotFound) Error() string {
	return fmt.Sprintf("storage: %q does not resolve for %s", e.Ref, e.Kind)
}

func (e *ErrPathNotFound) Unwrap() error { return e.notFound }

// addressKindTable maps an Address's terminal plane to the tree table that
// resolves it: the same three tables scopeTable already allow-lists, plus
// AddressRole, which resolves to none (no scopedConfig addresses a role
// today), so it always falls to the not-ok branch and reports ErrPathNotFound
// like any other kind mismatch.
func addressKindTable(k AddressKind) (scopeTable, bool) {
	switch k {
	case AddressLocation:
		return locationTable, true
	case AddressComponent:
		return componentTable, true
	case AddressSystem:
		return systemTable, true
	default:
		return "", false
	}
}

// resolveTreeRoot finds a root row (parent_id IS NULL) by name in tbl:
// location_root_name_key's shape, the first segment of every address's
// location-tree walk. Returns "" (not an error) when no row matches, so the
// caller can fold "does not exist" into ErrPathNotFound uniformly with every
// other kind of structural miss.
func resolveTreeRoot(ctx context.Context, q querier, tbl scopeTable, name string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `select id from `+string(tbl)+` where parent_id is null and name = $1`, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("storage: resolve %s root %q: %w", tbl, name, err)
	}
	return id, nil
}

// resolveTreeChild finds a direct child by (parent_id, name) in tbl: every
// root-tree segment after the first (location_parent_name_key), and every
// plane-tail segment after the first (component_parent_name_key /
// system_parent_name_key).
func resolveTreeChild(ctx context.Context, q querier, tbl scopeTable, parentID, name string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `select id from `+string(tbl)+` where parent_id = $1 and name = $2`, parentID, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("storage: resolve %s child %q: %w", tbl, name, err)
	}
	return id, nil
}

// resolvePlaneRoot finds the top-level row of a plane switch ($comp/$sys):
// component_location_name_key / system_location_name_key when locID is
// non-nil (the address gave at least one location segment before the
// accessor), component_orphan_name_key / system_orphan_name_key when it is
// nil (the accessor came first, addressing an unplaced row directly).
func resolvePlaneRoot(ctx context.Context, q querier, tbl scopeTable, locID *string, name string) (string, error) {
	var id string
	var err error
	if locID != nil {
		err = q.QueryRow(ctx,
			`select id from `+string(tbl)+` where parent_id is null and location_id = $1 and name = $2`,
			*locID, name).Scan(&id)
	} else {
		err = q.QueryRow(ctx,
			`select id from `+string(tbl)+` where parent_id is null and location_id is null and name = $1`,
			name).Scan(&id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("storage: resolve %s plane root %q: %w", tbl, name, err)
	}
	return id, nil
}

// resolvePath turns a parsed dotted address (#627 Task 12) into the uuid it
// names, by walking the location tree from a root and, if the address
// switches plane, descending the component or system tree from there. Each
// hop is a deterministic single-row match: the placement-scoped unique
// indexes (component_parent_name_key and its seven siblings, #627 Task 10)
// guarantee at most one row per (parent, name) or (location, name) pair, so
// this walk carries no ambiguity to detect, unlike a bare name. What it can
// hit is a structural miss (some segment does not exist) or a kind mismatch
// (the address's terminal plane is not cfg's table), and both report the
// same ErrPathNotFound.
//
// Deliberately NOT scope-aware: it is pure tree structure, run before any
// scope check, the same way a caller-supplied uuid carries no scope
// information of its own. loadByRef uses the uuid this returns exactly as it
// would use one the caller typed directly, so the existing resolveRef
// machinery still applies the caller's read/action scope to the row the
// address named, same as any other reference (architect ruling 2, #627:
// scope decides before ambiguity does; an address has no ambiguity to
// decide, but it gets the same non-disclosing treatment on a scope miss).
func resolvePath[T any](ctx context.Context, q querier, cfg scopedConfig[T], addr *Address, ref string) (string, error) {
	notFound := func() (string, error) {
		return "", &ErrPathNotFound{Kind: string(cfg.table), Ref: ref, notFound: cfg.notFound}
	}

	tbl, ok := addressKindTable(addr.Kind)
	if !ok || tbl != cfg.table {
		return notFound()
	}

	var locID *string
	for _, seg := range addr.Root {
		var id string
		var err error
		if locID == nil {
			id, err = resolveTreeRoot(ctx, q, locationTable, seg)
		} else {
			id, err = resolveTreeChild(ctx, q, locationTable, *locID, seg)
		}
		if err != nil {
			return "", err
		}
		if id == "" {
			return notFound()
		}
		locID = &id
	}

	if addr.Kind == AddressLocation {
		if locID == nil {
			return notFound()
		}
		return *locID, nil
	}

	if len(addr.Tail) == 0 {
		return notFound()
	}
	id, err := resolvePlaneRoot(ctx, q, tbl, locID, addr.Tail[0])
	if err != nil {
		return "", err
	}
	if id == "" {
		return notFound()
	}
	for _, seg := range addr.Tail[1:] {
		next, err := resolveTreeChild(ctx, q, tbl, id, seg)
		if err != nil {
			return "", err
		}
		if next == "" {
			return notFound()
		}
		id = next
	}
	return id, nil
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

// withoutCandidates redacts an *ErrAmbiguousName's Candidates and folds an
// *ErrPathNotFound down to the bare cfg.notFound sentinel it wraps, for a
// resolution path that has no caller scope to filter by at all (the three
// *NameTaken advisories, which intentionally resolve scope-blind so
// availability matches the placement bucket asked about rather than the
// caller's own grant, ADR the checkName routes document; and a cross-tier
// existence-only bind, whose caller scope is the wrong tier to apply here at
// all). The reference is still refused as ambiguous; the response just
// never names uuids the caller might have no scope to read, since nothing
// here can tell which ones do.
//
// The ErrPathNotFound fold exists for a reason the name alone does not
// suggest, worth stating because every one of these call sites is a
// reference carried in a request BODY, not a path parameter: #627 Task 12's
// resolvePath treats an address-shaped body reference (a create's
// parent/location/system, none of which carry a pattern tag) exactly like
// it treats a path parameter, and mapRefErr's *ErrPathNotFound case is
// matched by errors.As on the concrete type, which fires unconditionally
// and BEFORE the caller's own entity mapper ever runs its switch (the
// switch that already knows a component-create's missing location is 422,
// not the 404 a bare GET /locations/{name} would give). Folding here, at
// the one place every cross-tier bind already routes through, replaces the
// concrete *ErrPathNotFound with the bare sentinel BEFORE it leaves the
// gateway, so mapRefErr's ErrPathNotFound case no longer matches it and the
// caller's existing switch decides the status, same as it already does for
// the bare-name and uuid forms of the same reference.
func withoutCandidates(err error) error {
	var ambig *ErrAmbiguousName
	if errors.As(err, &ambig) {
		return &ErrAmbiguousName{Kind: ambig.Kind, Ref: ambig.Ref}
	}
	var notFound *ErrPathNotFound
	if errors.As(err, &notFound) {
		return notFound.Unwrap()
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
