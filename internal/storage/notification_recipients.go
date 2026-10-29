package storage

import (
	"context"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/rbac"
	"github.com/hyperscaleav/omniglass/internal/scope"
)

// The queue-time recipient filter (#1341, ADR-1341): a group or person
// target is expanded to the people it names at the transition, and each
// person is kept only when their own component read scope admits the alarm's
// component, so nothing about an alarm reaches a person who could not read it.
// It runs inside the alarm transition's transaction, under #618's savepoint,
// so it is set-based: one statement for the members, one for all their
// grants, one for the role index and one ancestor walk of the component, then
// a pure resolve per person. Its cost does not grow with a group's size.

// recipient is one person a target reaches, on the channels it names.
type recipient struct {
	id, name string
	channels []string
}

// recipientsFor expands the person targets among targetIDs to the human,
// active members they name today, keeping only those whose component read
// scope admits componentID. The map is keyed by target uuid, each list in
// username order.
func recipientsFor(ctx context.Context, q querier, componentID string, targetIDs []string) (map[string][]recipient, error) {
	rows, err := q.Query(ctx, `
		select t.id::text, pr.id::text, h.username, t.channels
		  from notification_target t
		  join principal_group_member m on m.group_id = t.principal_group_id
		  join principal pr on pr.id = m.principal_id
		  join human h on h.principal_id = pr.id
		 where t.id = any($1::uuid[]) and t.kind = 'principal_group' and pr.active and pr.archived_at is null
		union all
		select t.id::text, pr.id::text, h.username, t.channels
		  from notification_target t
		  join principal pr on pr.id = t.principal_id
		  join human h on h.principal_id = pr.id
		 where t.id = any($1::uuid[]) and t.kind = 'principal' and pr.active and pr.archived_at is null
		order by 1, 3`, targetIDs)
	if err != nil {
		return nil, fmt.Errorf("storage: expand notification recipients: %w", err)
	}
	defer rows.Close()
	type member struct {
		target string
		r      recipient
	}
	var members []member
	seen := map[string]bool{}
	var ids []string
	for rows.Next() {
		var m member
		if err := rows.Scan(&m.target, &m.r.id, &m.r.name, &m.r.channels); err != nil {
			return nil, fmt.Errorf("storage: scan notification recipient: %w", err)
		}
		members = append(members, m)
		if !seen[m.r.id] {
			seen[m.r.id] = true
			ids = append(ids, m.r.id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: expand notification recipients: %w", err)
	}
	out := map[string][]recipient{}
	if len(members) == 0 {
		return out, nil
	}
	admitted, err := readersOf(ctx, q, componentID, ids)
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		if admitted[m.r.id] {
			out[m.target] = append(out[m.target], m.r)
		}
	}
	return out, nil
}

// readersOf reports which of the principals can read the component: each
// one's component read scope, resolved from their effective grants exactly
// as a request of theirs would resolve it (scope.Resolve over the role
// index), judged against the component's ancestry walked once.
func readersOf(ctx context.Context, q querier, componentID string, principalIDs []string) (map[string]bool, error) {
	grants, idx, chain, err := accessOf(ctx, q, componentID, principalIDs)
	if err != nil {
		return nil, err
	}
	return admitReaders(principalIDs, grants, idx, chain), nil
}

// subscribersOf reports which personal rule owners may be told about the
// component (#1353): they read it, and they still hold notification:subscribe,
// so withholding the permission from a person stops their existing rules too.
func subscribersOf(ctx context.Context, q querier, componentID string, ownerIDs []string) (map[string]bool, error) {
	grants, idx, chain, err := accessOf(ctx, q, componentID, ownerIDs)
	if err != nil {
		return nil, err
	}
	out := admitReaders(ownerIDs, grants, idx, chain)
	for id := range out {
		roles := make([]string, 0, len(grants[id]))
		for _, g := range grants[id] {
			roles = append(roles, g.Role)
		}
		if !idx.Flatten(roles).Allows("notification", "subscribe") {
			delete(out, id)
		}
	}
	return out, nil
}

// accessOf loads what judging principals against one component takes, in
// three statements whatever their number: their effective grants, the role
// index, and the component's ancestry.
func accessOf(ctx context.Context, q querier, componentID string, principalIDs []string) (map[string][]scope.Grant, rbac.RoleIndex, []string, error) {
	grants := make(map[string][]scope.Grant, len(principalIDs))
	if err := eachPrincipalGrant(ctx, q, principalIDs, func(owner string, g Grant) {
		sg := scope.Grant{Role: g.Role, ScopeKind: g.ScopeKind, ScopeOp: g.ScopeOp}
		if g.ScopeID != nil {
			sg.ScopeID = *g.ScopeID
		}
		grants[owner] = append(grants[owner], sg)
	}); err != nil {
		return nil, nil, nil, err
	}
	idx, err := roleIndexOf(ctx, q)
	if err != nil {
		return nil, nil, nil, err
	}
	chain, err := componentChain(ctx, q, componentID)
	if err != nil {
		return nil, nil, nil, err
	}
	return grants, idx, chain, nil
}

// admitReaders is the pure half of readersOf: a principal is admitted when
// its component read scope admits the component whose ancestry is chain.
func admitReaders(principalIDs []string, grants map[string][]scope.Grant, idx rbac.RoleIndex, chain []string) map[string]bool {
	out := make(map[string]bool, len(principalIDs))
	for _, id := range principalIDs {
		if scope.Resolve(grants[id], idx, "component", "read").AdmitsChain(chain) {
			out[id] = true
		}
	}
	return out
}

// roleIndexOf reads the role index in one statement, the same shape the API
// caches from ListRoles.
func roleIndexOf(ctx context.Context, q querier) (rbac.RoleIndex, error) {
	rows, err := q.Query(ctx, `select name, permissions, inherits from role`)
	if err != nil {
		return rbac.RoleIndex{}, fmt.Errorf("storage: read role index: %w", err)
	}
	defer rows.Close()
	var roles []rbac.Role
	for rows.Next() {
		var r rbac.Role
		if err := rows.Scan(&r.ID, &r.Permissions, &r.Inherits); err != nil {
			return rbac.RoleIndex{}, fmt.Errorf("storage: scan role: %w", err)
		}
		roles = append(roles, r)
	}
	if err := rows.Err(); err != nil {
		return rbac.RoleIndex{}, fmt.Errorf("storage: read role index: %w", err)
	}
	return rbac.NewRoleIndex(roles), nil
}

// componentChain is a component's ancestry, itself first, walked once
// (cycle-guarded, as inScopeTree's walk is). A component that does not exist
// has an empty chain, which no rooted scope admits.
func componentChain(ctx context.Context, q querier, componentID string) ([]string, error) {
	rows, err := q.Query(ctx, `
		with recursive anc(id, parent_id, depth) as (
			select id, parent_id, 0 from component where id = $1::uuid
			union all
			select c.id, c.parent_id, anc.depth + 1 from component c join anc on c.id = anc.parent_id
		) cycle id set is_cycle using path
		select id::text from anc where not is_cycle order by depth`, componentID)
	if err != nil {
		return nil, fmt.Errorf("storage: component ancestry: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("storage: scan component ancestry: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
