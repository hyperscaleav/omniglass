package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The choice side of the role model (#626): a role can belong to an
// exclusive-or group instead of contributing to its system unconditionally.
// role_choice is the group; choice_alternate is one way to satisfy it;
// system_role.alternate_id (role_declarations.go, roles.go) is how a role
// joins one. internal/health.SystemVerdictWith is the pure fold that reads
// the grouping this file writes; splitHealthRoles (health.go) is what turns
// a system's resolved roles back into that fold's shape.

// AlternateSpec is one way to satisfy a choice, an ordered list of names.
type AlternateSpec struct {
	Name  string
	Label string
}

// RoleChoiceSpec is the declaration input for one choice: an exclusive-or
// group a system satisfies through exactly one of its named alternates.
type RoleChoiceSpec struct {
	Name  string
	Label string
	// Alternates is in the order a tie between two equally-satisfied
	// alternates breaks by (internal/health.Choice.Active): position is
	// assigned 1..n from this order.
	Alternates []AlternateSpec
}

// ErrChoiceNotFound and ErrAlternateNotFound are the not-found sentinels for
// the choice write surface, the same shape ErrRoleNotFound already gives
// system_role.
var (
	ErrChoiceNotFound    = errors.New("storage: role choice not found")
	ErrAlternateNotFound = errors.New("storage: choice alternate not found")
)

// ChoiceInUseShortfall is the refusal when a choice, or one of its
// alternates, still has roles attached (#626): alternate_id is ON DELETE
// RESTRICT, never SET NULL (see the migration comment on
// system_role_alternate_fk), because a role in no alternate reads as
// unconditional, so silently detaching it on delete would promote it from
// conditional to mandatory. Alternate is empty when the whole choice was
// named rather than one alternate within it.
type ChoiceInUseShortfall struct {
	Choice    string
	Alternate string
	Roles     []string
}

func (e *ChoiceInUseShortfall) Error() string {
	who := e.Choice
	if e.Alternate != "" {
		who = e.Choice + "/" + e.Alternate
	}
	return fmt.Sprintf("storage: %q still holds %s; detach or delete them first", who, strings.Join(e.Roles, ", "))
}

// ResolveAlternate turns a "choice/alternate" wire reference into the
// choice_alternate id SystemRoleSpec.AlternateID expects, scoped to the
// owner about to declare the role. The composite FK
// (system_role_alternate_fk) also refuses an id from a foreign owner, but
// resolving it explicitly here means an unknown or malformed reference is a
// clean ErrRoleRefNotFound before any write, not a silent NULL that would
// read as "detach" (alternate_id is nullable, so a bad name resolving to
// NULL through a bare subquery would have exactly that ambiguity, unlike
// accepted_types/pinned_products where a NOT NULL join column catches it).
//
// An empty ref resolves to a pointer to "", the explicit detach value; this
// never returns a nil *string, since SystemRoleSpec reserves nil for "the
// caller did not mention this field at all", a distinction only the caller
// (which knows what the request omitted) can make.
func (p *PG) ResolveAlternate(ctx context.Context, ownerKind, ownerID, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	choiceName, altName, ok := strings.Cut(ref, "/")
	if !ok {
		return "", ErrRoleRefNotFound
	}
	col, err := roleOwnerColumn(ownerKind)
	if err != nil {
		return "", err
	}
	// Resolved once (see role_declarations.go's roleOwnerArg): a system
	// owner's ownerID is ambiguity-safe here rather than left for
	// roleOwnerExpr's scalar subquery, which raises SQLSTATE 21000 the
	// moment two systems share a name (#627).
	ownerArg, err := p.roleOwnerArg(ctx, p.pool, ownerKind, ownerID)
	if err != nil {
		return "", err
	}
	var id string
	err = p.pool.QueryRow(ctx, fmt.Sprintf(`
		select ca.id from choice_alternate ca
		join role_choice rc on rc.id = ca.choice_id
		where rc.owner_kind = $1 and rc.%s = %s and rc.name = $3 and ca.name = $4`,
		col, roleOwnerExpr(ownerKind)), ownerKind, ownerArg, choiceName, altName).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrRoleRefNotFound
	}
	if err != nil {
		return "", fmt.Errorf("storage: resolve alternate ref %s/%s/%s: %w", ownerKind, ownerID, ref, err)
	}
	return id, nil
}

// SeedRoleChoice installs one choice and its alternates for the boot-seed
// phase. DISPLAY_NAME is the one field that stays truly insert-if-absent,
// the same lane SeedSystemRole and SeedStandard use: an operator's edit to
// it survives a re-run, never reasserted. NAME (membership: which
// alternates exist at all) and POSITION are NOT operator-owned, and both
// reconcile to the declared set every boot, existing alternates included.
// This has to be true of both together, not just position: a fixed
// 1-based-index-at-insert-time with no membership reconciliation (the
// original shape) crashed server boot the first time a release was
// anything but a pure append (a new name landing at an already-taken
// position collides on choice_alternate_position_key), and fixing
// position alone still crashed on a rename or a removal, because the
// dropped name's row was never touched, kept its old position, and
// collided with whatever the declared set now put there. A rename reads
// identically to a drop-plus-add on this lane (there is no "renamed from"
// tracking), so both take the same path: any alternate under this choice
// not present in spec.Alternates is deleted, unless a role still points at
// it, in which case the whole call refuses with ChoiceInUseShortfall
// rather than let the FK's RESTRICT surface as a bare constraint violation
// or silently detach the role. Every declared alternate's position is then
// written unconditionally to its 1-based index in spec.Alternates, inside
// one transaction with choice_alternate_position_key's uniqueness
// deferred (see the migration comment there): the reconciliation and the
// reassignment can both pass through a duplicate mid-transaction on the
// way to a valid permutation, and only the state at commit has to be one.
//
// The alternates' owner columns are stamped from the choice row itself,
// never taken from the caller (the FK that keeps a role from joining a
// foreign alternate depends on that). Returns the alternate ids by name,
// resolved whether this call just created them or they already existed, so
// the roles that join them can be seeded in the same pass without a second
// lookup.
func (p *PG) SeedRoleChoice(ctx context.Context, ownerKind, ownerID string, spec RoleChoiceSpec) (map[string]string, error) {
	if err := ValidateName("role_choice", spec.Name); err != nil {
		return nil, err
	}
	col, err := roleOwnerColumn(ownerKind)
	if err != nil {
		return nil, err
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin seed role choice: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Resolved once, inside this transaction (see role_declarations.go's
	// roleOwnerArg): SeedRoleChoice's only non-test caller passes
	// ownerKind="standard" today (a registry, unaffected), but this is a
	// published gateway method whose ownerID is documented name-or-uuid, and
	// a future system-owned seed must not raise SQLSTATE 21000 on server
	// boot the moment two systems share a name (#627).
	ownerArg, err := p.roleOwnerArg(ctx, tx, ownerKind, ownerID)
	if err != nil {
		return nil, err
	}

	var choiceID string
	err = tx.QueryRow(ctx, fmt.Sprintf(`
		insert into role_choice (owner_kind, %s, name, label)
		values ($1, %s, $3, $4)
		on conflict (owner_kind, owner_ref, name) do nothing
		returning id`, col, roleOwnerExpr(ownerKind)),
		ownerKind, ownerArg, spec.Name, labelOrNull(spec.Label)).Scan(&choiceID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, fmt.Sprintf(
			`select id from role_choice where owner_kind = $1 and %s = %s and name = $3`,
			col, roleOwnerExpr(ownerKind)), ownerKind, ownerArg, spec.Name).Scan(&choiceID)
	}
	if err != nil {
		return nil, mapRoleWriteErr(err)
	}

	if _, err := tx.Exec(ctx, `set constraints choice_alternate_position_key deferred`); err != nil {
		return nil, fmt.Errorf("storage: defer alternate position uniqueness: %w", err)
	}

	// Reconcile the STORED alternates to the DECLARED set before touching
	// position: converging the declared names on their declared positions
	// is not enough on its own, because a rename or a removal against an
	// already-seeded database leaves a name that dropped OUT of
	// spec.Alternates still sitting in the table under its old position,
	// which then collides with whichever declared name lands there now. A
	// rename is indistinguishable from a drop-plus-add here (the seed has
	// no "renamed from" tracking), so both are handled the same way: any
	// alternate under this choice not named in spec.Alternates is removed,
	// unless a role still points at it, in which case the whole call
	// refuses with ChoiceInUseShortfall rather than let the FK's RESTRICT
	// surface as a bare constraint violation, or silently detach the role.
	declared := make([]string, len(spec.Alternates))
	for i, alt := range spec.Alternates {
		declared[i] = alt.Name
	}
	orphanRows, err := tx.Query(ctx,
		`select id, name from choice_alternate where choice_id = $1 and name <> all($2::text[])`,
		choiceID, declared)
	if err != nil {
		return nil, fmt.Errorf("storage: find orphaned alternates %s: %w", spec.Name, err)
	}
	type orphan struct{ id, name string }
	var orphans []orphan
	for orphanRows.Next() {
		var o orphan
		if err := orphanRows.Scan(&o.id, &o.name); err != nil {
			orphanRows.Close()
			return nil, fmt.Errorf("storage: scan orphaned alternate: %w", err)
		}
		orphans = append(orphans, o)
	}
	if err := orphanRows.Err(); err != nil {
		orphanRows.Close()
		return nil, fmt.Errorf("storage: iterate orphaned alternates: %w", err)
	}
	orphanRows.Close()
	for _, o := range orphans {
		attached, err := attachedRoleNames(ctx, tx, o.id)
		if err != nil {
			return nil, err
		}
		if len(attached) > 0 {
			return nil, &ChoiceInUseShortfall{Choice: spec.Name, Alternate: o.name, Roles: attached}
		}
		if _, err := tx.Exec(ctx, `delete from choice_alternate where id = $1`, o.id); err != nil {
			return nil, mapRoleWriteErr(err)
		}
	}

	out := make(map[string]string, len(spec.Alternates))
	for i, alt := range spec.Alternates {
		if err := ValidateName("choice_alternate", alt.Name); err != nil {
			return nil, err
		}
		var altID string
		err := tx.QueryRow(ctx, `
			insert into choice_alternate (choice_id, owner_kind, standard_id, system_id, name, label, position)
			select rc.id, rc.owner_kind, rc.standard_id, rc.system_id, $2, $3, $4
			from role_choice rc where rc.id = $1
			on conflict (choice_id, name) do update
				set position = excluded.position
			returning id`, choiceID, alt.Name, labelOrNull(alt.Label), i+1).Scan(&altID)
		if err != nil {
			return nil, mapRoleWriteErr(err)
		}
		out[alt.Name] = altID
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit seed role choice: %w", err)
	}
	return out, nil
}

// attachedRoleNames lists the roles currently naming altID as their
// alternate, the roles a delete would otherwise silently detach.
func attachedRoleNames(ctx context.Context, q txQuerier, altID string) ([]string, error) {
	rows, err := q.Query(ctx, `select name from system_role where alternate_id = $1 order by name`, altID)
	if err != nil {
		return nil, fmt.Errorf("storage: roles attached to alternate %s: %w", altID, err)
	}
	defer rows.Close()
	return scanNames(rows, "roles attached to alternate")
}

// attachedRoleNamesForChoice is attachedRoleNames widened to every
// alternate under one choice, for DeleteChoice's wider refusal.
func attachedRoleNamesForChoice(ctx context.Context, q txQuerier, choiceID string) ([]string, error) {
	rows, err := q.Query(ctx, `
		select sr.name from system_role sr
		join choice_alternate ca on ca.id = sr.alternate_id
		where ca.choice_id = $1
		order by sr.name`, choiceID)
	if err != nil {
		return nil, fmt.Errorf("storage: roles attached to choice %s: %w", choiceID, err)
	}
	defer rows.Close()
	return scanNames(rows, "roles attached to choice")
}

// DeleteAlternate withdraws one alternate from a choice, refusing with
// ChoiceInUseShortfall while any role still names it: alternate_id's ON
// DELETE RESTRICT would raise a bare foreign-key violation for the same
// case, this names which roles are in the way so an operator has something
// to act on.
func (p *PG) DeleteAlternate(ctx context.Context, actorID, ownerKind, ownerID, choiceName, altName string) error {
	col, err := roleOwnerColumn(ownerKind)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete alternate: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Resolved once (see SeedRoleChoice / role_declarations.go's roleOwnerArg).
	ownerArg, err := p.roleOwnerArg(ctx, tx, ownerKind, ownerID)
	if err != nil {
		return err
	}

	var altID string
	err = tx.QueryRow(ctx, fmt.Sprintf(`
		select ca.id from choice_alternate ca
		join role_choice rc on rc.id = ca.choice_id
		where rc.owner_kind = $1 and rc.%s = %s and rc.name = $3 and ca.name = $4`,
		col, roleOwnerExpr(ownerKind)), ownerKind, ownerArg, choiceName, altName).Scan(&altID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAlternateNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: resolve alternate %s/%s/%s/%s: %w", ownerKind, ownerID, choiceName, altName, err)
	}

	attached, err := attachedRoleNames(ctx, tx, altID)
	if err != nil {
		return err
	}
	if len(attached) > 0 {
		return &ChoiceInUseShortfall{Choice: choiceName, Alternate: altName, Roles: attached}
	}

	if _, err := tx.Exec(ctx, `delete from choice_alternate where id = $1`, altID); err != nil {
		return mapRoleWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "choice_alternate", altID, nil, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete alternate: %w", err)
	}
	return nil
}

// DeleteChoice withdraws a whole choice and every alternate under it,
// refusing with ChoiceInUseShortfall while any role anywhere in the choice
// still names one of its alternates: the same reasoning as DeleteAlternate,
// widened to the group.
func (p *PG) DeleteChoice(ctx context.Context, actorID, ownerKind, ownerID, name string) error {
	col, err := roleOwnerColumn(ownerKind)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete choice: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Resolved once (see DeleteAlternate).
	ownerArg, err := p.roleOwnerArg(ctx, tx, ownerKind, ownerID)
	if err != nil {
		return err
	}

	var choiceID string
	err = tx.QueryRow(ctx, fmt.Sprintf(
		`select id from role_choice where owner_kind = $1 and %s = %s and name = $3`,
		col, roleOwnerExpr(ownerKind)), ownerKind, ownerArg, name).Scan(&choiceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrChoiceNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: resolve choice %s/%s/%s: %w", ownerKind, ownerID, name, err)
	}

	attached, err := attachedRoleNamesForChoice(ctx, tx, choiceID)
	if err != nil {
		return err
	}
	if len(attached) > 0 {
		return &ChoiceInUseShortfall{Choice: name, Roles: attached}
	}

	if _, err := tx.Exec(ctx, `delete from role_choice where id = $1`, choiceID); err != nil {
		return mapRoleWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "role_choice", choiceID, nil, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete choice: %w", err)
	}
	return nil
}
