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

// ProductMetric is one line of a product's declared-metric contract, the metric
// twin of ProductProperty: the product's instances carry this metric, optionally
// with a default, optionally required. The metric catalog owns data_type, unit,
// and precision, so none is repeated here; a contract row only names the metric
// and how the product presents it.
type ProductMetric struct {
	ID             string
	ProductID      string
	ProductName    string
	MetricTypeName string
	MetricTypeID   string
	DefaultValue   json.RawMessage // nil when the contract sets no default
	Required       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ProductMetricSpec is the write payload for one contract line. The metric is
// addressed by name, so a spec is complete on its own: there is no separate
// create and update shape (a set is an upsert on the product/metric pair).
type ProductMetricSpec struct {
	MetricTypeName string
	DefaultValue   json.RawMessage
	Required       bool
}

// product_id stores the product's uuid; its handle comes back beside it so a
// caller reads what it wrote rather than an opaque key.
const productMetricCols = `id, product_id,
	(select p.name from product p where p.id = product_metric.product_id) as product_handle,
	(select mt.name from metric_type mt where mt.id = product_metric.metric_type_id) as metric_type_name,
	product_metric.metric_type_id as metric_type_id,
	default_value, required, created_at, updated_at`

func scanProductMetric(row pgx.Row) (*ProductMetric, error) {
	var (
		pm  ProductMetric
		def []byte // NULL when the contract sets no default
	)
	if err := row.Scan(&pm.ID, &pm.ProductID, &pm.ProductName, &pm.MetricTypeName, &pm.MetricTypeID, &def, &pm.Required, &pm.CreatedAt, &pm.UpdatedAt); err != nil {
		return nil, err
	}
	pm.DefaultValue = copyRaw(def)
	return &pm, nil
}

// mapProductMetricWriteErr translates Postgres constraint violations on a
// contract write into sentinels the caller already handles. The two foreign keys
// are told apart by constraint name so the fault names the side that is missing:
// an unknown product is ErrTypeNotFound (what the official guard would have
// returned), an unknown metric is the catalog's ErrMetricTypeNotFound.
func mapProductMetricWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrTypeExists
		case "23503": // foreign_key_violation
			if pgErr.ConstraintName == "product_metric_metric_type_id_fkey" {
				return ErrMetricTypeNotFound
			}
			return ErrTypeNotFound
		}
	}
	return fmt.Errorf("storage: product metric write: %w", err)
}

// upsertProductMetricRow writes one contract line, keyed by the unique
// (product_id, metric_type_id): the first write inserts, a later one revises the
// default and the required flag in place. Shared by the audited operator path
// and the boot-seed path so both keep the same semantics. It runs on any
// querier, so the seed path needs no transaction for its single statement.
func upsertProductMetricRow(ctx context.Context, q querier, productID string, spec ProductMetricSpec) (*ProductMetric, error) {
	if _, err := resolveProductRef(ctx, q, productID); err != nil {
		return nil, ErrTypeNotFound
	}
	if err := requireMetricType(ctx, q, spec.MetricTypeName); err != nil {
		return nil, err
	}
	pm, err := scanProductMetric(q.QueryRow(ctx, `
		insert into product_metric (product_id, metric_type_id, default_value, required)
		values ((select id from product where `+registryRefCol(productID)+` = $1),
		        (select id from metric_type where name = $2), $3, $4)
		on conflict (product_id, metric_type_id) do update
			set default_value = excluded.default_value,
			    required      = excluded.required,
			    updated_at    = now()
		returning `+productMetricCols,
		productID, spec.MetricTypeName, []byte(spec.DefaultValue), spec.Required))
	if err != nil {
		return nil, mapProductMetricWriteErr(err)
	}
	return pm, nil
}

// ListProductMetrics returns a product's declared-metric contract, ordered by
// metric name. A product that declares nothing lists empty; an unknown product
// is indistinguishable from one with an empty contract, since the read side has
// nothing to disclose.
func (p *PG) ListProductMetrics(ctx context.Context, productID string) ([]ProductMetric, error) {
	rows, err := p.pool.Query(ctx, `select `+productMetricCols+` from product_metric where product_id = (select id from product where `+registryRefCol(productID)+` = $1) order by (select mt.name from metric_type mt where mt.id = product_metric.metric_type_id)`, productID)
	if err != nil {
		return nil, fmt.Errorf("storage: list product metrics %q: %w", productID, err)
	}
	defer rows.Close()
	out := []ProductMetric{}
	for rows.Next() {
		pm, err := scanProductMetric(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan product metric: %w", err)
		}
		out = append(out, *pm)
	}
	return out, rows.Err()
}

// SetProductMetric declares a metric on a product (or revises the declaration),
// audited. Official products carry seed-owned contracts and are read-only
// (ErrTypeOfficial); an unknown product is ErrTypeNotFound and an unknown
// metric is ErrMetricTypeNotFound. The audit names the product as the resource
// because the contract belongs to it, with the verb reflecting whether this
// write added the line or revised one already there.
func (p *PG) SetProductMetric(ctx context.Context, actorID, productID string, spec ProductMetricSpec) (*ProductMetric, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin set product metric: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "product", productID); err != nil {
		return nil, err
	}
	// The before-image decides create vs update and gives the audit its old side.
	var before any
	prior, err := scanProductMetric(tx.QueryRow(ctx,
		`select `+productMetricCols+` from product_metric where product_id = (select id from product where `+registryRefCol(productID)+` = $1) and metric_type_id = (select id from metric_type where name = $2)`,
		productID, spec.MetricTypeName))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("storage: load product metric %q/%q: %w", productID, spec.MetricTypeName, err)
	default:
		before = prior
	}

	pm, err := upsertProductMetricRow(ctx, tx, productID, spec)
	if err != nil {
		return nil, err
	}
	verb := "create"
	if before != nil {
		verb = "update"
	}
	if err := writeAuditRes(ctx, tx, actorID, verb, "product", productID, before, pm); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit set product metric: %w", err)
	}
	return pm, nil
}

// DeleteProductMetric withdraws one metric from a product's contract, audited
// as an update to the product. Official products are read-only
// (ErrTypeOfficial); an undeclared metric is ErrTypeNotFound.
func (p *PG) DeleteProductMetric(ctx context.Context, actorID, productID, metricName string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete product metric: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "product", productID); err != nil {
		return err
	}
	// Delete and capture the before-image in one statement, so the audit records
	// the withdrawn declaration and a missing row is caught without a second read.
	before, err := scanProductMetric(tx.QueryRow(ctx, `
		delete from product_metric
		where product_id = (select id from product where `+registryRefCol(productID)+` = $1) and metric_type_id = (select id from metric_type where name = $2)
		returning `+productMetricCols, productID, metricName))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTypeNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: delete product metric %q/%q: %w", productID, metricName, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "product", productID, before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete product metric: %w", err)
	}
	return nil
}

// UpsertProductMetric installs one contract line for the boot-seed phase.
// Idempotent, and deliberately unguarded and unaudited: the seed owns the
// official products' contracts, so the official read-only rule (which protects
// them from operators) does not apply to the writer that ships them.
func (p *PG) UpsertProductMetric(ctx context.Context, productID string, spec ProductMetricSpec) error {
	_, err := upsertProductMetricRow(ctx, p.pool, productID, spec)
	return err
}
