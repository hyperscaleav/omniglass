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
	// ID is the uuid primary key, Name the renameable kebab handle. Its two
	// references carry both forms for the same reason the estate bodies do: the
	// id is what the row points at, the name is what an operator reads and types.
	ID                string
	Name              string
	DisplayName       string
	VendorID          *string
	VendorName        *string
	DriverID          *string
	DriverName        *string
	ParentProductID   *string
	ParentProductName *string
	Kind              string
	Official          bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Capabilities      []string
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

// The two arcs store uuids, so each is selected beside a scalar subquery for its
// target's current handle. Both derived columns are aliased: an unaliased
// `select v.name` would emit a second output column called `name` and make
// `order by name` ambiguous.
const productCols = `id, name, display_name,
	vendor_id, (select v.name from vendor v where v.id = product.vendor_id) as vendor_handle,
	driver_id, (select d.name from driver d where d.id = product.driver_id) as driver_handle, kind,
	parent_product_id, (select q.name from product q where q.id = product.parent_product_id) as parent_handle,
	official, created_at, updated_at`

// resolveProductRef turns a handle or uuid into the product's uuid, for the
// columns that store one.
func resolveProductRef(ctx context.Context, q querier, ref string) (string, error) {
	var id string
	if err := q.QueryRow(ctx, `select id from product where `+registryRefCol(ref)+` = $1`, ref).Scan(&id); err != nil {
		return "", ErrProductRefNotFound
	}
	return id, nil
}

// resolveVendorRef does the same for a vendor reference.
func resolveVendorRef(ctx context.Context, q querier, ref string) (string, error) {
	var id string
	if err := q.QueryRow(ctx, `select id from vendor where `+registryRefCol(ref)+` = $1`, ref).Scan(&id); err != nil {
		return "", ErrProductRefNotFound
	}
	return id, nil
}

// productRefs resolves a product's two references from whatever form the caller
// supplied (handle or uuid) to the uuids the columns store. An unknown reference
// is ErrProductRefNotFound rather than a NULL that would silently unlink it.
func productRefs(ctx context.Context, q querier, m *Product) error {
	if m.VendorID != nil && *m.VendorID != "" {
		id, err := resolveVendorRef(ctx, q, *m.VendorID)
		if err != nil {
			return err
		}
		m.VendorID = &id
	}
	if m.DriverID != nil && *m.DriverID != "" {
		id, err := resolveDriverRef(ctx, q, *m.DriverID)
		if err != nil {
			return err
		}
		m.DriverID = &id
	}
	if m.ParentProductID != nil && *m.ParentProductID != "" {
		id, err := resolveProductRef(ctx, q, *m.ParentProductID)
		if err != nil {
			return err
		}
		m.ParentProductID = &id
	}
	return nil
}

// resolveDriverRef turns a driver handle or uuid into the driver's uuid.
func resolveDriverRef(ctx context.Context, q querier, ref string) (string, error) {
	var id string
	if err := q.QueryRow(ctx, `select id from driver where `+registryRefCol(ref)+` = $1`, ref).Scan(&id); err != nil {
		return "", ErrProductRefNotFound
	}
	return id, nil
}

func scanProduct(row pgx.Row) (*Product, error) {
	var m Product
	if err := row.Scan(&m.ID, &m.Name, &m.DisplayName, &m.VendorID, &m.VendorName, &m.DriverID, &m.DriverName, &m.Kind, &m.ParentProductID, &m.ParentProductName, &m.Official, &m.CreatedAt, &m.UpdatedAt); err != nil {
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
		case "23502": // not-null on capability_id: an unknown capability name resolved to null
			if pgErr.ColumnName == "capability_id" {
				return ErrProductRefNotFound
			}
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
	rows, err := q.Query(ctx, `select cap.name from product_capability pc join capability cap on cap.id = pc.capability_id where pc.product_id = (select id from product where `+registryRefCol(productID)+` = $1) order by cap.name`, productID)
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
func replaceProductCapabilities(ctx context.Context, tx pgx.Tx, productRef string, caps []string) error {
	productID, err := resolveProductRef(ctx, tx, productRef)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from product_capability where product_id = $1`, productID); err != nil {
		return fmt.Errorf("storage: clear product capabilities %q: %w", productRef, err)
	}
	for _, c := range caps {
		if _, err := tx.Exec(ctx, `
			insert into product_capability (product_id, capability_id)
			values ($1, (select id from capability where name = $2 or id::text = $2))
			on conflict (product_id, capability_id) do nothing`, productID, c); err != nil {
			return mapProductWriteErr(err)
		}
	}
	return nil
}

// UpsertProduct installs or updates a product by HANDLE and sets its capability
// set, the boot-seed phase's write. The seed ships kebab handles, not uuids, so
// the conflict target is the handle: re-seeding `cisco-room-bar` updates that row
// in place, re-establishes its capabilities, and its id never moves.
func (p *PG) UpsertProduct(ctx context.Context, m Product) error {
	if m.Kind == "" {
		m.Kind = string(ProductDevice)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin upsert product: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := productRefs(ctx, tx, &m); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into product (name, display_name, vendor_id, driver_id, kind, parent_product_id, official)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (name) do update
			set display_name      = excluded.display_name,
			    vendor_id         = excluded.vendor_id,
			    driver_id         = excluded.driver_id,
			    kind              = excluded.kind,
			    parent_product_id = excluded.parent_product_id,
			    official          = excluded.official,
			    updated_at        = now()`,
		m.Name, m.DisplayName, m.VendorID, m.DriverID, m.Kind, m.ParentProductID, m.Official); err != nil {
		return fmt.Errorf("storage: upsert product %q: %w", m.Name, err)
	}
	pid, err := resolveProductRef(ctx, tx, m.Name)
	if err != nil {
		return err
	}
	if err := replaceProductCapabilities(ctx, tx, pid, m.Capabilities); err != nil {
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
	rows, err := p.pool.Query(ctx, `select `+productCols+` from product order by display_name, name`)
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
	rows, err := p.pool.Query(ctx, `select pc.product_id, cap.name from product_capability pc join capability cap on cap.id = pc.capability_id order by pc.product_id, cap.name`)
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
	m, err := scanProduct(p.pool.QueryRow(ctx, `select `+productCols+` from product where `+registryRefCol(id)+` = $1`, id))
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

	if err := productRefs(ctx, tx, &m); err != nil {
		return nil, err
	}
	if err := tx.QueryRow(ctx, `
		insert into product (name, display_name, vendor_id, driver_id, kind, parent_product_id, official)
		values ($1, $2, $3, $4, $5, $6, false)
		returning id, created_at, updated_at`,
		m.Name, m.DisplayName, m.VendorID, m.DriverID, m.Kind, m.ParentProductID).
		Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt); err != nil {
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
	// Re-read so the projected handles (vendor, parent) come back populated; the
	// input struct only ever carried the ids.
	return p.GetProduct(ctx, m.ID)
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
	// A patch's references arrive as handles or uuids; the columns store uuids, so
	// resolve each before the update (an unset one stays nil and is left unchanged).
	resolved := Product{VendorID: patch.VendorID, DriverID: patch.DriverID, ParentProductID: patch.ParentProductID}
	if err := productRefs(ctx, tx, &resolved); err != nil {
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
		where `+registryRefCol(id)+` = $1
		returning `+productCols,
		id, patch.DisplayName, resolved.VendorID, resolved.DriverID, patch.Kind, resolved.ParentProductID))
	if err != nil {
		return nil, mapProductWriteErr(err)
	}
	if patch.Capabilities != nil {
		if err := replaceProductCapabilities(ctx, tx, id, *patch.Capabilities); err != nil {
			return nil, err
		}
		// A product is a contract, so its capability set is the default every
		// component built to it provides. Withdrawing one can drop a role below its
		// quorum in systems nobody touched, which is a real transition in each of
		// them: an edit here is a health event over there. In the same transaction,
		// so a component can never provide less than the record says it does.
		if err := p.recomputeProductComponents(ctx, tx, id); err != nil {
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
	if _, err := tx.Exec(ctx, `delete from product where `+registryRefCol(id)+` = $1`, id); err != nil {
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
