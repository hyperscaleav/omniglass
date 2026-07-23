package storage

import (
	"context"
	"fmt"
)

// Property is a canonical registered signal: the typed keyspace entry a datapoint
// observes and a field declares. Unit/Precision are observed-only; Kind
// (metric/state/log) is null for a declared-only property; Validation/FusionPolicy
// are raw jsonb passed through. Official marks a seed-owned, read-only property. A
// property is addressed by its key (a canonical dotted identifier).
type Property struct {
	// ID is the uuid primary key; Name is the renameable handle (ADR-0062). A
	// property is addressed by name on the wire, and the id is the stable form the
	// contract and telemetry foreign keys store.
	ID           string
	Name         string
	DisplayName  string
	Kind         *string
	DataType     string
	Unit         *string
	Precision    *int
	Validation   []byte
	FusionPolicy []byte
	Description  string
	Official     bool
}

// InterfaceType is a connection kind. Built marks that a node-side adapter
// exists for it.
type InterfaceType struct {
	Name        string
	Official    bool
	Description string
	Built       bool
}

// UpsertProperty installs an official property, authoritative on conflict (the
// boot-seed bucket): an operator's custom properties (official=false) are keyed by a
// distinct name and untouched.
func (p *PG) UpsertProperty(ctx context.Context, prop Property) error {
	_, err := p.pool.Exec(ctx, `
		insert into property (name, display_name, kind, data_type, unit, precision, validation, fusion_policy, description, official)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		on conflict (name) do update set
			display_name = excluded.display_name, kind = excluded.kind, data_type = excluded.data_type,
			unit = excluded.unit, precision = excluded.precision, validation = excluded.validation,
			fusion_policy = excluded.fusion_policy, description = excluded.description, official = excluded.official`,
		prop.Name, prop.DisplayName, prop.Kind, prop.DataType, prop.Unit, prop.Precision, prop.Validation, prop.FusionPolicy, prop.Description, prop.Official)
	if err != nil {
		return fmt.Errorf("storage: upsert property %q: %w", prop.Name, err)
	}
	return nil
}

// ListProperties returns every registered property (official and custom). No
// scope.Set: the registry is estate-wide reference data, not a scoped resource.
func (p *PG) ListProperties(ctx context.Context) ([]Property, error) {
	rows, err := p.pool.Query(ctx, `select id, name, coalesce(display_name, ''), kind, data_type, unit, precision, validation, fusion_policy, description, official from property`)
	if err != nil {
		return nil, fmt.Errorf("storage: list properties: %w", err)
	}
	defer rows.Close()
	var out []Property
	for rows.Next() {
		var prop Property
		if err := rows.Scan(&prop.ID, &prop.Name, &prop.DisplayName, &prop.Kind, &prop.DataType, &prop.Unit, &prop.Precision, &prop.Validation, &prop.FusionPolicy, &prop.Description, &prop.Official); err != nil {
			return nil, fmt.Errorf("storage: scan property: %w", err)
		}
		out = append(out, prop)
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
