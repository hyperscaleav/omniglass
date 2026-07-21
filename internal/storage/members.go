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
type Member struct {
	ID          string
	SystemID    string
	ComponentID string
	IsPrimary   bool
}

const memberCols = `id, system_id, component_id, is_primary`

func scanMember(row pgx.Row) (*Member, error) {
	var m Member
	if err := row.Scan(&m.ID, &m.SystemID, &m.ComponentID, &m.IsPrimary); err != nil {
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

	if err := p.resolveMembershipEnds(ctx, tx, systemName, componentName, write); err != nil {
		return err
	}
	if err := addMemberTx(ctx, tx, systemName, componentName); err != nil {
		return err
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "system_member", systemName, nil,
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
// rather than making the operator say it twice.
func addMemberTx(ctx context.Context, q txQuerier, systemName, componentName string) error {
	// The primary is decided by the row's own absence of competition: it is the
	// default only when this component has no membership yet. Doing it in the
	// insert keeps "first one wins" true even when two rooms claim a component at
	// the same moment, since the partial unique index refuses the second.
	if _, err := q.Exec(ctx, `
		insert into system_member (system_id, component_id, is_primary)
		select $1, $2, not exists (select 1 from system_member where component_id = $2)
		on conflict (system_id, component_id) do nothing`,
		systemName, componentName); err != nil {
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

	if err := p.resolveMembershipEnds(ctx, tx, systemName, componentName, write); err != nil {
		return err
	}
	var staffing int
	if err := tx.QueryRow(ctx, `
		select count(*) from role_assignment where system_id = $1 and component_id = $2`,
		systemName, componentName).Scan(&staffing); err != nil {
		return fmt.Errorf("storage: count member roles: %w", err)
	}
	if staffing > 0 {
		return ErrMemberOccupied
	}
	tag, err := tx.Exec(ctx, `delete from system_member where system_id = $1 and component_id = $2`,
		systemName, componentName)
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
	if err := promoteSolePrimary(ctx, tx, componentName); err != nil {
		return err
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "system_member", systemName,
		map[string]string{"system": systemName, "component": componentName}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit remove member: %w", err)
	}
	return nil
}

// promoteSolePrimary makes a component's only remaining membership its default,
// when losing one left it without a default and with nothing to choose between.
func promoteSolePrimary(ctx context.Context, q txQuerier, componentName string) error {
	if _, err := q.Exec(ctx, `
		update system_member set is_primary = true, updated_at = now()
		where component_id = $1
		  and not exists (select 1 from system_member p where p.component_id = $1 and p.is_primary)
		  and (select count(*) from system_member c where c.component_id = $1) = 1`,
		componentName); err != nil {
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

	if err := p.resolveMembershipEnds(ctx, tx, systemName, componentName, write); err != nil {
		return err
	}
	// Clear first: the index permits at most one primary per component, so the old
	// one has to go before the new one lands.
	if _, err := tx.Exec(ctx, `
		update system_member set is_primary = false, updated_at = now()
		where component_id = $1 and is_primary and system_id <> $2`,
		componentName, systemName); err != nil {
		return fmt.Errorf("storage: clear primary member: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		update system_member set is_primary = true, updated_at = now()
		where component_id = $1 and system_id = $2`,
		componentName, systemName)
	if err != nil {
		return fmt.Errorf("storage: set primary member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrMemberNotFound
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "system_member", systemName, nil,
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
	if _, err := scopedGet(ctx, p, systemConfig, systemName, read); err != nil {
		return nil, err
	}
	return p.membersWhere(ctx, `system_id = $1 order by component_id`, systemName)
}

// ComponentMemberships returns the systems a component is bound into, ordered by
// name. This is the many-valued direction, and the one the old single pointer
// could not express: a shared device answers with every system it serves.
func (p *PG) ComponentMemberships(ctx context.Context, componentName string, read scope.Set) ([]Member, error) {
	if _, err := scopedGet(ctx, p, componentConfig, componentName, read); err != nil {
		return nil, err
	}
	return p.membersWhere(ctx, `component_id = $1 order by system_id`, componentName)
}

func (p *PG) membersWhere(ctx context.Context, where string, arg string) ([]Member, error) {
	rows, err := p.pool.Query(ctx, `select `+memberCols+` from system_member where `+where, arg)
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
// is not) and the component must exist.
func (p *PG) resolveMembershipEnds(ctx context.Context, q txQuerier, systemName, componentName string, write scope.Set) error {
	inScope, err := p.ownerInScope(ctx, q, "system", systemName, write)
	if err != nil {
		return err
	}
	if !inScope {
		return ErrSystemNotFound
	}
	if _, err := scopedByName(ctx, q, componentConfig, componentName); err != nil {
		return err // ErrComponentNotFound when absent
	}
	return nil
}
