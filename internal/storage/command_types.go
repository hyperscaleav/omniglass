package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	Label               string
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
	// ErrCommandTypeTargetArc names the exclusive arc: the arc CHECK in the
	// schema is the backstop, this sentinel is the message the caller sees.
	ErrCommandTypeTargetArc = errors.New("storage: a command type targets a property or a metric, never both")
)

// commandTypeCols selects a command type with each target arm resolved to a name
// (the both-forms handle), NULL rendered as the empty string.
const commandTypeCols = `id, name, label, description, params_schema, settle_window_seconds,
	coalesce((select pt.name from property_type pt where pt.id = command_type.target_property_type_id), '') as target,
	coalesce((select mt.name from metric_type mt where mt.id = command_type.target_metric_type_id), '') as metric_target, official`

func scanCommandType(row pgx.Row) (*CommandType, error) {
	var ct CommandType
	if err := row.Scan(&ct.ID, &ct.Name, &ct.Label, &ct.Description, &ct.ParamsSchema,
		&ct.SettleWindowSeconds, &ct.TargetPropertyType, &ct.TargetMetricType, &ct.Official); err != nil {
		return nil, err
	}
	return &ct, nil
}

// UpsertCommandType installs an official command type, authoritative on conflict (the
// boot-seed bucket). Either target arm, when set, resolves by name up front rather
// than with a bare subselect in the insert: a subselect silently NULLs an
// unregistered name, and a seed file must refuse, naming the row, instead of
// quietly dropping its target. Mirrors UpsertEventType otherwise.
func (p *PG) UpsertCommandType(ctx context.Context, ct CommandType) error {
	// The same schema guard as the CRUD lane: the seed must not ship a
	// params_schema an operator would be refused.
	if err := checkSchemaFragment(ct.ParamsSchema, "params_schema"); err != nil {
		return fmt.Errorf("storage: upsert command type %q: %w: %w", ct.Name, ErrCommandTypeInvalid, err)
	}
	// Named like the target refusals beside it: a seed defect has to say which
	// row it is in, since the seed run is what a boot fails on.
	if err := checkSettleWindow(ct.SettleWindowSeconds); err != nil {
		return fmt.Errorf("storage: upsert command type %q: %w", ct.Name, err)
	}
	propID, metricID, err := resolveTargets(ctx, p.pool, ct.TargetPropertyType, ct.TargetMetricType)
	if err != nil {
		return fmt.Errorf("storage: upsert command type %q: %w", ct.Name, err)
	}
	_, err = p.pool.Exec(ctx, `
		insert into command_type (name, label, description, params_schema, settle_window_seconds, target_property_type_id, target_metric_type_id, official)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
		on conflict (name) do update set
			label = excluded.label, description = excluded.description,
			params_schema = excluded.params_schema, settle_window_seconds = excluded.settle_window_seconds,
			target_property_type_id = excluded.target_property_type_id,
			target_metric_type_id = excluded.target_metric_type_id, official = excluded.official`,
		ct.Name, ct.Label, ct.Description, schemaArg(ct.ParamsSchema), ct.SettleWindowSeconds, propID, metricID, ct.Official)
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
	if err := RejectAddressForm("command_type", name); err != nil {
		return nil, err
	}
	ct, err := scanCommandType(p.pool.QueryRow(ctx, `select `+commandTypeCols+` from command_type where name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCommandTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get command type %q: %w", name, err)
	}
	return ct, nil
}

// CommandTypeSpec is the create input for a custom command type. The target is a
// two-armed exclusive arc: at most one of TargetPropertyType and TargetMetricType.
type CommandTypeSpec struct {
	Name                string
	Label               string
	Description         string
	ParamsSchema        []byte
	SettleWindowSeconds int
	TargetPropertyType  string
	TargetMetricType    string
}

// CommandTypePatch carries the mutable fields; a nil field is unchanged. The name is
// fixed at create. The target is one fact with two forms: setting either arm
// non-empty replaces it wholesale (the other arm clears), the empty string clears
// the named arm, and naming both arms non-empty is the arc refusal.
type CommandTypePatch struct {
	Label               *string
	Description         *string
	ParamsSchema        []byte
	SettleWindowSeconds *int
	TargetPropertyType  *string
	TargetMetricType    *string
}

func guardCommandTypeMutable(ctx context.Context, q querier, name string) error {
	if err := RejectAddressForm("command_type", name); err != nil {
		return err
	}
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

// checkSettleWindow refuses a negative settle window (#718).
//
// The window is a DURATION the driver states, so the domain has a floor at zero
// and nothing below it means anything. What makes a negative one worth a
// refusal rather than a shrug is that it is INVISIBLE: [Settle] tests
// `windowSeconds > 0`, so -5 behaves exactly as 0 does, terminal by
// construction with no waiting at all. An operator who typed one would see the
// number they typed on the row and get behaviour that has nothing to do with
// it, on every command of that type, with nothing anywhere saying so.
//
// Zero is deliberately not refused. It is the documented way to say "settle
// immediately", a claim about intent that ADR-0108 made terminal before any
// arithmetic runs, and the shipped `reboot` type carries it. Refusing zero
// beside a target arm was considered under #718 and is not taken here: see the
// issue for why that is an architect's call rather than this guard's, since it
// would reverse ADR-0108's stated consequence for an operator's own zero-window
// type. This floor is the smallest one that admits every meaning the field has.
func checkSettleWindow(seconds int) error {
	if seconds < 0 {
		return fmt.Errorf("%w: settle_window_seconds is a duration and cannot be negative (got %d); 0 means settle immediately", ErrCommandTypeInvalid, seconds)
	}
	return nil
}

// resolveTarget checks a target name exists in the named classifier catalog and
// returns its id, or an invalid error naming the target. An empty name is a nil
// id (no target on that arm). The catalog is a compile-time constant at every
// call site ("property_type" or "metric_type"), never caller input.
func resolveTarget(ctx context.Context, q querier, catalog, name string) (*string, error) {
	if name == "" {
		return nil, nil
	}
	noun := strings.TrimSuffix(catalog, "_type")
	var id string
	err := q.QueryRow(ctx, `select id from `+catalog+` where name = $1`, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: target %s %q is not registered", ErrCommandTypeInvalid, noun, name)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: resolve target %s %q: %w", noun, name, err)
	}
	return &id, nil
}

// resolveTargets resolves both target arms at once, refusing when both are set:
// the exclusive arc surfaces here as a named refusal, with the schema's CHECK as
// the backstop rather than the message.
func resolveTargets(ctx context.Context, q querier, propertyName, metricName string) (propID, metricID *string, err error) {
	if propertyName != "" && metricName != "" {
		return nil, nil, ErrCommandTypeTargetArc
	}
	if propID, err = resolveTarget(ctx, q, "property_type", propertyName); err != nil {
		return nil, nil, err
	}
	if metricID, err = resolveTarget(ctx, q, "metric_type", metricName); err != nil {
		return nil, nil, err
	}
	return propID, metricID, nil
}

// CreateEventType-style create: a custom (official=false) command type, audited. The
// name must be a valid key, the params_schema well-formed JSON, and the target (when
// set) a registered property or metric type, at most one arm of the exclusive arc.
// A duplicate name is ErrCommandTypeExists.
func (p *PG) CreateCommandType(ctx context.Context, actorID string, spec CommandTypeSpec) (*CommandType, error) {
	if err := ValidateName("command_type", spec.Name); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCommandTypeInvalid, err)
	}
	if err := checkSchemaFragment(spec.ParamsSchema, "params_schema"); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCommandTypeInvalid, err)
	}
	if err := checkSettleWindow(spec.SettleWindowSeconds); err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create command type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	propTarget, metricTarget, err := resolveTargets(ctx, tx, spec.TargetPropertyType, spec.TargetMetricType)
	if err != nil {
		return nil, err
	}
	// The insert returns the generated id so the audit row can key on the primary
	// key, which survives a later rename, rather than on the name.
	var ctID string
	if err := tx.QueryRow(ctx,
		`insert into command_type (name, label, description, params_schema, settle_window_seconds, target_property_type_id, target_metric_type_id, official)
		 values ($1, $2, $3, $4, $5, $6, $7, false)
		 returning id`,
		spec.Name, spec.Label, spec.Description, schemaArg(spec.ParamsSchema), spec.SettleWindowSeconds, propTarget, metricTarget).Scan(&ctID); err != nil {
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
	if err := checkSchemaFragment(patch.ParamsSchema, "params_schema"); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCommandTypeInvalid, err)
	}
	if patch.SettleWindowSeconds != nil {
		if err := checkSettleWindow(*patch.SettleWindowSeconds); err != nil {
			return nil, err
		}
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
	// Each arm is resolved and replaced only when the patch sets it. Naming both
	// arms non-empty in one patch is the arc refusal, before any SQL runs.
	if patch.TargetPropertyType != nil && patch.TargetMetricType != nil &&
		*patch.TargetPropertyType != "" && *patch.TargetMetricType != "" {
		return nil, ErrCommandTypeTargetArc
	}
	var propTarget, metricTarget *string
	if patch.TargetPropertyType != nil {
		propTarget, err = resolveTarget(ctx, tx, "property_type", *patch.TargetPropertyType)
		if err != nil {
			return nil, err
		}
	}
	if patch.TargetMetricType != nil {
		metricTarget, err = resolveTarget(ctx, tx, "metric_type", *patch.TargetMetricType)
		if err != nil {
			return nil, err
		}
	}
	// The target is one fact with two forms: writing a non-empty arm clears the
	// other, so the arc CHECK can never trip on a patch that names one target.
	if _, err := tx.Exec(ctx, `
		update command_type set
			label          = coalesce($2, label),
			description           = coalesce($3, description),
			params_schema         = coalesce($4, params_schema),
			settle_window_seconds = coalesce($5, settle_window_seconds),
			target_property_type_id = case
				when $6::boolean then $7::uuid
				when $8::boolean and $9::uuid is not null then null
				else target_property_type_id end,
			target_metric_type_id = case
				when $8::boolean then $9::uuid
				when $6::boolean and $7::uuid is not null then null
				else target_metric_type_id end
		where name = $1`,
		name, patch.Label, patch.Description, schemaArg(patch.ParamsSchema), patch.SettleWindowSeconds,
		patch.TargetPropertyType != nil, propTarget, patch.TargetMetricType != nil, metricTarget); err != nil {
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
