package storage

import (
	"context"
	"fmt"
)

// Key is a canonical registered key: the typed keyspace entry a datapoint observes
// and a field declares. Unit/Precision are observed-only; Kind (metric/state/log) is
// null for a declared-only key; Validation/FusionPolicy are raw jsonb passed through.
// Official marks a seed-owned, read-only key.
type Key struct {
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

// UpsertKey installs an official key, authoritative on conflict (the boot-seed
// bucket): an operator's custom keys (official=false) are keyed by a distinct name
// and untouched.
func (p *PG) UpsertKey(ctx context.Context, k Key) error {
	_, err := p.pool.Exec(ctx, `
		insert into canonical_key (name, display_name, kind, data_type, unit, precision, validation, fusion_policy, description, official)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		on conflict (name) do update set
			display_name = excluded.display_name, kind = excluded.kind, data_type = excluded.data_type,
			unit = excluded.unit, precision = excluded.precision, validation = excluded.validation,
			fusion_policy = excluded.fusion_policy, description = excluded.description, official = excluded.official`,
		k.Name, k.DisplayName, k.Kind, k.DataType, k.Unit, k.Precision, k.Validation, k.FusionPolicy, k.Description, k.Official)
	if err != nil {
		return fmt.Errorf("storage: upsert canonical_key %q: %w", k.Name, err)
	}
	return nil
}

// ListKeys returns every registered key (official and custom). No scope.Set: the
// registry is estate-wide reference data, not a scoped resource.
func (p *PG) ListKeys(ctx context.Context) ([]Key, error) {
	rows, err := p.pool.Query(ctx, `select name, coalesce(display_name, ''), kind, data_type, unit, precision, validation, fusion_policy, description, official from canonical_key`)
	if err != nil {
		return nil, fmt.Errorf("storage: list canonical_keys: %w", err)
	}
	defer rows.Close()
	var out []Key
	for rows.Next() {
		var k Key
		if err := rows.Scan(&k.Name, &k.DisplayName, &k.Kind, &k.DataType, &k.Unit, &k.Precision, &k.Validation, &k.FusionPolicy, &k.Description, &k.Official); err != nil {
			return nil, fmt.Errorf("storage: scan canonical_key: %w", err)
		}
		out = append(out, k)
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
