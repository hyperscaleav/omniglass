package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hyperscaleav/omniglass/internal/scope"
)

// Identity provider groups map to Omniglass groups (#2363, ADR-2361 point
// 4). An admin maps a value of the groups claim, keyed by the issuer that
// sends it, to a user group; each SSO sign-in, and each re-check refresh
// whose id_token carries the claim, sets the person's idp memberships from
// that issuer to exactly the mapped set, in the transaction that signs them
// in or accepts the refresh, each add and remove audited. A local membership
// is never touched and stands in for an idp one. No mapping reaches an owner:
// a mapping to a group with an owner grant is refused, and so is an owner
// grant on a mapped group.

// Membership sources.
const (
	MemberSourceLocal = "local"
	MemberSourceIdP   = "idp"
)

// ErrGroupMappingNotFound is a mapping that is not the named group's.
var ErrGroupMappingNotFound = errors.New("storage: group mapping not found")

// ErrGroupMappingExists is a claim value the group already maps from that issuer.
var ErrGroupMappingExists = errors.New("storage: the group already maps that claim value")

// ErrGroupMappingOwner is the owner guard (ADR-2361 point 4): a mapping to a
// group that carries an owner grant, or an owner grant on a mapped group.
var ErrGroupMappingOwner = errors.New("storage: an identity provider group may not reach an owner grant")

// ErrGroupMappingNoIssuer is a mapping that names no issuer while single
// sign-on names none either.
var ErrGroupMappingNoIssuer = errors.New("storage: the mapping names no issuer and single sign-on is not configured")

// GroupIdPMapping maps one groups-claim value from one issuer to a group.
type GroupIdPMapping struct {
	ID         string
	GroupID    string
	Group      string
	Issuer     string
	ClaimValue string
	CreatedAt  time.Time
}

// IdPGroups is what one verified id_token says of a person's groups: the
// issuer and the values of its groups claim (empty when it carries none).
type IdPGroups struct {
	Issuer string
	Values []string
}

// mappingImage is a mapping's audit image.
type mappingImage struct {
	GroupID    string `json:"group_id"`
	Issuer     string `json:"issuer"`
	ClaimValue string `json:"claim_value"`
}

// groupMemberImage is a membership's audit image.
type groupMemberImage struct {
	GroupID string `json:"group_id"`
	UserID  string `json:"user_id"`
	Source  string `json:"source"`
	Issuer  string `json:"issuer,omitempty"`
}

// ownerGrantOnGroupSQL is true when the group ($1) carries an owner grant at
// any scope.
const ownerGrantOnGroupSQL = `exists (select 1 from user_grant g join role r on r.id = g.role_id
	where g.group_id = $1 and r.namespace = 'og' and r.name = 'owner')`

// lockGroup resolves a group and holds its row, so the owner guard's two
// sides (a mapping, an owner grant) cannot race past each other.
func lockGroup(ctx context.Context, tx pgx.Tx, groupRef string) (string, error) {
	id, err := groupIDByRef(ctx, tx, groupRef)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `select 1 from user_group where id = $1 for update`, id); err != nil {
		return "", fmt.Errorf("storage: lock group: %w", err)
	}
	return id, nil
}

// ListGroupIdPMappings lists a group's mappings, by issuer then claim value.
// Requires an all-scope grant.
func (p *PG) ListGroupIdPMappings(ctx context.Context, groupRef string, read scope.Set) ([]GroupIdPMapping, error) {
	if !read.All {
		return nil, ErrUserForbidden
	}
	groupID, err := groupIDByRef(ctx, p.pool, groupRef)
	if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `select m.id, m.group_id, g.name, m.issuer, m.claim_value, m.created_at
		from user_group_idp_mapping m join user_group g on g.id = m.group_id
		where m.group_id = $1 order by m.issuer, m.claim_value`, groupID)
	if err != nil {
		return nil, fmt.Errorf("storage: list group mappings: %w", err)
	}
	defer rows.Close()
	out := []GroupIdPMapping{}
	for rows.Next() {
		var m GroupIdPMapping
		if err := rows.Scan(&m.ID, &m.GroupID, &m.Group, &m.Issuer, &m.ClaimValue, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("storage: scan group mapping: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateGroupIdPMapping maps a claim value from issuer to a group, audited.
// An empty issuer is the configured provider's. Requires an all-scope grant.
// A group that carries an owner grant is ErrGroupMappingOwner; a duplicate
// ErrGroupMappingExists.
func (p *PG) CreateGroupIdPMapping(ctx context.Context, actorID, groupRef, issuer, claimValue string, action scope.Set) (*GroupIdPMapping, error) {
	if !action.All {
		return nil, ErrUserForbidden
	}
	if issuer == "" {
		s, err := p.platformSettings(ctx, p.pool)
		if err != nil {
			return nil, err
		}
		if issuer = s.SSO.Issuer; issuer == "" {
			return nil, ErrGroupMappingNoIssuer
		}
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create group mapping: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	groupID, err := lockGroup(ctx, tx, groupRef)
	if err != nil {
		return nil, err
	}
	var owner bool
	if err := tx.QueryRow(ctx, `select `+ownerGrantOnGroupSQL, groupID).Scan(&owner); err != nil {
		return nil, fmt.Errorf("storage: owner guard: %w", err)
	}
	if owner {
		return nil, ErrGroupMappingOwner
	}
	m := GroupIdPMapping{GroupID: groupID, Issuer: issuer, ClaimValue: claimValue}
	err = tx.QueryRow(ctx, `insert into user_group_idp_mapping (group_id, issuer, claim_value) values ($1, $2, $3)
		returning id, created_at, (select name from user_group where id = $1)`, groupID, issuer, claimValue).Scan(&m.ID, &m.CreatedAt, &m.Group)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, ErrGroupMappingExists
	}
	if err != nil {
		return nil, fmt.Errorf("storage: create group mapping: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "user_group_idp_mapping", m.ID, nil,
		mappingImage{GroupID: groupID, Issuer: issuer, ClaimValue: claimValue}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create group mapping: %w", err)
	}
	return &m, nil
}

// DeleteGroupIdPMapping removes one of a group's mappings, audited. When it
// was the group's last mapping from its issuer, that issuer's idp memberships
// of the group go with it, each audited, since no claim can justify them any
// more; otherwise they wait for the next sign-in or refresh. Requires an
// all-scope grant.
func (p *PG) DeleteGroupIdPMapping(ctx context.Context, actorID, groupRef, mappingID string, action scope.Set) error {
	if !action.All {
		return ErrUserForbidden
	}
	if !isUUID(mappingID) {
		return ErrGroupMappingNotFound
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete group mapping: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	groupID, err := lockGroup(ctx, tx, groupRef)
	if err != nil {
		return err
	}
	img := mappingImage{GroupID: groupID}
	err = tx.QueryRow(ctx, `delete from user_group_idp_mapping where id = $1 and group_id = $2 returning issuer, claim_value`,
		mappingID, groupID).Scan(&img.Issuer, &img.ClaimValue)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrGroupMappingNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: delete group mapping: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "user_group_idp_mapping", mappingID, img, nil); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `delete from user_group_member m
		where m.group_id = $1 and m.source = 'idp' and m.issuer = $2
		  and not exists (select 1 from user_group_idp_mapping x where x.group_id = $1 and x.issuer = $2)
		returning m.user_id::text`, groupID, img.Issuer)
	if err != nil {
		return fmt.Errorf("storage: drop unmapped idp memberships: %w", err)
	}
	users, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("storage: scan unmapped idp memberships: %w", err)
	}
	for _, u := range users {
		if err := writeAuditRes(ctx, tx, actorID, "delete", "user_group_member", groupID,
			groupMemberImage{GroupID: groupID, UserID: u, Source: MemberSourceIdP, Issuer: img.Issuer}, nil); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete group mapping: %w", err)
	}
	return nil
}

// syncIdPGroups sets a person's idp memberships from g.Issuer to exactly the
// groups its claim values map to, inside the caller's transaction: the rows
// no value maps to any more are removed, the newly mapped ones added, each
// audited as the system's act. Another issuer's rows and every local row are
// left alone (a local row already in a mapped group stands in for the idp
// one). A group that carries an owner grant is never joined, whatever a
// mapping says (the guard's backstop).
func syncIdPGroups(ctx context.Context, tx pgx.Tx, userID string, g IdPGroups) error {
	values := g.Values
	if values == nil {
		values = []string{}
	}
	const mapped = `select x.group_id from user_group_idp_mapping x
		where x.issuer = $2 and x.claim_value = any($3)
		  and not exists (select 1 from user_grant gr join role r on r.id = gr.role_id
		      where gr.group_id = x.group_id and r.namespace = 'og' and r.name = 'owner')`
	removed, err := tx.Query(ctx, `delete from user_group_member m
		where m.user_id = $1 and m.source = 'idp' and m.issuer = $2 and m.group_id not in (`+mapped+`)
		returning m.group_id::text`, userID, g.Issuer, values)
	if err != nil {
		return fmt.Errorf("storage: sync idp groups (remove): %w", err)
	}
	gone, err := pgx.CollectRows(removed, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("storage: scan removed idp groups: %w", err)
	}
	added, err := tx.Query(ctx, `insert into user_group_member (group_id, user_id, source, issuer)
		select distinct group_id, $1::uuid, 'idp', $2 from (`+mapped+`) s
		on conflict (group_id, user_id) do nothing
		returning group_id::text`, userID, g.Issuer, values)
	if err != nil {
		return fmt.Errorf("storage: sync idp groups (add): %w", err)
	}
	joined, err := pgx.CollectRows(added, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("storage: scan added idp groups: %w", err)
	}
	for _, groupID := range gone {
		if err := writeAuditRes(ctx, tx, "", "delete", "user_group_member", groupID,
			groupMemberImage{GroupID: groupID, UserID: userID, Source: MemberSourceIdP, Issuer: g.Issuer}, nil); err != nil {
			return err
		}
	}
	for _, groupID := range joined {
		if err := writeAuditRes(ctx, tx, "", "create", "user_group_member", groupID, nil,
			groupMemberImage{GroupID: groupID, UserID: userID, Source: MemberSourceIdP, Issuer: g.Issuer}); err != nil {
			return err
		}
	}
	return nil
}
