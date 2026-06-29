package storage

import (
	"context"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/scope"
)

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

// inScopeTree reports whether targetID falls within a resolved scope on tbl: an
// all scope always holds; otherwise the target is in scope when itself or an
// ancestor is one of the scope roots (the ancestor walk, cheaper than expanding
// every root's subtree for a membership test). The CYCLE clause guards a
// corrupted parent edge.
func inScopeTree(ctx context.Context, q querier, tbl scopeTable, targetID string, set scope.Set) (bool, error) {
	if set.All {
		return true, nil
	}
	if len(set.IDs) == 0 {
		return false, nil
	}
	var ok bool
	err := q.QueryRow(ctx, `
		with recursive anc(id, parent_id) as (
			select id, parent_id from `+string(tbl)+` where id = $1
			union all
			select t.id, t.parent_id from `+string(tbl)+` t join anc on t.id = anc.parent_id
		) cycle id set is_cycle using path
		select exists(select 1 from anc where id = any($2::uuid[]))`, targetID, set.IDs).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("storage: scope check on %s: %w", tbl, err)
	}
	return ok, nil
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
