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
	Name        string
	DisplayName string
}

// RoleChoiceSpec is the declaration input for one choice: an exclusive-or
// group a system satisfies through exactly one of its named alternates.
type RoleChoiceSpec struct {
	Name        string
	DisplayName string
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

// SeedRoleChoice installs one choice and its alternates for the boot-seed
// phase, insert-if-absent on the same lane SeedSystemRole and SeedStandard
// already use: an operator's edit to a shipped choice or alternate survives
// the next boot, so a re-run neither duplicates nor reasserts over it. The
// alternates' owner columns are stamped from the choice row itself, never
// taken from the caller (the FK that keeps a role from joining a foreign
// alternate depends on that). Returns the alternate ids by name, resolved
// whether this call just created them or they already existed, so the roles
// that join them can be seeded in the same pass without a second lookup.
func (p *PG) SeedRoleChoice(ctx context.Context, ownerKind, ownerID string, spec RoleChoiceSpec) (map[string]string, error) {
	if err := ValidateName("role_choice", spec.Name); err != nil {
		return nil, err
	}
	col, err := roleOwnerColumn(ownerKind)
	if err != nil {
		return nil, err
	}

	var choiceID string
	err = p.pool.QueryRow(ctx, fmt.Sprintf(`
		insert into role_choice (owner_kind, %s, name, display_name)
		values ($1, %s, $3, $4)
		on conflict (owner_kind, owner_ref, name) do nothing
		returning id`, col, roleOwnerExpr(ownerKind)),
		ownerKind, ownerID, spec.Name, spec.DisplayName).Scan(&choiceID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = p.pool.QueryRow(ctx, fmt.Sprintf(
			`select id from role_choice where owner_kind = $1 and %s = %s and name = $3`,
			col, roleOwnerExpr(ownerKind)), ownerKind, ownerID, spec.Name).Scan(&choiceID)
	}
	if err != nil {
		return nil, mapRoleWriteErr(err)
	}

	out := make(map[string]string, len(spec.Alternates))
	for i, alt := range spec.Alternates {
		if err := ValidateName("choice_alternate", alt.Name); err != nil {
			return nil, err
		}
		var altID string
		err := p.pool.QueryRow(ctx, `
			insert into choice_alternate (choice_id, owner_kind, standard_id, system_id, name, display_name, position)
			select rc.id, rc.owner_kind, rc.standard_id, rc.system_id, $2, $3, $4
			from role_choice rc where rc.id = $1
			on conflict (choice_id, name) do nothing
			returning id`, choiceID, alt.Name, alt.DisplayName, i+1).Scan(&altID)
		if errors.Is(err, pgx.ErrNoRows) {
			err = p.pool.QueryRow(ctx,
				`select id from choice_alternate where choice_id = $1 and name = $2`,
				choiceID, alt.Name).Scan(&altID)
		}
		if err != nil {
			return nil, fmt.Errorf("storage: seed alternate %s/%s: %w", spec.Name, alt.Name, err)
		}
		out[alt.Name] = altID
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

	var altID string
	err = tx.QueryRow(ctx, fmt.Sprintf(`
		select ca.id from choice_alternate ca
		join role_choice rc on rc.id = ca.choice_id
		where rc.owner_kind = $1 and rc.%s = %s and rc.name = $3 and ca.name = $4`,
		col, roleOwnerExpr(ownerKind)), ownerKind, ownerID, choiceName, altName).Scan(&altID)
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

	var choiceID string
	err = tx.QueryRow(ctx, fmt.Sprintf(
		`select id from role_choice where owner_kind = $1 and %s = %s and name = $3`,
		col, roleOwnerExpr(ownerKind)), ownerKind, ownerID, name).Scan(&choiceID)
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
