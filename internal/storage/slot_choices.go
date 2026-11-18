package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The choice side of the slot model (#626): a slot can belong to an
// exclusive-or group instead of contributing to its system unconditionally.
// slot_choice is the group; choice_alternate is one way to satisfy it;
// system_slot.alternate_id (slot_declarations.go, slots.go) is how a slot
// joins one. internal/health.SystemVerdictWith is the pure fold that reads
// the grouping this file writes; splitHealthSlots (health.go) is what turns
// a system's resolved slots back into that fold's shape.

// AlternateSpec is one way to satisfy a choice, an ordered list of names.
type AlternateSpec struct {
	Name  string
	Label string
}

// SlotChoiceSpec is the declaration input for one choice: an exclusive-or
// group a system satisfies through exactly one of its named alternates.
type SlotChoiceSpec struct {
	Name  string
	Label string
	// Alternates is in the order a tie between two equally-satisfied
	// alternates breaks by (internal/health.Choice.Active): position is
	// assigned 1..n from this order.
	Alternates []AlternateSpec
}

// ErrChoiceNotFound and ErrAlternateNotFound are the not-found sentinels for
// the choice write surface, the same shape ErrSlotNotFound already gives
// system_slot.
var (
	ErrChoiceNotFound    = errors.New("storage: slot choice not found")
	ErrAlternateNotFound = errors.New("storage: choice alternate not found")
)

// ChoiceInUseShortfall is the refusal when a choice, or one of its
// alternates, still has slots attached (#626): alternate_id is ON DELETE
// RESTRICT, never SET NULL (see the migration comment on
// system_slot_alternate_fk), because a slot in no alternate reads as
// unconditional, so silently detaching it on delete would promote it from
// conditional to mandatory. Alternate is empty when the whole choice was
// named rather than one alternate within it.
type ChoiceInUseShortfall struct {
	Choice    string
	Alternate string
	Slots     []string
}

func (e *ChoiceInUseShortfall) Error() string {
	who := e.Choice
	if e.Alternate != "" {
		who = e.Choice + "/" + e.Alternate
	}
	return fmt.Sprintf("storage: %q still holds %s; detach or delete them first", who, strings.Join(e.Slots, ", "))
}

// ResolveAlternate turns a "choice/alternate" wire reference into the
// choice_alternate id SystemSlotSpec.AlternateID expects, scoped to the
// owner about to declare the slot. The composite FK
// (system_slot_alternate_fk) also refuses an id from a foreign owner, but
// resolving it explicitly here means an unknown or malformed reference is a
// clean ErrSlotRefNotFound before any write, not a silent NULL that would
// read as "detach" (alternate_id is nullable, so a bad name resolving to
// NULL through a bare subquery would have exactly that ambiguity, unlike
// accepted_types/pinned_products where a NOT NULL join column catches it).
//
// An empty ref resolves to a pointer to "", the explicit detach value; this
// never returns a nil *string, since SystemSlotSpec reserves nil for "the
// caller did not mention this field at all", a distinction only the caller
// (which knows what the request omitted) can make.
func (p *PG) ResolveAlternate(ctx context.Context, ownerKind, ownerID, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	choiceName, altName, ok := strings.Cut(ref, "/")
	if !ok {
		return "", ErrSlotRefNotFound
	}
	col, err := slotOwnerColumn(ownerKind)
	if err != nil {
		return "", err
	}
	// Resolved once (see slot_declarations.go's slotOwnerArg): a system
	// owner's ownerID is ambiguity-safe here rather than left for
	// slotOwnerExpr's scalar subquery, which raises SQLSTATE 21000 the
	// moment two systems share a name (#627).
	ownerArg, err := p.slotOwnerArg(ctx, p.pool, ownerKind, ownerID)
	if err != nil {
		return "", err
	}
	var id string
	err = p.pool.QueryRow(ctx, fmt.Sprintf(`
		select ca.id from choice_alternate ca
		join slot_choice rc on rc.id = ca.choice_id
		where rc.owner_kind = $1 and rc.%s = %s and rc.name = $3 and ca.name = $4`,
		col, slotOwnerExpr(ownerKind)), ownerKind, ownerArg, choiceName, altName).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrSlotRefNotFound
	}
	if err != nil {
		return "", fmt.Errorf("storage: resolve alternate ref %s/%s/%s: %w", ownerKind, ownerID, ref, err)
	}
	return id, nil
}

// attachedSlotNames lists the slots currently naming altID as their
// alternate, the slots a delete would otherwise silently detach.
func attachedSlotNames(ctx context.Context, q txQuerier, altID string) ([]string, error) {
	rows, err := q.Query(ctx, `select name from system_slot where alternate_id = $1 order by name`, altID)
	if err != nil {
		return nil, fmt.Errorf("storage: slots attached to alternate %s: %w", altID, err)
	}
	defer rows.Close()
	return scanNames(rows, "slots attached to alternate")
}

// attachedSlotNamesForChoice is attachedSlotNames widened to every
// alternate under one choice, for DeleteChoice's wider refusal.
func attachedSlotNamesForChoice(ctx context.Context, q txQuerier, choiceID string) ([]string, error) {
	rows, err := q.Query(ctx, `
		select sr.name from system_slot sr
		join choice_alternate ca on ca.id = sr.alternate_id
		where ca.choice_id = $1
		order by sr.name`, choiceID)
	if err != nil {
		return nil, fmt.Errorf("storage: slots attached to choice %s: %w", choiceID, err)
	}
	defer rows.Close()
	return scanNames(rows, "slots attached to choice")
}

// DeleteAlternate withdraws one alternate from a choice, refusing with
// ChoiceInUseShortfall while any slot still names it: alternate_id's ON
// DELETE RESTRICT would raise a bare foreign-key violation for the same
// case, this names which slots are in the way so an operator has something
// to act on.
func (p *PG) DeleteAlternate(ctx context.Context, actorID, ownerKind, ownerID, choiceName, altName string) error {
	col, err := slotOwnerColumn(ownerKind)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete alternate: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Resolved once (see slot_declarations.go's slotOwnerArg).
	ownerArg, err := p.slotOwnerArg(ctx, tx, ownerKind, ownerID)
	if err != nil {
		return err
	}

	var altID string
	err = tx.QueryRow(ctx, fmt.Sprintf(`
		select ca.id from choice_alternate ca
		join slot_choice rc on rc.id = ca.choice_id
		where rc.owner_kind = $1 and rc.%s = %s and rc.name = $3 and ca.name = $4`,
		col, slotOwnerExpr(ownerKind)), ownerKind, ownerArg, choiceName, altName).Scan(&altID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAlternateNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: resolve alternate %s/%s/%s/%s: %w", ownerKind, ownerID, choiceName, altName, err)
	}

	attached, err := attachedSlotNames(ctx, tx, altID)
	if err != nil {
		return err
	}
	if len(attached) > 0 {
		return &ChoiceInUseShortfall{Choice: choiceName, Alternate: altName, Slots: attached}
	}

	if _, err := tx.Exec(ctx, `delete from choice_alternate where id = $1`, altID); err != nil {
		return mapSlotWriteErr(err)
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
// refusing with ChoiceInUseShortfall while any slot anywhere in the choice
// still names one of its alternates: the same reasoning as DeleteAlternate,
// widened to the group.
func (p *PG) DeleteChoice(ctx context.Context, actorID, ownerKind, ownerID, name string) error {
	col, err := slotOwnerColumn(ownerKind)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete choice: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Resolved once (see DeleteAlternate).
	ownerArg, err := p.slotOwnerArg(ctx, tx, ownerKind, ownerID)
	if err != nil {
		return err
	}

	var choiceID string
	err = tx.QueryRow(ctx, fmt.Sprintf(
		`select id from slot_choice where owner_kind = $1 and %s = %s and name = $3`,
		col, slotOwnerExpr(ownerKind)), ownerKind, ownerArg, name).Scan(&choiceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrChoiceNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: resolve choice %s/%s/%s: %w", ownerKind, ownerID, name, err)
	}

	attached, err := attachedSlotNamesForChoice(ctx, tx, choiceID)
	if err != nil {
		return err
	}
	if len(attached) > 0 {
		return &ChoiceInUseShortfall{Choice: name, Slots: attached}
	}

	if _, err := tx.Exec(ctx, `delete from slot_choice where id = $1`, choiceID); err != nil {
		return mapSlotWriteErr(err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "slot_choice", choiceID, nil, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete choice: %w", err)
	}
	return nil
}
