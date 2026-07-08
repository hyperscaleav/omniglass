package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Principal groups: a group holds role x scope grants that its members inherit,
// so an admin assigns access to a team once instead of per user. Membership is
// static (an explicit join) and flat (a group does not contain groups). A group
// grant lives in the same principal_grant table as a direct grant (keyed by
// group_id instead of principal_id); the grant loader unions them into a
// member's pr.Grants, so inheritance needs no change to flatten or scope
// resolution. Managing groups is all-scope admin work, like the principal
// directory, so these require an all-scope grant (checked here, gated by a
// principal_group capability on the route).

// ErrGroupNotFound is returned when no group has the given id.
var ErrGroupNotFound = errors.New("storage: group not found")

// ErrGroupExists is returned when a group name is already taken.
var ErrGroupExists = errors.New("storage: group name already exists")

// Group is a named set of principals that share inherited grants.
type Group struct {
	ID          string
	Name        string
	DisplayName string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// GroupSpec is the input to create a group.
type GroupSpec struct {
	Name        string
	DisplayName string
	Description string
}

// GroupPatch updates a group's presentational fields (never its id or name key
// semantics beyond a rename).
type GroupPatch struct {
	Name        *string
	DisplayName *string
	Description *string
}

// GroupMember is a lightweight view of a principal in a group's roster.
type GroupMember struct {
	PrincipalID string
	Kind        string
	Username    string
	DisplayName string
}

const groupCols = `id, name, coalesce(display_name, ''), coalesce(description, ''), created_at, updated_at`

func scanGroup(row interface{ Scan(...any) error }) (*Group, error) {
	var g Group
	if err := row.Scan(&g.ID, &g.Name, &g.DisplayName, &g.Description, &g.CreatedAt, &g.UpdatedAt); err != nil {
		return nil, err
	}
	return &g, nil
}

// CreateGroup creates a principal group, audited. Requires an all-scope grant. A
// duplicate name is ErrGroupExists.
func (p *PG) CreateGroup(ctx context.Context, actorID string, spec GroupSpec, action scope.Set) (*Group, error) {
	if !action.All {
		return nil, ErrPrincipalForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create group: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	g, err := scanGroup(tx.QueryRow(ctx,
		`insert into principal_group (name, display_name, description) values ($1, $2, $3) returning `+groupCols,
		spec.Name, nullize(spec.DisplayName), nullize(spec.Description)))
	if err != nil {
		return nil, mapGroupWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "principal_group", g.ID, nil, g); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create group: %w", err)
	}
	return g, nil
}

// ListGroups returns every group, ordered by name. Requires an all-scope grant.
func (p *PG) ListGroups(ctx context.Context, read scope.Set) ([]Group, error) {
	if !read.All {
		return nil, ErrPrincipalForbidden
	}
	rows, err := p.pool.Query(ctx, `select `+groupCols+` from principal_group order by name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list groups: %w", err)
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan group: %w", err)
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

// GetGroup resolves one group by id. Requires an all-scope grant; an unknown id
// is ErrGroupNotFound.
func (p *PG) GetGroup(ctx context.Context, id string, read scope.Set) (*Group, error) {
	if !read.All {
		return nil, ErrPrincipalForbidden
	}
	g, err := scanGroup(p.pool.QueryRow(ctx, `select `+groupCols+` from principal_group where id = $1`, id))
	if err != nil {
		return nil, notFoundOr(err, ErrGroupNotFound)
	}
	return g, nil
}

// UpdateGroup patches a group's name and presentational fields, audited. A
// duplicate name is ErrGroupExists; an unknown id is ErrGroupNotFound.
func (p *PG) UpdateGroup(ctx context.Context, actorID, id string, patch GroupPatch, action scope.Set) (*Group, error) {
	if !action.All {
		return nil, ErrPrincipalForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update group: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := scanGroup(tx.QueryRow(ctx, `select `+groupCols+` from principal_group where id = $1 for update`, id))
	if err != nil {
		return nil, notFoundOr(err, ErrGroupNotFound)
	}
	name, display, desc := before.Name, before.DisplayName, before.Description
	if patch.Name != nil {
		name = *patch.Name
	}
	if patch.DisplayName != nil {
		display = *patch.DisplayName
	}
	if patch.Description != nil {
		desc = *patch.Description
	}
	after, err := scanGroup(tx.QueryRow(ctx,
		`update principal_group set name = $2, display_name = $3, description = $4, updated_at = now() where id = $1 returning `+groupCols,
		id, name, nullize(display), nullize(desc)))
	if err != nil {
		return nil, mapGroupWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "principal_group", id, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update group: %w", err)
	}
	return after, nil
}

// DeleteGroup removes a group (and, by cascade, its memberships and group
// grants), audited. Requires an all-scope grant; an unknown id is
// ErrGroupNotFound.
func (p *PG) DeleteGroup(ctx context.Context, actorID, id string, action scope.Set) error {
	if !action.All {
		return ErrPrincipalForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete group: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := scanGroup(tx.QueryRow(ctx, `select `+groupCols+` from principal_group where id = $1`, id))
	if err != nil {
		return notFoundOr(err, ErrGroupNotFound)
	}
	if _, err := tx.Exec(ctx, `delete from principal_group where id = $1`, id); err != nil {
		return fmt.Errorf("storage: delete group: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "principal_group", id, before, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AddGroupMember adds a principal to a group, audited and idempotent (re-adding
// an existing member is a no-op). Requires an all-scope grant.
func (p *PG) AddGroupMember(ctx context.Context, actorID, groupID, principalID string, action scope.Set) error {
	if !action.All {
		return ErrPrincipalForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin add member: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`insert into principal_group_member (group_id, principal_id) values ($1, $2) on conflict do nothing`, groupID, principalID)
	if err != nil {
		return mapMemberWriteErr(err)
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx) // already a member: nothing changed, nothing to audit
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "principal_group_member", groupID, nil, map[string]string{"group_id": groupID, "principal_id": principalID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RemoveGroupMember removes a principal from a group, audited. Requires an
// all-scope grant. Removing a non-member is a no-op.
func (p *PG) RemoveGroupMember(ctx context.Context, actorID, groupID, principalID string, action scope.Set) error {
	if !action.All {
		return ErrPrincipalForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin remove member: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `delete from principal_group_member where group_id = $1 and principal_id = $2`, groupID, principalID)
	if err != nil {
		return fmt.Errorf("storage: remove member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "principal_group_member", groupID, map[string]string{"group_id": groupID, "principal_id": principalID}, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListGroupMembers returns the principals in a group. Requires an all-scope grant.
func (p *PG) ListGroupMembers(ctx context.Context, groupID string, read scope.Set) ([]GroupMember, error) {
	if !read.All {
		return nil, ErrPrincipalForbidden
	}
	// A group can hold humans and service accounts; show each by its natural name
	// (a human's username, a service's label), so the roster reads cleanly for both.
	rows, err := p.pool.Query(ctx,
		`select p.id, p.kind, coalesce(h.username, ''), coalesce(h.display_name, s.label, '')
		   from principal_group_member m
		   join principal p on p.id = m.principal_id
		   left join human h on h.principal_id = p.id
		   left join service s on s.principal_id = p.id
		  where m.group_id = $1
		  order by coalesce(h.username, s.label, p.id::text)`, groupID)
	if err != nil {
		return nil, fmt.Errorf("storage: list members: %w", err)
	}
	defer rows.Close()
	out := []GroupMember{}
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.PrincipalID, &m.Kind, &m.Username, &m.DisplayName); err != nil {
			return nil, fmt.Errorf("storage: scan member: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListGroupsForPrincipal returns the groups a principal belongs to, for showing
// where inherited access comes from. Requires an all-scope grant.
func (p *PG) ListGroupsForPrincipal(ctx context.Context, principalID string, read scope.Set) ([]Group, error) {
	if !read.All {
		return nil, ErrPrincipalForbidden
	}
	rows, err := p.pool.Query(ctx,
		`select `+groupColsPrefixed("g")+`
		   from principal_group_member m join principal_group g on g.id = m.group_id
		  where m.principal_id = $1 order by g.name`, principalID)
	if err != nil {
		return nil, fmt.Errorf("storage: list principal groups: %w", err)
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan group: %w", err)
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

// CreateGroupGrant assigns a role x scope to a group, audited. Its members
// inherit it. Requires an all-scope grant; the escalation cover-check (a granter
// may not grant above its own tier) is applied by the API handler, as for a
// direct grant. Validation mirrors CreateGrant.
func (p *PG) CreateGroupGrant(ctx context.Context, actorID, groupID string, spec GrantSpec, action scope.Set) (*Grant, error) {
	if !action.All {
		return nil, ErrPrincipalForbidden
	}
	if spec.ScopeOp == "" {
		spec.ScopeOp = scope.OpSubtree
	}
	if spec.ScopeOp != scope.OpSubtree && spec.ScopeOp != scope.OpSubtreeExclRoot && spec.ScopeOp != scope.OpSelf {
		return nil, ErrBadScope
	}
	if spec.ScopeKind == "all" {
		spec.ScopeID = ""
		spec.ScopeOp = scope.OpSubtree
	} else if spec.ScopeID == "" {
		return nil, ErrBadScope
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create group grant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if spec.ScopeKind != "all" {
		tbl, ok := scopeKindTable(spec.ScopeKind)
		if !ok {
			return nil, ErrBadScope
		}
		var exists bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from `+string(tbl)+` where id = $1)`, spec.ScopeID).Scan(&exists); err != nil {
			return nil, fmt.Errorf("storage: scope target check: %w", err)
		}
		if !exists {
			return nil, ErrBadScope
		}
	}

	var gid string
	err = tx.QueryRow(ctx,
		`insert into principal_grant (group_id, role_id, scope_kind, scope_id, scope_op) values ($1, $2, $3, $4, $5) returning id`,
		groupID, spec.Role, spec.ScopeKind, nullize(spec.ScopeID), spec.ScopeOp).Scan(&gid)
	if err != nil {
		return nil, mapGrantWriteErr(err)
	}
	g := Grant{ID: gid, Role: spec.Role, ScopeKind: spec.ScopeKind, ScopeOp: spec.ScopeOp, GroupID: &groupID}
	if spec.ScopeID != "" {
		g.ScopeID = &spec.ScopeID
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "principal_grant", gid, nil, g); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create group grant: %w", err)
	}
	return &g, nil
}

// RevokeGroupGrant deletes one grant from a group, audited. Requires an all-scope
// grant. An unknown grant (for that group) is ErrGrantNotFound.
func (p *PG) RevokeGroupGrant(ctx context.Context, actorID, groupID, grantID string, action scope.Set) error {
	if !action.All {
		return ErrPrincipalForbidden
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin revoke group grant: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var g Grant
	err = tx.QueryRow(ctx,
		`select id, role_id, scope_kind, scope_id, scope_op from principal_grant where id = $1 and group_id = $2`,
		grantID, groupID).Scan(&g.ID, &g.Role, &g.ScopeKind, &g.ScopeID, &g.ScopeOp)
	if err != nil {
		return notFoundOr(err, ErrGrantNotFound)
	}
	if _, err := tx.Exec(ctx, `delete from principal_grant where id = $1`, grantID); err != nil {
		return fmt.Errorf("storage: delete group grant: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "principal_grant", grantID, g, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListGroupGrants returns a group's grants. Requires an all-scope grant.
func (p *PG) ListGroupGrants(ctx context.Context, groupID string, read scope.Set) ([]Grant, error) {
	if !read.All {
		return nil, ErrPrincipalForbidden
	}
	rows, err := p.pool.Query(ctx,
		`select id, role_id, scope_kind, scope_id, scope_op from principal_grant where group_id = $1 order by created_at`, groupID)
	if err != nil {
		return nil, fmt.Errorf("storage: list group grants: %w", err)
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.ID, &g.Role, &g.ScopeKind, &g.ScopeID, &g.ScopeOp); err != nil {
			return nil, fmt.Errorf("storage: scan group grant: %w", err)
		}
		g.GroupID = &groupID
		out = append(out, g)
	}
	return out, rows.Err()
}

func groupColsPrefixed(a string) string {
	return a + ".id, " + a + ".name, coalesce(" + a + ".display_name, ''), coalesce(" + a + ".description, ''), " + a + ".created_at, " + a + ".updated_at"
}

// notFoundOr maps a no-rows result (or a malformed-uuid lookup, which identifies
// no row) to the given not-found error, and wraps anything else.
func notFoundOr(err error, nf error) error {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nf
	case errors.As(err, &pgErr) && pgErr.Code == "22P02": // malformed uuid text: no such row
		return nf
	default:
		return fmt.Errorf("storage: lookup: %w", err)
	}
}

// mapGroupWriteErr translates a duplicate group name into ErrGroupExists.
func mapGroupWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrGroupExists
	}
	return fmt.Errorf("storage: write group: %w", err)
}

// mapMemberWriteErr translates a foreign-key violation (unknown group or
// principal) into ErrGroupNotFound / ErrPrincipalNotFound.
func mapMemberWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		if pgErr.ConstraintName == "principal_group_member_principal_id_fkey" {
			return ErrPrincipalNotFound
		}
		return ErrGroupNotFound
	}
	return fmt.Errorf("storage: add member: %w", err)
}
