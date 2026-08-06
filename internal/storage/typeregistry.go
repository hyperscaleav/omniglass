package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The type registries and catalogs (location_type, standard, vendor, driver,
// capability, product) are flat, unscoped reference tables sharing one shape: a
// stable id, an official flag (seed-owned rows), and a display_name. They
// are not scoped-tree entities, so they use these registry helpers rather than
// scopedcrud. Operator rows are official=false; seeded rows are official=true and
// read-only. A row cannot be deleted while inventory still references it (the
// parent FK also enforces this; the pre-count turns the raw FK error into a clean
// ErrTypeInUse).
var (
	ErrTypeNotFound = errors.New("storage: type not found")
	ErrTypeExists   = errors.New("storage: type id already exists")
	ErrTypeOfficial = errors.New("storage: official type is read-only")
	ErrTypeInUse    = errors.New("storage: type is referenced by existing rows")
)

// registryAuditImage loads a registry row as a column-keyed map for an audit
// image (#228): an update records (before, after) in the same shape and a
// delete records the whole row it removed, so reference-data history is
// complete and the console's field diff lines up key for key. row_to_json
// keeps the helper table-agnostic; every table name is a compile-time
// constant at its call site and registryRefCol is the sanctioned dynamic
// fragment. A missing row surfaces pgx.ErrNoRows for the caller to map to
// its own not-found sentinel (the guard has usually run, so this is the
// deleted-between-guard-and-write race).
func registryAuditImage(ctx context.Context, tx pgx.Tx, table, ref string) (map[string]any, error) {
	var img map[string]any
	if err := tx.QueryRow(ctx, `select row_to_json(t) from `+table+` t where `+registryRefCol(ref)+` = $1`, ref).Scan(&img); err != nil {
		return nil, err
	}
	return img, nil
}

// auditImageID reads the primary key out of a row image the caller already
// holds. An audit row must key on the entity, never on whichever reference the
// caller used to address it, or a rename orphans every row keyed on the old
// name. Delete paths fetch the before image but no typed struct, so this reuses
// the id already in hand instead of spending a second round trip on it.
func auditImageID(img map[string]any) string {
	id, _ := img["id"].(string)
	return id
}

// typeRef names the parent table and column that reference a type id, for the
// delete-in-use guard (e.g. {"location", "location_type"}).
type typeRef struct {
	table string
	col   string
}

// isUniqueViolation reports whether err is a Postgres unique_violation (23505),
// used to turn a duplicate type id into ErrTypeExists.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// registryRefCol picks the column that addresses a registry row. Every registry
// now has a uuid primary key and a renameable `name` (epic #262), so a caller
// passes whichever form it holds and this decides the column. The two can never
// collide: a handle is kebab and a uuid is not.
func registryRefCol(ref string) string {
	if isUUID(ref) {
		return "id"
	}
	return "name"
}

// guardTypeMutable loads a type row's official flag by id: ErrTypeNotFound if
// absent, ErrTypeOfficial if seed-owned. Update and delete call it first.
func guardTypeMutable(ctx context.Context, q querier, table, id string) error {
	var official bool
	err := q.QueryRow(ctx, `select official from `+table+` where `+registryRefCol(id)+` = $1`, id).Scan(&official)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTypeNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: load %s %q: %w", table, id, err)
	}
	if official {
		return ErrTypeOfficial
	}
	return nil
}

// countTypeRefs counts inventory rows referencing a type id, for the
// delete-in-use guard.
func countTypeRefs(ctx context.Context, q querier, ref typeRef, id string) (int, error) {
	var n int
	if err := q.QueryRow(ctx, `select count(*) from `+ref.table+` where `+ref.col+` = $1`, id).Scan(&n); err != nil {
		return 0, fmt.Errorf("storage: count %s refs: %w", ref.table, err)
	}
	return n, nil
}

// deleteTypeRow removes a custom type row by id in one transaction: refuses an
// official row (ErrTypeOfficial), refuses a row still referenced by inventory
// (ErrTypeInUse), deletes, and audits. resource is the audit label
// (e.g. "location_type").
func deleteTypeRow(ctx context.Context, p *PG, table, resource string, ref typeRef, actorID, id string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete %s: %w", table, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, table, id); err != nil {
		return err
	}
	// A registry is addressed by either form; the refs count and the delete both
	// key on the row's uuid, so resolve it once.
	var uid string
	if err := tx.QueryRow(ctx, `select id from `+table+` where `+registryRefCol(id)+` = $1`, id).Scan(&uid); err != nil {
		return ErrTypeNotFound
	}
	n, err := countTypeRefs(ctx, tx, ref, uid)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrTypeInUse
	}
	before, err := registryAuditImage(ctx, tx, table, uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTypeNotFound
		}
		return fmt.Errorf("storage: audit image %s %q: %w", table, id, err)
	}
	if _, err := tx.Exec(ctx, `delete from `+table+` where id = $1`, uid); err != nil {
		return fmt.Errorf("storage: delete %s %q: %w", table, id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", resource, uid, before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete %s: %w", table, err)
	}
	return nil
}

// ErrReferenced is a delete refused because some other row still points at the
// target. It is deliberately separate from the per-entity "occupied" sentinels,
// which mean the narrower and knowable "this row has structural children": the
// delete path sees only that a foreign key stopped it, not which one, so naming
// a cause here would state something it has not established.
var ErrReferenced = errors.New("storage: row is still referenced by another record")

// isReferencedViolation reports whether a delete failed because another row still
// references the target: foreign_key_violation (23503) or the explicit
// restrict_violation (23001) an ON DELETE RESTRICT raises. Both mean the same
// thing to an operator, that the row is still in use.
func isReferencedViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503" || pgErr.Code == "23001"
	}
	return false
}

// requireProperty resolves a property reference (handle or uuid) and returns
// ErrPropertyNotFound when it names nothing, so an unknown property on a contract
// write is the named catalog error rather than a NULL that trips the arc opaquely.
func requireProperty(ctx context.Context, q querier, ref string) error {
	var known bool
	if err := q.QueryRow(ctx, `select true from property_type where `+registryRefCol(ref)+` = $1`, ref).Scan(&known); err != nil {
		return ErrPropertyTypeNotFound
	}
	return nil
}

// requireMetricType is requireProperty on the metric lane: an unknown metric on
// a contract write is ErrMetricTypeNotFound, never a NULL tripping the arc.
func requireMetricType(ctx context.Context, q querier, ref string) error {
	var known bool
	if err := q.QueryRow(ctx, `select true from metric_type where `+registryRefCol(ref)+` = $1`, ref).Scan(&known); err != nil {
		return ErrMetricTypeNotFound
	}
	return nil
}

// requireRegistryRow resolves any registry reference (handle or uuid) against
// its table and returns ErrTypeNotFound when it names nothing: the classifier-
// existence guard the contract seed paths share, so an unknown classifier is
// the named error rather than a not-null violation from a NULL subselect.
func requireRegistryRow(ctx context.Context, q querier, table, ref string) error {
	var known bool
	if err := q.QueryRow(ctx, `select true from `+table+` where `+registryRefCol(ref)+` = $1`, ref).Scan(&known); err != nil {
		return ErrTypeNotFound
	}
	return nil
}
