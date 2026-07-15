package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ComponentMake is a registry row naming a manufacturer (e.g. "Cisco",
// "Crestron"): a stable id, the official flag, a display_name, and optional
// contact metadata (icon, support phone, website). It is a flat registry like
// component_type: no tree. DeleteComponentMake refuses a make still
// referenced by a component_model (the in-use guard). The registry lists
// alphabetically by display_name; there is no ordering field.
type ComponentMake struct {
	ID           string
	Official     bool
	DisplayName  string
	Icon         string
	SupportPhone string
	Website      string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ComponentMakePatch carries the mutable fields of a component_make update; a
// nil field is left unchanged.
type ComponentMakePatch struct {
	DisplayName  *string
	Icon         *string
	SupportPhone *string
	Website      *string
}

const componentMakeCols = `id, official, display_name, icon, support_phone, website, created_at, updated_at`

func scanComponentMake(row pgx.Row) (*ComponentMake, error) {
	var m ComponentMake
	if err := row.Scan(&m.ID, &m.Official, &m.DisplayName, &m.Icon, &m.SupportPhone, &m.Website, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

// UpsertComponentMake installs or updates a component make by id, the
// boot-seed phase's write. Idempotent: re-seeding the same id updates it in
// place.
func (p *PG) UpsertComponentMake(ctx context.Context, m ComponentMake) error {
	_, err := p.pool.Exec(ctx, `
		insert into component_make (id, official, display_name, icon, support_phone, website)
		values ($1, $2, $3, $4, $5, $6)
		on conflict (id) do update
			set official      = excluded.official,
			    display_name  = excluded.display_name,
			    icon          = excluded.icon,
			    support_phone = excluded.support_phone,
			    website       = excluded.website,
			    updated_at    = now()`,
		m.ID, m.Official, m.DisplayName, m.Icon, m.SupportPhone, m.Website)
	if err != nil {
		return fmt.Errorf("storage: upsert component_make %q: %w", m.ID, err)
	}
	return nil
}

// ListComponentMakes returns every component make, ordered alphabetically by
// display_name then id.
func (p *PG) ListComponentMakes(ctx context.Context) ([]ComponentMake, error) {
	rows, err := p.pool.Query(ctx, `select `+componentMakeCols+` from component_make order by display_name, id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list component_makes: %w", err)
	}
	defer rows.Close()
	var out []ComponentMake
	for rows.Next() {
		m, err := scanComponentMake(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan component_make: %w", err)
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// GetComponentMake resolves one component make by id. An unknown id is
// ErrTypeNotFound.
func (p *PG) GetComponentMake(ctx context.Context, id string) (*ComponentMake, error) {
	m, err := scanComponentMake(p.pool.QueryRow(ctx, `select `+componentMakeCols+` from component_make where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get component_make %q: %w", id, err)
	}
	return m, nil
}

// CreateComponentMake inserts a custom (official=false) component_make and
// audits it. A duplicate id is ErrTypeExists.
func (p *PG) CreateComponentMake(ctx context.Context, actorID string, m ComponentMake) (*ComponentMake, error) {
	m.Official = false
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create component_make: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := tx.QueryRow(ctx, `
		insert into component_make (id, official, display_name, icon, support_phone, website)
		values ($1, false, $2, $3, $4, $5)
		returning created_at, updated_at`,
		m.ID, m.DisplayName, m.Icon, m.SupportPhone, m.Website).
		Scan(&m.CreatedAt, &m.UpdatedAt); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrTypeExists
		}
		return nil, fmt.Errorf("storage: insert component_make %q: %w", m.ID, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "component_make", m.ID, nil, m); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create component_make: %w", err)
	}
	return &m, nil
}

// UpdateComponentMake patches a custom component_make's display_name, icon,
// support_phone, or website (nil fields unchanged) and audits it. Official
// rows are read-only (ErrTypeOfficial); an unknown id is ErrTypeNotFound.
func (p *PG) UpdateComponentMake(ctx context.Context, actorID, id string, patch ComponentMakePatch) (*ComponentMake, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update component_make: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "component_make", id); err != nil {
		return nil, err
	}
	m, err := scanComponentMake(tx.QueryRow(ctx, `
		update component_make set
			display_name  = coalesce($2, display_name),
			icon          = coalesce($3, icon),
			support_phone = coalesce($4, support_phone),
			website       = coalesce($5, website),
			updated_at    = now()
		where id = $1
		returning `+componentMakeCols,
		id, patch.DisplayName, patch.Icon, patch.SupportPhone, patch.Website))
	if err != nil {
		return nil, fmt.Errorf("storage: update component_make %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "component_make", id, nil, m); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update component_make: %w", err)
	}
	return m, nil
}

// DeleteComponentMake removes a custom component_make, refusing an official
// row (ErrTypeOfficial) and refusing a make still referenced by a
// component_model (ErrTypeInUse), mirroring the type registries' in-use
// delete guard.
func (p *PG) DeleteComponentMake(ctx context.Context, actorID, id string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete component_make: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "component_make", id); err != nil {
		return err
	}
	n, err := countTypeRefs(ctx, tx, typeRef{table: "component_model", col: "make_id"}, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrTypeInUse
	}
	if _, err := tx.Exec(ctx, `delete from component_make where id = $1`, id); err != nil {
		return fmt.Errorf("storage: delete component_make %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "component_make", id, map[string]string{"id": id}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete component_make: %w", err)
	}
	return nil
}
