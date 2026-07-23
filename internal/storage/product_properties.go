package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ProductProperty is one line of a product's declared-property contract: the
// product exposes this property, optionally with a default, optionally required.
// The property catalog owns data_type and validation, so neither is repeated
// here; a contract row only names the property and how the product presents it.
type ProductProperty struct {
	ID           string
	ProductID    string
	ProductName  string
	PropertyName string
	DefaultValue json.RawMessage // nil when the contract sets no default
	Required     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ProductPropertySpec is the write payload for one contract line. The property
// is addressed by name, so a spec is complete on its own: there is no separate
// create and update shape (a set is an upsert on the product/property pair).
type ProductPropertySpec struct {
	PropertyName string
	DefaultValue json.RawMessage
	Required     bool
}

// product_id stores the product's uuid; its handle comes back beside it so a
// caller reads what it wrote rather than an opaque key.
const productPropertyCols = `id, product_id,
	(select p.name from product p where p.id = product_property.product_id) as product_handle,
	(select pr.name from property pr where pr.id = product_property.property_id) as property_name,
	default_value, required, created_at, updated_at`

func scanProductProperty(row pgx.Row) (*ProductProperty, error) {
	var (
		pp  ProductProperty
		def []byte // NULL when the contract sets no default
	)
	if err := row.Scan(&pp.ID, &pp.ProductID, &pp.ProductName, &pp.PropertyName, &def, &pp.Required, &pp.CreatedAt, &pp.UpdatedAt); err != nil {
		return nil, err
	}
	pp.DefaultValue = copyRaw(def)
	return &pp, nil
}

// mapProductPropertyWriteErr translates Postgres constraint violations on a
// contract write into sentinels the caller already handles. The two foreign keys
// are told apart by constraint name so the fault names the side that is missing:
// an unknown product is ErrTypeNotFound (what the official guard would have
// returned), an unknown property is the catalog's ErrPropertyNotFound.
func mapProductPropertyWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrTypeExists
		case "23503": // foreign_key_violation
			if pgErr.ConstraintName == "product_property_property_id_fkey" {
				return ErrPropertyNotFound
			}
			return ErrTypeNotFound
		}
	}
	return fmt.Errorf("storage: product property write: %w", err)
}

// upsertProductPropertyRow writes one contract line, keyed by the unique
// (product_id, property_name): the first write inserts, a later one revises the
// default and the required flag in place. Shared by the audited operator path
// and the boot-seed path so both keep the same semantics. It runs on any
// querier, so the seed path needs no transaction for its single statement.
func upsertProductPropertyRow(ctx context.Context, q querier, productID string, spec ProductPropertySpec) (*ProductProperty, error) {
	if _, err := resolveProductRef(ctx, q, productID); err != nil {
		return nil, ErrTypeNotFound
	}
	if err := requireProperty(ctx, q, spec.PropertyName); err != nil {
		return nil, err
	}
	pp, err := scanProductProperty(q.QueryRow(ctx, `
		insert into product_property (product_id, property_id, default_value, required)
		values ((select id from product where `+productRefCol(productID)+` = $1),
		        (select id from property where name = $2), $3, $4)
		on conflict (product_id, property_id) do update
			set default_value = excluded.default_value,
			    required      = excluded.required,
			    updated_at    = now()
		returning `+productPropertyCols,
		productID, spec.PropertyName, []byte(spec.DefaultValue), spec.Required))
	if err != nil {
		return nil, mapProductPropertyWriteErr(err)
	}
	return pp, nil
}

// ListProductProperties returns a product's declared-property contract, ordered
// by property name. A product that declares nothing lists empty; an unknown
// product is indistinguishable from one with an empty contract, since the read
// side has nothing to disclose.
func (p *PG) ListProductProperties(ctx context.Context, productID string) ([]ProductProperty, error) {
	rows, err := p.pool.Query(ctx, `select `+productPropertyCols+` from product_property where product_id = (select id from product where `+productRefCol(productID)+` = $1) order by (select pr.name from property pr where pr.id = product_property.property_id)`, productID)
	if err != nil {
		return nil, fmt.Errorf("storage: list product properties %q: %w", productID, err)
	}
	defer rows.Close()
	out := []ProductProperty{}
	for rows.Next() {
		pp, err := scanProductProperty(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan product property: %w", err)
		}
		out = append(out, *pp)
	}
	return out, rows.Err()
}

// SetProductProperty declares a property on a product (or revises the
// declaration), audited. Official products carry seed-owned contracts and are
// read-only (ErrTypeOfficial); an unknown product is ErrTypeNotFound and an
// unknown property is ErrPropertyNotFound. The audit names the product as the
// resource because the contract belongs to it, with the verb reflecting whether
// this write added the line or revised one already there.
func (p *PG) SetProductProperty(ctx context.Context, actorID, productID string, spec ProductPropertySpec) (*ProductProperty, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin set product property: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "product", productID); err != nil {
		return nil, err
	}
	// The before-image decides create vs update and gives the audit its old side.
	var before any
	prior, err := scanProductProperty(tx.QueryRow(ctx,
		`select `+productPropertyCols+` from product_property where product_id = (select id from product where `+productRefCol(productID)+` = $1) and property_id = (select id from property where name = $2)`,
		productID, spec.PropertyName))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("storage: load product property %q/%q: %w", productID, spec.PropertyName, err)
	default:
		before = prior
	}

	pp, err := upsertProductPropertyRow(ctx, tx, productID, spec)
	if err != nil {
		return nil, err
	}
	verb := "create"
	if before != nil {
		verb = "update"
	}
	if err := writeAuditRes(ctx, tx, actorID, verb, "product", productID, before, pp); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit set product property: %w", err)
	}
	return pp, nil
}

// DeleteProductProperty withdraws one property from a product's contract,
// audited as an update to the product. Official products are read-only
// (ErrTypeOfficial); an undeclared property is ErrTypeNotFound.
func (p *PG) DeleteProductProperty(ctx context.Context, actorID, productID, propertyName string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete product property: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "product", productID); err != nil {
		return err
	}
	// Delete and capture the before-image in one statement, so the audit records
	// the withdrawn declaration and a missing row is caught without a second read.
	before, err := scanProductProperty(tx.QueryRow(ctx, `
		delete from product_property
		where product_id = (select id from product where `+productRefCol(productID)+` = $1) and property_id = (select id from property where name = $2)
		returning `+productPropertyCols, productID, propertyName))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTypeNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: delete product property %q/%q: %w", productID, propertyName, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "product", productID, before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete product property: %w", err)
	}
	return nil
}

// UpsertProductProperty installs one contract line for the boot-seed phase.
// Idempotent, and deliberately unguarded and unaudited: the seed owns the
// official products' contracts, so the official read-only rule (which protects
// them from operators) does not apply to the writer that ships them.
func (p *PG) UpsertProductProperty(ctx context.Context, productID string, spec ProductPropertySpec) error {
	_, err := upsertProductPropertyRow(ctx, p.pool, productID, spec)
	return err
}
