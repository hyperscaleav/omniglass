package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// MetricType is a canonical registered signal on the metric lane of the catalog
// (#587): a numeric series key (int/float) carrying the numeric facts, unit and
// precision, that the property lane never holds. FusionPolicy is raw jsonb passed
// through. Official marks a seed-owned, read-only metric type.
type MetricType struct {
	// ID is the uuid primary key; Name is the renameable handle (ADR-0062). A
	// metric type is addressed by name on the wire, and the id is the stable form
	// the contract and telemetry foreign keys store.
	ID           string
	Name         string
	Label        string
	DataType     string
	Unit         *string
	Precision    *int
	FusionPolicy []byte
	Description  string
	Official     bool
}

// Sentinels for metric_type CRUD. Official (seed-owned) metric types are read-only.
var (
	ErrMetricTypeNotFound = errors.New("storage: metric type not found")
	ErrMetricTypeExists   = errors.New("storage: metric type name already exists")
	ErrMetricTypeOfficial = errors.New("storage: official metric type is read-only")
	ErrMetricTypeInvalid  = errors.New("storage: metric type is invalid")
)

// MetricTypeSpec is the create input for a metric type.
type MetricTypeSpec struct {
	Name        string
	Label       string
	DataType    string
	Unit        *string
	Precision   *int
	Description string
}

// MetricTypePatch carries the mutable fields of a metric-type update; a nil field
// is unchanged. DataType is fixed at create (a series' type must not shift under
// its consumers).
type MetricTypePatch struct {
	Label       *string
	Description *string
	Unit        *string
	Precision   *int
}

const metricTypeCols = `id, name, label, data_type, unit, "precision", fusion_policy, description, official`

func scanMetricType(row pgx.Row) (*MetricType, error) {
	var mt MetricType
	if err := row.Scan(&mt.ID, &mt.Name, &mt.Label, &mt.DataType, &mt.Unit, &mt.Precision, &mt.FusionPolicy, &mt.Description, &mt.Official); err != nil {
		return nil, err
	}
	return &mt, nil
}

// UpsertMetricType installs an official metric type, authoritative on conflict
// (the boot-seed bucket): an operator's custom metric types (official=false) are
// keyed by a distinct name and untouched. Mirrors UpsertPropertyType.
func (p *PG) UpsertMetricType(ctx context.Context, mt MetricType) error {
	_, err := p.pool.Exec(ctx, `
		insert into metric_type (name, label, data_type, unit, "precision", fusion_policy, description, official)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
		on conflict (name) do update set
			label = excluded.label, data_type = excluded.data_type,
			unit = excluded.unit, "precision" = excluded."precision",
			fusion_policy = excluded.fusion_policy, description = excluded.description,
			official = excluded.official`,
		mt.Name, mt.Label, mt.DataType, mt.Unit, mt.Precision, mt.FusionPolicy, mt.Description, mt.Official)
	if err != nil {
		return fmt.Errorf("storage: upsert metric type %q: %w", mt.Name, err)
	}
	return nil
}

// ListMetricTypes returns every registered metric type (official and custom). No
// scope.Set: the registry is estate-wide reference data, not a scoped resource.
func (p *PG) ListMetricTypes(ctx context.Context) ([]MetricType, error) {
	rows, err := p.pool.Query(ctx, `select `+metricTypeCols+` from metric_type`)
	if err != nil {
		return nil, fmt.Errorf("storage: list metric types: %w", err)
	}
	defer rows.Close()
	var out []MetricType
	for rows.Next() {
		var mt MetricType
		if err := rows.Scan(&mt.ID, &mt.Name, &mt.Label, &mt.DataType, &mt.Unit, &mt.Precision, &mt.FusionPolicy, &mt.Description, &mt.Official); err != nil {
			return nil, fmt.Errorf("storage: scan metric type: %w", err)
		}
		out = append(out, mt)
	}
	return out, rows.Err()
}

// GetMetricType returns one metric type by name. The registry is estate-wide
// reference data, so there is no scope injection.
func (p *PG) GetMetricType(ctx context.Context, name string) (*MetricType, error) {
	if err := RejectAddressForm("metric_type", name); err != nil {
		return nil, err
	}
	mt, err := scanMetricType(p.pool.QueryRow(ctx, `select `+metricTypeCols+` from metric_type where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMetricTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get metric type %q: %w", name, err)
	}
	return mt, nil
}

// guardMetricTypeMutable loads a metric type's official flag by name:
// ErrMetricTypeNotFound if absent, ErrMetricTypeOfficial if seed-owned. Update and
// delete call it first.
func guardMetricTypeMutable(ctx context.Context, q querier, name string) error {
	if err := RejectAddressForm("metric_type", name); err != nil {
		return err
	}
	var official bool
	err := q.QueryRow(ctx, `select official from metric_type where name = $1`, name).Scan(&official)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMetricTypeNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: load metric type %q: %w", name, err)
	}
	if official {
		return ErrMetricTypeOfficial
	}
	return nil
}

// CreateMetricType inserts a custom (official=false) metric type and audits it.
// The name must be a valid canonical key. A duplicate name is ErrMetricTypeExists.
func (p *PG) CreateMetricType(ctx context.Context, actorID string, spec MetricTypeSpec) (*MetricType, error) {
	if err := ValidateName("metric_type", spec.Name); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMetricTypeInvalid, err)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create metric type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Cross-registry uniqueness, the mirror of the check in CreatePropertyType: the
	// three ingest registries (property, metric, event) share one resolution
	// namespace, and a name in two of them resolves to nothing at ingest, so refuse
	// it here where the operator can still choose a different one.
	var clash bool
	if err := tx.QueryRow(ctx,
		`select exists (select 1 from property_type where name = $1)
		     or exists (select 1 from event_type where name = $1)`, spec.Name).Scan(&clash); err != nil {
		return nil, fmt.Errorf("storage: check registry clash for %q: %w", spec.Name, err)
	}
	if clash {
		return nil, ErrMetricTypeExists
	}

	// The insert returns the generated id so the audit row can key on the primary
	// key, which survives a later rename, rather than on the name.
	var mtID string
	if err := tx.QueryRow(ctx,
		`insert into metric_type (name, label, data_type, unit, "precision", description, official)
		 values ($1, $2, $3, $4, $5, $6, false)
		 returning id`,
		spec.Name, spec.Label, spec.DataType, spec.Unit, spec.Precision, spec.Description).Scan(&mtID); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrMetricTypeExists
		}
		return nil, fmt.Errorf("storage: insert metric type %q: %w", spec.Name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "metric_type", mtID, nil, spec); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create metric type: %w", err)
	}
	return p.GetMetricType(ctx, spec.Name)
}

// UpdateMetricType patches a custom metric type's mutable fields (nil unchanged)
// and audits it. Official metric types are read-only; an unknown name is
// ErrMetricTypeNotFound.
func (p *PG) UpdateMetricType(ctx context.Context, actorID, name string, patch MetricTypePatch) (*MetricType, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update metric type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardMetricTypeMutable(ctx, tx, name); err != nil {
		return nil, err
	}
	before, err := registryAuditImage(ctx, tx, "metric_type", name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMetricTypeNotFound
		}
		return nil, fmt.Errorf("storage: audit image metric_type %q: %w", name, err)
	}
	if _, err := tx.Exec(ctx, `
		update metric_type set
			label = coalesce($2, label),
			description  = coalesce($3, description),
			unit         = coalesce($4, unit),
			"precision"  = coalesce($5, "precision")
		where name = $1`,
		name, patch.Label, patch.Description, patch.Unit, patch.Precision); err != nil {
		return nil, fmt.Errorf("storage: update metric type %q: %w", name, err)
	}
	mt, err := scanMetricType(tx.QueryRow(ctx, `select `+metricTypeCols+` from metric_type where name = $1`, name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMetricTypeNotFound
		}
		return nil, fmt.Errorf("storage: reload metric type %q: %w", name, err)
	}
	after, err := registryAuditImage(ctx, tx, "metric_type", name)
	if err != nil {
		return nil, fmt.Errorf("storage: audit image metric_type %q: %w", name, err)
	}
	// A metric type is addressed by name, but the audit row keys on the uuid the
	// before-image already carries: a rename must not orphan the trail.
	if err := writeAuditRes(ctx, tx, actorID, "update", "metric_type", auditImageID(before), before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update metric type: %w", err)
	}
	return mt, nil
}

// DeleteMetricType removes a custom metric type and audits it. Official metric
// types are read-only.
func (p *PG) DeleteMetricType(ctx context.Context, actorID, name string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete metric type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardMetricTypeMutable(ctx, tx, name); err != nil {
		return err
	}
	before, err := registryAuditImage(ctx, tx, "metric_type", name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMetricTypeNotFound
		}
		return fmt.Errorf("storage: audit image metric_type %q: %w", name, err)
	}
	if _, err := tx.Exec(ctx, `delete from metric_type where name = $1`, name); err != nil {
		return fmt.Errorf("storage: delete metric type %q: %w", name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "metric_type", auditImageID(before), before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete metric type: %w", err)
	}
	return nil
}
