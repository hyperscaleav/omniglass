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

// guardTypeMutable loads a type row's official flag by id: ErrTypeNotFound if
// absent, ErrTypeOfficial if seed-owned. Update and delete call it first.
func guardTypeMutable(ctx context.Context, q querier, table, id string) error {
	var official bool
	err := q.QueryRow(ctx, `select official from `+table+` where id = $1`, id).Scan(&official)
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
	n, err := countTypeRefs(ctx, tx, ref, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrTypeInUse
	}
	if _, err := tx.Exec(ctx, `delete from `+table+` where id = $1`, id); err != nil {
		return fmt.Errorf("storage: delete %s %q: %w", table, id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", resource, id, map[string]string{"id": id}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete %s: %w", table, err)
	}
	return nil
}
