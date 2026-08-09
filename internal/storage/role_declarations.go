package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
// property_value's scan does. alternate_id is here (unlike the rest of the
// choice-side identity, which the read side does not surface yet) because
// SetSystemRole and DeleteSystemRole's before/after images are built from
// this list: omitting it left the audit trail blind to exactly the field
// this fix round made deliberately writable, so a role moving between
// alternates left no evidence.
const systemRoleCols = `id, owner_kind, name, display_name, quorum, capacity, position_labels, impact, alternate_id, created_at, updated_at`

// roleOwnerColumn maps a role owner kind to its exclusive-arc column. Every
// identifier it returns is a compile-time constant, never caller input, so
// interpolating one into a statement is safe.
// roleOwnerExpr is the SQL for the value system_role's arc stores, given
// whatever roleOwnerArg resolved $2 to. Both arcs accept either form (name or
// uuid, ADR-0062): a standard's row is a registry entry addressed by handle or
// uuid directly; a system's is resolved once by roleOwnerArg before this
// expression ever runs; the `or id::text = $2` arm here is what makes an
// already-resolved id (or an ownerID that happened to be uuid-shaped) match.
// Keeping this as an expression means the surrounding statements do not
// branch on owner kind.
func roleOwnerExpr(ownerKind string) string {
	if ownerKind == "system" {
		return `(select id from system where name = $2 or id::text = $2)`
	}
	return `(select id from standard where name = $2 or id::text = $2)`
}

// roleOwnerArg resolves ownerID once for the role-owner arc, ambiguity-safe
// (#627). For a system owner: a name or uuid that identifies exactly one row
// resolves to its id, which roleOwnerExpr's `id::text = $2` arm then matches
// directly, never re-deriving it from a name a second query could land on a
// different row for. A name that identifies NONE is passed through
// unresolved, so the write falls through to the existing
// system_role_owner_arc_check -> ErrRoleRefNotFound path exactly as before
// (that CHECK, not this function, is what turns an unknown owner into a
// clean sentinel; forcing an early not-found here would trade a stable
// sentinel for a different one). A name that identifies two or more is
// refused outright as ErrAmbiguousName, rather than ever reaching a query
// that could raise SQLSTATE 21000. A standard owner is untouched: standard
// names stay globally unique, and roleOwnerExpr already resolves either form
// for it.
func (p *PG) roleOwnerArg(ctx context.Context, q querier, ownerKind, ownerID string) (string, error) {
	if ownerKind != "system" {
		return ownerID, nil
	}
	sys, err := scopedByName(ctx, q, systemConfig, ownerID)
	if errors.Is(err, ErrSystemNotFound) {
		return ownerID, nil
	} else if err != nil {
		return "", err
	}
	return sys.ID, nil
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
		&r.Impact, &r.AlternateID, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// normalizePositionLabels returns a non-nil slice so a set the write does not
// carry lands as an empty text[], not SQL NULL (position_labels is not null).
func normalizePositionLabels(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// roleWriteColumns is the system_role columns SetSystemRole's upsert may
// write, in statement order, and roleColumnFields maps each one to the wire
// field an update_mask names it by. The pair is what lets the UPDATE branch be
// built from the write set rather than hand-maintained per field.
var (
	roleWriteColumns = []string{"display_name", "quorum", "capacity", "position_labels", "impact", "alternate_id"}
	roleColumnFields = map[string]string{
		"display_name":    RoleFieldDisplayName,
		"quorum":          RoleFieldQuorum,
		"capacity":        RoleFieldCapacity,
		"position_labels": RoleFieldPositionLabels,
		"impact":          RoleFieldImpact,
		"alternate_id":    RoleFieldAlternate,
	}
)

// roleTypedSlot reads a role's typed-slot requirement as it stands: the
// accepted types and pinned products by name, both ordered, both empty rather
// than nil when the role holds none. SetSystemRole calls it for a set its
// write did not name, where the spec no longer describes what the role holds.
func roleTypedSlot(ctx context.Context, q querier, roleID string) ([]string, []string, error) {
	types, products := []string{}, []string{}
	if err := q.QueryRow(ctx, `
		select coalesce((select array_agg(ct.name order by ct.name)
		                   from system_role_type rt join component_type ct on ct.id = rt.component_type_id
		                  where rt.role_id = $1), '{}'),
		       coalesce((select array_agg(pr.name order by pr.name)
		                   from system_role_product rp join product pr on pr.id = rp.product_id
		                  where rp.role_id = $1), '{}')`, roleID).Scan(&types, &products); err != nil {
		return nil, nil, fmt.Errorf("storage: read role typed slot %s: %w", roleID, err)
	}
	return types, products, nil
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
	ownerArg, err := p.roleOwnerArg(ctx, p.pool, ownerKind, ownerID)
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

	rows, err := p.pool.Query(ctx, q, ownerKind, ownerArg)
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
// so the write is an upsert and the surface's save is idempotent.
//
// WHICH fields it writes is spec.Write, the resolved update_mask (#666): a
// field outside the write set keeps whatever the role already has, and on a
// create takes its default, so an edit can no longer carry away a field it
// never mentioned (#639) and a masked field carrying its zero value clears
// (#638). A caller that sets no mask writes the fields it populated, which is
// what this did for every field but Capacity and AlternateID before the mask
// existed.
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
	write := spec.writeFields()
	// Each value is taken only if the write set names its field, then
	// defaulted: a field the write does not write contributes its default to
	// the INSERT branch (a create has nothing to preserve) and is discarded
	// by the UPDATE branch, which reads the stored column instead.
	display := spec.DisplayName
	if !write.Has(RoleFieldDisplayName) {
		display = ""
	}
	if display == "" {
		// A minimal write still reads properly on a surface. The default
		// lives here rather than in the API layer so it applies to the
		// value, not to the decision to write one: the API's body is what
		// says whether display_name is being written at all.
		display = spec.Name
	}
	quorum := spec.Quorum
	if !write.Has(RoleFieldQuorum) {
		quorum = 0
	}
	if quorum < 1 {
		quorum = 1
	}
	// The impact is validated before the mask narrows anything: a value that
	// is not one of the three is a request fault whether or not this write
	// was going to store it.
	if spec.Impact != "" && !roleImpacts[spec.Impact] {
		return nil, ErrRoleImpact
	}
	impact := spec.Impact
	if !write.Has(RoleFieldImpact) {
		impact = ""
	}
	if impact == "" {
		impact = "degraded"
	}
	capacity := spec.Capacity
	if !write.Has(RoleFieldCapacity) {
		capacity = nil
	}
	labels := spec.PositionLabels
	if !write.Has(RoleFieldPositionLabels) {
		labels = nil
	}
	positionLabels := normalizePositionLabels(labels)
	alternateID := spec.AlternateID
	if !write.Has(RoleFieldAlternate) {
		alternateID = nil
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin set role: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Resolved once, inside this transaction, and reused for both queries
	// below: see roleOwnerArg.
	ownerArg, err := p.roleOwnerArg(ctx, tx, ownerKind, ownerID)
	if err != nil {
		return nil, err
	}

	// The before-image decides create vs update and gives the audit its old side.
	var before any
	prior, err := scanSystemRole(tx.QueryRow(ctx, fmt.Sprintf(
		`select `+systemRoleCols+` from system_role where owner_kind = $1 and %s = %s and name = $3`, col, roleOwnerExpr(ownerKind)),
		ownerKind, ownerArg, spec.Name))
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
	if capacity != nil && prior != nil {
		var sysName string
		var have int
		err := tx.QueryRow(ctx, `
			select s.name, count(*) from system_role_assignment ra
			  join system s on s.id = ra.system_id
			 where ra.role_id = $1
			 group by s.name having count(*) > $2
			 order by count(*) desc, s.name limit 1`, prior.ID, *capacity).Scan(&sysName, &have)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// nothing exceeds the new cap
		case err != nil:
			return nil, fmt.Errorf("storage: capacity precheck %s: %w", prior.ID, err)
		default:
			return nil, &CapacityShortfall{Role: prior.DisplayName, System: sysName, Have: have, Want: *capacity}
		}
	}

	// The UPDATE branch is built from the write set: a field it names takes
	// the row this statement proposed (excluded), a field it does not keeps
	// the stored column. Choosing between the two in the STATEMENT rather
	// than in Go keeps the whole decision inside the one upsert, so a
	// preserved field is read and rewritten atomically instead of being
	// merged from a row this transaction read a moment earlier.
	//
	// alternate_id needs no special case any more: excluded.alternate_id is
	// already the nullif'd value from the VALUES list above, so writing it
	// stores the caller's alternate (or NULL, the explicit detach), and not
	// writing it keeps the existing one. That is the same three-state
	// reading the old raw-$9 case expression implemented by hand, now the
	// same rule every other field follows.
	assign := func(col string) string {
		if write.Has(roleColumnFields[col]) {
			return col + " = excluded." + col
		}
		return col + " = system_role." + col
	}
	sets := make([]string, 0, len(roleWriteColumns)+1)
	for _, c := range roleWriteColumns {
		sets = append(sets, assign(c))
	}
	sets = append(sets, "updated_at = now()")

	r, err := scanSystemRole(tx.QueryRow(ctx, fmt.Sprintf(`
		insert into system_role (owner_kind, %s, name, display_name, quorum, capacity, position_labels, impact, alternate_id)
		values ($1, %s, $3, $4, $5, $6, $7, $8, nullif($9, '')::uuid)
		on conflict (owner_kind, standard_id, system_id, name) do update
			set %s
		returning `+systemRoleCols, col, roleOwnerExpr(ownerKind), strings.Join(sets, ",\n\t\t\t    ")),
		ownerKind, ownerArg, spec.Name, display, quorum, capacity, positionLabels, impact, alternateID))
	if err != nil {
		return nil, mapRoleWriteErr(err)
	}
	r.OwnerID = ownerID

	// The typed-slot guard's requirement, replaced wholesale when the write
	// set names it: what the caller sends is what the role accepts (types)
	// and pins (products) afterwards. A set the write does not name is left
	// exactly as it stands, and read back below rather than echoed, since
	// the spec no longer says what the role holds.
	if write.Has(RoleFieldAcceptedTypes) {
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
	}

	if write.Has(RoleFieldPinnedProducts) {
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
	}

	if !write.Has(RoleFieldAcceptedTypes) || !write.Has(RoleFieldPinnedProducts) {
		types, products, err := roleTypedSlot(ctx, tx, r.ID)
		if err != nil {
			return nil, err
		}
		if !write.Has(RoleFieldAcceptedTypes) {
			r.AcceptedTypes = types
		}
		if !write.Has(RoleFieldPinnedProducts) {
			r.PinnedProducts = products
		}
	}

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
	//
	// ownerArg, not ownerID: for a system owner it is already resolved (see
	// roleOwnerArg above), so systemsForRoleOwner's own resolve is a
	// same-row, id-keyed lookup rather than a second pass over the caller's
	// original reference. Its result feeds recomputeChain directly (not
	// recomputeSystems' name-based wrapper), since every ownerRef it returns
	// already carries an id.
	affected, err := p.systemsForRoleOwner(ctx, tx, ownerKind, ownerArg)
	if err != nil {
		return nil, err
	}
	if err := p.recomputeChain(ctx, tx, nil, affected, nil); err != nil {
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

	// Resolved once (see SetSystemRole / roleOwnerArg).
	ownerArg, err := p.roleOwnerArg(ctx, tx, ownerKind, ownerID)
	if err != nil {
		return err
	}

	// Delete and capture the before-image in one statement, so the audit records
	// the withdrawn declaration and a missing row is caught without a second read.
	before, err := scanSystemRole(tx.QueryRow(ctx, fmt.Sprintf(`
		delete from system_role
		where owner_kind = $1 and %s = %s and name = $3
		returning `+systemRoleCols, col, roleOwnerExpr(ownerKind)), ownerKind, ownerArg, name))
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
	// ownerArg, not ownerID; see SetSystemRole's comment on the same shape.
	affected, err := p.systemsForRoleOwner(ctx, tx, ownerKind, ownerArg)
	if err != nil {
		return err
	}
	if err := p.recomputeChain(ctx, tx, nil, affected, nil); err != nil {
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
	ownerArg, err := p.roleOwnerArg(ctx, p.pool, ownerKind, ownerID)
	if err != nil {
		return err
	}
	var id string
	err = p.pool.QueryRow(ctx, fmt.Sprintf(`
		insert into system_role (owner_kind, %s, name, display_name, quorum, capacity, position_labels, impact, alternate_id)
		values ($1, %s, $3, $4, $5, $6, $7, $8, nullif($9, '')::uuid)
		on conflict (owner_kind, standard_id, system_id, name) do nothing
		returning id`, col, roleOwnerExpr(ownerKind)),
		ownerKind, ownerArg, spec.Name, spec.DisplayName, max(spec.Quorum, 1), spec.Capacity, positionLabels, impact, spec.AlternateID).Scan(&id)
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
