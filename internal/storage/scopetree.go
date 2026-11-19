package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/hyperscaleav/omniglass/internal/scope"
)

// ErrScopeWiring is a scope resolved for a resource that cannot be checked
// against the table at hand (#754): an unregistered resource, or one registered
// on another tier. It is neither a not-found nor a forbidden, because nothing
// about the caller's grants decided it: the code passed the wrong set, and every
// non-all caller would otherwise be denied by an empty or foreign set that reads
// exactly like a legitimately empty grant. No entity mapper names it, so it
// reaches the caller as a 500, and checkScopeWiring logs it.
var ErrScopeWiring = errors.New("storage: scope resolved for a resource that cannot be checked against this table")

// checkScopeWiring is the resolve paths' assertion that set may be checked
// against tier: scope.Covers(set.Resource, tier), the relationship Resolve's own
// applicableKinds encodes. An all set holds everywhere, and a hand-built set
// (no Resource) carries no resolution to check.
func checkScopeWiring(set scope.Set, tier string) error {
	if set.All || set.Resource == "" || scope.Covers(set.Resource, tier) {
		return nil
	}
	slog.Error("a scope was resolved for a resource that cannot be checked against this table; register the resource's tier in internal/scope or pass the set resolved for this one",
		"facility", "scope", "resolved_for", set.Resource, "table", tier)
	return fmt.Errorf("%w: resolved for %q, checked against %q", ErrScopeWiring, set.Resource, tier)
}

// uuidRoots keeps only the scope roots that are syntactically valid uuids. A
// malformed root (for example a grant mis-scoped to an entity name rather than its
// id) is dropped, so it contributes nothing to the scope, rather than erroring the
// whole `id = any($1::uuid[])` query. Grant creation validates the target up
// front; this is the defense-in-depth that keeps a bad grant from 500-ing a list.
func uuidRoots(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, err := uuid.Parse(id); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// The scoped-tree primitive: the recursive ABAC walks shared by every fleet
// entity that is a parent_id self-referencing tree (location, system, and
// component). The entity-specific columns and scanning stay in each entity's
// file; only the membership and subtree-expansion SQL lives here, so the
// over-permit-safe scope filter is written once and reused.

// scopeTable is an allow-listed tree table. The value is a compile-time
// constant, never user input, so interpolating it into the recursive CTEs is
// injection-safe.
type scopeTable string

const (
	locationTable  scopeTable = "location"
	systemTable    scopeTable = "system"
	componentTable scopeTable = "component"
)

// scopeKindTable maps a grant scope_kind to its tree table for validating a scope
// target. Only the tree tiers are addressable; "all" has no target and "group" is
// not built yet, so both report false.
func scopeKindTable(kind string) (scopeTable, bool) {
	switch kind {
	case "location":
		return locationTable, true
	case "system":
		return systemTable, true
	case "component":
		return componentTable, true
	}
	return "", false
}

// inScopeTree reports whether targetID falls within a resolved scope on tbl: an
// all scope always holds; otherwise the target is in scope when itself or an
// ancestor is an inclusive scope root, OR a STRICT ancestor is an excluded root
// (a subtree_excl_root grant covers a root's descendants but not the root itself,
// for the modify actions), OR the target is itself a self root (a self grant
// matches exactly the one row, no descendant walk). Inclusive and excluded roots
// are disjoint; a broader inclusive ancestor still admits an id that is another
// grant's excluded root (inclusive wins). The ancestor walk is cheaper than
// expanding every root's subtree; the CYCLE clause guards a corrupted parent edge.
// A system or component target is also in scope when it is placed under one of
// the set's cross-tier location roots (placedCTEs).
func inScopeTree(ctx context.Context, q querier, tbl scopeTable, targetID string, set scope.Set) (bool, error) {
	if set.All {
		return true, nil
	}
	roots := uuidRoots(set.IDs)
	selfIDs := uuidRoots(set.SelfIDs)
	locRoots := placedRoots(tbl, set)
	if len(roots) == 0 && len(selfIDs) == 0 && len(locRoots) == 0 {
		return false, nil
	}
	excluded := uuidRoots(set.ExcludeRootIDs)
	inclusive := subtractRoots(roots, excluded)
	args := []any{targetID, inclusive, excluded, selfIDs}
	placed, placedArm := "", ""
	if len(locRoots) > 0 {
		placed = ", " + placedCTEs(tbl, 5, "placed")
		placedArm = " or $1::uuid in (select id from placed)"
		args = append(args, locRoots)
	}
	var ok bool
	err := q.QueryRow(ctx, `
		with recursive anc(id, parent_id) as (
			select id, parent_id from `+string(tbl)+` where id = $1
			union all
			select t.id, t.parent_id from `+string(tbl)+` t join anc on t.id = anc.parent_id
		) cycle id set is_cycle using path`+placed+`
		select $1::uuid = any($4::uuid[])
		    or exists(select 1 from anc where id = any($2::uuid[]))
		    or exists(select 1 from anc where id = any($3::uuid[]) and id <> $1)`+placedArm,
		args...).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("storage: scope check on %s: %w", tbl, err)
	}
	return ok, nil
}

// placeAdmits reports whether a root system or component created at
// locationID is inside create: an all scope places anywhere, and otherwise the
// location must sit at or under one of the set's cross-tier location roots
// (#1498). Both subtree ops keep their root for create, so the root location
// itself is a valid place; a self grant contributes no root at all.
func placeAdmits(ctx context.Context, q querier, locationID string, create scope.Set) (bool, error) {
	if create.All {
		return true, nil
	}
	return inScopeTree(ctx, q, locationTable, locationID, scope.Set{IDs: create.LocationIDs})
}

// InScopeIDs reports, for a batch of candidate row ids of a tree resource
// (location/system/component), which are inside a resolved scope: an all scope
// admits every candidate, a non-tree resource or empty candidate set admits none.
// It is the batch companion to inScopeTree, used to compute per-row UI action
// affordances (create/update/delete on a row) without a query per row per action:
// one query per action scope answers the whole page. It applies the same
// inclusive-subtree plus exclude-root-descendants logic the enforcement uses, so
// the UI hint can never disagree with the gateway's per-action decision.
func (p *PG) InScopeIDs(ctx context.Context, resource string, ids []string, set scope.Set) (map[string]bool, error) {
	tbl, ok := scopeKindTable(resource)
	if !ok {
		return map[string]bool{}, nil // a non-tree resource: nothing in scope
	}
	return inScopeIDs(ctx, p.pool, tbl, ids, set)
}

// inScopeIDs is InScopeIDs over a querier and a known tree table, so a caller
// already inside a transaction (a name resolve, #1133) narrows a whole candidate
// set in one statement rather than an inScopeTree per candidate.
func inScopeIDs(ctx context.Context, q querier, tbl scopeTable, ids []string, set scope.Set) (map[string]bool, error) {
	out := make(map[string]bool, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	if set.All {
		for _, id := range ids {
			out[id] = true
		}
		return out, nil
	}
	roots := uuidRoots(set.IDs)
	candidates := uuidRoots(ids)
	selfIDs := uuidRoots(set.SelfIDs)
	locRoots := placedRoots(tbl, set)
	if len(candidates) == 0 || (len(roots) == 0 && len(selfIDs) == 0 && len(locRoots) == 0) {
		return out, nil
	}
	excluded := uuidRoots(set.ExcludeRootIDs)
	inclusive := subtractRoots(roots, excluded)
	args := []any{inclusive, excluded, candidates, selfIDs}
	placed, placedArm := "", ""
	if len(locRoots) > 0 {
		placed = ", " + placedCTEs(tbl, 5, "placed")
		placedArm = " or id in (select id from placed)"
		args = append(args, locRoots)
	}
	rows, err := q.Query(ctx, `
		with recursive sub(id) as (
			select id from `+string(tbl)+` where id = any($1::uuid[])
			union all
			select t.id from `+string(tbl)+` t join sub on t.parent_id = sub.id
		) cycle id set sub_cyc using sub_path,
		subx(id, is_root) as (
			select id, true from `+string(tbl)+` where id = any($2::uuid[])
			union all
			select t.id, false from `+string(tbl)+` t join subx on t.parent_id = subx.id
		) cycle id set subx_cyc using subx_path`+placed+`
		select id from `+string(tbl)+`
		where id = any($3::uuid[])
		  and (id in (select id from sub) or id in (select id from subx where not is_root) or id = any($4::uuid[])`+placedArm+`)`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("storage: in-scope ids on %s: %w", tbl, err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("storage: scan in-scope id: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

// subtractRoots returns the ids in a that are not in b, a set difference over the
// small scope-root slices.
func subtractRoots(a, b []string) []string {
	if len(b) == 0 {
		return a
	}
	drop := make(map[string]bool, len(b))
	for _, id := range b {
		drop[id] = true
	}
	out := make([]string, 0, len(a))
	for _, id := range a {
		if !drop[id] {
			out = append(out, id)
		}
	}
	return out
}

// scopedListSQL builds the list query for tbl selecting cols, ordered by name,
// over read, with scopedListArgs' binds. An all scope selects every row (no
// args); a rooted scope expands each subtree root to its descendants (the
// recursive descendant walk, cycle-guarded) and also matches any self root by
// id equality, taking the subtree root array as $1 and the self root array as
// $2. A system or component scope with cross-tier location roots also admits
// the rows placed under them (placedCTEs), taking those roots as $3. A list is
// always a read, and read never excludes a root, so there is no exclude-root
// arm here.
func scopedListSQL(tbl scopeTable, cols string, read scope.Set) string {
	if read.All {
		return `select ` + cols + ` from ` + string(tbl) + ` order by name`
	}
	placed, placedArm := "", ""
	if len(placedRoots(tbl, read)) > 0 {
		placed = ", " + placedCTEs(tbl, 3, "placed")
		placedArm = " or id in (select id from placed)"
	}
	return `
		with recursive sub(id) as (
			select id from ` + string(tbl) + ` where id = any($1::uuid[])
			union all
			select t.id from ` + string(tbl) + ` t join sub on t.parent_id = sub.id
		) cycle id set is_cycle using path` + placed + `
		select ` + cols + ` from ` + string(tbl) + `
		where id in (select id from sub) or id = any($2::uuid[])` + placedArm + `
		order by name`
}

// scopedListArgs is the bind values scopedListSQL's rooted form takes for read
// over tbl, in order, and false when no well-formed root of any kind survives
// (nothing is in scope). An all scope binds nothing.
func scopedListArgs(tbl scopeTable, read scope.Set) ([]any, bool) {
	if read.All {
		return nil, true
	}
	roots, selfIDs, locRoots := uuidRoots(read.IDs), uuidRoots(read.SelfIDs), placedRoots(tbl, read)
	if len(roots) == 0 && len(selfIDs) == 0 && len(locRoots) == 0 {
		return nil, false
	}
	args := []any{roots, selfIDs}
	if len(locRoots) > 0 {
		args = append(args, locRoots)
	}
	return args, true
}

// placedRoots is the set's cross-tier location roots (#1498) that apply to
// tbl, filtered to well-formed uuids: only a system or component row has a
// placement, so a location table (or any other) takes none.
func placedRoots(tbl scopeTable, set scope.Set) []string {
	if tbl != systemTable && tbl != componentTable {
		return nil
	}
	return uuidRoots(set.LocationIDs)
}

// placedCTEs is the cross-tier expansion (#1498): recursive CTE bodies, without
// the leading "with recursive", ending in name(id), the rows of tbl placed under
// the location roots bound at $arg (its helpers are name_loc and name_sys, so
// two expansions can share one WITH). A location root covers its location
// subtree. A system is placed there when its own location is in that subtree,
// and a component when its effective place is: its own location, or, when it
// has none, its primary system's placement. Each placed row brings its
// descendants on its own tier, the subtree a same-tier grant on it would
// cover. tbl is systemTable or componentTable (placedRoots guards this).
func placedCTEs(tbl scopeTable, arg int, name string) string {
	a := strconv.Itoa(arg)
	loc, sys := name+"_loc", name+"_sys"
	locCTE := loc + `(id) as (
			select id from location where id = any($` + a + `::uuid[])
			union all
			select t.id from location t join ` + loc + ` on t.parent_id = ` + loc + `.id
		) cycle id set is_cycle using path`
	sysCTE := func(cte string) string {
		return cte + `(id) as (
			select id from system where location_id in (select id from ` + loc + `)
			union all
			select t.id from system t join ` + cte + ` on t.parent_id = ` + cte + `.id
		) cycle id set is_cycle using path`
	}
	if tbl == systemTable {
		return locCTE + `,
		` + sysCTE(name)
	}
	return locCTE + `,
		` + sysCTE(sys) + `,
		` + name + `(id) as (
			select c.id from component c
			where c.location_id in (select id from ` + loc + `)
			   or (c.location_id is null and exists(select 1 from system_member m
				where m.component_id = c.id and m.is_primary and m.system_id in (select id from ` + sys + `)))
			union all
			select t.id from component t join ` + name + ` on t.parent_id = ` + name + `.id
		) cycle id set is_cycle using path`
}

// The arc-scope primitive: the exclusive-arc counterpart of scopedListSQL for
// rows owned on the component / system / location arc (secret, variable, the
// pushed-telemetry owner check). It injects the read scope into the query
// itself, one round trip regardless of row count, replicating inScopeTree's
// semantics per tier: an owner is in scope when it IS a self root, when it sits
// at or under an inclusive subtree root of its own tier, or when it sits
// strictly under an excluded root (subtree_excl_root: the boundary row is out,
// its descendants are in). A platform-owned row (all arc columns NULL) never
// matches the arc predicate; the caller gates platform rows on read.All.

// arcScopeArgs returns the three shared bind values for the arc CTEs and
// predicate, in order: inclusive subtree roots (roots minus excluded), excluded
// roots, and self roots, each filtered to well-formed uuids.
func arcScopeArgs(set scope.Set) (inclusive, excluded, selfIDs []string) {
	roots := uuidRoots(set.IDs)
	excluded = uuidRoots(set.ExcludeRootIDs)
	return subtractRoots(roots, excluded), excluded, uuidRoots(set.SelfIDs)
}

// arcScopeCTEs builds the six recursive descendant expansions (an inclusive
// walk and a strict-descendants-of-excluded walk per tier), binding inclusive
// roots as $incl and excluded roots as $excl (1-based arg positions).
func arcScopeCTEs(incl, excl int) string {
	one := func(tbl scopeTable, name string) string {
		t := string(tbl)
		return name + `_sub(id) as (
			select id from ` + t + ` where id = any($` + strconv.Itoa(incl) + `::uuid[])
			union all
			select t.id from ` + t + ` t join ` + name + `_sub on t.parent_id = ` + name + `_sub.id
		) cycle id set is_cycle using path,
		` + name + `_exc(id) as (
			select id from ` + t + ` where parent_id = any($` + strconv.Itoa(excl) + `::uuid[])
			union all
			select t.id from ` + t + ` t join ` + name + `_exc on t.parent_id = ` + name + `_exc.id
		) cycle id set is_cycle using path`
	}
	return `with recursive ` +
		one(componentTable, "comp") + `,
		` + one(systemTable, "sys") + `,
		` + one(locationTable, "loc")
}

// arcScopePredicate is the WHERE fragment over an aliased owner arc, binding
// self roots as $self. The alias is the row's table alias (e.g. "s").
func arcScopePredicate(alias string, self int) string {
	arm := func(col, name string) string {
		c := alias + `.` + col
		return `(` + c + ` is not null and (` + c + ` = any($` + strconv.Itoa(self) + `::uuid[])
			or ` + c + ` in (select id from ` + name + `_sub)
			or ` + c + ` in (select id from ` + name + `_exc)))`
	}
	return `(` + arm("component_id", "comp") + `
		or ` + arm("system_id", "sys") + `
		or ` + arm("location_id", "loc") + `)`
}

// readFilter is a read scope over one tree tier rendered as SQL, for a query
// that reads rows hanging off that tier (an alarm or a streak on a component)
// rather than the tier's own rows. With is a leading recursive CTE (empty
// when none is needed), Args its bind values in order, and Pred a WHERE
// fragment over a column holding the tier's id. It is the same subtree plus
// self-root semantics scopedListSQL applies; read never excludes a root.
type readFilter struct {
	With string
	Args []any
	pred func(col string) string
	// cte is With's one recursive expansion without its leading "with
	// recursive", so two filters over different tiers can share one WITH
	// (joinReadFilters); empty when the filter needs none.
	cte string
}

// Pred is the WHERE fragment admitting col when its row is in scope.
func (f readFilter) Pred(col string) string { return f.pred(col) }

// newReadFilter renders read over tbl, binding its roots from $first: an
// all scope admits every row, a scope with no well-formed root admits none,
// and a rooted scope admits each subtree root's descendants and each self
// root, and on the system or component tier each row placed under a
// cross-tier location root (placedCTEs, a third bind).
func newReadFilter(tbl scopeTable, read scope.Set, first int) readFilter {
	return newNamedReadFilter(tbl, read, first, "vis")
}

// newNamedReadFilter is newReadFilter with its expansion named cte, so a query
// reading rows that hang off two tiers (an identity binding's owner is a
// component or a system) can filter each in its own tier's read scope.
func newNamedReadFilter(tbl scopeTable, read scope.Set, first int, cte string) readFilter {
	if read.All {
		return readFilter{pred: func(string) string { return "true" }}
	}
	roots, selfIDs, locRoots := uuidRoots(read.IDs), uuidRoots(read.SelfIDs), placedRoots(tbl, read)
	if len(roots) == 0 && len(selfIDs) == 0 && len(locRoots) == 0 {
		return readFilter{pred: func(string) string { return "false" }}
	}
	t := string(tbl)
	r, s := strconv.Itoa(first), strconv.Itoa(first+1)
	body := cte + `(id) as (
			select id from ` + t + ` where id = any($` + r + `::uuid[])
			union all
			select t.id from ` + t + ` t join ` + cte + ` on t.parent_id = ` + cte + `.id
		) cycle id set is_cycle using path `
	args := []any{roots, selfIDs}
	placed := ""
	if len(locRoots) > 0 {
		placed = cte + "_placed"
		body += `,
		` + placedCTEs(tbl, first+2, placed) + ` `
		args = append(args, locRoots)
	}
	return readFilter{
		With: `with recursive ` + body,
		Args: args,
		cte:  body,
		pred: func(col string) string {
			p := `(` + col + ` in (select id from ` + cte + `) or ` + col + ` = any($` + s + `::uuid[])`
			if placed != "" {
				p += ` or ` + col + ` in (select id from ` + placed + `)`
			}
			return p + `)`
		},
	}
}

// joinReadFilters is the one leading WITH for several named filters (each
// built with its own cte name and its own argument positions), empty when none
// needs an expansion.
func joinReadFilters(fs ...readFilter) string {
	var ctes []string
	for _, f := range fs {
		if f.cte != "" {
			ctes = append(ctes, f.cte)
		}
	}
	if len(ctes) == 0 {
		return ""
	}
	return `with recursive ` + strings.Join(ctes, ", ")
}
