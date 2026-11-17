package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The declaration side of the slot model: who DECLARES a slot (a standard, so
// every conforming system inherits it, or one system ad-hoc) and the
// typed-slot requirement it enforces at assignment. The resolvers in slots.go
// read what this writes; nothing here resolves, so the two stay separable.

// ErrUnknownSlotOwner guards the slot owner-arc column mapping. A slot owner is
// a standard or a system and nothing else, so an unrecognized kind is refused
// rather than left to write a NULL through the arc.
var ErrUnknownSlotOwner = errors.New("storage: unknown slot owner_kind")

// OwnerID is not in the column list (it lives in whichever arc column the owner
// kind selects), so the caller stamps it from the address it queried by, the way
// property_value's scan does. alternate_id is here (unlike the rest of the
// choice-side identity, which the read side does not surface yet) because
// SetSystemSlot and DeleteSystemSlot's before/after images are built from
// this list: omitting it left the audit trail blind to exactly the field
// this fix round made deliberately writable, so a slot moving between
// alternates left no evidence.
var systemSlotCols = `id, owner_kind, name, coalesce(label, ''), quorum, capacity, position_labels, impact, alternate_id, ` +
	alternateRefExpr("system_slot") + `, created_at, updated_at`

// alternateRefExpr renders a slot's alternate membership as the address the
// write body takes, "choice-name/alternate-name", empty when the slot is
// unconditional (#640). A scalar subquery rather than a join so it drops into
// a select list, a RETURNING clause, and a per-row correlated read alike
// without changing the shape of the statement around it; table is whatever the
// surrounding statement calls system_slot.
func alternateRefExpr(table string) string {
	return `coalesce((select rc.name || '/' || ca.name
	                     from choice_alternate ca join slot_choice rc on rc.id = ca.choice_id
	                    where ca.id = ` + table + `.alternate_id), '')`
}

// slotOwnerColumn maps a slot owner kind to its exclusive-arc column. Every
// identifier it returns is a compile-time constant, never caller input, so
// interpolating one into a statement is safe.
// slotOwnerExpr is the SQL for the value system_slot's arc stores, given
// whatever slotOwnerArg resolved $2 to. Both arcs accept either form (name or
// uuid, ADR-0062): a standard's row is a catalog entry addressed by handle or
// uuid directly; a system's is resolved once by slotOwnerArg before this
// expression ever runs; the `or id::text = $2` arm here is what makes an
// already-resolved id (or an ownerID that happened to be uuid-shaped) match.
// Keeping this as an expression means the surrounding statements do not
// branch on owner kind.
func slotOwnerExpr(ownerKind string) string {
	if ownerKind == "system" {
		return `(select id from system where name = $2 or id::text = $2)`
	}
	return `(select id from standard where id::text = $2 or id = (select e.id from standard_in_effect() e where e.name = $2))`
}

// slotOwnerArg resolves ownerID once for the slot-owner arc, ambiguity-safe
// (#627). For a system owner: a name or uuid that identifies exactly one row
// resolves to its id, which slotOwnerExpr's `id::text = $2` arm then matches
// directly, never re-deriving it from a name a second query could land on a
// different row for. A name that identifies NONE is passed through
// unresolved, so the write falls through to the existing
// system_slot_owner_arc_check -> ErrSlotRefNotFound path exactly as before
// (that CHECK, not this function, is what turns an unknown owner into a
// clean sentinel; forcing an early not-found here would trade a stable
// sentinel for a different one). A name that identifies two or more is
// refused outright as ErrAmbiguousName, rather than ever reaching a query
// that could raise SQLSTATE 21000. A standard owner is untouched: standard
// names stay globally unique, and slotOwnerExpr already resolves either form
// for it.
func (p *PG) slotOwnerArg(ctx context.Context, q querier, ownerKind, ownerID string) (string, error) {
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

// standardRef resolves a standard by name or uuid to its id and name, or
// ErrUnknownStandard. A bare name resolves along the search path (#1432), so it
// names one standard.
func standardRef(ctx context.Context, q querier, ref string) (id, name string, err error) {
	err = q.QueryRow(ctx, `select id, name from `+refRows("standard", ref)+` standard where `+catalogRefCol(ref)+` = $1`, ref).Scan(&id, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrUnknownStandard
	}
	if err != nil {
		return "", "", fmt.Errorf("storage: resolve standard %s: %w", ref, err)
	}
	return id, name, nil
}

func slotOwnerColumn(ownerKind string) (string, error) {
	switch ownerKind {
	case "standard":
		return "standard_id", nil
	case "system":
		return "system_id", nil
	default:
		return "", ErrUnknownSlotOwner
	}
}

func scanSystemSlot(row pgx.Row) (*SystemSlot, error) {
	var r SystemSlot
	if err := row.Scan(&r.ID, &r.OwnerKind, &r.Name, &r.Label, &r.Quorum, &r.Capacity, &r.PositionLabels,
		&r.Impact, &r.AlternateID, &r.Alternate, &r.CreatedAt, &r.UpdatedAt); err != nil {
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

// slotWriteColumns is the system_slot columns SetSystemSlot's upsert may
// write, in statement order, and slotColumnFields maps each one to the wire
// field an update_mask names it by. The pair is what lets the UPDATE branch be
// built from the write set rather than hand-maintained per field.
var (
	slotWriteColumns = []string{"label", "quorum", "capacity", "position_labels", "impact", "alternate_id"}
	slotColumnFields = map[string]string{
		"label":           SlotFieldLabel,
		"quorum":          SlotFieldQuorum,
		"capacity":        SlotFieldCapacity,
		"position_labels": SlotFieldPositionLabels,
		"impact":          SlotFieldImpact,
		"alternate_id":    SlotFieldAlternate,
	}
)

// slotGuardNames reads a slot's typed-slot requirement as it stands: the
// accepted types and pinned products by name, both ordered, both empty rather
// than nil when the slot holds none. SetSystemSlot calls it for a set its
// write did not name, where the spec no longer describes what the slot holds.
func slotGuardNames(ctx context.Context, q querier, slotID string) ([]string, []string, error) {
	types, products := []string{}, []string{}
	if err := q.QueryRow(ctx, `
		select coalesce((select array_agg(ct.name order by ct.name)
		                   from system_slot_type rt join component_type ct on ct.id = rt.component_type_id
		                  where rt.slot_id = $1), '{}'),
		       coalesce((select array_agg(pr.name order by pr.name)
		                   from system_slot_product rp join product pr on pr.id = rp.product_id
		                  where rp.slot_id = $1), '{}')`, slotID).Scan(&types, &products); err != nil {
		return nil, nil, fmt.Errorf("storage: read slot typed slot %s: %w", slotID, err)
	}
	return types, products, nil
}

// ListSystemSlots returns the slots one owner declares itself, ordered by
// name, each with its typed-slot requirement. This is the declaration read,
// not the resolution: a system's list carries only its ad-hoc slots, never
// the ones it inherits from its standard (EffectiveSlots is what merges the
// two arcs).
//
// A standard that does not exist is ErrUnknownStandard, not an empty list
// (#505): "declares nothing" and "is not there" are different answers, and
// an empty list for a typo read as the first one.
func (p *PG) ListSystemSlots(ctx context.Context, ownerKind, ownerID string) ([]SystemSlot, error) {
	col, err := slotOwnerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	if ownerKind == "standard" {
		if _, _, err := standardRef(ctx, p.pool, ownerID); err != nil {
			return nil, err
		}
	}
	ownerArg, err := p.slotOwnerArg(ctx, p.pool, ownerKind, ownerID)
	if err != nil {
		return nil, err
	}
	// The columns are spelled out rather than reusing systemSlotCols: the join
	// needs them qualified by the slot alias.
	q := fmt.Sprintf(`
		select r.id, r.owner_kind, r.name, coalesce(r.label, ''), r.quorum, r.capacity, r.position_labels, r.impact,
		       `+alternateRefExpr("r")+` as alternate, r.created_at, r.updated_at,
		       coalesce(array_agg(distinct ct.name order by ct.name) filter (where ct.name is not null), '{}') as types,
		       coalesce(array_agg(distinct pr.name order by pr.name) filter (where pr.name is not null), '{}') as products
		from system_slot r
		left join system_slot_type rt on rt.slot_id = r.id
		left join component_type ct on ct.id = rt.component_type_id
		left join system_slot_product rp on rp.slot_id = r.id
		left join product pr on pr.id = rp.product_id
		where r.owner_kind = $1 and r.%s = %s
		group by r.id
		order by r.name`, col, slotOwnerExpr(ownerKind))

	rows, err := p.pool.Query(ctx, q, ownerKind, ownerArg)
	if err != nil {
		return nil, fmt.Errorf("storage: list slots %s/%s: %w", ownerKind, ownerID, err)
	}
	defer rows.Close()

	out := []SystemSlot{}
	for rows.Next() {
		var r SystemSlot
		if err := rows.Scan(&r.ID, &r.OwnerKind, &r.Name, &r.Label, &r.Quorum, &r.Capacity, &r.PositionLabels,
			&r.Impact, &r.Alternate, &r.CreatedAt, &r.UpdatedAt, &r.AcceptedTypes, &r.PinnedProducts); err != nil {
			return nil, fmt.Errorf("storage: scan slot: %w", err)
		}
		r.OwnerID = ownerID
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetSystemSlot declares a slot on a standard or a system, or revises the
// declaration in place: the slot is addressed by name within its owner arc,
// so the write is an upsert and the surface's save is idempotent.
//
// WHICH fields it writes is spec.Write, the resolved update_mask (#666): a
// field outside the write set keeps whatever the slot already has, and on a
// create takes its default, so an edit can no longer carry away a field it
// never mentioned (#639) and a masked field carrying its zero value clears
// (#638). A caller that sets no mask writes the fields it populated, which is
// what this did for every field but Capacity and AlternateID before the mask
// existed.
//
// A quorum below one means one: a slot no component need fill is not a slot.
// An owner, type, or product that does not exist is ErrSlotRefNotFound (a
// request fault), never a server error.
func (p *PG) SetSystemSlot(ctx context.Context, actorID, ownerKind, ownerID string, spec SystemSlotSpec) (*SystemSlot, error) {
	if err := ValidateName("system_slot", spec.Name); err != nil {
		return nil, err
	}
	col, err := slotOwnerColumn(ownerKind)
	if err != nil {
		return nil, err
	}
	write := spec.writeFields()
	// Each value is taken only if the write set names its field, then
	// defaulted: a field the write does not write contributes its default to
	// the INSERT branch (a create has nothing to preserve) and is discarded
	// by the UPDATE branch, which reads the stored column instead.
	display := spec.Label
	if !write.Has(SlotFieldLabel) {
		display = ""
	}
	if display == "" {
		// A minimal write still reads properly on a surface. The default
		// lives here rather than in the API layer so it applies to the
		// value, not to the decision to write one: the API's body is what
		// says whether label is being written at all.
		display = spec.Name
	}
	quorum := spec.Quorum
	if !write.Has(SlotFieldQuorum) {
		quorum = 0
	}
	if quorum < 1 {
		quorum = 1
	}
	// The impact is validated before the mask narrows anything: a value that
	// is not one of the three is a request fault whether or not this write
	// was going to store it.
	if spec.Impact != "" && !slotImpacts[spec.Impact] {
		return nil, ErrSlotImpact
	}
	impact := spec.Impact
	if !write.Has(SlotFieldImpact) {
		impact = ""
	}
	if impact == "" {
		impact = "degraded"
	}
	capacity := spec.Capacity
	if !write.Has(SlotFieldCapacity) {
		capacity = nil
	}
	labels := spec.PositionLabels
	if !write.Has(SlotFieldPositionLabels) {
		labels = nil
	}
	positionLabels := normalizePositionLabels(labels)
	alternateID := spec.AlternateID
	if !write.Has(SlotFieldAlternate) {
		alternateID = nil
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin set slot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// First, before any other lock: see slotNameLockKey.
	if err := lockSlotNames(ctx, tx); err != nil {
		return nil, err
	}

	// Resolved once, inside this transaction, and reused for both queries
	// below: see slotOwnerArg.
	ownerArg, err := p.slotOwnerArg(ctx, tx, ownerKind, ownerID)
	if err != nil {
		return nil, err
	}

	// A slot name means one slot per system (#503, ADR-0503): declaring a
	// name on a system its standard declares, or on a standard a conforming
	// system declares itself, is refused naming both owners. Checked on
	// every write, not only a create, so an edit to either half of a
	// collision written before the rule surfaces it rather than revising it
	// in place. An unknown standard is refused here as the unknown owner it
	// is (the owner arc CHECK is the backstop for a direct write).
	switch ownerKind {
	case "system":
		if err := systemArcCollision(ctx, tx, ownerArg, spec.Name); err != nil {
			return nil, err
		}
	case "standard":
		stdID, stdName, err := standardRef(ctx, tx, ownerArg)
		if errors.Is(err, ErrUnknownStandard) {
			return nil, ErrSlotRefNotFound
		} else if err != nil {
			return nil, err
		}
		if err := standardArcCollision(ctx, tx, stdID, stdName, spec.Name); err != nil {
			return nil, err
		}
	}

	// The before-image decides create vs update and gives the audit its old side.
	var before any
	prior, err := scanSystemSlot(tx.QueryRow(ctx, fmt.Sprintf(
		`select `+systemSlotCols+` from system_slot where owner_kind = $1 and %s = %s and name = $3`, col, slotOwnerExpr(ownerKind)),
		ownerKind, ownerArg, spec.Name))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("storage: load slot %s/%s/%s: %w", ownerKind, ownerID, spec.Name, err)
	default:
		prior.OwnerID = ownerID
		before = prior
	}

	// Lowering capacity below what some conforming system already has
	// assigned refuses, listing every system that would exceed the new cap,
	// worst first: a standard-owned slot is staffed independently in every
	// system that conforms to it, so any one of them can exceed a new, lower
	// cap regardless of what this system (the one whose edit triggered the
	// check) looks like. The list is fleet-wide, beyond any scope the
	// declarer holds, so it carries ids for the API to name only the systems
	// that caller may read (#1216).
	if capacity != nil && prior != nil {
		rows, err := tx.Query(ctx, `
			select s.id, s.name, count(*) from system_slot_assignment ra
			  join system s on s.id = ra.system_id
			 where ra.slot_id = $1
			 group by s.id, s.name having count(*) > $2
			 order by count(*) desc, s.name, s.id`, prior.ID, *capacity)
		if err != nil {
			return nil, fmt.Errorf("storage: capacity precheck %s: %w", prior.ID, err)
		}
		var excess []CapacityExcess
		for rows.Next() {
			var e CapacityExcess
			if err := rows.Scan(&e.SystemID, &e.System, &e.Have); err != nil {
				rows.Close()
				return nil, fmt.Errorf("storage: capacity precheck %s: %w", prior.ID, err)
			}
			excess = append(excess, e)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("storage: capacity precheck %s: %w", prior.ID, err)
		}
		if len(excess) > 0 {
			return nil, &CapacityShortfall{
				Slot: prior.Label, System: excess[0].System, Have: excess[0].Have, Want: *capacity, Exceeding: excess,
			}
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
		if write.Has(slotColumnFields[col]) {
			return col + " = excluded." + col
		}
		return col + " = system_slot." + col
	}
	sets := make([]string, 0, len(slotWriteColumns)+1)
	for _, c := range slotWriteColumns {
		sets = append(sets, assign(c))
	}
	sets = append(sets, "updated_at = now()")

	r, err := scanSystemSlot(tx.QueryRow(ctx, fmt.Sprintf(`
		insert into system_slot (owner_kind, %s, name, label, quorum, capacity, position_labels, impact, alternate_id)
		values ($1, %s, $3, $4, $5, $6, $7, $8, nullif($9, '')::uuid)
		on conflict (owner_kind, standard_id, system_id, name) do update
			set %s
		returning `+systemSlotCols, col, slotOwnerExpr(ownerKind), strings.Join(sets, ",\n\t\t\t    ")),
		ownerKind, ownerArg, spec.Name, labelOrNull(display), quorum, capacity, positionLabels, impact, alternateID))
	if err != nil {
		return nil, mapSlotWriteErr(err)
	}
	r.OwnerID = ownerID

	// The typed-slot guard's requirement, replaced wholesale when the write
	// set names it: what the caller sends is what the slot accepts (types)
	// and pins (products) afterwards. A set the write does not name is left
	// exactly as it stands, and read back below rather than echoed, since
	// the spec no longer says what the slot holds.
	if write.Has(SlotFieldAcceptedTypes) {
		if _, err := tx.Exec(ctx, `delete from system_slot_type where slot_id = $1`, r.ID); err != nil {
			return nil, fmt.Errorf("storage: clear slot types %s: %w", r.ID, err)
		}
		if len(spec.AcceptedTypes) > 0 {
			if _, err := tx.Exec(ctx, `
				insert into system_slot_type (slot_id, component_type_id)
				select $1, (select id from component_type where id::text = c or id = (select e.id from component_type_in_effect() e where e.name = c))
				from unnest($2::text[]) c
				on conflict (slot_id, component_type_id) do nothing`, r.ID, spec.AcceptedTypes); err != nil {
				return nil, mapSlotWriteErr(err)
			}
		}
		r.AcceptedTypes = append([]string(nil), spec.AcceptedTypes...)
	}

	if write.Has(SlotFieldPinnedProducts) {
		if _, err := tx.Exec(ctx, `delete from system_slot_product where slot_id = $1`, r.ID); err != nil {
			return nil, fmt.Errorf("storage: clear slot products %s: %w", r.ID, err)
		}
		if len(spec.PinnedProducts) > 0 {
			if _, err := tx.Exec(ctx, `
				insert into system_slot_product (slot_id, product_id)
				select $1, (select id from product where id::text = c or id = (select e.id from product_in_effect() e where e.name = c))
				from unnest($2::text[]) c
				on conflict (slot_id, product_id) do nothing`, r.ID, spec.PinnedProducts); err != nil {
				return nil, mapSlotWriteErr(err)
			}
		}
		r.PinnedProducts = append([]string(nil), spec.PinnedProducts...)
	}

	if !write.Has(SlotFieldAcceptedTypes) || !write.Has(SlotFieldPinnedProducts) {
		types, products, err := slotGuardNames(ctx, tx, r.ID)
		if err != nil {
			return nil, err
		}
		if !write.Has(SlotFieldAcceptedTypes) {
			r.AcceptedTypes = types
		}
		if !write.Has(SlotFieldPinnedProducts) {
			r.PinnedProducts = products
		}
	}

	verb := "create"
	if before != nil {
		verb = "update"
	}
	if err := writeAuditRes(ctx, tx, actorID, verb, "system_slot", r.ID, before, r); err != nil {
		return nil, err
	}
	// A declaration change moves health without touching a component: a raised
	// quorum or a changed impact can impair a slot that was fine a moment ago.
	// A standard's declaration moves every conforming system at once.
	//
	// ownerArg, not ownerID: for a system owner it is already resolved (see
	// slotOwnerArg above), so systemsForSlotOwner's own resolve is a
	// same-row, id-keyed lookup rather than a second pass over the caller's
	// original reference. Its result feeds recomputeChain directly (not
	// recomputeSystems' name-based wrapper), since every ownerRef it returns
	// already carries an id.
	affected, err := p.systemsForSlotOwner(ctx, tx, ownerKind, ownerArg)
	if err != nil {
		return nil, err
	}
	// The systems' slot sets moved, and with them their members.
	if err := markSystemCalcRules(ctx, tx, ownerRefIDs(affected)...); err != nil {
		return nil, err
	}
	if err := p.recomputeChain(ctx, tx, nil, affected, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit set slot: %w", err)
	}
	return r, nil
}

// DeleteSystemSlot withdraws a slot from its owner, taking its typed-slot
// requirement and every assignment to it with it (both cascade). A slot the
// owner does not declare is ErrSlotNotFound, so withdrawing twice is an
// explicit miss rather than a silent no-op.
func (p *PG) DeleteSystemSlot(ctx context.Context, actorID, ownerKind, ownerID, name string) error {
	col, err := slotOwnerColumn(ownerKind)
	if err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete slot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// First, before any other lock: see slotNameLockKey. A withdrawal cascades
	// to the slot's assignments, which a standard change may be carrying.
	if err := lockSlotNames(ctx, tx); err != nil {
		return err
	}

	// Resolved once (see SetSystemSlot / slotOwnerArg).
	ownerArg, err := p.slotOwnerArg(ctx, tx, ownerKind, ownerID)
	if err != nil {
		return err
	}

	// Delete and capture the before-image in one statement, so the audit records
	// the withdrawn declaration and a missing row is caught without a second read.
	before, err := scanSystemSlot(tx.QueryRow(ctx, fmt.Sprintf(`
		delete from system_slot
		where owner_kind = $1 and %s = %s and name = $3
		returning `+systemSlotCols, col, slotOwnerExpr(ownerKind)), ownerKind, ownerArg, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSlotNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: delete slot %s/%s/%s: %w", ownerKind, ownerID, name, err)
	}
	before.OwnerID = ownerID
	if err := writeAuditRes(ctx, tx, actorID, "delete", "system_slot", before.ID, before, nil); err != nil {
		return err
	}
	// Withdrawing a slot can only improve a system: the impaired slot it was
	// contributing is gone. Recompute so the recovery is recorded as an edge.
	// ownerArg, not ownerID; see SetSystemSlot's comment on the same shape.
	affected, err := p.systemsForSlotOwner(ctx, tx, ownerKind, ownerArg)
	if err != nil {
		return err
	}
	// The systems' slot sets moved, and with them their members.
	if err := markSystemCalcRules(ctx, tx, ownerRefIDs(affected)...); err != nil {
		return err
	}
	if err := p.recomputeChain(ctx, tx, nil, affected, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete slot: %w", err)
	}
	return nil
}

// insertSeededGuard writes a template slot's accepted types and pinned products,
// inside the create's transaction. A name the catalog lacks resolves to NULL on
// a NOT NULL column and is ErrSlotRefNotFound, rolling the whole slot back.
func insertSeededGuard(ctx context.Context, tx pgx.Tx, slotID string, types, products []string) error {
	if len(types) > 0 {
		if _, err := tx.Exec(ctx, `
			insert into system_slot_type (slot_id, component_type_id)
			select $1, (select id from component_type where id::text = c or id = (select e.id from component_type_in_effect() e where e.name = c))
			from unnest($2::text[]) c
			on conflict (slot_id, component_type_id) do nothing`, slotID, types); err != nil {
			return mapSlotWriteErr(err)
		}
	}
	if len(products) > 0 {
		if _, err := tx.Exec(ctx, `
			insert into system_slot_product (slot_id, product_id)
			select $1, (select id from product where id::text = c or id = (select e.id from product_in_effect() e where e.name = c))
			from unnest($2::text[]) c
			on conflict (slot_id, product_id) do nothing`, slotID, products); err != nil {
			return mapSlotWriteErr(err)
		}
	}
	return nil
}

// ownerRefIDs is the refs' ids.
func ownerRefIDs(refs []ownerRef) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.ID
	}
	return out
}
