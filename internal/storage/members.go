package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
)

// Membership sentinels. Occupied is the delete-refused answer, kept distinct from
// the generic ErrReferenced because here the gateway DOES know the cause: a
// membership is only ever held open by a role assignment.
var (
	ErrMemberNotFound = errors.New("storage: component is not a member of this system")
	ErrMemberOccupied = errors.New("storage: member still fills a role in this system")
)

// Member is a component's binding to a system. IsPrimary marks the one that
// answers a question asked without a system in hand; it is a default for
// context-free callers, not a resolution rule.
//
// SystemCount is how many systems this component belongs to in total, carried on
// every row because "is this shared" is a fact about the component that a single
// binding cannot answer. Without it a reader has only IsPrimary, and those are
// different questions: a component whose default is here can still serve three
// other systems, and a surface that inferred sharing from the default would call
// that one exclusive.
type Member struct {
	ID          string
	SystemID    string
	ComponentID string
	IsPrimary   bool
	SystemCount int
}

const memberCols = `m.id, s.name, c.name, m.is_primary,
	(select count(*) from system_member peer where peer.component_id = m.component_id)`

// memberFrom joins both ends back to their names, because a membership is
// displayed as "this component, in this system" and the arc now stores ids.
const memberFrom = ` from system_member m
	join system s on s.id = m.system_id
	join component c on c.id = m.component_id`

func scanMember(row pgx.Row) (*Member, error) {
	var m Member
	if err := row.Scan(&m.ID, &m.SystemID, &m.ComponentID, &m.IsPrimary, &m.SystemCount); err != nil {
		return nil, err
	}
	return &m, nil
}

// AddMember binds a component into a system, idempotently. The first membership a
// component gets becomes its primary with nobody asking: a component in exactly
// one system, which is nearly all of them, must never surface the concept. A
// later membership does not steal that default.
func (p *PG) AddMember(ctx context.Context, actorID, systemName, componentName string, write scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin add member: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	systemID, componentID, err := p.resolveMembershipEnds(ctx, tx, systemName, componentName, write)
	if err != nil {
		return err
	}
	if err := addMemberTx(ctx, tx, systemID, componentID); err != nil {
		return err
	}
	// A component's label can read its PRIMARY system's type (#685), and the
	// first membership a component gets becomes that primary with nobody
	// asking, so a bind is a label write path. Restamped here rather than
	// inside addMemberTx because CreateComponent's own stamp already runs after
	// its membership, and a cascade there would render the same row twice.
	if err := p.cascadeComponentLabel(ctx, tx, componentID); err != nil {
		return err
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "system_member", systemID, nil,
		map[string]string{"system": systemName, "component": componentName}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit add member: %w", err)
	}
	return nil
}

// addMemberTx is the insert on its own, so the assignment path can bind a
// component into the system it is being staffed into within the same transaction
// rather than making the operator say it twice. Takes both ids directly: every
// caller (AddMember, AssignRole) has already resolved them, so there is no
// name left to re-derive an id from, ambiguously or otherwise (#627).
func addMemberTx(ctx context.Context, q txQuerier, systemID, componentID string) error {
	// Whether this membership becomes the default is decided by reading the
	// component's other memberships, which is compare-then-act and therefore wrong
	// under READ COMMITTED without serializing it: two rooms claiming a component
	// at the same instant would each see none and each claim the default, and the
	// partial unique index would then fail the loser's write outright rather than
	// letting it become an ordinary member. Two rooms wired at once is an ordinary
	// afternoon, so this is not a corner.
	if err := lockMemberComponent(ctx, q, componentID); err != nil {
		return err
	}
	// The primary is the row's own absence of competition: it is the default only
	// when this component has no membership yet.
	if _, err := q.Exec(ctx, `
		insert into system_member (system_id, component_id, is_primary)
		values ($1::uuid, $2::uuid, not exists (select 1 from system_member where component_id = $2::uuid))
		on conflict (system_id, component_id) do nothing`,
		systemID, componentID); err != nil {
		return fmt.Errorf("storage: add member: %w", err)
	}
	return nil
}

// RemoveMember unbinds a component from a system. Refused while the component
// still fills a role there: removing it would leave the system staffed by a
// non-member, which is the contradiction this table exists to make impossible.
func (p *PG) RemoveMember(ctx context.Context, actorID, systemName, componentName string, write scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin remove member: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	systemID, componentID, err := p.resolveMembershipEndsForRemoval(ctx, tx, systemName, componentName, write)
	if err != nil {
		return err
	}
	if err := lockMemberComponent(ctx, tx, componentID); err != nil {
		return err
	}
	var staffing int
	if err := tx.QueryRow(ctx, `
		select count(*) from system_role_assignment
		where system_id = $1::uuid
		  and component_id = $2::uuid`,
		systemID, componentID).Scan(&staffing); err != nil {
		return fmt.Errorf("storage: count member roles: %w", err)
	}
	if staffing > 0 {
		return ErrMemberOccupied
	}
	tag, err := tx.Exec(ctx, `delete from system_member
		where system_id = $1::uuid
		  and component_id = $2::uuid`,
		systemID, componentID)
	if err != nil {
		return fmt.Errorf("storage: remove member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMemberNotFound
	}
	// The default cannot be left pointing at a membership that no longer exists. If
	// exactly one remains it becomes the default, which is the same rule that gave
	// the first membership its default: a component with one system never carries
	// an unanswered question.
	if err := promoteSolePrimary(ctx, tx, componentID); err != nil {
		return err
	}
	// Unbinding moves the primary twice over: away from the system just
	// removed, and (via the promotion above) possibly onto the sole survivor.
	// Either way what .SystemTypeLabel reads has changed (#685).
	if err := p.cascadeComponentLabel(ctx, tx, componentID); err != nil {
		return err
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "system_member", systemID,
		map[string]string{"system": systemName, "component": componentName}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit remove member: %w", err)
	}
	return nil
}

// lockMemberComponent serializes every write that can move a component's default.
// The component is the unit because the default is a property of the component,
// not of one binding, and it is the same key from either side of the relation.
// Keyed on the id, not the name: two components could share a name once #627
// lands, and a name-keyed lock would serialize two unrelated components'
// writes against each other for no reason (or, worse, none at all, if it
// still let both interleave because the string happened to differ from the
// row it should have matched).
func lockMemberComponent(ctx context.Context, q txQuerier, componentID string) error {
	return lockAdvisory(ctx, q, "system_member/"+componentID)
}

// promoteSolePrimary makes a component's only remaining membership its default,
// when losing one left it without a default and with nothing to choose between.
// Takes the component's id directly (see addMemberTx).
func promoteSolePrimary(ctx context.Context, q txQuerier, componentID string) error {
	if _, err := q.Exec(ctx, `
		update system_member set is_primary = true, updated_at = now()
		where component_id = $1::uuid
		  and not exists (select 1 from system_member p
		                  where p.component_id = $1::uuid and p.is_primary)
		  and (select count(*) from system_member c
		       where c.component_id = $1::uuid) = 1`,
		componentID); err != nil {
		return fmt.Errorf("storage: promote sole primary: %w", err)
	}
	return nil
}

// SetPrimaryMember moves the default to this membership. The move is one
// statement per side inside one transaction, so there is never a moment with two
// defaults (which the partial unique index would refuse) or none.
func (p *PG) SetPrimaryMember(ctx context.Context, actorID, systemName, componentName string, write scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin set primary member: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	systemID, componentID, err := p.resolveMembershipEnds(ctx, tx, systemName, componentName, write)
	if err != nil {
		return err
	}
	if err := lockMemberComponent(ctx, tx, componentID); err != nil {
		return err
	}
	// Clear first: the index permits at most one primary per component, so the old
	// one has to go before the new one lands.
	if _, err := tx.Exec(ctx, `
		update system_member set is_primary = false, updated_at = now()
		where component_id = $1::uuid and is_primary
		  and system_id <> $2::uuid`,
		componentID, systemID); err != nil {
		return fmt.Errorf("storage: clear primary member: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		update system_member set is_primary = true, updated_at = now()
		where component_id = $1::uuid
		  and system_id = $2::uuid`,
		componentID, systemID)
	if err != nil {
		return fmt.Errorf("storage: set primary member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMemberNotFound
	}
	// Re-defaulting is the purest case of the membership write path: nothing
	// about the component or its systems changed except WHICH one answers a
	// question asked without a system in hand, and that is exactly what
	// .SystemTypeLabel reads (#685).
	if err := p.cascadeComponentLabel(ctx, tx, componentID); err != nil {
		return err
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "system_member", systemID, nil,
		map[string]string{"system": systemName, "component": componentName, "primary": "true"}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit set primary member: %w", err)
	}
	return nil
}

// ListMembers returns the components bound into a system, ordered by name.
func (p *PG) ListMembers(ctx context.Context, systemName string, read scope.Set) ([]Member, error) {
	sys, err := scopedGet(ctx, p, systemConfig, systemName, read)
	if err != nil {
		return nil, err
	}
	return p.membersWhere(ctx, `m.system_id = $1::uuid order by c.name`, sys.ID)
}

// ComponentMemberships returns the systems a component is bound into, ordered by
// name. This is the many-valued direction, and the one the old single pointer
// could not express: a shared device answers with every system it serves.
func (p *PG) ComponentMemberships(ctx context.Context, componentName string, read scope.Set) ([]Member, error) {
	c, err := scopedGet(ctx, p, componentConfig, componentName, read)
	if err != nil {
		return nil, err
	}
	return p.membersWhere(ctx, `m.component_id = $1::uuid order by s.name`, c.ID)
}

func (p *PG) membersWhere(ctx context.Context, where string, arg string) ([]Member, error) {
	rows, err := p.pool.Query(ctx, `select `+memberCols+memberFrom+` where `+where, arg)
	if err != nil {
		return nil, fmt.Errorf("storage: list members: %w", err)
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan member: %w", err)
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// resolveMembershipEnds checks both ends of the binding before it is written: the
// system must be in the caller's write scope (a non-disclosing not-found when it
// is not) and the component must exist. It returns both ids, resolved once here
// rather than left for AddMember/SetPrimaryMember to re-derive from the
// caller's name (ambiguous the moment two rows share it, #627); the scope
// check already loads the system row, so its id costs nothing extra.
//
// AddMember and SetPrimaryMember only: RemoveMember uses
// resolveMembershipEndsForRemoval instead (#627 review round 3), because the
// component it resolves must already BE a member, and estate-wide is the
// wrong set to judge that in. AddMember's own component is not yet a member
// (that is the point of adding it), so it cannot use a members-only resolve;
// its console callsite addresses the component by uuid now regardless (round
// 3's assign/add fix), where this estate-wide scopedByName's ambiguity branch
// cannot fire. SetPrimaryMember carries the same estate-wide-ambiguity shape
// RemoveMember had; left alone here, out of this round's scope (#645 named
// only unassign and remove).
func (p *PG) resolveMembershipEnds(ctx context.Context, q txQuerier, systemName, componentName string, write scope.Set) (systemID, componentID string, err error) {
	// scopedByNameInScope, not scopedByName-then-inScopeTree: ruling 2
	// (#627) requires ambiguity judged inside write, not estate-wide.
	sys, err := scopedByNameInScope(ctx, q, systemConfig, systemName, "system", write)
	if err != nil {
		return "", "", err // ErrSystemNotFound when absent or out of scope
	}
	// scopedByName, not scopedByNameInScope: write is resolved for "system"
	// here (the system check above), and a component's ancestor chain is
	// unrelated to it, so checking write against componentConfig could never
	// match (the tier-mismatch shape a review caught elsewhere).
	// withoutCandidates closes the disclosure an ambiguous component name
	// would otherwise carry: every matching uuid estate-wide, including ones
	// this system:update-only caller holds no component:read grant to see.
	c, err := scopedByName(ctx, q, componentConfig, componentName)
	if err != nil {
		return "", "", withoutCandidates(err) // ErrComponentNotFound when absent
	}
	return sys.ID, c.ID, nil
}

// resolveMembershipEndsForRemoval resolves the same pair resolveMembershipEnds
// does, but judges the component's name ambiguity within THIS system's current
// members, not estate-wide (#627 review round 3, closing #645 without a wire
// change): RemoveMember is naming a component it already believes is a member
// here, so a same-named component elsewhere in the estate that was never a
// member of this system was never actually a candidate. A name matching
// nothing anywhere is ErrComponentNotFound; a name matching something real
// that just is not a member here is ErrMemberNotFound, the same sentinel
// RemoveMember's own delete already falls back to when its statement affects
// zero rows, reused rather than duplicated.
func (p *PG) resolveMembershipEndsForRemoval(ctx context.Context, q txQuerier, systemName, componentName string, write scope.Set) (systemID, componentID string, err error) {
	sys, err := scopedByNameInScope(ctx, q, systemConfig, systemName, "system", write)
	if err != nil {
		return "", "", err
	}
	all, members, err := loadByRefWithin(ctx, q, componentConfig, componentName, func(id string) (bool, error) {
		var ok bool
		err := q.QueryRow(ctx, `select exists(
			select 1 from system_member
			where system_id = $1::uuid and component_id = $2::uuid)`,
			sys.ID, id).Scan(&ok)
		return ok, err
	})
	if err != nil {
		return "", "", err
	}
	c, err := resolveMatches(componentConfig, componentName, members)
	if err != nil {
		if errors.Is(err, ErrComponentNotFound) {
			if len(all) > 0 {
				return "", "", ErrMemberNotFound
			}
			return "", "", ErrComponentNotFound
		}
		return "", "", withoutCandidates(err)
	}
	return sys.ID, c.ID, nil
}
