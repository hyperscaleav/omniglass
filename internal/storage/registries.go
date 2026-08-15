package storage

import (
	"context"
	"fmt"
)

// PropertyType is a canonical registered signal on the property lane of the
// catalog (#587): the non-numeric value domain a sample observes and a field
// declares (string/bool/json). Numeric signals live in MetricType, which carries
// the numeric facts (unit, precision); the lane IS the value type, so no kind
// discriminator remains. Validation/FusionPolicy are raw jsonb passed through.
// Official marks a seed-owned, read-only property.
type PropertyType struct {
	// ID is the uuid primary key; Name is the renameable handle (ADR-0062). A
	// property is addressed by name on the wire, and the id is the stable form the
	// contract and telemetry foreign keys store.
	ID           string
	Name         string
	Label        string
	DataType     string
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

// UpsertPropertyType installs an official property, authoritative on conflict (the
// boot-seed bucket): an operator's custom properties (official=false) are keyed by a
// distinct name and untouched. The validation fragment passes the same schema guard
// as the CRUD lane: the seed must not ship a schema an operator would be refused.
func (p *PG) UpsertPropertyType(ctx context.Context, prop PropertyType) error {
	if err := checkSchemaFragment(prop.Validation, "validation"); err != nil {
		return fmt.Errorf("storage: upsert property %q: %w: %w", prop.Name, ErrPropertyTypeInvalid, err)
	}
	_, err := p.pool.Exec(ctx, `
		insert into property_type (name, label, data_type, validation, fusion_policy, description, official)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (name) do update set
			label = excluded.label, data_type = excluded.data_type,
			validation = excluded.validation, fusion_policy = excluded.fusion_policy,
			description = excluded.description, official = excluded.official`,
		prop.Name, prop.Label, prop.DataType, prop.Validation, prop.FusionPolicy, prop.Description, prop.Official)
	if err != nil {
		return fmt.Errorf("storage: upsert property %q: %w", prop.Name, err)
	}
	return nil
}

// ListPropertyTypes returns every registered property (official and custom). No
// scope.Set: the registry is estate-wide reference data, not a scoped resource.
func (p *PG) ListPropertyTypes(ctx context.Context) ([]PropertyType, error) {
	rows, err := p.pool.Query(ctx, `select `+propertyCols+` from property_type`)
	if err != nil {
		return nil, fmt.Errorf("storage: list properties: %w", err)
	}
	defer rows.Close()
	var out []PropertyType
	for rows.Next() {
		var prop PropertyType
		if err := rows.Scan(&prop.ID, &prop.Name, &prop.Label, &prop.DataType, &prop.Validation, &prop.FusionPolicy, &prop.Description, &prop.Official); err != nil {
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
