package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// strandedAssignment is one assignment a standard change has to settle: an
// occupant of a role the system no longer has, because the role belongs to
// the standard it is leaving.
type strandedAssignment struct {
	ID            string
	RoleID        string
	RoleName      string
	Standard      string
	ComponentID   string
	ComponentName string
	Position      int
}

// carryTarget is the role a stranded assignment's role name means on the new
// standard, with what the guard and the capacity need to judge each occupant.
type carryTarget struct {
	ID             string
	Label          string
	Capacity       *int
	AcceptedTypes  []roleRef
	PinnedProducts []roleRef
	// Taken holds the positions already occupied in the target role, the
	// carried ones included as they land.
	Taken map[int]bool
}

// carryStaffing settles a system's staffing after its standard moved (#506,
// ADR-0506), inside the change's transaction, after the system's row is
// updated and before its health recomputes. Every assignment whose role is
// neither the system's own nor its current standard's is either carried or
// released:
//
//   - carried onto the role of the same name the new standard declares, when
//     the component is still a typed match for it (the guard runs again) and
//     the role has room under its capacity, lowest positions first. It keeps
//     its position when that is free and within the capacity, and takes the
//     lowest free one otherwise. The row is moved, not replaced, so its id and
//     its history hold, and the move is audited as an update.
//   - released otherwise (no such role, a component that no longer fits, an
//     occupant past the capacity), audited as a delete. A released component
//     stays a member: giving up a role never ends membership, so it can be
//     unbound, restaffed or deleted at once.
//
// A name means one role in a system (ADR-0503), so the match by name cannot
// restaff onto the wrong role. standardID is the new standard, nil for a
// one-off system, which releases every inherited assignment.
//
// The system's row is already held for update by the caller, which is what
// keeps a staffing write out (lockSystemForStaffing); the stranded rows are
// taken for update and the target role for key share, so a concurrent
// withdrawal of either waits rather than leaving the carry pointing at a row
// that is gone.
func (p *PG) carryStaffing(ctx context.Context, tx pgx.Tx, actorID, systemID, systemName string, standardID *string) error {
	stranded, err := loadStrandedAssignments(ctx, tx, systemID)
	if err != nil || len(stranded) == 0 {
		return err
	}
	var newStandard string
	if standardID != nil {
		if err := tx.QueryRow(ctx, `select name from standard where id = $1::uuid`, *standardID).Scan(&newStandard); err != nil {
			return fmt.Errorf("storage: carry standard %s: %w", *standardID, err)
		}
	}
	targets := map[string]*carryTarget{}
	for _, a := range stranded {
		target, seen := targets[a.RoleName]
		if !seen {
			if target, err = p.loadCarryTarget(ctx, tx, systemID, standardID, a.RoleName); err != nil {
				return err
			}
			targets[a.RoleName] = target
		}
		position, fits, err := p.carryPosition(ctx, tx, target, a)
		if err != nil {
			return err
		}
		if fits {
			if err := carryAssignment(ctx, tx, actorID, systemName, newStandard, target, a, position); err != nil {
				return err
			}
			continue
		}
		if err := releaseAssignment(ctx, tx, actorID, systemName, a); err != nil {
			return err
		}
	}
	return nil
}

// loadStrandedAssignments reads the assignments a standard change has to
// settle, ordered by role name and then position, which is the order the
// capacity is filled in. Taken for update, so a role withdrawal racing the
// change waits for it (and one that already committed has taken its rows).
func loadStrandedAssignments(ctx context.Context, tx pgx.Tx, systemID string) ([]strandedAssignment, error) {
	rows, err := tx.Query(ctx, `
		select ra.id, ra.role_id, r.name, coalesce(st.name, ''), ra.component_id, c.name, ra.position
		  from system_role_assignment ra
		  join system s on s.id = ra.system_id
		  join system_role r on r.id = ra.role_id
		  join component c on c.id = ra.component_id
		  left join standard st on st.id = r.standard_id
		 where ra.system_id = $1::uuid
		   and not (r.owner_kind = 'system' and r.system_id = s.id)
		   and not (r.owner_kind = 'standard' and r.standard_id is not distinct from s.standard_id)
		 order by r.name, ra.position
		   for update of ra`, systemID)
	if err != nil {
		return nil, fmt.Errorf("storage: stranded assignments %s: %w", systemID, err)
	}
	defer rows.Close()
	var out []strandedAssignment
	for rows.Next() {
		var a strandedAssignment
		if err := rows.Scan(&a.ID, &a.RoleID, &a.RoleName, &a.Standard, &a.ComponentID, &a.ComponentName, &a.Position); err != nil {
			return nil, fmt.Errorf("storage: scan stranded assignment: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// loadCarryTarget resolves the role a stranded role name means on the new
// standard, or nil when the system has no standard now or its standard does
// not declare the name. The role row is taken for key share so it cannot be
// withdrawn under the carry.
func (p *PG) loadCarryTarget(ctx context.Context, tx pgx.Tx, systemID string, standardID *string, roleName string) (*carryTarget, error) {
	if standardID == nil {
		return nil, nil
	}
	t := &carryTarget{Taken: map[int]bool{}}
	err := tx.QueryRow(ctx, `
		select id, coalesce(label, ''), capacity from system_role
		 where owner_kind = 'standard' and standard_id = $1::uuid and name = $2
		   for key share`, *standardID, roleName).Scan(&t.ID, &t.Label, &t.Capacity)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: carry target %s: %w", roleName, err)
	}
	if t.AcceptedTypes, t.PinnedProducts, err = roleSlot(ctx, tx, t.ID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `select position from system_role_assignment where system_id = $1::uuid and role_id = $2`, systemID, t.ID)
	if err != nil {
		return nil, fmt.Errorf("storage: carry target positions %s: %w", roleName, err)
	}
	defer rows.Close()
	for rows.Next() {
		var pos int
		if err := rows.Scan(&pos); err != nil {
			return nil, fmt.Errorf("storage: scan carry target position: %w", err)
		}
		t.Taken[pos] = true
	}
	return t, rows.Err()
}

// carryPosition decides whether a stranded occupant is carried onto target,
// and at which position. It is not carried when there is no target, when the
// target is full, or when the guard refuses the component; a shortfall here is
// the reason to release, not an error.
func (p *PG) carryPosition(ctx context.Context, tx pgx.Tx, target *carryTarget, a strandedAssignment) (position int, fits bool, err error) {
	if target == nil {
		return 0, false, nil
	}
	if target.Capacity != nil && len(target.Taken) >= *target.Capacity {
		return 0, false, nil
	}
	err = p.slotShortfall(ctx, tx, a.ComponentID, a.ComponentName, target.Label, target.AcceptedTypes, target.PinnedProducts)
	var typeShort *TypeShortfall
	var pinShort *ProductPinShortfall
	if errors.As(err, &typeShort) || errors.As(err, &pinShort) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return carriedPosition(a.Position, target.Capacity, target.Taken), true, nil
}

// carriedPosition is where a carried occupant lands: its own position when that
// is free and within the capacity, otherwise the lowest free one. The caller
// has already established there is room, so a free position within the
// capacity always exists.
func carriedPosition(own int, capacity *int, taken map[int]bool) int {
	within := func(n int) bool { return capacity == nil || n <= *capacity }
	if !taken[own] && within(own) {
		return own
	}
	n := 1
	for taken[n] {
		n++
	}
	return n
}

// carryAssignment moves one assignment onto the new standard's role, audited
// as an update of the assignment naming the role on both sides.
func carryAssignment(ctx context.Context, tx pgx.Tx, actorID, systemName, newStandard string, target *carryTarget, a strandedAssignment, position int) error {
	if _, err := tx.Exec(ctx, `update system_role_assignment set role_id = $2, position = $3 where id = $1`,
		a.ID, target.ID, position); err != nil {
		return mapRoleWriteErr(err)
	}
	target.Taken[position] = true
	return writeAuditRes(ctx, tx, actorID, "update", "system_role_assignment", a.ID,
		map[string]any{"system": systemName, "standard": a.Standard, "role": a.RoleName, "component": a.ComponentName, "position": a.Position},
		map[string]any{"system": systemName, "standard": newStandard, "role": a.RoleName, "component": a.ComponentName, "position": position})
}

// releaseAssignment drops one assignment, audited as a delete of it. The
// membership is untouched.
func releaseAssignment(ctx context.Context, tx pgx.Tx, actorID, systemName string, a strandedAssignment) error {
	if _, err := tx.Exec(ctx, `delete from system_role_assignment where id = $1`, a.ID); err != nil {
		return fmt.Errorf("storage: release assignment %s: %w", a.ID, err)
	}
	return writeAuditRes(ctx, tx, actorID, "delete", "system_role_assignment", a.ID,
		map[string]any{"system": systemName, "standard": a.Standard, "role": a.RoleName, "component": a.ComponentName, "position": a.Position}, nil)
}
