package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The batched principal load (#671).
//
// loadPrincipal fills ONE principal in three statements: its kind profile, its
// effective grants, its group memberships. ListPrincipals called it once per row,
// so the admin directory read at 1+3N, seven statements for two principals and
// sixty-seven for twenty-two, and it is the one directory that grows with the
// organisation rather than the fleet and has no pagination to hide behind.
//
// loadPrincipals reads the same three things for a whole page in three
// statements, keyed by principal id and assembled in memory: the shape #648 used
// for the address walk. loadPrincipal STAYS, unchanged, as the single-row entry
// point and as the reference implementation the batch is measured against
// (principal_batch_test.go), the way PathOf stayed after #648. Two readers of one
// fact drift unless one of them is the oracle.
//
// The kind profile is a UNION over the three profile tables rather than a query
// per kind PRESENT in the page. A branch per kind would be flat in page size too,
// but its cost would depend on the MIX (a directory holding humans, service
// accounts and nodes costing two statements more than one holding only humans),
// and a count that varies with the fixture's composition is a count no assertion
// can state as a number.

// loadPrincipals fills every principal's kind profile, effective grants and group
// memberships in a fixed three statements, whatever the page size. The slice is
// filled in place; ids must be distinct.
func (p *PG) loadPrincipals(ctx context.Context, prs []Principal) error {
	if len(prs) == 0 {
		return nil
	}
	ids := make([]string, len(prs))
	at := make(map[string]*Principal, len(prs))
	for i := range prs {
		ids[i] = prs[i].ID
		at[prs[i].ID] = &prs[i]
	}
	if err := p.attachPrincipalProfiles(ctx, ids, at); err != nil {
		return err
	}
	if err := p.attachPrincipalGrants(ctx, ids, at); err != nil {
		return err
	}
	return p.attachPrincipalGroups(ctx, ids, at)
}

// attachPrincipalProfiles fills the kind profile of every principal in the page.
//
// The three profile tables have three different shapes, so the union widens them
// to one: column three is the kind's own identifying string (a human's username,
// a service's name, a node's name) and the human-only columns are padded on the
// other two branches. A principal of a profiled kind with no profile row is a
// broken invariant, and it is reported here with the same error the single-row
// path raises rather than silently returning a principal with a nil profile.
func (p *PG) attachPrincipalProfiles(ctx context.Context, ids []string, at map[string]*Principal) error {
	rows, err := p.pool.Query(ctx,
		`select principal_id, 'human'::text as kind, username, coalesce(email, ''), coalesce(label, ''),
		        must_change_password, avatar is not null, avatar_updated_at
		   from human where principal_id = any($1::uuid[])
		  union all
		 select principal_id, 'service'::text, name, '', '', false, false, null::timestamptz
		   from service where principal_id = any($1::uuid[])
		  union all
		 select principal_id, 'node'::text, name, '', '', false, false, null::timestamptz
		   from node where principal_id = any($1::uuid[])`, ids)
	if err != nil {
		return fmt.Errorf("storage: load principal profiles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, kind, ident, email, label string
			mustChange, hasAvatar         bool
			avatarAt                      *time.Time
		)
		if err := rows.Scan(&id, &kind, &ident, &email, &label, &mustChange, &hasAvatar, &avatarAt); err != nil {
			return fmt.Errorf("storage: scan principal profile: %w", err)
		}
		pr, ok := at[id]
		if !ok || pr.Kind != kind {
			continue
		}
		switch kind {
		case "human":
			pr.Human = &HumanProfile{
				Username: ident, Email: email, Label: label,
				MustChangePassword: mustChange, HasAvatar: hasAvatar, AvatarUpdatedAt: avatarAt,
			}
		case "service":
			pr.Service = &ServiceProfile{Name: ident}
		case "node":
			pr.Node = &NodeProfile{Name: ident}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: load principal profiles: %w", err)
	}
	for _, id := range ids {
		pr := at[id]
		missing := (pr.Kind == "human" && pr.Human == nil) ||
			(pr.Kind == "service" && pr.Service == nil) ||
			(pr.Kind == "node" && pr.Node == nil)
		if missing {
			return fmt.Errorf("storage: load %s: %w", pr.Kind, pgx.ErrNoRows)
		}
	}
	return nil
}

// attachPrincipalGrants fills every principal's EFFECTIVE grants: its own, plus
// the grants of every group it belongs to.
//
// The inner union is loadPrincipal's `principal_id = $1 or group_id in (...)`
// read as two disjoint sources rather than one predicate, which is what lets the
// row carry the principal it belongs to. They are disjoint by schema, not by
// luck: principal_grant_target_ck admits exactly one of principal_id and
// group_id, so no grant can arrive down both branches and be counted twice.
//
// The ordering matches the single-row path (direct grants first, then each
// group's, oldest first within a group) with the owner prepended so a page's rows
// arrive grouped; assembly then appends in that order.
func (p *PG) attachPrincipalGrants(ctx context.Context, ids []string, at map[string]*Principal) error {
	rows, err := p.pool.Query(ctx,
		`select o.owner, g.id, (select name from role where id = g.role_id), g.scope_kind, g.scope_id, g.scope_op,
		        g.group_id, coalesce(pg.label, pg.name)
		   from (select id as grant_id, principal_id as owner
		           from principal_grant where principal_id = any($1::uuid[])
		          union all
		         select dg.id, m.principal_id
		           from principal_grant dg
		           join principal_group_member m on m.group_id = dg.group_id
		          where m.principal_id = any($1::uuid[])) o
		   join principal_grant g on g.id = o.grant_id
		   left join principal_group pg on pg.id = g.group_id
		  order by o.owner, g.group_id nulls first, g.created_at`, ids)
	if err != nil {
		return fmt.Errorf("storage: load grants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var owner string
		var g Grant
		if err := rows.Scan(&owner, &g.ID, &g.Role, &g.ScopeKind, &g.ScopeID, &g.ScopeOp, &g.GroupID, &g.GroupName); err != nil {
			return fmt.Errorf("storage: scan grant: %w", err)
		}
		if pr, ok := at[owner]; ok {
			pr.Grants = append(pr.Grants, g)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: grants: %w", err)
	}
	return nil
}

// attachPrincipalGroups fills the group memberships (id + label) the admin
// directory names beside a principal's inherited access.
func (p *PG) attachPrincipalGroups(ctx context.Context, ids []string, at map[string]*Principal) error {
	rows, err := p.pool.Query(ctx,
		`select m.principal_id, g.id, coalesce(g.label, g.name)
		   from principal_group_member m join principal_group g on g.id = m.group_id
		  where m.principal_id = any($1::uuid[])
		  order by m.principal_id, g.name`, ids)
	if err != nil {
		return fmt.Errorf("storage: load groups: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var owner string
		var ref PrincipalGroupRef
		if err := rows.Scan(&owner, &ref.ID, &ref.Name); err != nil {
			return fmt.Errorf("storage: scan group ref: %w", err)
		}
		if pr, ok := at[owner]; ok {
			pr.Groups = append(pr.Groups, ref)
		}
	}
	return rows.Err()
}
