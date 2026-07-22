package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
)

// SystemRole is a slot a system needs filled, declared either on a standard (and
// inherited by every conforming system) or directly on one system.
type SystemRole struct {
	ID           string
	OwnerKind    string // standard | system
	OwnerID      string // the standard id or the system name
	Name         string
	DisplayName  string
	Quorum       int
	Capabilities []string // the capability ids the role requires, all of them
	// Impact is what an impaired role means for its system: outage, degraded, or
	// none. It lives on the role because the same broken component matters
	// differently depending on the slot it was filling, and it is the only input
	// the rollup takes from the declaration side beyond the requirement itself.
	Impact    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SystemRoleSpec is the declaration input. Capabilities replace the required set
// wholesale on an update, matching how a product's capability set behaves.
type SystemRoleSpec struct {
	Name         string
	DisplayName  string
	Quorum       int
	Capabilities []string
	Impact       string // outage | degraded | none; empty means degraded
}

// EffectiveRole is one role resolved for a system: the declaration plus who fills
// it here. FromStandard distinguishes a role inherited from the system's standard
// from one declared directly on the system, so the surface can show which is which.
// Understaffed is Quorum minus the number of assignments, floored at zero.
type EffectiveRole struct {
	SystemRole
	FromStandard bool
	AssignedTo   []string // component names filling this role in this system
}

// Assigned reports how many components fill the role.
func (e EffectiveRole) Assigned() int { return len(e.AssignedTo) }

// Understaffed reports how many more components the role wants before its quorum
// is met. Zero means the role is adequately staffed.
func (e EffectiveRole) Understaffed() int {
	if short := e.Quorum - len(e.AssignedTo); short > 0 {
		return short
	}
	return 0
}

// Role sentinels. A capability shortfall is its own error so the API can report
// WHICH capabilities are missing: an operator cannot act on a bare refusal.
var (
	ErrRoleNotFound      = errors.New("storage: system role not found")
	ErrRoleExists        = errors.New("storage: a role with this name is already declared here")
	ErrRoleRefNotFound   = errors.New("storage: role references a missing owner or capability")
	ErrAssignmentMissing = errors.New("storage: role assignment not found")
	ErrRoleImpact        = errors.New("storage: unknown role impact")
)

// roleImpacts is the impact domain, mirroring the table's CHECK. Validating here
// turns a typo into a named refusal rather than a constraint violation.
var roleImpacts = map[string]bool{"outage": true, "degraded": true, "none": true}

// CapabilityShortfall is the assignment refusal: the component does not provide
// every capability the role requires. It names the gap so the caller can say so.
type CapabilityShortfall struct {
	Component string
	Role      string
	Missing   []string
}

func (e *CapabilityShortfall) Error() string {
	return fmt.Sprintf("storage: component %q cannot fill role %q, missing capability: %s",
		e.Component, e.Role, strings.Join(e.Missing, ", "))
}

// EffectiveCapabilities resolves what a component actually provides: the
// capabilities its product declares, plus the ones the component adds, minus the
// ones the component suppresses. A productless component resolves to just its own
// additions, which is what lets it be staffed at all.
//
// This is the set the assignment guard checks, so it is the single definition of
// "what this component can do" for the whole platform.
// It aggregates to a single row so it runs on the narrow querier (which carries
// only QueryRow) and therefore works standalone or inside the assignment's
// transaction.
func (p *PG) EffectiveCapabilities(ctx context.Context, q querier, componentName string) ([]string, error) {
	var caps []string
	err := q.QueryRow(ctx, `
		select coalesce(array_agg(cap order by cap), '{}')
		from (
			-- what the product declares
			select pc.capability_id as cap
			from component c
			join product_capability pc on pc.product_id = c.product_id
			where c.name = $1
			union
			-- what the component adds on its own
			select cc.capability_id
			from component_capability cc
			where cc.component_id = $1 and cc.present
		) provided
		where cap not in (
			-- minus what the component suppresses
			select capability_id from component_capability
			where component_id = $1 and not present
		)`, componentName).Scan(&caps)
	if err != nil {
		return nil, fmt.Errorf("storage: effective capabilities %s: %w", componentName, err)
	}
	return caps, nil
}

// EffectiveRoles resolves the roles a system needs filled: those its standard
// declares (inherited) plus those declared directly on it (ad-hoc), each with its
// required capabilities and current assignments. A one-off system has only the
// ad-hoc arm. The system must be within the read scope; out of scope is the
// non-disclosing ErrSystemNotFound.
func (p *PG) EffectiveRoles(ctx context.Context, systemName string, read scope.Set) ([]EffectiveRole, error) {
	inScope, err := p.ownerInScope(ctx, p.pool, "system", systemName, read)
	if err != nil {
		return nil, err
	}
	if !inScope {
		return nil, ErrSystemNotFound
	}
	rows, err := p.pool.Query(ctx, `
		with sys as (
			select name, standard_id from system where name = $1
		),
		roles as (
			-- inherited: declared on the standard this system conforms to
			select r.*, true as from_standard
			from sys join system_role r on r.owner_kind = 'standard' and r.standard_id = sys.standard_id
			union all
			-- ad-hoc: declared directly on this system
			select r.*, false as from_standard
			from sys join system_role r on r.owner_kind = 'system' and r.system_id = sys.name
		)
		select roles.id, roles.name, roles.display_name, roles.quorum, roles.impact, roles.from_standard,
		       roles.created_at, roles.updated_at,
		       coalesce(array_agg(distinct rc.capability_id) filter (where rc.capability_id is not null), '{}') as caps,
		       coalesce(array_agg(distinct ra.component_id) filter (where ra.component_id is not null), '{}') as assigned
		from roles
		left join role_capability rc on rc.role_id = roles.id
		left join role_assignment ra on ra.role_id = roles.id and ra.system_id = $1
		group by roles.id, roles.name, roles.display_name, roles.quorum, roles.impact, roles.from_standard,
		         roles.created_at, roles.updated_at
		order by roles.name`, systemName)
	if err != nil {
		return nil, fmt.Errorf("storage: effective roles %s: %w", systemName, err)
	}
	defer rows.Close()

	var out []EffectiveRole
	for rows.Next() {
		var e EffectiveRole
		if err := rows.Scan(&e.ID, &e.Name, &e.DisplayName, &e.Quorum, &e.Impact, &e.FromStandard,
			&e.CreatedAt, &e.UpdatedAt, &e.Capabilities, &e.AssignedTo); err != nil {
			return nil, fmt.Errorf("storage: scan effective role: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AssignRole binds a component to a role in a system, refusing the assignment when
// the component does not provide every capability the role requires. The guard is
// the point of the capability model: it reports the missing capabilities by name so
// the operator can fix the component or pick a different one.
//
// Idempotent: assigning the same component to the same role twice is a no-op.
func (p *PG) AssignRole(ctx context.Context, actorID, systemName, roleName, componentName string, write scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin assign role: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inScope, err := p.ownerInScope(ctx, tx, "system", systemName, write)
	if err != nil {
		return err
	}
	if !inScope {
		return ErrSystemNotFound
	}

	roleID, required, err := p.resolveRole(ctx, tx, systemName, roleName)
	if err != nil {
		return err
	}
	// Confirm the component exists before judging what it provides. An absent
	// component resolves to an empty capability set, which would otherwise be
	// reported as "missing everything": a capability shortfall for a component that
	// is not there is a confusing answer to a simple typo.
	if _, err := scopedByName(ctx, tx, componentConfig, componentName); err != nil {
		return err // ErrComponentNotFound when absent
	}
	provided, err := p.EffectiveCapabilities(ctx, tx, componentName)
	if err != nil {
		return err
	}
	if missing := missingCapabilities(required, provided); len(missing) > 0 {
		return &CapabilityShortfall{Component: componentName, Role: roleName, Missing: missing}
	}

	// Staffing a role IS membership, so the binding is created here rather than
	// asked of the operator as a separate step. A component filling a job in a
	// system that the system does not count as a member is the contradiction
	// system_member exists to make impossible.
	if err := addMemberTx(ctx, tx, systemName, componentName); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into role_assignment (system_id, role_id, component_id)
		values ($1, $2, $3)
		on conflict (system_id, role_id, component_id) do nothing`,
		systemName, roleID, componentName); err != nil {
		return mapRoleWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "role_assignment", roleID, nil,
		map[string]string{"system": systemName, "role": roleName, "component": componentName}); err != nil {
		return err
	}
	// Staffing changes health: the role may have just reached quorum. The system is
	// named explicitly rather than left to the component's assignments, so assign
	// and unassign take the same path.
	if err := p.recomputeChain(ctx, tx, []string{componentName}, []string{systemName}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit assign role: %w", err)
	}
	return nil
}

// UnassignRole removes a component from a role, returning ErrAssignmentMissing
// when it was not filling it.
func (p *PG) UnassignRole(ctx context.Context, actorID, systemName, roleName, componentName string, write scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin unassign role: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inScope, err := p.ownerInScope(ctx, tx, "system", systemName, write)
	if err != nil {
		return err
	}
	if !inScope {
		return ErrSystemNotFound
	}
	roleID, _, err := p.resolveRole(ctx, tx, systemName, roleName)
	if err != nil {
		return err
	}
	var id string
	if err := tx.QueryRow(ctx, `
		delete from role_assignment
		where system_id = $1 and role_id = $2 and component_id = $3
		returning id`, systemName, roleID, componentName).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAssignmentMissing
		}
		return fmt.Errorf("storage: unassign role: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "role_assignment", id, nil, nil); err != nil {
		return err
	}
	// The assignment row is already gone, so walking the component's assignments
	// would no longer reach this system. Naming it is what makes the drop visible.
	if err := p.recomputeChain(ctx, tx, []string{componentName}, []string{systemName}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit unassign role: %w", err)
	}
	return nil
}

// resolveRole finds a role by name for a system, looking at both arcs (the
// system's own roles and those its standard declares), and returns its id and the
// capabilities it requires.
func (p *PG) resolveRole(ctx context.Context, q querier, systemName, roleName string) (string, []string, error) {
	var (
		id   string
		caps []string
	)
	err := q.QueryRow(ctx, `
		with sys as (select name, standard_id from system where name = $1)
		select r.id,
		       coalesce(array_agg(rc.capability_id) filter (where rc.capability_id is not null), '{}')
		from sys
		join system_role r
		     on (r.owner_kind = 'system' and r.system_id = sys.name)
		     or (r.owner_kind = 'standard' and r.standard_id = sys.standard_id)
		left join role_capability rc on rc.role_id = r.id
		where r.name = $2
		group by r.id`, systemName, roleName).Scan(&id, &caps)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, ErrRoleNotFound
	} else if err != nil {
		return "", nil, fmt.Errorf("storage: resolve role %s/%s: %w", systemName, roleName, err)
	}
	return id, caps, nil
}

// missingCapabilities returns the required capabilities the provided set does not
// cover, in the order they were required, so the refusal reads predictably.
func missingCapabilities(required, provided []string) []string {
	have := make(map[string]bool, len(provided))
	for _, c := range provided {
		have[c] = true
	}
	var missing []string
	for _, c := range required {
		if !have[c] {
			missing = append(missing, c)
		}
	}
	return missing
}

func mapRoleWriteErr(err error) error {
	if isUniqueViolation(err) {
		return ErrRoleExists
	}
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) && pgErr.SQLState() == "23503" {
		return ErrRoleRefNotFound
	}
	return fmt.Errorf("storage: role write: %w", err)
}

// ComponentCapabilities is the Gateway-facing EffectiveCapabilities: the same
// resolved set, on the pool, for callers outside a transaction.
func (p *PG) ComponentCapabilities(ctx context.Context, componentName string) ([]string, error) {
	return p.EffectiveCapabilities(ctx, p.pool, componentName)
}
