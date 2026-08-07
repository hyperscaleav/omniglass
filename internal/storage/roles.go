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
	ID           string
	OwnerKind    string // standard | system
	OwnerID      string // the standard id or the system name
	Name         string
	DisplayName  string
	Quorum       int
	Capabilities []string // the capability ids the role requires, all of them
	// AcceptedTypes is the component_type names the role's typed-slot guard
	// accepts (a component's product's component_type_id must fall within one
	// of these subtrees, self-inclusive); empty means any type. PinnedProducts
	// optionally narrows further to specific products within an accepted type;
	// empty means any product of an accepted type. This pair, not Capabilities,
	// is what AssignRole checks (#626).
	AcceptedTypes  []string
	PinnedProducts []string
	// Impact is what an impaired role means for its system: outage, degraded, or
	// none. It lives on the role because the same broken component matters
	// differently depending on the slot it was filling, and it is the only input
	// the rollup takes from the declaration side beyond the requirement itself.
	Impact    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SystemRoleSpec is the declaration input. Capabilities, AcceptedTypes, and
// PinnedProducts each replace their required set wholesale on an update,
// matching how a product's capability set behaves.
type SystemRoleSpec struct {
	Name           string
	DisplayName    string
	Quorum         int
	Capabilities   []string
	AcceptedTypes  []string
	PinnedProducts []string
	Impact         string // outage | degraded | none; empty means degraded
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

// Role sentinels. A typed-slot refusal is its own error so the API can name
// WHICH type or product is wanted: an operator cannot act on a bare refusal.
var (
	ErrRoleNotFound      = errors.New("storage: system role not found")
	ErrRoleExists        = errors.New("storage: a role with this name is already declared here")
	ErrRoleRefNotFound   = errors.New("storage: role references a missing owner, type, or product")
	ErrAssignmentMissing = errors.New("storage: role assignment not found")
	ErrRoleImpact        = errors.New("storage: unknown role impact")
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

// EffectiveCapabilities resolves what a component actually provides: the
// capabilities its product declares, plus the ones the component adds, minus the
// ones the component suppresses.
//
// No longer what AssignRole checks (the typed-slot guard compares the
// component's product's component_type instead, #626); this resolved set is
// now read only by the health rollup's alarm-impact model (an alarm names a
// capability it degrades, and a role's still-live Capabilities decide which
// roles that affects), pending its own retirement alongside the capability
// tables (Task 5). "Productless" is also no longer a real case: every
// component carries a product (the #614 floor), a bare create resolving to
// generic-device, so this always has a product's declared set to start from.
// It aggregates to a single row so it runs on the narrow querier (which carries
// only QueryRow) and therefore works standalone or inside a recompute's
// transaction.
func (p *PG) EffectiveCapabilities(ctx context.Context, q querier, componentName string) ([]string, error) {
	var caps []string
	// The set is resolved by capability id and projected as name at the end (a
	// capability id is a uuid; its handle is what the API and health rules speak).
	err := q.QueryRow(ctx, `
		select coalesce(array_agg(cap.name order by cap.name), '{}')
		from capability cap
		where cap.id in (
			select pc.capability_id
			from component c
			join product_capability pc on pc.product_id = c.product_id
			where c.name = $1
			union
			select cc.capability_id
			from component_capability cc
			where cc.component_id = (select id from component where name = $1) and cc.present
		)
		and cap.id not in (
			select capability_id from component_capability
			where component_id = (select id from component where name = $1) and not present
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
			select id, name, standard_id from system where name = $1
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
		select roles.id, roles.name, roles.display_name, roles.quorum, roles.impact, roles.from_standard,
		       roles.created_at, roles.updated_at,
		       coalesce(array_agg(distinct cap.name) filter (where cap.name is not null), '{}') as caps,
		       coalesce(array_agg(distinct ct.name) filter (where ct.name is not null), '{}') as types,
		       coalesce(array_agg(distinct pr.name) filter (where pr.name is not null), '{}') as products,
		       coalesce(array_agg(distinct ac.name) filter (where ac.name is not null), '{}') as assigned
		from roles
		left join system_role_capability rc on rc.role_id = roles.id
		left join capability cap on cap.id = rc.capability_id
		left join system_role_type rt on rt.role_id = roles.id
		left join component_type ct on ct.id = rt.component_type_id
		left join system_role_product rp on rp.role_id = roles.id
		left join product pr on pr.id = rp.product_id
		left join system_role_assignment ra on ra.role_id = roles.id
		     and ra.system_id = (select id from system where name = $1)
		left join component ac on ac.id = ra.component_id
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
			&e.CreatedAt, &e.UpdatedAt, &e.Capabilities, &e.AcceptedTypes, &e.PinnedProducts, &e.AssignedTo); err != nil {
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

	inScope, err := p.ownerInScope(ctx, tx, "system", systemName, write)
	if err != nil {
		return err
	}
	if !inScope {
		return ErrSystemNotFound
	}

	roleID, roleDisplay, acceptedTypes, pinnedProducts, err := p.resolveRole(ctx, tx, systemName, roleName)
	if err != nil {
		return err
	}
	// Confirm the component exists before judging what it is. An absent
	// component has no product to classify, which would otherwise surface as a
	// confusing type refusal for what is really a typo.
	if _, err := scopedByName(ctx, tx, componentConfig, componentName); err != nil {
		return err // ErrComponentNotFound when absent
	}
	cls, err := p.classifyComponent(ctx, tx, componentName)
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

	// Staffing a role IS membership, so the binding is created here rather than
	// asked of the operator as a separate step. A component filling a job in a
	// system that the system does not count as a member is the contradiction
	// system_member exists to make impossible.
	if err := addMemberTx(ctx, tx, systemName, componentName); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into system_role_assignment (system_id, role_id, component_id)
		values ((select id from system where name = $1), $2,
		        (select id from component where name = $3))
		on conflict (system_id, role_id, component_id) do nothing`,
		systemName, roleID, componentName); err != nil {
		return mapRoleWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "system_role_assignment", roleID, nil,
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
	roleID, _, _, _, err := p.resolveRole(ctx, tx, systemName, roleName)
	if err != nil {
		return err
	}
	var assignmentID string
	if err := tx.QueryRow(ctx, `
		delete from system_role_assignment
		where system_id = (select id from system where name = $1) and role_id = $2
		  and component_id = (select id from component where name = $3)
		returning id`, systemName, roleID, componentName).Scan(&assignmentID); err != nil {
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
	if err := p.recomputeChain(ctx, tx, []string{componentName}, []string{systemName}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit unassign role: %w", err)
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
// product of an accepted type).
func (p *PG) resolveRole(ctx context.Context, q txQuerier, systemName, roleName string) (id, displayName string, acceptedTypes, pinnedProducts []roleRef, err error) {
	err = q.QueryRow(ctx, `
		with sys as (select id, name, standard_id from system where name = $1)
		select r.id, r.display_name
		from sys
		join system_role r
		     on (r.owner_kind = 'system' and r.system_id = sys.id)
		     or (r.owner_kind = 'standard' and r.standard_id = sys.standard_id)
		where r.name = $2`, systemName, roleName).Scan(&id, &displayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil, nil, ErrRoleNotFound
	} else if err != nil {
		return "", "", nil, nil, fmt.Errorf("storage: resolve role %s/%s: %w", systemName, roleName, err)
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

// classifyComponent resolves a component's typed-slot classification.
func (p *PG) classifyComponent(ctx context.Context, q querier, componentName string) (*componentClassification, error) {
	var c componentClassification
	err := q.QueryRow(ctx, `
		select pr.id, pr.name, ct.id, ct.name
		from component c
		join product pr on pr.id = c.product_id
		join component_type ct on ct.id = pr.component_type_id
		where c.name = $1`, componentName).Scan(&c.ProductID, &c.ProductName, &c.TypeID, &c.TypeName)
	if err != nil {
		return nil, fmt.Errorf("storage: classify component %s: %w", componentName, err)
	}
	return &c, nil
}

func mapRoleWriteErr(err error) error {
	if isUniqueViolation(err) {
		return ErrRoleExists
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23503" ||
		// an unknown capability, component_type, or product name resolves to
		// null on a not-null arc
		(pgErr.Code == "23502" && (pgErr.ColumnName == "capability_id" ||
			pgErr.ColumnName == "component_type_id" || pgErr.ColumnName == "product_id"))) {
		return ErrRoleRefNotFound
	}
	return fmt.Errorf("storage: role write: %w", err)
}

// ComponentCapabilities is the Gateway-facing EffectiveCapabilities: the same
// resolved set, on the pool, for callers outside a transaction.
func (p *PG) ComponentCapabilities(ctx context.Context, componentName string) ([]string, error) {
	return p.EffectiveCapabilities(ctx, p.pool, componentName)
}
