package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CommandType is a registered command in the driver-owned catalog: what a component
// can be told, the "do" half of the telemetry model (the twin of property_type and
// event_type). SettleWindowSeconds is a driver fact (how long the device takes to
// actuate); the target is a two-armed exclusive arc (#590): TargetPropertyType
// names the property a settleable command sets, TargetMetricType the metric ("set
// volume to 50" opens an intended numeric), at most one set and both empty for a
// fire-and-forget command like reboot. ParamsSchema is an optional JSON Schema for
// the invocation params. Official marks a seed-owned, read-only type.
type CommandType struct {
	ID                  string
	Name                string
	DisplayName         string
	Description         string
	ParamsSchema        []byte
	SettleWindowSeconds int
	TargetPropertyType  string
	TargetMetricType    string
	Official            bool
}

// Sentinels for command_type CRUD. Official (seed-owned) command types are read-only.
var (
	ErrCommandTypeNotFound = errors.New("storage: command type not found")
	ErrCommandTypeExists   = errors.New("storage: command type name already exists")
	ErrCommandTypeOfficial = errors.New("storage: official command type is read-only")
	ErrCommandTypeInvalid  = errors.New("storage: command type is invalid")
)

// commandTypeCols selects a command type with each target arm resolved to a name
// (the both-forms handle), NULL rendered as the empty string.
const commandTypeCols = `id, name, coalesce(display_name, ''), description, params_schema, settle_window_seconds,
	coalesce((select pt.name from property_type pt where pt.id = command_type.target_property_type_id), '') as target,
	coalesce((select mt.name from metric_type mt where mt.id = command_type.target_metric_type_id), '') as metric_target, official`

func scanCommandType(row pgx.Row) (*CommandType, error) {
	var ct CommandType
	if err := row.Scan(&ct.ID, &ct.Name, &ct.DisplayName, &ct.Description, &ct.ParamsSchema,
		&ct.SettleWindowSeconds, &ct.TargetPropertyType, &ct.TargetMetricType, &ct.Official); err != nil {
		return nil, err
	}
	return &ct, nil
}

// targetArg resolves the target property name to a SQL argument: the empty string
// stays a nil (NULL target), so a fire-and-forget command carries no target.
func targetArg(name string) any {
	if name == "" {
		return nil
	}
	return name
}

// UpsertCommandType installs an official command type, authoritative on conflict (the
// boot-seed bucket). The target property, when set, resolves by name. Mirrors
// UpsertEventType.
func (p *PG) UpsertCommandType(ctx context.Context, ct CommandType) error {
	_, err := p.pool.Exec(ctx, `
		insert into command_type (name, display_name, description, params_schema, settle_window_seconds, target_property_type_id, official)
		values ($1, $2, $3, $4, $5, (select id from property_type where name = $6), $7)
		on conflict (name) do update set
			display_name = excluded.display_name, description = excluded.description,
			params_schema = excluded.params_schema, settle_window_seconds = excluded.settle_window_seconds,
			target_property_type_id = excluded.target_property_type_id, official = excluded.official`,
		ct.Name, ct.DisplayName, ct.Description, schemaArg(ct.ParamsSchema), ct.SettleWindowSeconds, targetArg(ct.TargetPropertyType), ct.Official)
	if err != nil {
		return fmt.Errorf("storage: upsert command type %q: %w", ct.Name, err)
	}
	return nil
}

// ListCommandTypes returns every registered command type. Estate-wide reference data.
func (p *PG) ListCommandTypes(ctx context.Context) ([]CommandType, error) {
	rows, err := p.pool.Query(ctx, `select `+commandTypeCols+` from command_type`)
	if err != nil {
		return nil, fmt.Errorf("storage: list command types: %w", err)
	}
	defer rows.Close()
	var out []CommandType
	for rows.Next() {
		ct, err := scanCommandType(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan command type: %w", err)
		}
		out = append(out, *ct)
	}
	return out, rows.Err()
}

// GetCommandType resolves one command type by name.
func (p *PG) GetCommandType(ctx context.Context, name string) (*CommandType, error) {
	ct, err := scanCommandType(p.pool.QueryRow(ctx, `select `+commandTypeCols+` from command_type where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCommandTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get command type %q: %w", name, err)
	}
	return ct, nil
}

// CommandTypeSpec is the create input for a custom command type.
type CommandTypeSpec struct {
	Name                string
	DisplayName         string
	Description         string
	ParamsSchema        []byte
	SettleWindowSeconds int
	TargetPropertyType  string
}

// CommandTypePatch carries the mutable fields; a nil field is unchanged. The name is
// fixed at create.
type CommandTypePatch struct {
	DisplayName         *string
	Description         *string
	ParamsSchema        []byte
	SettleWindowSeconds *int
	TargetPropertyType  *string
}

func guardCommandTypeMutable(ctx context.Context, q querier, name string) error {
	var official bool
	err := q.QueryRow(ctx, `select official from command_type where name = $1`, name).Scan(&official)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCommandTypeNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: load command type %q: %w", name, err)
	}
	if official {
		return ErrCommandTypeOfficial
	}
	return nil
}

// resolveTarget checks a target property name exists (within the tx) and returns its
// id, or an invalid error. An empty name is a nil id (no target).
func resolveTarget(ctx context.Context, q querier, name string) (*string, error) {
	if name == "" {
		return nil, nil
	}
	var id string
	err := q.QueryRow(ctx, `select id from property_type where name = $1`, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: target property %q is not registered", ErrCommandTypeInvalid, name)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: resolve target property %q: %w", name, err)
	}
	return &id, nil
}

// CreateEventType-style create: a custom (official=false) command type, audited. The
// name must be a valid key, the params_schema well-formed JSON, and the target (when
// set) a registered property. A duplicate name is ErrCommandTypeExists.
func (p *PG) CreateCommandType(ctx context.Context, actorID string, spec CommandTypeSpec) (*CommandType, error) {
	if err := ValidateName("command_type", spec.Name); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCommandTypeInvalid, err)
	}
	if len(spec.ParamsSchema) > 0 && !json.Valid(spec.ParamsSchema) {
		return nil, fmt.Errorf("%w: params_schema is not valid JSON", ErrCommandTypeInvalid)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create command type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	target, err := resolveTarget(ctx, tx, spec.TargetPropertyType)
	if err != nil {
		return nil, err
	}
	// The insert returns the generated id so the audit row can key on the primary
	// key, which survives a later rename, rather than on the name.
	var ctID string
	if err := tx.QueryRow(ctx,
		`insert into command_type (name, display_name, description, params_schema, settle_window_seconds, target_property_type_id, official)
		 values ($1, $2, $3, $4, $5, $6, false)
		 returning id`,
		spec.Name, spec.DisplayName, spec.Description, schemaArg(spec.ParamsSchema), spec.SettleWindowSeconds, target).Scan(&ctID); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrCommandTypeExists
		}
		return nil, fmt.Errorf("storage: insert command type %q: %w", spec.Name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "command_type", ctID, nil, spec); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create command type: %w", err)
	}
	return p.GetCommandType(ctx, spec.Name)
}

// UpdateCommandType patches a custom command type's mutable fields (nil unchanged).
func (p *PG) UpdateCommandType(ctx context.Context, actorID, name string, patch CommandTypePatch) (*CommandType, error) {
	if len(patch.ParamsSchema) > 0 && !json.Valid(patch.ParamsSchema) {
		return nil, fmt.Errorf("%w: params_schema is not valid JSON", ErrCommandTypeInvalid)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update command type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardCommandTypeMutable(ctx, tx, name); err != nil {
		return nil, err
	}
	before, err := registryAuditImage(ctx, tx, "command_type", name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCommandTypeNotFound
		}
		return nil, fmt.Errorf("storage: audit image command_type %q: %w", name, err)
	}
	// The target is resolved and replaced only when the patch sets it.
	var target *string
	if patch.TargetPropertyType != nil {
		target, err = resolveTarget(ctx, tx, *patch.TargetPropertyType)
		if err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `
		update command_type set
			display_name          = coalesce($2, display_name),
			description           = coalesce($3, description),
			params_schema         = coalesce($4, params_schema),
			settle_window_seconds = coalesce($5, settle_window_seconds),
			target_property_type_id = case when $6::boolean then $7 else target_property_type_id end
		where name = $1`,
		name, patch.DisplayName, patch.Description, schemaArg(patch.ParamsSchema), patch.SettleWindowSeconds,
		patch.TargetPropertyType != nil, target); err != nil {
		return nil, fmt.Errorf("storage: update command type %q: %w", name, err)
	}
	ct, err := scanCommandType(tx.QueryRow(ctx, `select `+commandTypeCols+` from command_type where name = $1`, name))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCommandTypeNotFound
		}
		return nil, fmt.Errorf("storage: reload command type %q: %w", name, err)
	}
	after, err := registryAuditImage(ctx, tx, "command_type", name)
	if err != nil {
		return nil, fmt.Errorf("storage: audit image command_type %q: %w", name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "command_type", ct.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update command type: %w", err)
	}
	return ct, nil
}

// DeleteCommandType removes a custom command type. Official types are read-only.
func (p *PG) DeleteCommandType(ctx context.Context, actorID, name string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete command type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardCommandTypeMutable(ctx, tx, name); err != nil {
		return err
	}
	before, err := registryAuditImage(ctx, tx, "command_type", name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCommandTypeNotFound
		}
		return fmt.Errorf("storage: audit image command_type %q: %w", name, err)
	}
	if _, err := tx.Exec(ctx, `delete from command_type where name = $1`, name); err != nil {
		return fmt.Errorf("storage: delete command type %q: %w", name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "command_type", auditImageID(before), before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete command type: %w", err)
	}
	return nil
}
