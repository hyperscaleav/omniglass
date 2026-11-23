package storage

import (
	"context"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/scope"
)

// DeleteBlocker is one row standing in the way of a refused delete (#2463): a
// child that has to go first, an endpoint the component still owns, a system
// still placed in the location. Kind is the row's own resource (component,
// endpoint, system, location), so a console can link it and offer that kind's
// own remove; ID is its uuid, Name its address, Label what an operator reads.
type DeleteBlocker struct {
	Kind  string
	ID    string
	Name  string
	Label string
}

// DeleteBlockers is what a refused delete is waiting on, within the caller's
// sight. Items are the rows the caller may read; Hidden counts the rest (rows
// outside the caller's read scope on their own kind, and any past the per-arc
// listing cap), so the refusal can say "and N more" without naming a row the
// caller could not otherwise read (non-disclosure holds: a count says no more
// than the refusal itself already does).
type DeleteBlockers struct {
	Items  []DeleteBlocker
	Hidden int
}

// blockerListCap bounds how many rows one arc names. A refusal is a prompt to
// act, not a report: past this many the operator is better served by the
// list view, and the remainder still reaches them as the Hidden count.
const blockerListCap = 50

// blockerArc is one relation that refuses a delete: the rows of kind that
// point at the target, read by sql with the target's id as $1. sql selects
// (id, name, label, scope_id), where scope_id is the row of scopeTbl the
// caller's read scope on kind is judged against (an endpoint is read through
// its owning component, so its scope_id is the component's id; a
// component-less endpoint has none and is visible to an all read only).
type blockerArc struct {
	kind     string
	scopeTbl scopeTable
	sql      string
}

// deleteBlockerArcs are the relations each tree tier's delete refuses on: the
// structural children scopedDelete counts, the endpoints refuseOwnedEndpoints
// counts, and the restrict foreign keys the delete itself trips (a slot a
// component staffs, a system or a component placed in a location). A new
// restrict FK onto one of these tables belongs here too, or its refusal falls
// back to the unnamed message.
var deleteBlockerArcs = map[string][]blockerArc{
	"component": {
		{"component", componentTable, `select id, name, coalesce(nullif(label, ''), name), id from component where parent_id = $1 order by name`},
		{"endpoint", componentTable, `select id, name, coalesce(nullif(label, ''), name), component from endpoint where component = $1 order by name`},
		{"system", systemTable, `select distinct s.id, s.name, coalesce(nullif(s.label, ''), s.name), s.id
			from system_slot_assignment a join system s on s.id = a.system_id where a.component_id = $1 order by s.name`},
	},
	"system": {
		{"system", systemTable, `select id, name, coalesce(nullif(label, ''), name), id from system where parent_id = $1 order by name`},
	},
	"location": {
		{"location", locationTable, `select id, name, coalesce(nullif(label, ''), name), id from location where parent_id = $1 order by name`},
		{"system", systemTable, `select id, name, coalesce(nullif(label, ''), name), id from system where location_id = $1 order by name`},
		{"component", componentTable, `select id, name, coalesce(nullif(label, ''), name), id from component where location_id = $1 order by name`},
	},
}

// DeleteBlockersOf names what stands in the way of deleting the tree entity
// kind addressed by ref. The target is resolved within the caller's read scope
// on kind (a caller who cannot see the target learns nothing, the 404 the
// delete itself would give); each blocker is named only when readOf(its kind)
// admits it, and counted otherwise. It reads, never writes: the console calls
// it through the refused delete, after the delete has already rolled back.
func (p *PG) DeleteBlockersOf(ctx context.Context, kind, ref string, readOf func(kind string) scope.Set) (*DeleteBlockers, error) {
	var id string
	switch kind {
	case "component":
		v, err := scopedByNameInScope(ctx, p.pool, componentConfig, ref, kind, readOf(kind))
		if err != nil {
			return nil, err
		}
		id = v.ID
	case "system":
		v, err := scopedByNameInScope(ctx, p.pool, systemConfig, ref, kind, readOf(kind))
		if err != nil {
			return nil, err
		}
		id = v.ID
	case "location":
		v, err := scopedByNameInScope(ctx, p.pool, locationConfig, ref, kind, readOf(kind))
		if err != nil {
			return nil, err
		}
		id = v.ID
	default:
		return nil, ErrUnknownOwnerKind
	}
	out := &DeleteBlockers{}
	for _, arc := range deleteBlockerArcs[kind] {
		if err := p.collectBlockers(ctx, arc, id, readOf(arc.kind), out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// collectBlockers reads one arc's rows, narrows them to the caller's read
// scope in one statement (inScopeIDs, the same predicate the gateway's reads
// enforce), and appends the visible ones up to the cap; the rest are counted.
func (p *PG) collectBlockers(ctx context.Context, arc blockerArc, targetID string, read scope.Set, out *DeleteBlockers) error {
	rows, err := p.pool.Query(ctx, arc.sql, targetID)
	if err != nil {
		return fmt.Errorf("storage: delete blockers (%s): %w", arc.kind, err)
	}
	type cand struct {
		b       DeleteBlocker
		scopeID *string
	}
	var cands []cand
	var scopeIDs []string
	for rows.Next() {
		c := cand{b: DeleteBlocker{Kind: arc.kind}}
		if err := rows.Scan(&c.b.ID, &c.b.Name, &c.b.Label, &c.scopeID); err != nil {
			rows.Close()
			return fmt.Errorf("storage: scan delete blocker (%s): %w", arc.kind, err)
		}
		if c.scopeID != nil {
			scopeIDs = append(scopeIDs, *c.scopeID)
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: delete blockers (%s): %w", arc.kind, err)
	}
	visible, err := inScopeIDs(ctx, p.pool, arc.scopeTbl, scopeIDs, read)
	if err != nil {
		return err
	}
	named := 0
	for _, c := range cands {
		// A row with no scope anchor (a component-less endpoint) is readable
		// by an all scope only, exactly as ListEndpoints reads it.
		ok := read.All || (c.scopeID != nil && visible[*c.scopeID])
		if ok && named < blockerListCap {
			out.Items = append(out.Items, c.b)
			named++
			continue
		}
		out.Hidden++
	}
	return nil
}
