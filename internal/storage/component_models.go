package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ComponentModel is a registry row naming a make + model product (e.g.
// "Crestron DM-NVX-363"): a stable id, the official flag, a required make_id
// pointing at a component_make, product identity (model_number, family),
// optional lifecycle timestamps, and optional front/back image pointers at
// the files primitive. Like component_make it is a flat registry (no tree),
// listed alphabetically by display_name. Unlike component_make, deleting a
// make referenced by a model is refused (the in-use guard lives on
// DeleteComponentMake, since the reference runs make -> model).
type ComponentModel struct {
	ID           string
	Official     bool
	DisplayName  string
	MakeID       string
	ModelNumber  string
	Family       string
	ReleasedAt   *time.Time
	EosAt        *time.Time
	EolAt        *time.Time
	FrontImageID *string
	BackImageID  *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ComponentModelPatch carries the mutable fields of a component_model update;
// a nil field is left unchanged. MakeID is not patchable in this slice.
type ComponentModelPatch struct {
	DisplayName  *string
	ModelNumber  *string
	Family       *string
	FrontImageID *string
	BackImageID  *string
	ReleasedAt   *time.Time
	EosAt        *time.Time
	EolAt        *time.Time
}

const componentModelCols = `id, official, display_name, make_id, model_number, family,
	released_at, eos_at, eol_at, front_image_id, back_image_id, created_at, updated_at`

func scanComponentModel(row pgx.Row) (*ComponentModel, error) {
	var m ComponentModel
	if err := row.Scan(
		&m.ID, &m.Official, &m.DisplayName, &m.MakeID, &m.ModelNumber, &m.Family,
		&m.ReleasedAt, &m.EosAt, &m.EolAt, &m.FrontImageID, &m.BackImageID,
		&m.CreatedAt, &m.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &m, nil
}

// UpsertComponentModel installs or updates a component model by id, the
// boot-seed phase's write. Idempotent: re-seeding the same id updates it in
// place.
func (p *PG) UpsertComponentModel(ctx context.Context, m ComponentModel) error {
	_, err := p.pool.Exec(ctx, `
		insert into component_model (id, official, display_name, make_id, model_number, family,
			released_at, eos_at, eol_at, front_image_id, back_image_id)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		on conflict (id) do update
			set official       = excluded.official,
			    display_name   = excluded.display_name,
			    make_id        = excluded.make_id,
			    model_number   = excluded.model_number,
			    family         = excluded.family,
			    released_at    = excluded.released_at,
			    eos_at         = excluded.eos_at,
			    eol_at         = excluded.eol_at,
			    front_image_id = excluded.front_image_id,
			    back_image_id  = excluded.back_image_id,
			    updated_at     = now()`,
		m.ID, m.Official, m.DisplayName, m.MakeID, m.ModelNumber, m.Family,
		m.ReleasedAt, m.EosAt, m.EolAt, m.FrontImageID, m.BackImageID)
	if err != nil {
		return fmt.Errorf("storage: upsert component_model %q: %w", m.ID, err)
	}
	return nil
}

// ListComponentModels returns every component model, ordered alphabetically
// by display_name then id.
func (p *PG) ListComponentModels(ctx context.Context) ([]ComponentModel, error) {
	rows, err := p.pool.Query(ctx, `select `+componentModelCols+` from component_model order by display_name, id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list component_models: %w", err)
	}
	defer rows.Close()
	var out []ComponentModel
	for rows.Next() {
		m, err := scanComponentModel(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan component_model: %w", err)
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// GetComponentModel resolves one component model by id. An unknown id is
// ErrTypeNotFound.
func (p *PG) GetComponentModel(ctx context.Context, id string) (*ComponentModel, error) {
	m, err := scanComponentModel(p.pool.QueryRow(ctx, `select `+componentModelCols+` from component_model where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get component_model %q: %w", id, err)
	}
	return m, nil
}

// CreateComponentModel inserts a custom (official=false) component_model and
// audits it. A duplicate id is ErrTypeExists. A make_id that does not name an
// existing component_make fails the foreign key and is returned as a wrapped
// error (the API layer maps it to 422).
func (p *PG) CreateComponentModel(ctx context.Context, actorID string, m ComponentModel) (*ComponentModel, error) {
	m.Official = false
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create component_model: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := tx.QueryRow(ctx, `
		insert into component_model (id, official, display_name, make_id, model_number, family,
			released_at, eos_at, eol_at, front_image_id, back_image_id)
		values ($1, false, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		returning created_at, updated_at`,
		m.ID, m.DisplayName, m.MakeID, m.ModelNumber, m.Family,
		m.ReleasedAt, m.EosAt, m.EolAt, m.FrontImageID, m.BackImageID).
		Scan(&m.CreatedAt, &m.UpdatedAt); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrTypeExists
		}
		return nil, fmt.Errorf("storage: insert component_model %q: %w", m.ID, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "component_model", m.ID, nil, m); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create component_model: %w", err)
	}
	return &m, nil
}

// UpdateComponentModel patches a custom component_model's display_name,
// model_number, family, lifecycle timestamps, or image pointers (nil fields
// unchanged) and audits it. Official rows are read-only (ErrTypeOfficial); an
// unknown id is ErrTypeNotFound. make_id is not patchable in this slice.
// TODO(#260): released_at/eos_at/eol_at/front_image_id/back_image_id are
// set/replace-only; clearing them needs explicit-null patch semantics
// (coalesce keeps the old value when the field is nil, and nil is also what
// an absent field decodes to, so there is no way to distinguish "leave
// unchanged" from "clear" for these nullable columns today).
func (p *PG) UpdateComponentModel(ctx context.Context, actorID, id string, patch ComponentModelPatch) (*ComponentModel, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update component_model: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "component_model", id); err != nil {
		return nil, err
	}
	m, err := scanComponentModel(tx.QueryRow(ctx, `
		update component_model set
			display_name   = coalesce($2, display_name),
			model_number   = coalesce($3, model_number),
			family         = coalesce($4, family),
			released_at    = coalesce($5, released_at),
			eos_at         = coalesce($6, eos_at),
			eol_at         = coalesce($7, eol_at),
			front_image_id = coalesce($8, front_image_id),
			back_image_id  = coalesce($9, back_image_id),
			updated_at     = now()
		where id = $1
		returning `+componentModelCols,
		id, patch.DisplayName, patch.ModelNumber, patch.Family,
		patch.ReleasedAt, patch.EosAt, patch.EolAt, patch.FrontImageID, patch.BackImageID))
	if err != nil {
		return nil, fmt.Errorf("storage: update component_model %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "component_model", id, nil, m); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update component_model: %w", err)
	}
	return m, nil
}

// DeleteComponentModel removes a custom component_model, refusing an official
// row (ErrTypeOfficial). Nothing references component_model in this slice, so
// there is no in-use guard here (compare DeleteComponentMake, which does guard
// on component_model referencing it).
func (p *PG) DeleteComponentModel(ctx context.Context, actorID, id string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete component_model: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "component_model", id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from component_model where id = $1`, id); err != nil {
		return fmt.Errorf("storage: delete component_model %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "component_model", id, map[string]string{"id": id}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete component_model: %w", err)
	}
	return nil
}
