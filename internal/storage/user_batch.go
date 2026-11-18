package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The batched user load (#671).
//
// loadUser fills ONE user in three statements: its kind profile, its
// effective grants, its group memberships. ListUsers called it once per row,
// so the admin directory read at 1+3N, seven statements for two users and
// sixty-seven for twenty-two, and it is the one directory that grows with the
// organisation rather than the fleet and has no pagination to hide behind.
//
// loadUsers reads the same three things for a whole page in three
// statements, keyed by user id and assembled in memory: the shape #648 used
// for the address walk. loadUser STAYS, unchanged, as the single-row entry
// point and as the reference implementation the batch is measured against
// (user_batch_test.go), the way PathOf stayed after #648. Two readers of one
// fact drift unless one of them is the oracle.
//
// The kind profile is a UNION over the three profile tables rather than a query
// per kind PRESENT in the page. A branch per kind would be flat in page size too,
// but its cost would depend on the MIX (a directory holding humans, service
// accounts and outposts costing two statements more than one holding only people),
// and a count that varies with the fixture's composition is a count no assertion
// can state as a number.

// loadUsers fills every user's kind profile, effective grants and group
// memberships in a fixed three statements, whatever the page size. The slice is
// filled in place; ids must be distinct.
func (p *PG) loadUsers(ctx context.Context, prs []User) error {
	if len(prs) == 0 {
		return nil
	}
	ids := make([]string, len(prs))
	at := make(map[string]*User, len(prs))
	for i := range prs {
		ids[i] = prs[i].ID
		at[prs[i].ID] = &prs[i]
	}
	if err := p.attachUserProfiles(ctx, ids, at); err != nil {
		return err
	}
	if err := p.attachUserGrants(ctx, ids, at); err != nil {
		return err
	}
	return p.attachUserGroups(ctx, ids, at)
}

// attachUserProfiles fills the kind profile of every user in the page.
//
// The three profile tables have three different shapes, so the union widens them
// to one: column three is the profile's own identifying string (a person's username,
// a machine's name, an outpost's name) and the person-only columns are padded on the
// other two branches. Column two names the profile table, not the user's kind: a
// machine user carries either a machine row or an outpost row. A user with no
// profile row is a broken invariant, and it is reported here with the same error the single-row
// path raises rather than silently returning a user with a nil profile.
func (p *PG) attachUserProfiles(ctx context.Context, ids []string, at map[string]*User) error {
	rows, err := p.pool.Query(ctx,
		`select user_id, 'person'::text as kind, username, coalesce(email, ''), coalesce(label, ''),
		        must_change_password, avatar is not null, avatar_updated_at
		   from person where user_id = any($1::uuid[])
		  union all
		 select user_id, 'machine'::text, name, '', '', false, false, null::timestamptz
		   from machine where user_id = any($1::uuid[])
		  union all
		 select user_id, 'outpost'::text, name, '', '', false, false, null::timestamptz
		   from outpost where user_id = any($1::uuid[])`, ids)
	if err != nil {
		return fmt.Errorf("storage: load user profiles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, kind, ident, email, label string
			mustChange, hasAvatar         bool
			avatarAt                      *time.Time
		)
		if err := rows.Scan(&id, &kind, &ident, &email, &label, &mustChange, &hasAvatar, &avatarAt); err != nil {
			return fmt.Errorf("storage: scan user profile: %w", err)
		}
		pr, ok := at[id]
		if !ok || (kind == "person") != (pr.Kind == "person") {
			continue
		}
		switch kind {
		case "person":
			pr.Person = &PersonProfile{
				Username: ident, Email: email, Label: label,
				MustChangePassword: mustChange, HasAvatar: hasAvatar, AvatarUpdatedAt: avatarAt,
			}
		case "machine":
			pr.Machine = &MachineProfile{Name: ident}
		case "outpost":
			pr.Outpost = &OutpostProfile{Name: ident}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: load user profiles: %w", err)
	}
	for _, id := range ids {
		pr := at[id]
		missing := (pr.Kind == "person" && pr.Person == nil) ||
			(pr.Kind == "machine" && pr.Machine == nil && pr.Outpost == nil)
		if missing {
			return fmt.Errorf("storage: load %s: %w", pr.Kind, pgx.ErrNoRows)
		}
	}
	return nil
}

// attachUserGrants fills every user's EFFECTIVE grants: its own, plus
// the grants of every group it belongs to.
//
// The inner union is loadUser's `user_id = $1 or group_id in (...)`
// read as two disjoint sources rather than one predicate, which is what lets the
// row carry the user it belongs to. They are disjoint by schema, not by
// luck: user_grant_target_ck admits exactly one of user_id and
// group_id, so no grant can arrive down both branches and be counted twice.
//
// The ordering matches the single-row path (direct grants first, then each
// group's, oldest first within a group) with the owner prepended so a page's rows
// arrive grouped; assembly then appends in that order.
func (p *PG) attachUserGrants(ctx context.Context, ids []string, at map[string]*User) error {
	return eachUserGrant(ctx, p.pool, ids, func(owner string, g Grant) {
		if pr, ok := at[owner]; ok {
			pr.Grants = append(pr.Grants, g)
		}
	})
}

// eachUserGrant reads the effective grants of every user in ids in
// one statement, over any querier, so a caller inside a transaction (the
// notification queue's recipient filter, #1341) reads exactly what the
// directory reads, and calls fn for each in the order described above.
func eachUserGrant(ctx context.Context, q querier, ids []string, fn func(owner string, g Grant)) error {
	rows, err := q.Query(ctx,
		`select o.owner, g.id, (select name from role where id = g.role_id), g.scope_kind, g.scope_id, g.scope_op,
		        g.group_id, pg.name, pg.label
		   from (select id as grant_id, user_id as owner
		           from user_grant where user_id = any($1::uuid[])
		          union all
		         select dg.id, m.user_id
		           from user_grant dg
		           join user_group_member m on m.group_id = dg.group_id
		          where m.user_id = any($1::uuid[])) o
		   join user_grant g on g.id = o.grant_id
		   left join user_group pg on pg.id = g.group_id
		  order by o.owner, g.group_id nulls first, g.created_at`, ids)
	if err != nil {
		return fmt.Errorf("storage: load grants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var owner string
		var g Grant
		if err := rows.Scan(&owner, &g.ID, &g.Role, &g.ScopeKind, &g.ScopeID, &g.ScopeOp, &g.GroupID, &g.GroupName, &g.GroupLabel); err != nil {
			return fmt.Errorf("storage: scan grant: %w", err)
		}
		fn(owner, g)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: grants: %w", err)
	}
	return nil
}

// attachUserGroups fills the group memberships (id + label) the admin
// directory names beside a user's inherited access.
func (p *PG) attachUserGroups(ctx context.Context, ids []string, at map[string]*User) error {
	rows, err := p.pool.Query(ctx,
		`select m.user_id, g.id, coalesce(g.label, g.name)
		   from user_group_member m join user_group g on g.id = m.group_id
		  where m.user_id = any($1::uuid[])
		  order by m.user_id, g.name`, ids)
	if err != nil {
		return fmt.Errorf("storage: load groups: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var owner string
		var ref UserGroupRef
		if err := rows.Scan(&owner, &ref.ID, &ref.Name); err != nil {
			return fmt.Errorf("storage: scan group ref: %w", err)
		}
		if pr, ok := at[owner]; ok {
			pr.Groups = append(pr.Groups, ref)
		}
	}
	return rows.Err()
}
