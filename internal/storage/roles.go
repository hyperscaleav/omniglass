package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SystemRole is a slot a system needs filled, declared either on a standard (and
// inherited by every conforming system) or directly on one system.
type SystemRole struct {
	ID          string
	OwnerKind   string // standard | system
	OwnerID     string // the standard id or the system name
	Name        string
	DisplayName string
	Quorum      int
	// AcceptedTypes is the component_type names the role's typed-slot guard
	// accepts (a component's product's component_type_id must fall within one
	// of these subtrees, self-inclusive); empty means any type. PinnedProducts
	// optionally narrows further to specific products within an accepted type;
	// empty means any product of an accepted type. This pair is what
	// AssignRole checks (#626); once assigned, an occupant satisfies the slot
	// unless its own verdict is outage (internal/health); a merely degraded
	// occupant still counts.
	AcceptedTypes  []string
	PinnedProducts []string
	// Capacity is the most components the role will accept, an optional upper
	// bound above Quorum's floor; nil means unbounded. A pointer so "omitted"
	// (leave whatever is already declared alone) is distinguishable from "an
	// explicit small number": SetSystemRole otherwise wholesale-replaces a
	// declaration on every write, which would silently reset capacity to
	// unbounded on any edit that does not resend it.
	Capacity *int
	// PositionLabels names each position within the role, by index (position 1
	// is PositionLabels[0]); empty means unlabeled. Unlike Capacity this
	// wholesale-replaces on every write, the same rule AcceptedTypes and
	// PinnedProducts already follow: a label set is a list, not a single bound,
	// and there is no "leave unchanged" reading for one.
	PositionLabels []string
	// Impact is what an impaired role means for its system: outage, degraded, or
	// none. It lives on the role because the same broken component matters
	// differently depending on the slot it was filling, and it is the only input
	// the rollup takes from the declaration side beyond the requirement itself.
	Impact string
	// AlternateID is the choice_alternate this role joins (#626); nil means
	// unconditional. Carried here purely so SetSystemRole and
	// DeleteSystemRole's audit before/after images (systemRoleCols) capture
	// a change to it; nothing on the read side (ListSystemRoles,
	// systemRoleBody) surfaces it yet, deliberately deferred to the
	// operator-facing read surface.
	AlternateID *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// SystemRoleSpec is the declaration input. AcceptedTypes, PinnedProducts, and
// PositionLabels each replace their value wholesale on an update; Capacity
// and AlternateID do not (nil leaves whatever is already declared alone, see
// SystemRole.Capacity and the AlternateID comment below).
type SystemRoleSpec struct {
	Name           string
	DisplayName    string
	Quorum         int
	AcceptedTypes  []string
	PinnedProducts []string
	Capacity       *int
	PositionLabels []string
	Impact         string // outage | degraded | none; empty means degraded
	// AlternateID joins this role to one alternate of a choice (#626): nil
	// means "the caller did not mention this field", so an update leaves
	// whatever is already declared alone, the same reading Capacity already
	// gets one field up. A pointer to "" is the explicit detach path
	// (unconditional, always folded directly by health.SystemVerdictWith); a
	// pointer to a choice_alternate.id joins that alternate. This is nil, a
	// pointer to empty, or a pointer to a value on purpose: a plain string
	// (the pre-#626-fix-round shape) cannot tell "omitted" apart from
	// "explicitly detach" once both would write NULL, and every write that
	// wholesale-replaced this field silently promoted a role from
	// conditional to mandatory on any edit that did not resend it, the exact
	// failure amendment 7.1 blocked on the DELETE path re-entering through
	// the UPDATE path. Set from a choice_alternate.id the caller already
	// resolved (SeedRoleChoice returns the ids its choice created, the API
	// layer resolves a "choice/alternate" wire reference through
	// PG.ResolveAlternate before building this spec), never from a raw name
	// the write itself would have to look up: the composite FK
	// (system_role_alternate_fk) is what refuses an id that belongs to a
	// different owner, mapped to ErrRoleRefNotFound the same way an unknown
	// accepted type or pinned product is.
	AlternateID *string
}

// EffectiveRole is one role resolved for a system: the declaration plus who fills
// it here. FromStandard distinguishes a role inherited from the system's standard
// from one declared directly on the system, so the surface can show which is which.
// Understaffed is Quorum minus the number of assignments, floored at zero.
type EffectiveRole struct {
	SystemRole
	FromStandard bool
	AssignedTo   []string // component names filling this role in this system
	// Positions is AssignedTo's own 1-based position, index for index: a
	// role's occupants persist gaps (an unassign never compacts, #626), so
	// AssignedTo[i]'s real position is Positions[i], never i+1. A caller that
	// needs to address a specific occupant's slot (SwapPositions) must read
	// this rather than assume the array is dense.
	Positions []int
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

// Role sentinels. A typed-slot refusal is its own error so the API can name
// WHICH type or product is wanted: an operator cannot act on a bare refusal.
var (
	ErrRoleNotFound      = errors.New("storage: system role not found")
	ErrRoleExists        = errors.New("storage: a role with this name is already declared here")
	ErrRoleRefNotFound   = errors.New("storage: role references a missing owner, type, or product")
	ErrAssignmentMissing = errors.New("storage: role assignment not found")
	ErrRoleImpact        = errors.New("storage: unknown role impact")
	// ErrComponentAlreadyStaffed is the constraint-violation fallback for the
	// one-role-per-component rule (#626): AssignRole's own pre-check normally
	// catches this first and returns the richer ComponentStaffedShortfall, so
	// this sentinel only fires under a genuine race the pre-check's snapshot
	// missed.
	ErrComponentAlreadyStaffed = errors.New("storage: component already fills a different role in this system")
	// ErrRolePositionTaken is the constraint-violation fallback for a position
	// race: two concurrent assignments (or a swap) computed the same slot.
	ErrRolePositionTaken = errors.New("storage: that position is already taken")
	// ErrCapacityBelowQuorum is the declaration-alone refusal (422): a capacity
	// under the role's own quorum can never be satisfied by any staffing.
	ErrCapacityBelowQuorum = errors.New("storage: capacity must be at least the role's quorum")
)

// roleImpacts is the impact domain, mirroring the table's CHECK. Validating here
// turns a typo into a named refusal rather than a constraint violation.
var roleImpacts = map[string]bool{"outage": true, "degraded": true, "none": true}

// TypeShortfall is the assignment refusal when the component's product's
// component_type is not within the subtree of any type the role accepts. It
// names both parties in operator vocabulary: what the component actually is,
// and what the role wants, so the refusal is something an operator can act on.
type TypeShortfall struct {
	Component     string
	ComponentType string
	Role          string
	WantTypes     []string
}

func (e *TypeShortfall) Error() string {
	return fmt.Sprintf("storage: component %q is a %s; role %q wants a %s",
		e.Component, e.ComponentType, e.Role, strings.Join(e.WantTypes, " or a "))
}

// ProductPinShortfall is the assignment refusal when the role pins the slot to
// specific products and the component's product is not one of them: the
// component is the right type, just not one of the named products.
type ProductPinShortfall struct {
	Component        string
	ComponentProduct string
	Role             string
	WantProducts     []string
}

func (e *ProductPinShortfall) Error() string {
	return fmt.Sprintf("storage: component %q is product %q; role %q wants product %s",
		e.Component, e.ComponentProduct, e.Role, strings.Join(e.WantProducts, " or "))
}

// ComponentStaffedShortfall is the assignment refusal when the component
// already fills a DIFFERENT role in the same system: #626 permits a
// component at most one role per system, so this names which role it
// already holds rather than leaving the operator to guess. Assigning it to
// the SAME role it already holds is unaffected (idempotent, as before).
type ComponentStaffedShortfall struct {
	Component string
	HeldRole  string
	System    string
}

func (e *ComponentStaffedShortfall) Error() string {
	return fmt.Sprintf("storage: component %q already fills %q in %q; a component fills at most one role per system",
		e.Component, e.HeldRole, e.System)
}

// CapacityShortfall is the declaration refusal when lowering a role's
// capacity would put it below the number of components some conforming
// system already has assigned to it. A standard-owned role is staffed
// independently in every conforming system (assignments carry both role_id
// and system_id), so the pre-check refuses if ANY conforming system would
// exceed the new cap, naming whichever one exceeds it by the widest margin.
type CapacityShortfall struct {
	Role   string
	System string
	Have   int
	Want   int
}

func (e *CapacityShortfall) Error() string {
	return fmt.Sprintf("storage: role %q in system %q is filled by %d components; capacity cannot be lowered to %d",
		e.Role, e.System, e.Have, e.Want)
}

// CapacityFullShortfall is the assignment refusal when a role already holds
// as many components as its declared capacity allows. It is checked
// explicitly, before a position is even computed, rather than left to
// emerge from a position collision: a collision only reliably happens when
// positions are contiguous from 1 to capacity, and even then it names a
// position, not the cap, leaving an operator to go move an occupant that
// was never the problem. A gap below capacity (an unassign, or a capacity
// lowered below the row count that created it) lets the position search
// find a free slot within the gap and silently exceed the cap instead of
// colliding at all, so the explicit count is the only reliable enforcement.
type CapacityFullShortfall struct {
	Role     string
	System   string
	Have     int
	Capacity int
}

func (e *CapacityFullShortfall) Error() string {
	return fmt.Sprintf("storage: role %q in system %q already holds %d of its %d-component capacity",
		e.Role, e.System, e.Have, e.Capacity)
}

// EffectiveRoles resolves the roles a system needs filled: those its standard
// declares (inherited) plus those declared directly on it (ad-hoc), each with
// its typed-slot requirement and current assignments. A one-off system has
// only the ad-hoc arm. The system must be within the read scope; out of scope
// is the non-disclosing ErrSystemNotFound.
func (p *PG) EffectiveRoles(ctx context.Context, systemName string, read scope.Set) ([]EffectiveRole, error) {
	// Resolved once, here, rather than through ownerInScope (which does the
	// same lookup but discards the id): the query below binds sys.ID instead
	// of re-deriving it from systemName, which #627 no longer guarantees is
	// unique.
	sys, err := scopedByName(ctx, p.pool, systemConfig, systemName)
	if err != nil {
		return nil, err
	}
	inScope, err := inScopeTree(ctx, p.pool, systemTable, sys.ID, read)
	if err != nil {
		return nil, err
	}
	if !inScope {
		return nil, ErrSystemNotFound
	}
	// Correlated subqueries, not a GROUP BY over three LEFT JOINs: this query
	// joins types, products AND assignments in one shot, so a GROUP BY makes
	// assignment rows fan out once per (type, product) pair, and DISTINCT
	// (the previous shape) hid that cartesian product at the cost of losing
	// order. array_agg(distinct ... order by ra.position) is rejected by
	// Postgres outright (the ORDER BY expression must appear in the DISTINCT
	// argument list), so the assigned array is resolved as its own subquery,
	// scoped to this system, ordered by the occupant's position (#626).
	rows, err := p.pool.Query(ctx, `
		with sys as (
			select id, name, standard_id from system where id = $1::uuid
		),
		roles as (
			-- inherited: declared on the standard this system conforms to
			select r.*, true as from_standard
			from sys join system_role r on r.owner_kind = 'standard' and r.standard_id = sys.standard_id
			union all
			-- ad-hoc: declared directly on this system
			select r.*, false as from_standard
			from sys join system_role r on r.owner_kind = 'system' and r.system_id = sys.id
		)
		select roles.id, roles.name, roles.display_name, roles.quorum, roles.capacity, roles.position_labels,
		       roles.impact, roles.from_standard, roles.created_at, roles.updated_at,
		       coalesce((select array_agg(ct.name order by ct.name)
		                   from system_role_type rt join component_type ct on ct.id = rt.component_type_id
		                  where rt.role_id = roles.id), '{}') as types,
		       coalesce((select array_agg(pr.name order by pr.name)
		                   from system_role_product rp join product pr on pr.id = rp.product_id
		                  where rp.role_id = roles.id), '{}') as products,
		       coalesce((select array_agg(ac.name order by ra.position)
		                   from system_role_assignment ra join component ac on ac.id = ra.component_id
		                  where ra.role_id = roles.id and ra.system_id = sys.id), '{}') as assigned,
		       coalesce((select array_agg(ra.position order by ra.position)
		                   from system_role_assignment ra
		                  where ra.role_id = roles.id and ra.system_id = sys.id), '{}') as positions
		from roles, sys
		order by roles.name`, sys.ID)
	if err != nil {
		return nil, fmt.Errorf("storage: effective roles %s: %w", systemName, err)
	}
	defer rows.Close()

	var out []EffectiveRole
	for rows.Next() {
		var e EffectiveRole
		if err := rows.Scan(&e.ID, &e.Name, &e.DisplayName, &e.Quorum, &e.Capacity, &e.PositionLabels,
			&e.Impact, &e.FromStandard, &e.CreatedAt, &e.UpdatedAt,
			&e.AcceptedTypes, &e.PinnedProducts, &e.AssignedTo, &e.Positions); err != nil {
			return nil, fmt.Errorf("storage: scan effective role: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AssignRole binds a component to a role in a system, refusing the assignment
// when the component is not a typed match: its product's component_type must
// fall within the subtree of a type the role accepts (any type, if the role
// accepts none), and if the role pins specific products, the component's
// product must be one of them. The guard is the point of the typed-slot
// model: it names both parties (what the component is, what the role wants)
// so the operator can act on the refusal rather than guess.
//
// Idempotent: assigning the same component to the same role twice is a no-op.
func (p *PG) AssignRole(ctx context.Context, actorID, systemName, roleName, componentName string, write scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin assign role: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// system and component are each resolved once here, inside this
	// transaction, and their ids bound everywhere below: under scoped name
	// uniqueness (#627) a second name-lookup for either could land on a
	// different row sharing the same name, or fail outright with SQLSTATE
	// 21000 ("more than one row returned by a subquery used as an
	// expression").
	sys, err := scopedByName(ctx, tx, systemConfig, systemName)
	if err != nil {
		return err // ErrSystemNotFound when absent
	}
	inScope, err := inScopeTree(ctx, tx, systemTable, sys.ID, write)
	if err != nil {
		return err
	}
	if !inScope {
		return ErrSystemNotFound
	}

	roleID, roleDisplay, acceptedTypes, pinnedProducts, err := p.resolveRole(ctx, tx, sys.ID, roleName)
	if err != nil {
		return err
	}
	// Position allocation reads and acts on the same (system, role) staffing
	// snapshot it is about to change, so it takes this role's advisory lock
	// for the rest of the transaction (#626): two concurrent assigns must
	// not compute the same next-free position, and UnassignRole and
	// SwapPositions take the same key, so allocation and reordering
	// serialize against each other too.
	if err := lockAdvisory(ctx, tx, rolePositionLockKey(sys.ID, roleID)); err != nil {
		return err
	}
	// Confirm the component exists before judging what it is. An absent
	// component has no product to classify, which would otherwise surface as a
	// confusing type refusal for what is really a typo.
	component, err := scopedByName(ctx, tx, componentConfig, componentName)
	if err != nil {
		return err // ErrComponentNotFound when absent
	}
	cls, err := p.classifyComponent(ctx, tx, component.ID)
	if err != nil {
		return err
	}
	if len(acceptedTypes) > 0 {
		within := false
		for _, at := range acceptedTypes {
			ok, err := p.TypeIsWithin(ctx, cls.TypeID, at.ID)
			if err != nil {
				return err
			}
			if ok {
				within = true
				break
			}
		}
		if !within {
			return &TypeShortfall{
				Component: componentName, ComponentType: cls.TypeName,
				Role: roleDisplay, WantTypes: roleRefNames(acceptedTypes),
			}
		}
	}
	if len(pinnedProducts) > 0 {
		pinned := false
		for _, pp := range pinnedProducts {
			if pp.ID == cls.ProductID {
				pinned = true
				break
			}
		}
		if !pinned {
			return &ProductPinShortfall{
				Component: componentName, ComponentProduct: cls.ProductName,
				Role: roleDisplay, WantProducts: roleRefNames(pinnedProducts),
			}
		}
	}

	// A component fills at most one role per system (#626). This pre-check runs
	// ahead of the unique index so the refusal can name BOTH parties: the
	// component and the role it already holds, which a constraint name alone
	// cannot do. Excluded is the target role itself, so re-assigning to the
	// role a component already holds stays idempotent.
	var heldRole string
	err = tx.QueryRow(ctx, `
		select r.display_name from system_role_assignment ra
		join system_role r on r.id = ra.role_id
		where ra.system_id = $1::uuid
		  and ra.component_id = $2::uuid
		  and ra.role_id <> $3`, sys.ID, component.ID, roleID).Scan(&heldRole)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// not staffed elsewhere in this system
	case err != nil:
		return fmt.Errorf("storage: check existing staffing %s/%s: %w", systemName, componentName, err)
	default:
		return &ComponentStaffedShortfall{Component: componentName, HeldRole: heldRole, System: systemName}
	}

	// Capacity is enforced here explicitly, counting assignment rows for
	// this (system, role): with a component fully staffed at most once per
	// system (the constraint above) and its position column unique per
	// (system, role, position) and NOT NULL, a row, an occupied position,
	// and a distinct component all count the same thing here, so this is
	// the same count SetSystemRole's lowering pre-check already makes.
	// Skipped when the component already holds this exact role, so a full
	// role's existing occupants stay idempotent.
	var alreadyHere bool
	if err := tx.QueryRow(ctx, `select exists (
		select 1 from system_role_assignment
		 where system_id = $1::uuid
		   and role_id = $2 and component_id = $3::uuid)`,
		sys.ID, roleID, component.ID).Scan(&alreadyHere); err != nil {
		return fmt.Errorf("storage: check existing assignment %s/%s: %w", systemName, roleID, err)
	}
	if !alreadyHere {
		var capacity *int
		var have int
		if err := tx.QueryRow(ctx, `
			select r.capacity, (
				select count(*) from system_role_assignment ra
				 where ra.system_id = $1::uuid and ra.role_id = $2
			) from system_role r where r.id = $2`, sys.ID, roleID).Scan(&capacity, &have); err != nil {
			return fmt.Errorf("storage: role capacity check %s/%s: %w", systemName, roleID, err)
		}
		if capacity != nil && have >= *capacity {
			return &CapacityFullShortfall{Role: roleDisplay, System: systemName, Have: have, Capacity: *capacity}
		}
	}

	// Staffing a role IS membership, so the binding is created here rather than
	// asked of the operator as a separate step. A component filling a job in a
	// system that the system does not count as a member is the contradiction
	// system_member exists to make impossible.
	if err := addMemberTx(ctx, tx, sys.ID, component.ID); err != nil {
		return err
	}
	// Next free position: the lowest unused positive integer within
	// (system, role), capped at capacity when the role declares one. Not
	// max(position)+1: TestUnassignLeavesGapThenRefills requires a vacated
	// slot to be reused rather than growing without bound while the
	// occupant count stays under capacity.
	//
	// The search space is bounded by count+1 (pigeonhole: with n existing
	// occupants there are at most n gaps below the maximum, so a free slot
	// always exists at or before position n+1), further capped at capacity
	// when the role declares one. This is what keeps generate_series small
	// (proportional to the role's own occupancy, never a materialized
	// billion-row series for an unbounded role) while still finding a gap
	// rather than skipping past it. At a role already full to its capacity,
	// count+1 exceeds capacity, so the bound collapses to capacity itself;
	// every slot in [1, capacity] is occupied, nothing is found free, and
	// this resolves to position 1 (occupied), which then collides on the
	// deferrable uniqueness constraint below and is refused as
	// ErrRolePositionTaken rather than silently exceeding the declared cap.
	var position int
	if err := tx.QueryRow(ctx, `
		with cnt as (
			select count(*) as n from system_role_assignment
			 where system_id = $1::uuid and role_id = $2
		)
		select coalesce(min(g.n), 1)
		  from cnt, generate_series(1, least(coalesce((select capacity from system_role where id = $2), cnt.n + 1), cnt.n + 1)) g(n)
		 where not exists (
		     select 1 from system_role_assignment ra
		      where ra.system_id = $1::uuid
		        and ra.role_id = $2 and ra.position = g.n)`,
		sys.ID, roleID).Scan(&position); err != nil {
		return fmt.Errorf("storage: next free position %s/%s: %w", systemName, roleID, err)
	}
	if _, err := tx.Exec(ctx, `
		insert into system_role_assignment (system_id, role_id, component_id, position)
		values ($1::uuid, $2, $3::uuid, $4)
		on conflict (system_id, role_id, component_id) do nothing`,
		sys.ID, roleID, component.ID, position); err != nil {
		return mapRoleWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "system_role_assignment", roleID, nil,
		map[string]string{"system": systemName, "role": roleName, "component": componentName}); err != nil {
		return err
	}
	// Staffing changes health: the role may have just reached quorum. The system is
	// named explicitly rather than left to the component's assignments, so assign
	// and unassign take the same path.
	if err := p.recomputeChain(ctx, tx, []ownerRef{{ID: component.ID, Name: component.Name}}, []ownerRef{{ID: sys.ID, Name: sys.Name}}, nil); err != nil {
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

	// Resolved once (see AssignRole's comment): every statement below binds an
	// id rather than re-deriving one from a name.
	sys, err := scopedByName(ctx, tx, systemConfig, systemName)
	if err != nil {
		return err
	}
	inScope, err := inScopeTree(ctx, tx, systemTable, sys.ID, write)
	if err != nil {
		return err
	}
	if !inScope {
		return ErrSystemNotFound
	}
	roleID, _, _, _, err := p.resolveRole(ctx, tx, sys.ID, roleName)
	if err != nil {
		return err
	}
	component, err := scopedByName(ctx, tx, componentConfig, componentName)
	if err != nil {
		return err
	}
	// Same key AssignRole and SwapPositions take: an unassign vacating a
	// position must serialize against a concurrent assign that could
	// otherwise compute the same now-free slot as its "next free" before
	// this transaction's delete commits.
	if err := lockAdvisory(ctx, tx, rolePositionLockKey(sys.ID, roleID)); err != nil {
		return err
	}
	var assignmentID string
	if err := tx.QueryRow(ctx, `
		delete from system_role_assignment
		where system_id = $1::uuid and role_id = $2
		  and component_id = $3::uuid
		returning id`, sys.ID, roleID, component.ID).Scan(&assignmentID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAssignmentMissing
		}
		return fmt.Errorf("storage: unassign role: %w", err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "system_role_assignment", assignmentID, nil, nil); err != nil {
		return err
	}
	// The assignment row is already gone, so walking the component's assignments
	// would no longer reach this system. Naming it is what makes the drop visible.
	if err := p.recomputeChain(ctx, tx, []ownerRef{{ID: component.ID, Name: component.Name}}, []ownerRef{{ID: sys.ID, Name: sys.Name}}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit unassign role: %w", err)
	}
	return nil
}

// rolePositionLockKey is the advisory-lock key AssignRole, UnassignRole and
// SwapPositions all take on a (system, role) pair, so position allocation and
// reordering serialize against each other (#626). Both halves are ids, not
// names: two systems (like two roles across different owners) can share a
// name, but never an id.
func rolePositionLockKey(systemID, roleID string) string {
	return "system_role_assignment/" + systemID + "/" + roleID
}

// SwapPositions exchanges the positions of whichever components currently
// hold positions a and b within a role. It is an ordering change only: who
// occupies the role and the system's health are unaffected, so this does
// not recompute health the way AssignRole and UnassignRole do. Either
// position not currently held by an occupant is ErrAssignmentMissing, the
// same not-found the rest of the staffing surface uses for "there is
// nothing here to act on". Swapping a position with itself is a no-op.
func (p *PG) SwapPositions(ctx context.Context, actorID, systemName, roleName string, a, b int, write scope.Set) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin swap positions: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Resolved once (see AssignRole's comment).
	sys, err := scopedByName(ctx, tx, systemConfig, systemName)
	if err != nil {
		return err
	}
	inScope, err := inScopeTree(ctx, tx, systemTable, sys.ID, write)
	if err != nil {
		return err
	}
	if !inScope {
		return ErrSystemNotFound
	}
	roleID, _, _, _, err := p.resolveRole(ctx, tx, sys.ID, roleName)
	if err != nil {
		return err
	}
	if err := lockAdvisory(ctx, tx, rolePositionLockKey(sys.ID, roleID)); err != nil {
		return err
	}
	if a == b {
		return nil
	}

	// The uniqueness constraint on (system_id, role_id, position) is
	// DEFERRABLE INITIALLY IMMEDIATE precisely for this statement: a plain
	// unique index is checked per updated tuple, so a single UPDATE moving
	// two rows into each other's slots would raise the moment the first row
	// lands on the other's position. Deferring it to end-of-transaction
	// lets both land before either is checked. No ON CONFLICT may ever name
	// this constraint as its arbiter (Postgres refuses a deferrable
	// constraint as an arbiter); AssignRole's own arbiter is the unrelated,
	// non-deferrable (system_id, role_id, component_id) key.
	if _, err := tx.Exec(ctx, `set constraints system_role_assignment_position_key deferred`); err != nil {
		return fmt.Errorf("storage: defer position uniqueness: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		update system_role_assignment
		   set position = case position when $3 then $4 when $4 then $3 end
		 where system_id = $1::uuid
		   and role_id = $2
		   and position in ($3, $4)`, sys.ID, roleID, a, b)
	if err != nil {
		return mapRoleWriteErr(err)
	}
	if tag.RowsAffected() != 2 {
		return ErrAssignmentMissing
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "system_role_assignment", roleID, nil,
		map[string]any{"system": systemName, "role": roleName, "swap": [2]int{a, b}}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit swap positions: %w", err)
	}
	return nil
}

// roleRef pairs a registry row's id (for the subtree/membership check) with
// its name (for a refusal message): shared shape for a role's accepted
// component_types and its pinned products.
type roleRef struct {
	ID   uuid.UUID
	Name string
}

// roleRefNames projects a []roleRef to its names, in the order resolved
// (alphabetical, per resolveRole's queries), so a multi-type or multi-product
// refusal reads deterministically.
func roleRefNames(refs []roleRef) []string {
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.Name
	}
	return names
}

// resolveRole finds a role by name for a system, looking at both arcs (the
// system's own roles and those its standard declares), and returns its id,
// display name, and the typed-slot guard's requirements: the component_types
// it accepts (empty means any) and the products it pins (empty means any
// product of an accepted type). Takes the system's id directly: every caller
// has already resolved it (AssignRole, UnassignRole, SwapPositions), and the
// role name it looks up within stays scoped to a single owner arc either way,
// so binding the id here avoids one more name-subquery that would raise
// SQLSTATE 21000 the moment two systems share a name (#627).
func (p *PG) resolveRole(ctx context.Context, q txQuerier, systemID, roleName string) (id, displayName string, acceptedTypes, pinnedProducts []roleRef, err error) {
	err = q.QueryRow(ctx, `
		with sys as (select id, name, standard_id from system where id = $1::uuid)
		select r.id, r.display_name
		from sys
		join system_role r
		     on (r.owner_kind = 'system' and r.system_id = sys.id)
		     or (r.owner_kind = 'standard' and r.standard_id = sys.standard_id)
		where r.name = $2`, systemID, roleName).Scan(&id, &displayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil, nil, ErrRoleNotFound
	} else if err != nil {
		return "", "", nil, nil, fmt.Errorf("storage: resolve role %s/%s: %w", systemID, roleName, err)
	}

	typeRows, err := q.Query(ctx, `
		select ct.id, ct.name
		from system_role_type rt join component_type ct on ct.id = rt.component_type_id
		where rt.role_id = $1
		order by ct.name`, id)
	if err != nil {
		return "", "", nil, nil, fmt.Errorf("storage: role accepted types %s: %w", id, err)
	}
	defer typeRows.Close()
	for typeRows.Next() {
		var t roleRef
		if err := typeRows.Scan(&t.ID, &t.Name); err != nil {
			return "", "", nil, nil, fmt.Errorf("storage: scan accepted type: %w", err)
		}
		acceptedTypes = append(acceptedTypes, t)
	}
	if err := typeRows.Err(); err != nil {
		return "", "", nil, nil, err
	}

	prodRows, err := q.Query(ctx, `
		select pr.id, pr.name
		from system_role_product rp join product pr on pr.id = rp.product_id
		where rp.role_id = $1
		order by pr.name`, id)
	if err != nil {
		return "", "", nil, nil, fmt.Errorf("storage: role pinned products %s: %w", id, err)
	}
	defer prodRows.Close()
	for prodRows.Next() {
		var pr roleRef
		if err := prodRows.Scan(&pr.ID, &pr.Name); err != nil {
			return "", "", nil, nil, fmt.Errorf("storage: scan pinned product: %w", err)
		}
		pinnedProducts = append(pinnedProducts, pr)
	}
	return id, displayName, acceptedTypes, pinnedProducts, prodRows.Err()
}

// componentClassification is the pair the typed-slot guard compares against a
// role's accepted types and pinned products: the component's product and that
// product's component_type. Every component carries a product and every
// product a component_type (both NOT NULL floors, #614), so this always
// resolves once the component itself is confirmed to exist.
type componentClassification struct {
	ProductID   uuid.UUID
	ProductName string
	TypeID      uuid.UUID
	TypeName    string
}

// classifyComponent resolves a component's typed-slot classification. Takes
// the component's id directly: AssignRole has already resolved it (see its
// comment).
func (p *PG) classifyComponent(ctx context.Context, q querier, componentID string) (*componentClassification, error) {
	var c componentClassification
	err := q.QueryRow(ctx, `
		select pr.id, pr.name, ct.id, ct.name
		from component c
		join product pr on pr.id = c.product_id
		join component_type ct on ct.id = pr.component_type_id
		where c.id = $1::uuid`, componentID).Scan(&c.ProductID, &c.ProductName, &c.TypeID, &c.TypeName)
	if err != nil {
		return nil, fmt.Errorf("storage: classify component %s: %w", componentID, err)
	}
	return &c, nil
}

// mapRoleWriteErr discriminates a role write's Postgres error by constraint
// name (#626): a plain isUniqueViolation check cannot tell a name collision
// from a double-staffing race from a position race, and would report all
// three as "a role with this name is already declared here".
func mapRoleWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23505":
			switch pgErr.ConstraintName {
			case "system_role_assignment_component_key":
				return ErrComponentAlreadyStaffed
			case "system_role_assignment_position_key":
				return ErrRolePositionTaken
			}
			return ErrRoleExists
		case pgErr.Code == "23514" && pgErr.ConstraintName == "system_role_capacity_check":
			return ErrCapacityBelowQuorum
		case pgErr.Code == "23514" && (pgErr.ConstraintName == "system_role_owner_arc_check" || pgErr.ConstraintName == "role_choice_owner_arc_check"):
			// A bogus owner name resolves to NULL through roleOwnerExpr rather
			// than failing outright (#626): the arc CHECK is what catches it, so
			// the same unknown-reference sentinel a bad accepted type or pinned
			// product gets applies here too.
			return ErrRoleRefNotFound
		case pgErr.Code == "23503",
			// an unknown component_type, product, or alternate id resolves to
			// null (or, for the four-column system_role_alternate_fk, a
			// foreign owner) on a guarded reference
			pgErr.Code == "23502" && (pgErr.ColumnName == "component_type_id" || pgErr.ColumnName == "product_id"):
			return ErrRoleRefNotFound
		}
	}
	return fmt.Errorf("storage: role write: %w", err)
}
