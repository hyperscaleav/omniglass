package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// The declaration side of the role model: who DECLARES a role (a standard, so
// every conforming system inherits it, or one system ad-hoc) and the
// typed-slot requirement it enforces at assignment. The resolvers in roles.go
// read what this writes; nothing here resolves, so the two stay separable.

// ErrUnknownRoleOwner guards the role owner-arc column mapping. A role owner is
// a standard or a system and nothing else, so an unrecognized kind is refused
// rather than left to write a NULL through the arc.
var ErrUnknownRoleOwner = errors.New("storage: unknown role owner_kind")

// OwnerID is not in the column list (it lives in whichever arc column the owner
// kind selects), so the caller stamps it from the address it queried by, the way
// property_value's scan does.
const systemRoleCols = `id, owner_kind, name, display_name, quorum, capacity, position_labels, impact, created_at, updated_at`

// roleOwnerColumn maps a role owner kind to its exclusive-arc column. Every
// identifier it returns is a compile-time constant, never caller input, so
// interpolating one into a statement is safe.
// roleOwnerExpr is the SQL for the value system_role's arc stores. A standard is
// slug-keyed, so its id IS the reference; a system is uuid-keyed, so the name
// resolves. Keeping this as an expression means the surrounding statements do not
// branch on owner kind.
func roleOwnerExpr(ownerKind string) string {
	if ownerKind == "system" {
		return `(select id from system where name = $2)`
	}
	// A standard is addressed by its handle or its uuid, and the column stores
	// the uuid (ADR-0062).
	return `(select id from standard where name = $2 or id::text = $2)`
}

func roleOwnerColumn(ownerKind string) (string, error) {
	switch ownerKind {
	case "standard":
		return "standard_id", nil
	case "system":
		return "system_id", nil
	default:
		return "", ErrUnknownRoleOwner
	}
}

func scanSystemRole(row pgx.Row) (*SystemRole, error) {
	var r SystemRole
	if err := row.Scan(&r.ID, &r.OwnerKind, &r.Name, &r.DisplayName, &r.Quorum, &r.Capacity, &r.PositionLabels,
		&r.Impact, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// normalizePositionLabels returns a non-nil slice so an omitted set writes
// and reads back as an empty text[], not SQL NULL (position_labels is not
// null). Unlike capacity, a label set always wholesale-replaces: there is no
// "leave unchanged" reading for a list, the same rule accepted_types and
// pinned_products already follow.
func normalizePositionLabels(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ListSystemRoles returns the roles one owner declares itself, ordered by
// name, each with its typed-slot requirement. This is the declaration read,
// not the resolution: a system's list carries only its ad-hoc roles, never
// the ones it inherits from its standard (EffectiveRoles is what merges the
// two arcs).
func (p *PG) ListSystemRoles(ctx context.Context, ownerKind, ownerID string) ([]SystemRole, error) {
	col, err := roleOwnerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	// The columns are spelled out rather than reusing systemRoleCols: the join
	// needs them qualified by the role alias.
	q := fmt.Sprintf(`
		select r.id, r.owner_kind, r.name, r.display_name, r.quorum, r.capacity, r.position_labels, r.impact,
		       r.created_at, r.updated_at,
		       coalesce(array_agg(distinct ct.name order by ct.name) filter (where ct.name is not null), '{}') as types,
		       coalesce(array_agg(distinct pr.name order by pr.name) filter (where pr.name is not null), '{}') as products
		from system_role r
		left join system_role_type rt on rt.role_id = r.id
		left join component_type ct on ct.id = rt.component_type_id
		left join system_role_product rp on rp.role_id = r.id
		left join product pr on pr.id = rp.product_id
		where r.owner_kind = $1 and r.%s = %s
		group by r.id
		order by r.name`, col, roleOwnerExpr(ownerKind))

	rows, err := p.pool.Query(ctx, q, ownerKind, ownerID)
	if err != nil {
		return nil, fmt.Errorf("storage: list roles %s/%s: %w", ownerKind, ownerID, err)
	}
	defer rows.Close()

	out := []SystemRole{}
	for rows.Next() {
		var r SystemRole
		if err := rows.Scan(&r.ID, &r.OwnerKind, &r.Name, &r.DisplayName, &r.Quorum, &r.Capacity, &r.PositionLabels,
			&r.Impact, &r.CreatedAt, &r.UpdatedAt, &r.AcceptedTypes, &r.PinnedProducts); err != nil {
			return nil, fmt.Errorf("storage: scan role: %w", err)
		}
		r.OwnerID = ownerID
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetSystemRole declares a role on a standard or a system, or revises the
// declaration in place: the role is addressed by name within its owner arc,
// so the write is an upsert and the surface's save is idempotent. The
// typed-slot requirement (AcceptedTypes, PinnedProducts) is replaced
// wholesale in the same transaction: what the caller sends is what the role
// accepts afterwards, so a type can be dropped by omitting it.
//
// A quorum below one means one: a role no component need fill is not a role.
// An owner, type, or product that does not exist is ErrRoleRefNotFound (a
// request fault), never a server error.
func (p *PG) SetSystemRole(ctx context.Context, actorID, ownerKind, ownerID string, spec SystemRoleSpec) (*SystemRole, error) {
	if err := ValidateName("system_role", spec.Name); err != nil {
		return nil, err
	}
	col, err := roleOwnerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	quorum := spec.Quorum
	if quorum < 1 {
		quorum = 1
	}
	impact := spec.Impact
	if impact == "" {
		impact = "degraded"
	}
	if !roleImpacts[impact] {
		return nil, ErrRoleImpact
	}
	positionLabels := normalizePositionLabels(spec.PositionLabels)

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin set role: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The before-image decides create vs update and gives the audit its old side.
	var before any
	prior, err := scanSystemRole(tx.QueryRow(ctx, fmt.Sprintf(
		`select `+systemRoleCols+` from system_role where owner_kind = $1 and %s = %s and name = $3`, col, roleOwnerExpr(ownerKind)),
		ownerKind, ownerID, spec.Name))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("storage: load role %s/%s/%s: %w", ownerKind, ownerID, spec.Name, err)
	default:
		prior.OwnerID = ownerID
		before = prior
	}

	// Lowering capacity below what some conforming system already has
	// assigned refuses naming the worst-exceeding one: a standard-owned role
	// is staffed independently in every system that conforms to it, so any
	// one of them can exceed a new, lower cap regardless of what this system
	// (the one whose edit triggered the check) looks like.
	if spec.Capacity != nil && prior != nil {
		var sysName string
		var have int
		err := tx.QueryRow(ctx, `
			select s.name, count(*) from system_role_assignment ra
			  join system s on s.id = ra.system_id
			 where ra.role_id = $1
			 group by s.name having count(*) > $2
			 order by count(*) desc, s.name limit 1`, prior.ID, *spec.Capacity).Scan(&sysName, &have)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// nothing exceeds the new cap
		case err != nil:
			return nil, fmt.Errorf("storage: capacity precheck %s: %w", prior.ID, err)
		default:
			return nil, &CapacityShortfall{Role: prior.DisplayName, System: sysName, Have: have, Want: *spec.Capacity}
		}
	}

	r, err := scanSystemRole(tx.QueryRow(ctx, fmt.Sprintf(`
		insert into system_role (owner_kind, %s, name, display_name, quorum, capacity, position_labels, impact)
		values ($1, %s, $3, $4, $5, $6, $7, $8)
		on conflict (owner_kind, standard_id, system_id, name) do update
			set display_name    = excluded.display_name,
			    quorum          = excluded.quorum,
			    capacity        = coalesce(excluded.capacity, system_role.capacity),
			    position_labels = excluded.position_labels,
			    impact          = excluded.impact,
			    updated_at      = now()
		returning `+systemRoleCols, col, roleOwnerExpr(ownerKind)),
		ownerKind, ownerID, spec.Name, spec.DisplayName, quorum, spec.Capacity, positionLabels, impact))
	if err != nil {
		return nil, mapRoleWriteErr(err)
	}
	r.OwnerID = ownerID

	// The typed-slot guard's requirement, replaced wholesale: what the caller
	// sends is what the role accepts (types) and pins (products) afterwards.
	if _, err := tx.Exec(ctx, `delete from system_role_type where role_id = $1`, r.ID); err != nil {
		return nil, fmt.Errorf("storage: clear role types %s: %w", r.ID, err)
	}
	if len(spec.AcceptedTypes) > 0 {
		if _, err := tx.Exec(ctx, `
			insert into system_role_type (role_id, component_type_id)
			select $1, (select id from component_type where name = c or id::text = c)
			from unnest($2::text[]) c
			on conflict (role_id, component_type_id) do nothing`, r.ID, spec.AcceptedTypes); err != nil {
			return nil, mapRoleWriteErr(err)
		}
	}
	r.AcceptedTypes = append([]string(nil), spec.AcceptedTypes...)

	if _, err := tx.Exec(ctx, `delete from system_role_product where role_id = $1`, r.ID); err != nil {
		return nil, fmt.Errorf("storage: clear role products %s: %w", r.ID, err)
	}
	if len(spec.PinnedProducts) > 0 {
		if _, err := tx.Exec(ctx, `
			insert into system_role_product (role_id, product_id)
			select $1, (select id from product where name = c or id::text = c)
			from unnest($2::text[]) c
			on conflict (role_id, product_id) do nothing`, r.ID, spec.PinnedProducts); err != nil {
			return nil, mapRoleWriteErr(err)
		}
	}
	r.PinnedProducts = append([]string(nil), spec.PinnedProducts...)

	verb := "create"
	if before != nil {
		verb = "update"
	}
	if err := writeAuditRes(ctx, tx, actorID, verb, "system_role", r.ID, before, r); err != nil {
		return nil, err
	}
	// A declaration change moves health without touching a component: a raised
	// quorum or a changed impact can impair a role that was fine a moment ago.
	// A standard's declaration moves every conforming system at once.
	affected, err := p.systemsForRoleOwner(ctx, tx, ownerKind, ownerID)
	if err != nil {
		return nil, err
	}
	if err := p.recomputeSystems(ctx, tx, affected...); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit set role: %w", err)
	}
	return r, nil
}

// DeleteSystemRole withdraws a role from its owner, taking its typed-slot
// requirement and every assignment to it with it (both cascade). A role the
// owner does not declare is ErrRoleNotFound, so withdrawing twice is an
// explicit miss rather than a silent no-op.
func (p *PG) DeleteSystemRole(ctx context.Context, actorID, ownerKind, ownerID, name string) error {
	col, err := roleOwnerColumn(ownerKind)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete role: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Delete and capture the before-image in one statement, so the audit records
	// the withdrawn declaration and a missing row is caught without a second read.
	before, err := scanSystemRole(tx.QueryRow(ctx, fmt.Sprintf(`
		delete from system_role
		where owner_kind = $1 and %s = %s and name = $3
		returning `+systemRoleCols, col, roleOwnerExpr(ownerKind)), ownerKind, ownerID, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRoleNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: delete role %s/%s/%s: %w", ownerKind, ownerID, name, err)
	}
	before.OwnerID = ownerID
	if err := writeAuditRes(ctx, tx, actorID, "delete", "system_role", before.ID, before, nil); err != nil {
		return err
	}
	// Withdrawing a role can only improve a system: the impaired slot it was
	// contributing is gone. Recompute so the recovery is recorded as an edge.
	affected, err := p.systemsForRoleOwner(ctx, tx, ownerKind, ownerID)
	if err != nil {
		return err
	}
	if err := p.recomputeSystems(ctx, tx, affected...); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete role: %w", err)
	}
	return nil
}

// SeedSystemRole installs one declared role for the boot-seed phase: inserted when
// absent, never reasserted, so an operator who retunes a shipped standard's
// roles keeps their edit across restarts. Deliberately unaudited, the same lane
// SeedStandard uses for the standards these roles hang off.
func (p *PG) SeedSystemRole(ctx context.Context, ownerKind, ownerID string, spec SystemRoleSpec) error {
	col, err := roleOwnerColumn(ownerKind)
	if err != nil {
		return err
	}
	impact := spec.Impact
	if impact == "" {
		impact = "degraded"
	}
	if !roleImpacts[impact] {
		return ErrRoleImpact
	}
	positionLabels := normalizePositionLabels(spec.PositionLabels)
	var id string
	err = p.pool.QueryRow(ctx, fmt.Sprintf(`
		insert into system_role (owner_kind, %s, name, display_name, quorum, capacity, position_labels, impact)
		values ($1, %s, $3, $4, $5, $6, $7, $8)
		on conflict (owner_kind, standard_id, system_id, name) do nothing
		returning id`, col, roleOwnerExpr(ownerKind)),
		ownerKind, ownerID, spec.Name, spec.DisplayName, max(spec.Quorum, 1), spec.Capacity, positionLabels, impact).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // already there, and the operator owns it now
	}
	if err != nil {
		return mapRoleWriteErr(err)
	}
	if len(spec.AcceptedTypes) > 0 {
		if _, err := p.pool.Exec(ctx, `
			insert into system_role_type (role_id, component_type_id)
			select $1, (select id from component_type where name = c or id::text = c)
			from unnest($2::text[]) c
			on conflict (role_id, component_type_id) do nothing`, id, spec.AcceptedTypes); err != nil {
			return mapRoleWriteErr(err)
		}
	}
	if len(spec.PinnedProducts) > 0 {
		if _, err := p.pool.Exec(ctx, `
			insert into system_role_product (role_id, product_id)
			select $1, (select id from product where name = c or id::text = c)
			from unnest($2::text[]) c
			on conflict (role_id, product_id) do nothing`, id, spec.PinnedProducts); err != nil {
			return mapRoleWriteErr(err)
		}
	}
	return nil
}
