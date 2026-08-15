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
// component_type rows; an unknown reference is ErrProductRefNotFound (a
// request fault the API reports as 422). An out-of-set kind is
// ErrProductInvalidKind, caught before the write so the DB CHECK never has
// to. Existence, duplicate, official read-only, and in-use reuse the shared
// type-registry sentinels.
var (
	ErrProductRefNotFound = errors.New("storage: product references a missing vendor, driver, parent, or component_type")
	ErrProductInvalidKind = errors.New("storage: product kind is not one of device, app, service")
)

// ProductKind names what a product is: a physical device, a software app, or
// a hosted service. It is a closed set, checked in the DB and validated
// before the write. vm retired with the product_type_floor migration
// (20260807116000): the backfill folded every existing vm product into app.
type ProductKind string

const (
	ProductDevice  ProductKind = "device"
	ProductApp     ProductKind = "app"
	ProductService ProductKind = "service"
)

// validProductKind reports whether s is one of the known product kinds.
func validProductKind(s string) bool {
	switch ProductKind(s) {
	case ProductDevice, ProductApp, ProductService:
		return true
	}
	return false
}

// Product is a registry row naming a concrete SKU in the estate model (e.g.
// "Cisco Room Bar"): a stable id, the official flag, a display_name, a kind
// (device/app/service), the component_type it is classified under (the
// taxonomy above product; mic, camera, wireless-mic...), an optional icon
// override, and optional pointers at a vendor (who makes it), a driver (what
// talks to it), and a parent product (a variant it inherits from). The
// registry lists alphabetically by display_name; there is no ordering field.
type Product struct {
	// ID is the uuid primary key, Name the renameable name. Its two
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
	// ComponentType is the classification: a handle-or-uuid ref on a write
	// (CreateProduct/UpdateProduct resolve it), the component_type's current
	// name on a read. ComponentTypeID is the stable id beside it, the same
	// Type/TypeID split location_type uses: required, never nil once read back,
	// since the floor makes every product's component_type mandatory.
	ComponentType   string
	ComponentTypeID string
	// Icon is a product-level override of the component_type's icon (nullable:
	// unset inherits the type's, resolved the same way ResolveTypeFacts walks
	// component_type's own icon column).
	Icon *string
	// LabelRule is this product's label template (#682), the most specific tier
	// a component's label resolves through: null defers to the component_type
	// chain, and then to the global tier.
	LabelRule *string
	Official  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProductPatch carries the mutable fields of a product update; a nil field is
// left unchanged. ComponentType, like Kind, has no clear state: it is a
// required classification, so a patch either leaves it (nil) or
// reclassifies it (set), never empties it.
type ProductPatch struct {
	DisplayName     *string
	VendorID        *string
	DriverID        *string
	ParentProductID *string
	Kind            *string
	ComponentType   *string
	Icon            *string
	LabelRule       *string
}

// The two arcs store uuids, so each is selected beside a scalar subquery for its
// target's current handle. Both derived columns are aliased: an unaliased
// `select v.name` would emit a second output column called `name` and make
// `order by name` ambiguous.
const productCols = `id, name, display_name,
	vendor_id, (select v.name from vendor v where v.id = product.vendor_id) as vendor_handle,
	driver_id, (select d.name from driver d where d.id = product.driver_id) as driver_handle, kind,
	parent_product_id, (select q.name from product q where q.id = product.parent_product_id) as parent_handle,
	(select ct.name from component_type ct where ct.id = product.component_type_id) as component_type_handle, component_type_id,
	icon, label_rule,
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

// resolveComponentTypeRef turns a component_type handle or uuid into its
// uuid. An unknown reference is ErrProductRefNotFound, the same sentinel the
// product's other references use, so the API's single 422 branch covers it
// too: from a product write's point of view, an unknown component_type is no
// different from an unknown vendor or driver.
func resolveComponentTypeRef(ctx context.Context, q querier, ref string) (string, error) {
	var id string
	if err := q.QueryRow(ctx, `select id from component_type where `+registryRefCol(ref)+` = $1`, ref).Scan(&id); err != nil {
		return "", ErrProductRefNotFound
	}
	return id, nil
}

// genericComponentTypeForKind maps a product's kind to the generic
// component_type the product_type_backfill migration (and the boot seed)
// always provide. A product written with no explicit component_type
// classifies as the generic instance of its own kind, the same fallback the
// backfill applies to every product that predated the floor. The API layer
// requires an explicit component_type on create (a stricter operator-facing
// gate); this default only ever engages for a caller that writes the
// gateway directly (seed, devseed, tests), matching the component side's own
// generic-device fallback.
func genericComponentTypeForKind(kind string) string {
	switch ProductKind(kind) {
	case ProductService:
		return "generic-service"
	case ProductApp:
		return "generic-app"
	default:
		return "generic-device"
	}
}

func scanProduct(row pgx.Row) (*Product, error) {
	var m Product
	if err := row.Scan(&m.ID, &m.Name, &m.DisplayName, &m.VendorID, &m.VendorName, &m.DriverID, &m.DriverName, &m.Kind, &m.ParentProductID, &m.ParentProductName, &m.ComponentType, &m.ComponentTypeID, &m.Icon, &m.LabelRule, &m.Official, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

// mapProductWriteErr translates Postgres constraint violations on a product
// write into the product sentinels: a duplicate id is ErrTypeExists, and an
// unknown vendor/driver/parent/component_type FK is ErrProductRefNotFound.
// Anything else is wrapped with context.
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

// UpsertProduct installs or updates a product by HANDLE, the boot-seed
// phase's write. The seed ships name, not uuids, so the conflict target is
// the handle: re-seeding `cisco-room-bar` updates that row in place, and its
// id never moves.
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
	componentTypeRef := m.ComponentType
	if componentTypeRef == "" {
		componentTypeRef = genericComponentTypeForKind(m.Kind)
	}
	componentTypeID, err := resolveComponentTypeRef(ctx, tx, componentTypeRef)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		insert into product (name, display_name, vendor_id, driver_id, kind, parent_product_id, component_type_id, icon, label_rule, official)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		on conflict (name) do update
			set display_name      = excluded.display_name,
			    vendor_id         = excluded.vendor_id,
			    driver_id         = excluded.driver_id,
			    kind              = excluded.kind,
			    parent_product_id = excluded.parent_product_id,
			    component_type_id = excluded.component_type_id,
			    icon              = excluded.icon,
			    label_rule        = excluded.label_rule,
			    official          = excluded.official,
			    updated_at        = now()`,
		m.Name, m.DisplayName, m.VendorID, m.DriverID, m.Kind, m.ParentProductID, componentTypeID, m.Icon, nilIfEmpty(m.LabelRule), m.Official); err != nil {
		return fmt.Errorf("storage: upsert product %q: %w", m.Name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit upsert product: %w", err)
	}
	return nil
}

// ListProducts returns every product, ordered alphabetically by display_name,
// with unlabelled rows last and the name breaking ties (#613; see ListVendors for
// why the ordering is spelled nullif(...) nulls last).
func (p *PG) ListProducts(ctx context.Context) ([]Product, error) {
	rows, err := p.pool.Query(ctx, `select `+productCols+` from product order by nullif(display_name, '') nulls last, name`)
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
	return out, nil
}

// GetProduct resolves one product by id. An unknown id is ErrTypeNotFound.
func (p *PG) GetProduct(ctx context.Context, id string) (*Product, error) {
	m, err := scanProduct(p.pool.QueryRow(ctx, `select `+productCols+` from product where `+registryRefCol(id)+` = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get product %q: %w", id, err)
	}
	return m, nil
}

// CreateProduct inserts a custom (official=false) product and audits it. A
// duplicate id is ErrTypeExists; an unknown vendor/driver/parent/component_type
// is ErrProductRefNotFound; an out-of-set kind is ErrProductInvalidKind. An
// empty kind defaults to device; an empty component_type defaults to the
// generic instance of the resolved kind (genericComponentTypeForKind).
func (p *PG) CreateProduct(ctx context.Context, actorID string, m Product) (*Product, error) {
	if err := ValidateName("product", m.Name); err != nil {
		return nil, err
	}
	m.Official = false
	if m.Kind == "" {
		m.Kind = string(ProductDevice)
	}
	if !validProductKind(m.Kind) {
		return nil, ErrProductInvalidKind
	}
	if err := validateLabelRule(m.LabelRule); err != nil {
		return nil, err
	}
	m.LabelRule = nilIfEmpty(m.LabelRule)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create product: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := productRefs(ctx, tx, &m); err != nil {
		return nil, err
	}
	// component_type is required; an unclassified write (the direct-gateway
	// callers this default protects: seed, devseed, tests) falls back to the
	// generic instance of the product's own kind. The API layer requires an
	// explicit component_type on create, so this only ever fires below it.
	componentTypeRef := m.ComponentType
	if componentTypeRef == "" {
		componentTypeRef = genericComponentTypeForKind(m.Kind)
	}
	componentTypeID, err := resolveComponentTypeRef(ctx, tx, componentTypeRef)
	if err != nil {
		return nil, err
	}
	if err := tx.QueryRow(ctx, `
		insert into product (name, display_name, vendor_id, driver_id, kind, parent_product_id, component_type_id, icon, label_rule, official)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, false)
		returning id, created_at, updated_at`,
		m.Name, m.DisplayName, m.VendorID, m.DriverID, m.Kind, m.ParentProductID, componentTypeID, m.Icon, m.LabelRule).
		Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, mapProductWriteErr(err)
	}
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
// parent, component_type, or icon (nil fields unchanged) and audits it.
// Official rows are read-only (ErrTypeOfficial); an unknown id is
// ErrTypeNotFound; an unknown reference is ErrProductRefNotFound; an
// out-of-set kind is ErrProductInvalidKind.
func (p *PG) UpdateProduct(ctx context.Context, actorID, id string, patch ProductPatch) (*Product, error) {
	if patch.Kind != nil && !validProductKind(*patch.Kind) {
		return nil, ErrProductInvalidKind
	}
	if err := validateLabelRule(patch.LabelRule); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update product: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "product", id); err != nil {
		return nil, err
	}
	before, err := productAuditImage(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	// A patch's references arrive as handles or uuids; the columns store uuids, so
	// resolve each before the update (an unset one stays nil and is left unchanged).
	resolved := Product{VendorID: patch.VendorID, DriverID: patch.DriverID, ParentProductID: patch.ParentProductID}
	if err := productRefs(ctx, tx, &resolved); err != nil {
		return nil, err
	}
	// component_type has no clear state (it is required), so an unset patch
	// field resolves to nil and coalesce leaves it; a set one is resolved
	// strictly (no default-to-generic fallback here, unlike create: a patch
	// that names an unknown component_type is a mistake worth refusing, not a
	// hole worth papering over).
	var componentTypeID *string
	if patch.ComponentType != nil {
		ctid, err := resolveComponentTypeRef(ctx, tx, *patch.ComponentType)
		if err != nil {
			return nil, err
		}
		componentTypeID = &ctid
	}
	m, err := scanProduct(tx.QueryRow(ctx, `
		update product set
			display_name      = coalesce($2, display_name),
			vendor_id         = coalesce($3, vendor_id),
			driver_id         = coalesce($4, driver_id),
			kind              = coalesce($5, kind),
			parent_product_id = coalesce($6, parent_product_id),
			component_type_id = coalesce($7, component_type_id),
			icon              = coalesce($8, icon),
			-- An explicit empty string CLEARS the rule; see UpdateComponentType's
			-- own label_rule CASE for why this is not a coalesce.
			label_rule        = case
				when $9::text is null then label_rule
				when $9 = '' then null
				else $9::text
			end,
			updated_at        = now()
		where `+registryRefCol(id)+` = $1
		returning `+productCols,
		id, patch.DisplayName, resolved.VendorID, resolved.DriverID, patch.Kind, resolved.ParentProductID, componentTypeID, patch.Icon, patch.LabelRule))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTypeNotFound
		}
		return nil, mapProductWriteErr(err)
	}
	after, err := productAuditImage(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	// The caller addressed the product by uuid or by name; the audit row keys on
	// the uuid the before-image already carries, so a later rename cannot orphan it.
	if err := writeAuditRes(ctx, tx, actorID, "update", "product", auditImageID(before), before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update product: %w", err)
	}
	return m, nil
}

// productAuditImage is registryAuditImage under the product sentinel: an
// unknown ref reads as ErrTypeNotFound rather than a bare no-rows.
func productAuditImage(ctx context.Context, tx pgx.Tx, ref string) (map[string]any, error) {
	img, err := registryAuditImage(ctx, tx, "product", ref)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTypeNotFound
		}
		return nil, fmt.Errorf("storage: audit image product %q: %w", ref, err)
	}
	return img, nil
}

// DeleteProduct removes a custom product, refusing an official row
// (ErrTypeOfficial) or one still referenced by a component (ErrTypeInUse,
// from the component.product_id ON DELETE RESTRICT FK).
func (p *PG) DeleteProduct(ctx context.Context, actorID, id string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete product: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "product", id); err != nil {
		return err
	}
	before, err := productAuditImage(ctx, tx, id)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from product where `+registryRefCol(id)+` = $1`, id); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrTypeInUse
		}
		return fmt.Errorf("storage: delete product %q: %w", id, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "product", auditImageID(before), before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete product: %w", err)
	}
	return nil
}
