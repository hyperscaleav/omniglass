package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// VendorKind names the role a vendor plays in the estate model: who built a
// component (manufacturer), who installed it (integrator), or who wrote its
// software (developer). It is a closed set, checked in the DB and validated at
// the API edge.
type VendorKind string

const (
	VendorManufacturer VendorKind = "manufacturer"
	VendorIntegrator   VendorKind = "integrator"
	VendorDeveloper    VendorKind = "developer"
)

// validVendorKind reports whether s is one of the known vendor kinds.
func validVendorKind(s string) bool {
	switch VendorKind(s) {
	case VendorManufacturer, VendorIntegrator, VendorDeveloper:
		return true
	}
	return false
}

// Vendor is a registry row naming an organization in the estate model (e.g.
// "Cisco", "Crestron"): a stable id, the official flag, a display_name, a kind
// (manufacturer/integrator/developer), and optional contact metadata (icon,
// support phone, website). It is a flat registry like component_type: no tree,
// and no in-use delete guard in this slice (product will reference it). The
// registry lists alphabetically by display_name; there is no ordering field.
type Vendor struct {
	ID           string
	Official     bool
	DisplayName  string
	Kind         string
	Icon         string
	SupportPhone string
	Website      string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// VendorPatch carries the mutable fields of a vendor update; a nil field is
// left unchanged.
type VendorPatch struct {
	DisplayName  *string
	Kind         *string
	Icon         *string
	SupportPhone *string
	Website      *string
}

const vendorCols = `id, official, display_name, kind, icon, support_phone, website, created_at, updated_at`

func scanVendor(row pgx.Row) (*Vendor, error) {
	var m Vendor
	if err := row.Scan(&m.ID, &m.Official, &m.DisplayName, &m.Kind, &m.Icon, &m.SupportPhone, &m.Website, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

// UpsertVendor installs or updates a vendor by id, the boot-seed phase's write.
// Idempotent: re-seeding the same id updates it in place.
func (p *PG) UpsertVendor(ctx context.Context, m Vendor) error {
	_, err := p.pool.Exec(ctx, `
		insert into vendor (id, official, display_name, kind, icon, support_phone, website)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (id) do update
			set official      = excluded.official,
			    display_name  = excluded.display_name,
			    kind          = excluded.kind,
			    icon          = excluded.icon,
			    support_phone = excluded.support_phone,
			    website       = excluded.website,
			    updated_at    = now()`,
		m.ID, m.Official, m.DisplayName, m.Kind, m.Icon, m.SupportPhone, m.Website)
	if err != nil {
		return fmt.Errorf("storage: upsert vendor %q: %w", m.ID, err)
	}
	return nil
}

// ListVendors returns every vendor, ordered alphabetically by display_name then
// id.
func (p *PG) ListVendors(ctx context.Context) ([]Vendor, error) {
	rows, err := p.pool.Query(ctx, `select `+vendorCols+` from vendor order by display_name, id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list vendors: %w", err)
	}
	defer rows.Close()
	var out []Vendor
	for rows.Next() {
		m, err := scanVendor(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan vendor: %w", err)
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// GetVendor resolves one vendor by id. An unknown id is ErrTypeNotFound.
func (p *PG) GetVendor(ctx context.Context, id string) (*Vendor, error) {
	m, err := scanVendor(p.pool.QueryRow(ctx, `select `+vendorCols+` from vendor where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get vendor %q: %w", id, err)
	}
	return m, nil
}

// CreateVendor inserts a custom (official=false) vendor and audits it. A
// duplicate id is ErrTypeExists.
func (p *PG) CreateVendor(ctx context.Context, actorID string, m Vendor) (*Vendor, error) {
	m.Official = false
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create vendor: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := tx.QueryRow(ctx, `
		insert into vendor (id, official, display_name, kind, icon, support_phone, website)
		values ($1, false, $2, $3, $4, $5, $6)
		returning created_at, updated_at`,
		m.ID, m.DisplayName, m.Kind, m.Icon, m.SupportPhone, m.Website).
		Scan(&m.CreatedAt, &m.UpdatedAt); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrTypeExists
		}
		return nil, fmt.Errorf("storage: insert vendor %q: %w", m.ID, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "vendor", m.ID, nil, m); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create vendor: %w", err)
	}
	return &m, nil
}

// UpdateVendor patches a custom vendor's display_name, kind, icon,
// support_phone, or website (nil fields unchanged) and audits it. Official rows
// are read-only (ErrTypeOfficial); an unknown id is ErrTypeNotFound.
func (p *PG) UpdateVendor(ctx context.Context, actorID, id string, patch VendorPatch) (*Vendor, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update vendor: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "vendor", id); err != nil {
		return nil, err
	}
	m, err := scanVendor(tx.QueryRow(ctx, `
		update vendor set
			display_name  = coalesce($2, display_name),
			kind          = coalesce($3, kind),
			icon          = coalesce($4, icon),
			support_phone = coalesce($5, support_phone),
			website       = coalesce($6, website),
			updated_at    = now()
		where id = $1
		returning `+vendorCols,
		id, patch.DisplayName, patch.Kind, patch.Icon, patch.SupportPhone, patch.Website))
	if err != nil {
		return nil, fmt.Errorf("storage: update vendor %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "vendor", id, nil, m); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update vendor: %w", err)
	}
	return m, nil
}

// DeleteVendor removes a custom vendor, refusing an official row
// (ErrTypeOfficial). Nothing references vendor in this slice (a later product
// slice will), so there is no in-use guard.
func (p *PG) DeleteVendor(ctx context.Context, actorID, id string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete vendor: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "vendor", id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from vendor where id = $1`, id); err != nil {
		return fmt.Errorf("storage: delete vendor %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "vendor", id, map[string]string{"id": id}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete vendor: %w", err)
	}
	return nil
}
