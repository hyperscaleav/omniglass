package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// The registry rename (#555, ADR-0555): the door ADR-0062 promised. A registry
// took a uuid primary key and a renameable name so that a typo or a rebrand
// could be corrected, every foreign key holding the uuid; until this, nothing
// but raw SQL could move a registry's name. The rename is the house custom
// method (ADR-0076), one primitive for every registry it covers rather than a
// name field on five patch structs, and it is the same act on each: validate
// the new name, refuse an official row, move the one column, carry the
// registry's own by-value references along, and audit it in the transaction.

// ErrRegistryNotRenameable is a rename naming a table outside the set below: a
// table that is not a registry, or a registry whose rename has ripples this
// primitive does not carry yet. Refused by name rather than attempted.
var ErrRegistryNotRenameable = errors.New("storage: table is not a renameable registry")

// ErrRegistrySeededExample is a rename of a shipped example row: the boot seed
// plants it by name, only when absent (SeedStandard), so a renamed one would be
// planted again on the next boot beside the operator's row. Refused instead; a
// standard of the operator's own is created, or, with #827, forked from the
// template the example stands in for.
var ErrRegistrySeededExample = errors.New("storage: a seeded example keeps its name")

// renameableRegistry describes one registry the rename covers.
type renameableRegistry struct {
	// stamped says the table carries an updated_at the rename stamps.
	stamped bool
	// seeded says the table carries a seeded mark (a shipped example the boot
	// seed plants by name), which refuses the rename.
	seeded bool
	// carry rewrites the places this registry's name is stored BY VALUE rather
	// than through the uuid, inside the rename's transaction. Nil for a registry
	// referenced only by uuid.
	carry func(ctx context.Context, tx pgx.Tx, rowID, oldName, newName string) error
}

// renameableRegistries are the registries RenameRegistry moves the name of. The
// catalogs keyed by telemetry name (metric, property, event and command types),
// component_type, system_type and secret_type are not here yet: a catalog key is
// also what a node, a driver spec and an integration push by name, which a
// rename has to answer for first (#1188).
var renameableRegistries = map[string]renameableRegistry{
	"vendor":        {stamped: true},
	"driver":        {stamped: true, carry: carryDriverName},
	"product":       {stamped: true},
	"standard":      {stamped: true, seeded: true},
	"location_type": {carry: carryLocationTypeName},
	// A notification type is referenced only by uuid (an action rule's
	// foreign key) and is nothing a node or a driver pushes by name (#618).
	"notification_type": {stamped: true},
}

// RenameRegistry moves a registry row's name, addressed by its current name or
// its uuid, and returns the row's uuid. Every foreign key holds the uuid, so
// inside the platform nothing breaks; outside it, a script or a bookmark that
// typed the old name stops resolving, which is why the rename is its own act
// with its own permission rather than a patch field. A label rendered from the
// name is not restamped: like any edit to a fleet-wide classification, that is
// the recompute verb's to apply (label_recompute.go).
//
// Refusals: a table outside renameableRegistries is ErrRegistryNotRenameable;
// an illegal or uuid-shaped name is the name rule's error; an unknown ref is
// ErrTypeNotFound; an official row is ErrTypeOfficial, since the boot seed owns
// it by name and would re-create the old one on the next start; a seeded
// example (a standard the boot seed plants when absent) is
// ErrRegistrySeededExample, for the same reason; a taken name is ErrTypeExists;
// and a location_type may not take the reserved "root". A driver's rename
// re-derives its attached tasks (carryDriverName), and a location_type's
// carries its by-value copies (carryLocationTypeName).
func (p *PG) RenameRegistry(ctx context.Context, actorID, table, ref, newName string) (string, error) {
	reg, ok := renameableRegistries[table]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrRegistryNotRenameable, table)
	}
	if err := ValidateName(table, newName); err != nil {
		return "", err
	}
	if table == "location_type" && newName == RootPlacement {
		return "", ErrReservedTypeID
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("storage: begin rename %s: %w", table, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	seededCol := "false"
	if reg.seeded {
		seededCol = "seeded"
	}
	var rowID, oldName string
	var official, seeded bool
	err = tx.QueryRow(ctx, `select id, name, official, `+seededCol+` from `+table+` where `+registryRefCol(ref)+` = $1 for update`, ref).
		Scan(&rowID, &oldName, &official, &seeded)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTypeNotFound
	}
	if err != nil {
		return "", fmt.Errorf("storage: load %s %q: %w", table, ref, err)
	}
	if official {
		return "", ErrTypeOfficial
	}
	if seeded {
		return "", ErrRegistrySeededExample
	}
	if oldName == newName {
		return rowID, nil
	}
	before, err := registryAuditImage(ctx, tx, table, rowID)
	if err != nil {
		return "", fmt.Errorf("storage: audit image %s %q: %w", table, rowID, err)
	}
	set := `name = $2`
	if reg.stamped {
		set += `, updated_at = now()`
	}
	if _, err := tx.Exec(ctx, `update `+table+` set `+set+` where id = $1`, rowID, newName); err != nil {
		if isUniqueViolation(err) {
			return "", ErrTypeExists
		}
		return "", fmt.Errorf("storage: rename %s %q: %w", table, oldName, err)
	}
	if reg.carry != nil {
		if err := reg.carry(ctx, tx, rowID, oldName, newName); err != nil {
			return "", err
		}
	}
	after, err := registryAuditImage(ctx, tx, table, rowID)
	if err != nil {
		return "", fmt.Errorf("storage: audit image %s %q: %w", table, rowID, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "rename", table, rowID, before, after); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("storage: commit rename %s: %w", table, err)
	}
	return rowID, nil
}

// carryDriverName re-derives the tasks of every endpoint attached through the
// driver: each derived task bakes the driver's name into its spec, and its id
// hashes the spec, so without this the node would keep running tasks under a
// name the registry no longer has, and the next unrelated driver edit would
// take the churn instead. It is the re-derive a version change runs (#823):
// superseded tasks retire, the new ones take their place, and each endpoint is
// bumped so its node picks the set up.
func carryDriverName(ctx context.Context, tx pgx.Tx, rowID, _, _ string) error {
	return rederiveDriverTasks(ctx, tx, rowID)
}

// carryLocationTypeName rewrites the one by-value reference a location_type has:
// allowed_parent_types holds type NAMES (ADR-0062 left it a within-registry
// reference by name), on every type row and inside an operator's fork of a
// shipped type (registry_shadow.image). Without this a rename would silently
// strand every placement rule that named the type, refusing locations the
// operator had allowed. Order is kept in both, so a read shows the rule as the
// operator wrote it.
func carryLocationTypeName(ctx context.Context, tx pgx.Tx, _, oldName, newName string) error {
	if _, err := tx.Exec(ctx, `
		update location_type
		set allowed_parent_types = array_replace(allowed_parent_types, $1, $2)
		where $1 = any(allowed_parent_types)`, oldName, newName); err != nil {
		return fmt.Errorf("storage: carry location_type rename into allowed_parent_types: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		update registry_shadow
		set image = jsonb_set(image, '{allowed_parent_types}', (
				select coalesce(jsonb_agg(case when e.v = $2 then to_jsonb($3::text) else to_jsonb(e.v) end order by e.n), '[]'::jsonb)
				from jsonb_array_elements_text(image->'allowed_parent_types') with ordinality as e(v, n))),
		    updated_at = now()
		where registry = $1
		  and jsonb_typeof(image->'allowed_parent_types') = 'array'
		  and image->'allowed_parent_types' @> to_jsonb(array[$2::text])`,
		locationTypeRegistry, oldName, newName); err != nil {
		return fmt.Errorf("storage: carry location_type rename into forks: %w", err)
	}
	return nil
}
