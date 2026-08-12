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

// LocationTypeMetric is one line of a location type's declared-metric contract,
// the metric twin of LocationTypeProperty: every location of the type carries
// this metric, optionally with a default, optionally required. The metric
// catalog owns data_type, unit, and precision, so none is repeated here; a
// contract row only names the metric and how the type presents it.
type LocationTypeMetric struct {
	ID             string
	LocationTypeID string
	MetricTypeName string
	MetricTypeID   string
	DefaultValue   json.RawMessage // nil when the contract sets no default
	Required       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// LocationTypeMetricSpec is the write payload for one contract line. The metric
// is addressed by name, so a spec is complete on its own: there is no separate
// create and update shape (a set is an upsert on the location type/metric pair).
type LocationTypeMetricSpec struct {
	MetricTypeName string
	DefaultValue   json.RawMessage
	Required       bool
}

const locationTypeMetricCols = `id, location_type_id, (select mt.name from metric_type mt where mt.id = location_type_metric.metric_type_id) as metric_type_name, location_type_metric.metric_type_id as metric_type_id, default_value, required, created_at, updated_at`

func scanLocationTypeMetric(row pgx.Row) (*LocationTypeMetric, error) {
	var (
		lm  LocationTypeMetric
		def []byte // NULL when the contract sets no default
	)
	if err := row.Scan(&lm.ID, &lm.LocationTypeID, &lm.MetricTypeName, &lm.MetricTypeID, &def, &lm.Required, &lm.CreatedAt, &lm.UpdatedAt); err != nil {
		return nil, err
	}
	lm.DefaultValue = copyRaw(def)
	return &lm, nil
}

// mapLocationTypeMetricWriteErr translates Postgres constraint violations on a
// contract write into sentinels the caller already handles. The two foreign keys
// are told apart by constraint name so the fault names the side that is missing:
// an unknown location type is ErrTypeNotFound (what the official guard would
// have returned), an unknown metric is the catalog's ErrMetricTypeNotFound.
func mapLocationTypeMetricWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrTypeExists
		case "23503": // foreign_key_violation
			if pgErr.ConstraintName == "location_type_metric_metric_type_id_fkey" {
				return ErrMetricTypeNotFound
			}
			return ErrTypeNotFound
		}
	}
	return fmt.Errorf("storage: location type metric write: %w", err)
}

// upsertLocationTypeMetricRow writes one contract line, keyed by the unique
// (location_type_id, metric_type_id): the first write inserts, a later one
// revises the default and the required flag in place. Shared by the audited
// operator path and the boot-seed path so both keep the same semantics. It runs
// on any querier, so the seed path needs no transaction for its single statement.
func upsertLocationTypeMetricRow(ctx context.Context, q querier, locationTypeID string, spec LocationTypeMetricSpec) (*LocationTypeMetric, error) {
	if err := requireRegistryRow(ctx, q, "location_type", locationTypeID); err != nil {
		return nil, err
	}
	if err := requireMetricType(ctx, q, spec.MetricTypeName); err != nil {
		return nil, err
	}
	lm, err := scanLocationTypeMetric(q.QueryRow(ctx, `
		insert into location_type_metric (location_type_id, metric_type_id, default_value, required)
		values ((select id from location_type where `+registryRefCol(locationTypeID)+` = $1), (select id from metric_type where name = $2), $3, $4)
		on conflict (location_type_id, metric_type_id) do update
			set default_value = excluded.default_value,
			    required      = excluded.required,
			    updated_at    = now()
		returning `+locationTypeMetricCols,
		locationTypeID, spec.MetricTypeName, []byte(spec.DefaultValue), spec.Required))
	if err != nil {
		return nil, mapLocationTypeMetricWriteErr(err)
	}
	return lm, nil
}

// ListLocationTypeMetrics returns a location type's declared-metric contract,
// ordered by metric name. A location type that declares nothing lists empty; an
// unknown location type is indistinguishable from one with an empty contract,
// since the read side has nothing to disclose.
func (p *PG) ListLocationTypeMetrics(ctx context.Context, locationTypeID string) ([]LocationTypeMetric, error) {
	rows, err := p.pool.Query(ctx, `select `+locationTypeMetricCols+` from location_type_metric where location_type_id = (select id from location_type where `+registryRefCol(locationTypeID)+` = $1) order by (select mt.name from metric_type mt where mt.id = location_type_metric.metric_type_id)`, locationTypeID)
	if err != nil {
		return nil, fmt.Errorf("storage: list location type metrics %q: %w", locationTypeID, err)
	}
	defer rows.Close()
	out := []LocationTypeMetric{}
	for rows.Next() {
		lm, err := scanLocationTypeMetric(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan location type metric: %w", err)
		}
		out = append(out, *lm)
	}
	return out, rows.Err()
}

// SetLocationTypeMetric declares a metric on a location type (or revises the
// declaration), audited. An unknown location type is ErrTypeNotFound and an
// unknown metric is ErrMetricTypeNotFound. The audit names the location type as
// the resource because the contract belongs to it, with the verb reflecting
// whether this write added the line or revised one already there.
//
// A SHIPPED location type's contract is writable, for the reason
// SetLocationTypeProperty records on the property lane: a contract line is a row
// in its own table rather than a column of the registry row the fork covers, and
// no line here is seeded.
func (p *PG) SetLocationTypeMetric(ctx context.Context, actorID, locationTypeID string, spec LocationTypeMetricSpec) (*LocationTypeMetric, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin set location type metric: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := requireRegistryRow(ctx, tx, "location_type", locationTypeID); err != nil {
		return nil, err
	}
	// The before-image decides create vs update and gives the audit its old side.
	var before any
	prior, err := scanLocationTypeMetric(tx.QueryRow(ctx,
		`select `+locationTypeMetricCols+` from location_type_metric where location_type_id = (select id from location_type where `+registryRefCol(locationTypeID)+` = $1) and metric_type_id = (select id from metric_type where name = $2)`,
		locationTypeID, spec.MetricTypeName))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("storage: load location type metric %q/%q: %w", locationTypeID, spec.MetricTypeName, err)
	default:
		before = prior
	}

	lm, err := upsertLocationTypeMetricRow(ctx, tx, locationTypeID, spec)
	if err != nil {
		return nil, err
	}
	verb := "create"
	if before != nil {
		verb = "update"
	}
	if err := writeAuditRes(ctx, tx, actorID, verb, "location_type", locationTypeID, before, lm); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit set location type metric: %w", err)
	}
	return lm, nil
}

// DeleteLocationTypeMetric withdraws one metric from a location type's
// contract, audited as an update to the location type. An undeclared metric is
// ErrTypeNotFound. Shipped types are writable here for the reason
// SetLocationTypeMetric gives.
func (p *PG) DeleteLocationTypeMetric(ctx context.Context, actorID, locationTypeID, metricName string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete location type metric: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := requireRegistryRow(ctx, tx, "location_type", locationTypeID); err != nil {
		return err
	}
	// Delete and capture the before-image in one statement, so the audit records
	// the withdrawn declaration and a missing row is caught without a second read.
	before, err := scanLocationTypeMetric(tx.QueryRow(ctx, `
		delete from location_type_metric
		where location_type_id = (select id from location_type where `+registryRefCol(locationTypeID)+` = $1) and metric_type_id = (select id from metric_type where name = $2)
		returning `+locationTypeMetricCols, locationTypeID, metricName))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTypeNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: delete location type metric %q/%q: %w", locationTypeID, metricName, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "location_type", locationTypeID, before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete location type metric: %w", err)
	}
	return nil
}

// UpsertLocationTypeMetric installs one contract line for the boot-seed phase.
// Idempotent, and deliberately unaudited: a seed write is not an operator act.
// No location type contract ships today, so this is the lane a future one would
// arrive on rather than a writer with rows in flight.
func (p *PG) UpsertLocationTypeMetric(ctx context.Context, locationTypeID string, spec LocationTypeMetricSpec) error {
	_, err := upsertLocationTypeMetricRow(ctx, p.pool, locationTypeID, spec)
	return err
}
