package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Product sentinels. A product references vendor, driver, parent product, and
// capability rows; an unknown reference is ErrProductRefNotFound (a request
// fault the API reports as 422). An out-of-set kind is ErrProductInvalidKind,
// caught before the write so the DB CHECK never has to. Existence, duplicate,
// official read-only, and in-use reuse the shared type-registry sentinels.
var (
	ErrProductRefNotFound = errors.New("storage: product references a missing vendor, driver, parent, or capability")
	ErrProductInvalidKind = errors.New("storage: product kind is not one of device, app, service, vm")
)

// ProductKind names what a product is: a physical device, a software app, a
// hosted service, or a virtual machine. It is a closed set, checked in the DB
// and validated before the write.
type ProductKind string

const (
	ProductDevice  ProductKind = "device"
	ProductApp     ProductKind = "app"
	ProductService ProductKind = "service"
	ProductVM      ProductKind = "vm"
)

// validProductKind reports whether s is one of the known product kinds.
func validProductKind(s string) bool {
	switch ProductKind(s) {
	case ProductDevice, ProductApp, ProductService, ProductVM:
		return true
	}
	return false
}

// Product is a registry row naming a concrete SKU in the estate model (e.g.
// "Cisco Room Bar"): a stable id, the official flag, a display_name, a kind
// (device/app/service/vm), and optional pointers at a vendor (who makes it), a
// driver (what talks to it), and a parent product (a variant it inherits from).
// A product also provides a set of capabilities (mic, speaker, camera), loaded
// via the product_capability join and carried as a sorted slice of capability
// ids. The registry lists alphabetically by display_name; there is no ordering
// field.
type Product struct {
	ID              string
	DisplayName     string
	VendorID        *string
	DriverID        *string
	ParentProductID *string
	Kind            string
	Official        bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Capabilities    []string
}

// ProductPatch carries the mutable fields of a product update; a nil field is
// left unchanged. Capabilities is a full replacement when non-nil (the given
// set becomes the product's set); nil leaves the current capabilities alone.
type ProductPatch struct {
	DisplayName     *string
	VendorID        *string
	DriverID        *string
	ParentProductID *string
	Kind            *string
	Capabilities    *[]string
}

const productCols = `id, display_name, vendor_id, driver_id, kind, parent_product_id, official, created_at, updated_at`

func scanProduct(row pgx.Row) (*Product, error) {
	var m Product
	if err := row.Scan(&m.ID, &m.DisplayName, &m.VendorID, &m.DriverID, &m.Kind, &m.ParentProductID, &m.Official, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

// mapProductWriteErr translates Postgres constraint violations on a product
// write into the product sentinels: a duplicate id is ErrTypeExists, and an
// unknown vendor/driver/parent/capability FK is ErrProductRefNotFound. Anything
// else is wrapped with context.
func mapProductWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrTypeExists
		case "23503": // foreign_key_violation
			return ErrProductRefNotFound
		}
	}
	return fmt.Errorf("storage: product write: %w", err)
}

// productCapabilityLoader is the multi-row read surface shared by the pool and a
// transaction, so a product's capabilities load either standalone or inside a
// write transaction. (querier only exposes QueryRow, so capability loading needs
// its own interface.)
type productCapabilityLoader interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// loadProductCapabilities returns a product's capability ids, sorted.
func loadProductCapabilities(ctx context.Context, q productCapabilityLoader, productID string) ([]string, error) {
	rows, err := q.Query(ctx, `select capability_id from product_capability where product_id = $1 order by capability_id`, productID)
	if err != nil {
		return nil, fmt.Errorf("storage: load product capabilities %q: %w", productID, err)
	}
	defer rows.Close()
	caps := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, fmt.Errorf("storage: scan product capability: %w", err)
		}
		caps = append(caps, c)
	}
	return caps, rows.Err()
}

// replaceProductCapabilities makes a product's capability set exactly caps:
// delete the current rows, then insert the given ids (deduped). An unknown
// capability id is ErrProductRefNotFound.
func replaceProductCapabilities(ctx context.Context, tx pgx.Tx, productID string, caps []string) error {
	if _, err := tx.Exec(ctx, `delete from product_capability where product_id = $1`, productID); err != nil {
		return fmt.Errorf("storage: clear product capabilities %q: %w", productID, err)
	}
	for _, c := range caps {
		if _, err := tx.Exec(ctx, `
			insert into product_capability (product_id, capability_id)
			values ($1, $2)
			on conflict (product_id, capability_id) do nothing`, productID, c); err != nil {
			return mapProductWriteErr(err)
		}
	}
	return nil
}

// UpsertProduct installs or updates a product by id and sets its capability set,
// the boot-seed phase's write. Idempotent: re-seeding the same id updates it in
// place and re-establishes its capabilities.
func (p *PG) UpsertProduct(ctx context.Context, m Product) error {
	if m.Kind == "" {
		m.Kind = string(ProductDevice)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin upsert product: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		insert into product (id, display_name, vendor_id, driver_id, kind, parent_product_id, official)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (id) do update
			set display_name      = excluded.display_name,
			    vendor_id         = excluded.vendor_id,
			    driver_id         = excluded.driver_id,
			    kind              = excluded.kind,
			    parent_product_id = excluded.parent_product_id,
			    official          = excluded.official,
			    updated_at        = now()`,
		m.ID, m.DisplayName, m.VendorID, m.DriverID, m.Kind, m.ParentProductID, m.Official); err != nil {
		return fmt.Errorf("storage: upsert product %q: %w", m.ID, err)
	}
	if err := replaceProductCapabilities(ctx, tx, m.ID, m.Capabilities); err != nil {
		return fmt.Errorf("storage: upsert product %q capabilities: %w", m.ID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit upsert product: %w", err)
	}
	return nil
}

// ListProducts returns every product with its capabilities, ordered
// alphabetically by display_name then id.
func (p *PG) ListProducts(ctx context.Context) ([]Product, error) {
	rows, err := p.pool.Query(ctx, `select `+productCols+` from product order by display_name, id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list products: %w", err)
	}
	out := []Product{}
	for rows.Next() {
		m, err := scanProduct(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("storage: scan product: %w", err)
		}
		out = append(out, *m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: list products: %w", err)
	}

	caps, err := p.allProductCapabilities(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Capabilities = caps[out[i].ID]
	}
	return out, nil
}

// allProductCapabilities returns every product's capability ids keyed by product
// id, each slice sorted, in one query (avoids an N+1 over the list).
func (p *PG) allProductCapabilities(ctx context.Context) (map[string][]string, error) {
	rows, err := p.pool.Query(ctx, `select product_id, capability_id from product_capability order by product_id, capability_id`)
	if err != nil {
		return nil, fmt.Errorf("storage: list product capabilities: %w", err)
	}
	defer rows.Close()
	m := map[string][]string{}
	for rows.Next() {
		var pid, cid string
		if err := rows.Scan(&pid, &cid); err != nil {
			return nil, fmt.Errorf("storage: scan product capability: %w", err)
		}
		m[pid] = append(m[pid], cid)
	}
	return m, rows.Err()
}

// GetProduct resolves one product with its capabilities by id. An unknown id is
// ErrTypeNotFound.
func (p *PG) GetProduct(ctx context.Context, id string) (*Product, error) {
	m, err := scanProduct(p.pool.QueryRow(ctx, `select `+productCols+` from product where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get product %q: %w", id, err)
	}
	caps, err := loadProductCapabilities(ctx, p.pool, id)
	if err != nil {
		return nil, err
	}
	m.Capabilities = caps
	return m, nil
}

// CreateProduct inserts a custom (official=false) product with its capability
// set and audits it. A duplicate id is ErrTypeExists; an unknown
// vendor/driver/parent/capability is ErrProductRefNotFound; an out-of-set kind
// is ErrProductInvalidKind. An empty kind defaults to device.
func (p *PG) CreateProduct(ctx context.Context, actorID string, m Product) (*Product, error) {
	m.Official = false
	if m.Kind == "" {
		m.Kind = string(ProductDevice)
	}
	if !validProductKind(m.Kind) {
		return nil, ErrProductInvalidKind
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create product: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := tx.QueryRow(ctx, `
		insert into product (id, display_name, vendor_id, driver_id, kind, parent_product_id, official)
		values ($1, $2, $3, $4, $5, $6, false)
		returning created_at, updated_at`,
		m.ID, m.DisplayName, m.VendorID, m.DriverID, m.Kind, m.ParentProductID).
		Scan(&m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, mapProductWriteErr(err)
	}
	if err := replaceProductCapabilities(ctx, tx, m.ID, m.Capabilities); err != nil {
		return nil, err
	}
	caps, err := loadProductCapabilities(ctx, tx, m.ID)
	if err != nil {
		return nil, err
	}
	m.Capabilities = caps
	if err := writeAuditRes(ctx, tx, actorID, "create", "product", m.ID, nil, m); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create product: %w", err)
	}
	return &m, nil
}

// UpdateProduct patches a custom product's display_name, vendor, driver, kind,
// or parent (nil fields unchanged), replaces its capability set when
// Capabilities is non-nil, and audits it. Official rows are read-only
// (ErrTypeOfficial); an unknown id is ErrTypeNotFound; an unknown reference is
// ErrProductRefNotFound; an out-of-set kind is ErrProductInvalidKind.
func (p *PG) UpdateProduct(ctx context.Context, actorID, id string, patch ProductPatch) (*Product, error) {
	if patch.Kind != nil && !validProductKind(*patch.Kind) {
		return nil, ErrProductInvalidKind
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update product: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "product", id); err != nil {
		return nil, err
	}
	m, err := scanProduct(tx.QueryRow(ctx, `
		update product set
			display_name      = coalesce($2, display_name),
			vendor_id         = coalesce($3, vendor_id),
			driver_id         = coalesce($4, driver_id),
			kind              = coalesce($5, kind),
			parent_product_id = coalesce($6, parent_product_id),
			updated_at        = now()
		where id = $1
		returning `+productCols,
		id, patch.DisplayName, patch.VendorID, patch.DriverID, patch.Kind, patch.ParentProductID))
	if err != nil {
		return nil, mapProductWriteErr(err)
	}
	if patch.Capabilities != nil {
		if err := replaceProductCapabilities(ctx, tx, id, *patch.Capabilities); err != nil {
			return nil, err
		}
	}
	caps, err := loadProductCapabilities(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	m.Capabilities = caps
	if err := writeAuditRes(ctx, tx, actorID, "update", "product", id, nil, m); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update product: %w", err)
	}
	return m, nil
}

// DeleteProduct removes a custom product (its capability rows cascade), refusing
// an official row (ErrTypeOfficial) or one still referenced by a component
// (ErrTypeInUse, from the component.product_id ON DELETE RESTRICT FK).
func (p *PG) DeleteProduct(ctx context.Context, actorID, id string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete product: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "product", id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from product where id = $1`, id); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrTypeInUse
		}
		return fmt.Errorf("storage: delete product %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "product", id, map[string]string{"id": id}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete product: %w", err)
	}
	return nil
}
