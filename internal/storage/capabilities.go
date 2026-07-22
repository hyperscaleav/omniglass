package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Capability is a registry row naming what a component can do (e.g.
// "microphone", "display"): a stable id, the official flag, and a
// display_name. It is a flat vocabulary like component_type: no tree, and no
// in-use delete guard in this slice (nothing references a capability yet;
// product will). The registry lists alphabetically by display_name; there is
// no ordering field.
type Capability struct {
	// ID is the uuid primary key and Name the renameable kebab handle (ADR-0062).
	ID          string
	Name        string
	DisplayName string
	Official    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CapabilityPatch carries the mutable fields of a capability update; a nil
// field is left unchanged.
type CapabilityPatch struct {
	DisplayName *string
}

const capabilityCols = `id, name, display_name, official, created_at, updated_at`

func scanCapability(row pgx.Row) (*Capability, error) {
	var c Capability
	if err := row.Scan(&c.ID, &c.Name, &c.DisplayName, &c.Official, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

// UpsertCapability installs or updates a capability by id, the boot-seed
// phase's write. Idempotent: re-seeding the same id updates it in place.
func (p *PG) UpsertCapability(ctx context.Context, c Capability) error {
	_, err := p.pool.Exec(ctx, `
		insert into capability (name, display_name, official)
		values ($1, $2, $3)
		on conflict (name) do update
			set display_name = excluded.display_name,
			    official     = excluded.official,
			    updated_at   = now()`,
		c.Name, c.DisplayName, c.Official)
	if err != nil {
		return fmt.Errorf("storage: upsert capability %q: %w", c.Name, err)
	}
	return nil
}

// ListCapabilities returns every capability, ordered alphabetically by
// display_name then id.
func (p *PG) ListCapabilities(ctx context.Context) ([]Capability, error) {
	rows, err := p.pool.Query(ctx, `select `+capabilityCols+` from capability order by display_name, name`)
	if err != nil {
		return nil, fmt.Errorf("storage: list capabilities: %w", err)
	}
	defer rows.Close()
	var out []Capability
	for rows.Next() {
		c, err := scanCapability(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan capability: %w", err)
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// GetCapability resolves one capability by id. An unknown id is
// ErrTypeNotFound.
func (p *PG) GetCapability(ctx context.Context, id string) (*Capability, error) {
	c, err := scanCapability(p.pool.QueryRow(ctx, `select `+capabilityCols+` from capability where `+registryRefCol("capability", id)+` = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get capability %q: %w", id, err)
	}
	return c, nil
}

// CreateCapability inserts a custom (official=false) capability and audits it.
// A duplicate id is ErrTypeExists.
func (p *PG) CreateCapability(ctx context.Context, actorID string, c Capability) (*Capability, error) {
	c.Official = false
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create capability: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := tx.QueryRow(ctx, `
		insert into capability (name, display_name, official)
		values ($1, $2, false)
		returning id, created_at, updated_at`,
		c.Name, c.DisplayName).
		Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrTypeExists
		}
		return nil, fmt.Errorf("storage: insert capability %q: %w", c.Name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "capability", c.Name, nil, c); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create capability: %w", err)
	}
	return &c, nil
}

// UpdateCapability patches a custom capability's display_name (nil field
// unchanged) and audits it. Official rows are read-only (ErrTypeOfficial); an
// unknown id is ErrTypeNotFound.
func (p *PG) UpdateCapability(ctx context.Context, actorID, id string, patch CapabilityPatch) (*Capability, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update capability: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "capability", id); err != nil {
		return nil, err
	}
	c, err := scanCapability(tx.QueryRow(ctx, `
		update capability set
			display_name = coalesce($2, display_name),
			updated_at   = now()
		where `+registryRefCol("capability", id)+` = $1
		returning `+capabilityCols,
		id, patch.DisplayName))
	if err != nil {
		return nil, fmt.Errorf("storage: update capability %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "capability", id, nil, c); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update capability: %w", err)
	}
	return c, nil
}

// DeleteCapability removes a custom capability, refusing an official row
// (ErrTypeOfficial). Nothing references capability in this slice (a later
// product slice will), so there is no in-use guard.
func (p *PG) DeleteCapability(ctx context.Context, actorID, id string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete capability: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "capability", id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from capability where `+registryRefCol("capability", id)+` = $1`, id); err != nil {
		return fmt.Errorf("storage: delete capability %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "capability", id, map[string]string{"id": id}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete capability: %w", err)
	}
	return nil
}
