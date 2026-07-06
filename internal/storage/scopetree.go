package storage

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/hyperscaleav/omniglass/internal/scope"
)

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

// The scoped-tree primitive: the recursive ABAC walks shared by every estate
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
// (an ExcludeRoot grant covers a root's descendants but not the root itself, for
// the modify actions). Inclusive and excluded roots are disjoint; a broader
// inclusive ancestor still admits an id that is another grant's excluded root
// (inclusive wins). The ancestor walk is cheaper than expanding every root's
// subtree; the CYCLE clause guards a corrupted parent edge.
func inScopeTree(ctx context.Context, q querier, tbl scopeTable, targetID string, set scope.Set) (bool, error) {
	if set.All {
		return true, nil
	}
	roots := uuidRoots(set.IDs)
	if len(roots) == 0 {
		return false, nil
	}
	excluded := uuidRoots(set.ExcludeRootIDs)
	inclusive := subtractRoots(roots, excluded)
	var ok bool
	err := q.QueryRow(ctx, `
		with recursive anc(id, parent_id) as (
			select id, parent_id from `+string(tbl)+` where id = $1
			union all
			select t.id, t.parent_id from `+string(tbl)+` t join anc on t.id = anc.parent_id
		) cycle id set is_cycle using path
		select exists(select 1 from anc where id = any($2::uuid[]))
		    or exists(select 1 from anc where id = any($3::uuid[]) and id <> $1)`,
		targetID, inclusive, excluded).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("storage: scope check on %s: %w", tbl, err)
	}
	return ok, nil
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

// scopedListSQL builds the list query for tbl selecting cols, ordered by name.
// An all scope selects every row (no args); a rooted scope expands each root to
// its subtree (the recursive descendant walk, cycle-guarded) and filters to it,
// taking the root id array as $1.
func scopedListSQL(tbl scopeTable, cols string, all bool) string {
	if all {
		return `select ` + cols + ` from ` + string(tbl) + ` order by name`
	}
	return `
		with recursive sub(id) as (
			select id from ` + string(tbl) + ` where id = any($1::uuid[])
			union all
			select t.id from ` + string(tbl) + ` t join sub on t.parent_id = sub.id
		) cycle id set is_cycle using path
		select ` + cols + ` from ` + string(tbl) + `
		where id in (select id from sub)
		order by name`
}
