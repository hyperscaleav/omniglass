package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Driver is a registry row naming the implementation that gets/emits/sets a
// product's signals (e.g. "Generic SNMP", "Cisco xAPI"): a stable id, the
// official flag, a display_name, and a version string. It is a flat registry
// like vendor: no tree, and no in-use delete guard in this slice (nothing
// references a driver yet; product will). The registry lists alphabetically by
// display_name; there is no ordering field.
type Driver struct {
	// ID is the uuid primary key, Name the renameable slug handle (ADR-0062).
	ID          string
	Name        string
	DisplayName string
	Version     string
	Official    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// DriverPatch carries the mutable fields of a driver update; a nil field is
// left unchanged.
type DriverPatch struct {
	DisplayName *string
	Version     *string
}

const driverCols = `id, name, display_name, version, official, created_at, updated_at`

func scanDriver(row pgx.Row) (*Driver, error) {
	var d Driver
	if err := row.Scan(&d.ID, &d.Name, &d.DisplayName, &d.Version, &d.Official, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	return &d, nil
}

// UpsertDriver installs or updates a driver by id, the boot-seed phase's write.
// Idempotent: re-seeding the same id updates it in place.
func (p *PG) UpsertDriver(ctx context.Context, d Driver) error {
	_, err := p.pool.Exec(ctx, `
		insert into driver (name, display_name, version, official)
		values ($1, $2, $3, $4)
		on conflict (name) do update
			set display_name = excluded.display_name,
			    version      = excluded.version,
			    official     = excluded.official,
			    updated_at   = now()`,
		d.Name, d.DisplayName, d.Version, d.Official)
	if err != nil {
		return fmt.Errorf("storage: upsert driver %q: %w", d.Name, err)
	}
	return nil
}

// ListDrivers returns every driver, ordered alphabetically by display_name then
// id.
func (p *PG) ListDrivers(ctx context.Context) ([]Driver, error) {
	rows, err := p.pool.Query(ctx, `select `+driverCols+` from driver order by display_name, name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list drivers: %w", err)
	}
	defer rows.Close()
	var out []Driver
	for rows.Next() {
		d, err := scanDriver(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan driver: %w", err)
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// GetDriver resolves one driver by id. An unknown id is ErrTypeNotFound.
func (p *PG) GetDriver(ctx context.Context, id string) (*Driver, error) {
	d, err := scanDriver(p.pool.QueryRow(ctx, `select `+driverCols+` from driver where `+registryRefCol(id)+` = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get driver %q: %w", id, err)
	}
	return d, nil
}

// CreateDriver inserts a custom (official=false) driver and audits it. A
// duplicate id is ErrTypeExists.
func (p *PG) CreateDriver(ctx context.Context, actorID string, d Driver) (*Driver, error) {
	d.Official = false
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create driver: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := tx.QueryRow(ctx, `
		insert into driver (name, display_name, version, official)
		values ($1, $2, $3, false)
		returning id, created_at, updated_at`,
		d.Name, d.DisplayName, d.Version).
		Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrTypeExists
		}
		return nil, fmt.Errorf("storage: insert driver %q: %w", d.Name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "driver", d.ID, nil, d); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create driver: %w", err)
	}
	return &d, nil
}

// UpdateDriver patches a custom driver's display_name or version (nil fields
// unchanged) and audits it. Official rows are read-only (ErrTypeOfficial); an
// unknown id is ErrTypeNotFound.
func (p *PG) UpdateDriver(ctx context.Context, actorID, id string, patch DriverPatch) (*Driver, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update driver: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "driver", id); err != nil {
		return nil, err
	}
	d, err := scanDriver(tx.QueryRow(ctx, `
		update driver set
			display_name = coalesce($2, display_name),
			version      = coalesce($3, version),
			updated_at   = now()
		where `+registryRefCol(id)+` = $1
		returning `+driverCols,
		id, patch.DisplayName, patch.Version))
	if err != nil {
		return nil, fmt.Errorf("storage: update driver %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "driver", id, nil, d); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update driver: %w", err)
	}
	return d, nil
}

// DeleteDriver removes a custom driver, refusing an official row
// (ErrTypeOfficial). Nothing references driver in this slice (a later product
// slice will), so there is no in-use guard.
func (p *PG) DeleteDriver(ctx context.Context, actorID, id string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete driver: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "driver", id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from driver where `+registryRefCol(id)+` = $1`, id); err != nil {
		return fmt.Errorf("storage: delete driver %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "driver", id, map[string]string{"id": id}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete driver: %w", err)
	}
	return nil
}
