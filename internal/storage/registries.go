package storage

import (
	"context"
	"fmt"
)

// DatapointType is a governed measurement definition. Unit/Precision are
// metric-only; Validation/FusionPolicy are raw jsonb passed through.
type DatapointType struct {
	Scope        string
	Name         string
	DisplayName  string
	Kind         string
	ValueType    string
	Unit         *string
	Precision    *int
	Validation   []byte
	FusionPolicy []byte
	Description  string
}

// InterfaceType is a connection kind. Built marks that a node-side adapter
// exists for it.
type InterfaceType struct {
	Name        string
	Official    bool
	Description string
	Built       bool
}

// UpsertDatapointType installs an official measurement definition, authoritative
// on conflict (the boot-seed bucket): operator-added org/template rows are keyed
// on a different scope and untouched.
func (p *PG) UpsertDatapointType(ctx context.Context, dt DatapointType) error {
	_, err := p.pool.Exec(ctx, `
		insert into datapoint_type (scope, name, display_name, kind, value_type, unit, precision, validation, fusion_policy, description)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		on conflict (scope, name) do update set
			display_name = excluded.display_name, kind = excluded.kind, value_type = excluded.value_type,
			unit = excluded.unit, precision = excluded.precision, validation = excluded.validation,
			fusion_policy = excluded.fusion_policy, description = excluded.description`,
		dt.Scope, dt.Name, dt.DisplayName, dt.Kind, dt.ValueType, dt.Unit, dt.Precision, dt.Validation, dt.FusionPolicy, dt.Description)
	if err != nil {
		return fmt.Errorf("storage: upsert datapoint_type %q: %w", dt.Name, err)
	}
	return nil
}

// ListDatapointTypes returns every registered measurement definition across all
// scopes. No scope.Set: the registry is estate-wide reference data, not a
// scoped resource.
func (p *PG) ListDatapointTypes(ctx context.Context) ([]DatapointType, error) {
	rows, err := p.pool.Query(ctx, `select scope, name, coalesce(display_name, ''), kind, value_type, unit, precision, validation, fusion_policy, description from datapoint_type`)
	if err != nil {
		return nil, fmt.Errorf("storage: list datapoint_types: %w", err)
	}
	defer rows.Close()
	var out []DatapointType
	for rows.Next() {
		var dt DatapointType
		if err := rows.Scan(&dt.Scope, &dt.Name, &dt.DisplayName, &dt.Kind, &dt.ValueType, &dt.Unit, &dt.Precision, &dt.Validation, &dt.FusionPolicy, &dt.Description); err != nil {
			return nil, fmt.Errorf("storage: scan datapoint_type: %w", err)
		}
		out = append(out, dt)
	}
	return out, rows.Err()
}

// UpsertInterfaceType installs an official connection kind, authoritative on
// conflict.
func (p *PG) UpsertInterfaceType(ctx context.Context, it InterfaceType) error {
	_, err := p.pool.Exec(ctx, `
		insert into interface_type (name, official, description, built)
		values ($1, $2, $3, $4)
		on conflict (name) do update set
			official = excluded.official, description = excluded.description, built = excluded.built`,
		it.Name, it.Official, it.Description, it.Built)
	if err != nil {
		return fmt.Errorf("storage: upsert interface_type %q: %w", it.Name, err)
	}
	return nil
}

// ListInterfaceTypes returns every registered connection kind.
func (p *PG) ListInterfaceTypes(ctx context.Context) ([]InterfaceType, error) {
	rows, err := p.pool.Query(ctx, `select name, official, description, built from interface_type`)
	if err != nil {
		return nil, fmt.Errorf("storage: list interface_types: %w", err)
	}
	defer rows.Close()
	var out []InterfaceType
	for rows.Next() {
		var it InterfaceType
		if err := rows.Scan(&it.Name, &it.Official, &it.Description, &it.Built); err != nil {
			return nil, fmt.Errorf("storage: scan interface_type: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
