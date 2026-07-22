package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// The declaration side of the role model: who DECLARES a role (a standard, so
// every conforming system inherits it, or one system ad-hoc) and what a
// component claims it can do. The resolvers in roles.go read what this writes;
// nothing here resolves, so the two stay separable.

// ErrComponentCapabilityNotFound is clearing a capability fact the component
// never declared. Its own sentinel rather than ErrRoleNotFound, because the
// address that came up empty is a component's capability row, not a role.
var ErrComponentCapabilityNotFound = errors.New("storage: component capability not declared")

// ErrUnknownRoleOwner guards the role owner-arc column mapping. A role owner is
// a standard or a system and nothing else, so an unrecognized kind is refused
// rather than left to write a NULL through the arc.
var ErrUnknownRoleOwner = errors.New("storage: unknown role owner_kind")

// OwnerID is not in the column list (it lives in whichever arc column the owner
// kind selects), so the caller stamps it from the address it queried by, the way
// property_value's scan does.
const systemRoleCols = `id, owner_kind, name, display_name, quorum, impact, created_at, updated_at`

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
	if err := row.Scan(&r.ID, &r.OwnerKind, &r.Name, &r.DisplayName, &r.Quorum, &r.Impact,
		&r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListSystemRoles returns the roles one owner declares itself, ordered by name, each
// with the capabilities it requires. This is the declaration read, not the
// resolution: a system's list carries only its ad-hoc roles, never the ones it
// inherits from its standard (EffectiveRoles is what merges the two arcs).
func (p *PG) ListSystemRoles(ctx context.Context, ownerKind, ownerID string) ([]SystemRole, error) {
	col, err := roleOwnerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	// The columns are spelled out rather than reusing systemRoleCols: the join
	// needs them qualified by the role alias.
	q := fmt.Sprintf(`
		select r.id, r.owner_kind, r.name, r.display_name, r.quorum, r.impact, r.created_at, r.updated_at,
		       coalesce(array_agg(cap.name order by cap.name)
		                filter (where cap.name is not null), '{}') as caps
		from system_role r
		left join role_capability rc on rc.role_id = r.id
		left join capability cap on cap.id = rc.capability_id
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
		if err := rows.Scan(&r.ID, &r.OwnerKind, &r.Name, &r.DisplayName, &r.Quorum, &r.Impact,
			&r.CreatedAt, &r.UpdatedAt, &r.Capabilities); err != nil {
			return nil, fmt.Errorf("storage: scan role: %w", err)
		}
		r.OwnerID = ownerID
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetSystemRole declares a role on a standard or a system, or revises the declaration
// in place: the role is addressed by name within its owner arc, so the write is
// an upsert and the surface's save is idempotent. The required-capability set is
// replaced wholesale in the same transaction, matching how a product's
// capability set behaves: what the caller sends is what the role requires
// afterwards, so a capability can be dropped by omitting it.
//
// A quorum below one means one: a role no component need fill is not a role.
// An owner or capability that does not exist is ErrRoleRefNotFound (a request
// fault), never a server error.
func (p *PG) SetSystemRole(ctx context.Context, actorID, ownerKind, ownerID string, spec SystemRoleSpec) (*SystemRole, error) {
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

	r, err := scanSystemRole(tx.QueryRow(ctx, fmt.Sprintf(`
		insert into system_role (owner_kind, %s, name, display_name, quorum, impact)
		values ($1, %s, $3, $4, $5, $6)
		on conflict (owner_kind, standard_id, system_id, name) do update
			set display_name = excluded.display_name,
			    quorum       = excluded.quorum,
			    impact       = excluded.impact,
			    updated_at   = now()
		returning `+systemRoleCols, col, roleOwnerExpr(ownerKind)),
		ownerKind, ownerID, spec.Name, spec.DisplayName, quorum, impact))
	if err != nil {
		return nil, mapRoleWriteErr(err)
	}
	r.OwnerID = ownerID

	// Wholesale replacement: clear what the role required, then install the set
	// the caller sent, so the declaration is the whole truth after the write.
	if _, err := tx.Exec(ctx, `delete from role_capability where role_id = $1`, r.ID); err != nil {
		return nil, fmt.Errorf("storage: clear role capabilities %s: %w", r.ID, err)
	}
	if len(spec.Capabilities) > 0 {
		if _, err := tx.Exec(ctx, `
			insert into role_capability (role_id, capability_id)
			select $1, (select id from capability where name = c or id::text = c)
			from unnest($2::text[]) c
			on conflict (role_id, capability_id) do nothing`, r.ID, spec.Capabilities); err != nil {
			return nil, mapRoleWriteErr(err)
		}
	}
	r.Capabilities = append([]string(nil), spec.Capabilities...)

	verb := "create"
	if before != nil {
		verb = "update"
	}
	if err := writeAuditRes(ctx, tx, actorID, verb, "system_role", r.ID, before, r); err != nil {
		return nil, err
	}
	// A declaration change moves health without touching a component: a new
	// required capability, a raised quorum, or a changed impact can impair a role
	// that was fine a moment ago. A standard's declaration moves every conforming
	// system at once.
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

// DeleteSystemRole withdraws a role from its owner, taking its required capabilities
// and every assignment to it with it (both cascade). A role the owner does not
// declare is ErrRoleNotFound, so withdrawing twice is an explicit miss rather
// than a silent no-op.
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

// SetComponentCapability records one capability fact on a component, layered
// over what its product declares: present adds a capability the product does not
// claim, absent suppresses one it does. This is what lets a productless
// component be staffed while the assignment guard stays strict.
//
// Idempotent (the fact is keyed by component and capability). An unknown
// component or capability is ErrRoleRefNotFound, a request fault.
func (p *PG) SetComponentCapability(ctx context.Context, actorID, componentName, capabilityID string, present bool) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin set component capability: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id string
	if err := tx.QueryRow(ctx, `
		insert into component_capability (component_id, capability_id, present)
		values ((select id from component where name = $1),
		        (select id from capability where name = $2 or id::text = $2), $3)
		on conflict (component_id, capability_id) do update
			set present    = excluded.present,
			    updated_at = now()
		returning id`, componentName, capabilityID, present).Scan(&id); err != nil {
		return mapRoleWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "component_capability", id, nil,
		map[string]any{"component": componentName, "capability": capabilityID, "present": present}); err != nil {
		return err
	}
	// What the component provides is half of whether it satisfies a role, so a
	// capability fact moves the health of every system it staffs.
	if err := p.RecomputeHealth(ctx, tx, componentName); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit set component capability: %w", err)
	}
	return nil
}

// ClearComponentCapability removes the component's own fact about a capability,
// so the component falls back to whatever its product declares. Clearing a fact
// the component never declared is ErrComponentCapabilityNotFound.
func (p *PG) ClearComponentCapability(ctx context.Context, actorID, componentName, capabilityID string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin clear component capability: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id string
	if err := tx.QueryRow(ctx, `
		delete from component_capability
		where component_id = (select id from component where name = $1)
		  and capability_id = (select id from capability where name = $2 or id::text = $2)
		returning id`, componentName, capabilityID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrComponentCapabilityNotFound
		}
		return fmt.Errorf("storage: clear component capability %s/%s: %w", componentName, capabilityID, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "component_capability", id, nil, nil); err != nil {
		return err
	}
	// Falling back to the product's set can drop a capability a role required.
	if err := p.RecomputeHealth(ctx, tx, componentName); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit clear component capability: %w", err)
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
	var id string
	err = p.pool.QueryRow(ctx, fmt.Sprintf(`
		insert into system_role (owner_kind, %s, name, display_name, quorum, impact)
		values ($1, %s, $3, $4, $5, $6)
		on conflict (owner_kind, standard_id, system_id, name) do nothing
		returning id`, col, roleOwnerExpr(ownerKind)),
		ownerKind, ownerID, spec.Name, spec.DisplayName, max(spec.Quorum, 1), impact).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // already there, and the operator owns it now
	}
	if err != nil {
		return mapRoleWriteErr(err)
	}
	if len(spec.Capabilities) == 0 {
		return nil
	}
	if _, err := p.pool.Exec(ctx, `
		insert into role_capability (role_id, capability_id)
		select $1, (select id from capability where name = c or id::text = c)
		from unnest($2::text[]) c
		on conflict (role_id, capability_id) do nothing`, id, spec.Capabilities); err != nil {
		return mapRoleWriteErr(err)
	}
	return nil
}
