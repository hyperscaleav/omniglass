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

// StandardMetric is one line of a standard's declared-metric contract, the
// metric twin of StandardProperty: every system conforming to the standard
// carries this metric, optionally with a default, optionally required. The
// metric catalog owns data_type, unit, and precision, so none is repeated here;
// a contract row only names the metric and how the standard presents it.
type StandardMetric struct {
	ID             string
	StandardID     string
	StandardName   string
	MetricTypeName string
	MetricTypeID   string
	DefaultValue   json.RawMessage // nil when the contract sets no default
	Required       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// StandardMetricSpec is the write payload for one contract line. The metric is
// addressed by name, so a spec is complete on its own: there is no separate
// create and update shape (a set is an upsert on the standard/metric pair).
type StandardMetricSpec struct {
	MetricTypeName string
	DefaultValue   json.RawMessage
	Required       bool
}

const standardMetricCols = `id, standard_id,
	(select s.name from standard s where s.id = standard_metric.standard_id) as standard_handle, (select mt.name from metric_type mt where mt.id = standard_metric.metric_type_id) as metric_type_name, standard_metric.metric_type_id as metric_type_id, default_value, required, created_at, updated_at`

func scanStandardMetric(row pgx.Row) (*StandardMetric, error) {
	var (
		sm  StandardMetric
		def []byte // NULL when the contract sets no default
	)
	if err := row.Scan(&sm.ID, &sm.StandardID, &sm.StandardName, &sm.MetricTypeName, &sm.MetricTypeID, &def, &sm.Required, &sm.CreatedAt, &sm.UpdatedAt); err != nil {
		return nil, err
	}
	sm.DefaultValue = copyRaw(def)
	return &sm, nil
}

// mapStandardMetricWriteErr translates Postgres constraint violations on a
// contract write into sentinels the caller already handles. The two foreign keys
// are told apart by constraint name so the fault names the side that is missing:
// an unknown standard is ErrTypeNotFound (what the official guard would have
// returned), an unknown metric is the catalog's ErrMetricTypeNotFound.
func mapStandardMetricWriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrTypeExists
		case "23503": // foreign_key_violation
			if pgErr.ConstraintName == "standard_metric_metric_type_id_fkey" {
				return ErrMetricTypeNotFound
			}
			return ErrTypeNotFound
		}
	}
	return fmt.Errorf("storage: standard metric write: %w", err)
}

// upsertStandardMetricRow writes one contract line, keyed by the unique
// (standard_id, metric_type_id): the first write inserts, a later one revises
// the default and the required flag in place. Shared by the audited operator
// path and the boot-seed path so both keep the same semantics. It runs on any
// querier, so the seed path needs no transaction for its single statement.
func upsertStandardMetricRow(ctx context.Context, q querier, standardID string, spec StandardMetricSpec) (*StandardMetric, error) {
	if err := requireRegistryRow(ctx, q, "standard", standardID); err != nil {
		return nil, err
	}
	if err := requireMetricType(ctx, q, spec.MetricTypeName); err != nil {
		return nil, err
	}
	sm, err := scanStandardMetric(q.QueryRow(ctx, `
		insert into standard_metric (standard_id, metric_type_id, default_value, required)
		values ((select id from standard where `+registryRefCol(standardID)+` = $1), (select id from metric_type where name = $2), $3, $4)
		on conflict (standard_id, metric_type_id) do update
			set default_value = excluded.default_value,
			    required      = excluded.required,
			    updated_at    = now()
		returning `+standardMetricCols,
		standardID, spec.MetricTypeName, []byte(spec.DefaultValue), spec.Required))
	if err != nil {
		return nil, mapStandardMetricWriteErr(err)
	}
	return sm, nil
}

// ListStandardMetrics returns a standard's declared-metric contract, ordered by
// metric name. A standard that declares nothing lists empty; an unknown
// standard is indistinguishable from one with an empty contract, since the read
// side has nothing to disclose.
func (p *PG) ListStandardMetrics(ctx context.Context, standardID string) ([]StandardMetric, error) {
	rows, err := p.pool.Query(ctx, `select `+standardMetricCols+` from standard_metric where standard_id = (select id from standard where `+registryRefCol(standardID)+` = $1) order by (select mt.name from metric_type mt where mt.id = standard_metric.metric_type_id)`, standardID)
	if err != nil {
		return nil, fmt.Errorf("storage: list standard metrics %q: %w", standardID, err)
	}
	defer rows.Close()
	out := []StandardMetric{}
	for rows.Next() {
		sm, err := scanStandardMetric(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan standard metric: %w", err)
		}
		out = append(out, *sm)
	}
	return out, rows.Err()
}

// SetStandardMetric declares a metric on a standard (or revises the
// declaration), audited. Official standards carry seed-owned contracts and are
// read-only (ErrTypeOfficial); an unknown standard is ErrTypeNotFound and an
// unknown metric is ErrMetricTypeNotFound. The audit names the standard as the
// resource because the contract belongs to it, with the verb reflecting whether
// this write added the line or revised one already there.
func (p *PG) SetStandardMetric(ctx context.Context, actorID, standardID string, spec StandardMetricSpec) (*StandardMetric, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin set standard metric: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "standard", standardID); err != nil {
		return nil, err
	}
	// The before-image decides create vs update and gives the audit its old side.
	var before any
	prior, err := scanStandardMetric(tx.QueryRow(ctx,
		`select `+standardMetricCols+` from standard_metric where standard_id = (select id from standard where `+registryRefCol(standardID)+` = $1) and metric_type_id = (select id from metric_type where name = $2)`,
		standardID, spec.MetricTypeName))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("storage: load standard metric %q/%q: %w", standardID, spec.MetricTypeName, err)
	default:
		before = prior
	}

	sm, err := upsertStandardMetricRow(ctx, tx, standardID, spec)
	if err != nil {
		return nil, err
	}
	verb := "create"
	if before != nil {
		verb = "update"
	}
	if err := writeAuditRes(ctx, tx, actorID, verb, "standard", standardID, before, sm); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit set standard metric: %w", err)
	}
	return sm, nil
}

// DeleteStandardMetric withdraws one metric from a standard's contract, audited
// as an update to the standard. Official standards are read-only
// (ErrTypeOfficial); an undeclared metric is ErrTypeNotFound.
func (p *PG) DeleteStandardMetric(ctx context.Context, actorID, standardID, metricName string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete standard metric: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardTypeMutable(ctx, tx, "standard", standardID); err != nil {
		return err
	}
	// Delete and capture the before-image in one statement, so the audit records
	// the withdrawn declaration and a missing row is caught without a second read.
	before, err := scanStandardMetric(tx.QueryRow(ctx, `
		delete from standard_metric
		where standard_id = (select id from standard where `+registryRefCol(standardID)+` = $1) and metric_type_id = (select id from metric_type where name = $2)
		returning `+standardMetricCols, standardID, metricName))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrTypeNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: delete standard metric %q/%q: %w", standardID, metricName, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "standard", standardID, before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete standard metric: %w", err)
	}
	return nil
}

// UpsertStandardMetric installs one contract line for the boot-seed phase.
// Idempotent, and deliberately unguarded and unaudited: the seed owns the
// official standards' contracts, so the official read-only rule (which protects
// them from operators) does not apply to the writer that ships them.
func (p *PG) UpsertStandardMetric(ctx context.Context, standardID string, spec StandardMetricSpec) error {
	_, err := upsertStandardMetricRow(ctx, p.pool, standardID, spec)
	return err
}
