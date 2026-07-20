package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/key"
	"github.com/jackc/pgx/v5"
)

// Sentinels for canonical_key CRUD. Official (seed-owned) keys are read-only.
var (
	ErrKeyNotFound = errors.New("storage: key not found")
	ErrKeyExists   = errors.New("storage: key name already exists")
	ErrKeyOfficial = errors.New("storage: official key is read-only")
	ErrKeyInvalid  = errors.New("storage: key is invalid")
)

// KeySpec is the create input for a canonical key.
type KeySpec struct {
	Name        string
	DisplayName string
	Kind        *string
	DataType    string
	Unit        *string
	Precision   *int
	Validation  []byte
	Description string
}

// KeyPatch carries the mutable fields of a key update; a nil field is unchanged.
// DataType and Kind are fixed at create (a key's type must not shift under its
// consumers). Validation replaces wholesale when non-nil.
type KeyPatch struct {
	DisplayName *string
	Description *string
	Unit        *string
	Validation  []byte
}

const keyCols = `name, coalesce(display_name, ''), kind, data_type, unit, precision, validation, fusion_policy, description, official`

func scanKey(row pgx.Row) (*Key, error) {
	var k Key
	if err := row.Scan(&k.Name, &k.DisplayName, &k.Kind, &k.DataType, &k.Unit, &k.Precision, &k.Validation, &k.FusionPolicy, &k.Description, &k.Official); err != nil {
		return nil, err
	}
	return &k, nil
}

// GetKey returns one key by name. The registry is estate-wide reference data, so
// there is no scope injection.
func (p *PG) GetKey(ctx context.Context, name string) (*Key, error) {
	k, err := scanKey(p.pool.QueryRow(ctx, `select `+keyCols+` from canonical_key where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get canonical_key %q: %w", name, err)
	}
	return k, nil
}

// guardKeyMutable loads a key's official flag by name: ErrKeyNotFound if absent,
// ErrKeyOfficial if seed-owned. Update and delete call it first.
func guardKeyMutable(ctx context.Context, q querier, name string) error {
	var official bool
	err := q.QueryRow(ctx, `select official from canonical_key where name = $1`, name).Scan(&official)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrKeyNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: load canonical_key %q: %w", name, err)
	}
	if official {
		return ErrKeyOfficial
	}
	return nil
}

// CreateKey inserts a custom (official=false) key and audits it. The name must be a
// valid canonical key and the validation must be well-formed JSON. A duplicate name
// is ErrKeyExists.
func (p *PG) CreateKey(ctx context.Context, actorID string, spec KeySpec) (*Key, error) {
	if err := key.ValidateKey(spec.Name); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyInvalid, err)
	}
	if len(spec.Validation) > 0 && !json.Valid(spec.Validation) {
		return nil, fmt.Errorf("%w: validation is not valid JSON", ErrKeyInvalid)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create canonical_key: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`insert into canonical_key (name, display_name, kind, data_type, unit, precision, validation, description, official)
		 values ($1, $2, $3, $4, $5, $6, $7, $8, false)`,
		spec.Name, spec.DisplayName, spec.Kind, spec.DataType, spec.Unit, spec.Precision, spec.Validation, spec.Description); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrKeyExists
		}
		return nil, fmt.Errorf("storage: insert canonical_key %q: %w", spec.Name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "canonical_key", spec.Name, nil, spec); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create canonical_key: %w", err)
	}
	return p.GetKey(ctx, spec.Name)
}

// UpdateKey patches a custom key's mutable fields (nil unchanged) and audits it.
// Official keys are read-only; an unknown name is ErrKeyNotFound.
func (p *PG) UpdateKey(ctx context.Context, actorID, name string, patch KeyPatch) (*Key, error) {
	if len(patch.Validation) > 0 && !json.Valid(patch.Validation) {
		return nil, fmt.Errorf("%w: validation is not valid JSON", ErrKeyInvalid)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update canonical_key: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardKeyMutable(ctx, tx, name); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		update canonical_key set
			display_name = coalesce($2, display_name),
			description  = coalesce($3, description),
			unit         = coalesce($4, unit),
			validation   = coalesce($5, validation)
		where name = $1`,
		name, patch.DisplayName, patch.Description, patch.Unit, patch.Validation); err != nil {
		return nil, fmt.Errorf("storage: update canonical_key %q: %w", name, err)
	}
	k, err := scanKey(tx.QueryRow(ctx, `select `+keyCols+` from canonical_key where name = $1`, name))
	if err != nil {
		return nil, fmt.Errorf("storage: reload canonical_key %q: %w", name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "canonical_key", name, nil, k); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update canonical_key: %w", err)
	}
	return k, nil
}

// DeleteKey removes a custom key and audits it. Official keys are read-only.
func (p *PG) DeleteKey(ctx context.Context, actorID, name string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete canonical_key: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardKeyMutable(ctx, tx, name); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from canonical_key where name = $1`, name); err != nil {
		return fmt.Errorf("storage: delete canonical_key %q: %w", name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "canonical_key", name, map[string]string{"name": name}, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete canonical_key: %w", err)
	}
	return nil
}
