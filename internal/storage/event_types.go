package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/key"
	"github.com/jackc/pgx/v5"
)

// EventType is a registered occurrence key: the "happen" half of the telemetry
// model, the twin of PropertyType (the "know" half). An event is a different shape
// from a sample (an occurrence, not a value), so it gets its own registry.
// PayloadSchema is an optional jsonb schema for the occurrence payload. Official
// marks a seed-owned, read-only type. An event type is addressed by its name.
type EventType struct {
	ID            string
	Name          string
	Label         string
	Description   string
	PayloadSchema []byte
	Official      bool
}

// Sentinels for event_type CRUD. Official (seed-owned) event types are read-only.
var (
	ErrEventTypeNotFound = errors.New("storage: event type not found")
	ErrEventTypeExists   = errors.New("storage: event type name already exists")
	ErrEventTypeOfficial = errors.New("storage: official event type is read-only")
	ErrEventTypeInvalid  = errors.New("storage: event type is invalid")
)

// EventTypeSpec is the create input for a custom event type.
type EventTypeSpec struct {
	Name          string
	Label         string
	Description   string
	PayloadSchema []byte
}

// EventTypePatch carries the mutable fields of an event-type update; a nil field is
// unchanged. The name is fixed at create. PayloadSchema replaces wholesale when
// non-nil.
type EventTypePatch struct {
	Label         *string
	Description   *string
	PayloadSchema []byte
}

// UpsertEventType installs an official event type, authoritative on conflict (the
// boot-seed bucket): an operator's custom event types (official=false) are keyed by
// a distinct name and untouched. Mirrors UpsertPropertyType, schema guard included.
func (p *PG) UpsertEventType(ctx context.Context, et EventType) error {
	if err := checkSchemaFragment(et.PayloadSchema, "payload_schema"); err != nil {
		return fmt.Errorf("storage: upsert event type %q: %w: %w", et.Name, ErrEventTypeInvalid, err)
	}
	var schema any
	if len(et.PayloadSchema) > 0 {
		schema = string(et.PayloadSchema)
	}
	_, err := p.pool.Exec(ctx, `
		insert into event_type (name, label, description, payload_schema, official)
		values ($1, $2, $3, $4, $5)
		on conflict (name) do update set
			label = excluded.label, description = excluded.description,
			payload_schema = excluded.payload_schema, official = excluded.official`,
		et.Name, et.Label, et.Description, schema, et.Official)
	if err != nil {
		return fmt.Errorf("storage: upsert event type %q: %w", et.Name, err)
	}
	return nil
}

// ListEventTypes returns every registered event type (official and custom). No
// scope.Set: the registry is estate-wide reference data.
func (p *PG) ListEventTypes(ctx context.Context) ([]EventType, error) {
	rows, err := p.pool.Query(ctx, `select id, name, label, description, payload_schema, official from event_type`)
	if err != nil {
		return nil, fmt.Errorf("storage: list event types: %w", err)
	}
	defer rows.Close()
	var out []EventType
	for rows.Next() {
		var et EventType
		if err := rows.Scan(&et.ID, &et.Name, &et.Label, &et.Description, &et.PayloadSchema, &et.Official); err != nil {
			return nil, fmt.Errorf("storage: scan event type: %w", err)
		}
		out = append(out, et)
	}
	return out, rows.Err()
}

// GetEventType resolves one event type by name, ErrEventTypeNotFound when absent.
func (p *PG) GetEventType(ctx context.Context, name string) (*EventType, error) {
	if err := RejectAddressForm("event_type", name); err != nil {
		return nil, err
	}
	var et EventType
	err := p.pool.QueryRow(ctx,
		`select id, name, label, description, payload_schema, official from event_type where name = $1`,
		name).Scan(&et.ID, &et.Name, &et.Label, &et.Description, &et.PayloadSchema, &et.Official)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEventTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get event type %q: %w", name, err)
	}
	return &et, nil
}

// guardEventTypeMutable loads an event type's official flag: ErrEventTypeNotFound if
// absent, ErrEventTypeOfficial if seed-owned. Update and delete call it first.
func guardEventTypeMutable(ctx context.Context, q querier, name string) error {
	if err := RejectAddressForm("event_type", name); err != nil {
		return err
	}
	var official bool
	err := q.QueryRow(ctx, `select official from event_type where name = $1`, name).Scan(&official)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrEventTypeNotFound
	}
	if err != nil {
		return fmt.Errorf("storage: load event type %q: %w", name, err)
	}
	if official {
		return ErrEventTypeOfficial
	}
	return nil
}

func schemaArg(b []byte) any {
	if len(b) > 0 {
		return string(b)
	}
	return nil
}

// checkSchemaFragment guards a stored JSON Schema fragment at the door of every
// catalog write: well-formed JSON first (the yaml loader underneath would accept
// more), then the bare-required refusal (key.ValidateSchema), so a schema that
// would silently not enforce is refused with the offending property named. col
// names the column in the error, matching each catalog's vocabulary.
func checkSchemaFragment(fragment []byte, col string) error {
	if len(fragment) == 0 {
		return nil
	}
	if !json.Valid(fragment) {
		return fmt.Errorf("%s is not valid JSON", col)
	}
	if err := key.ValidateSchema(fragment); err != nil {
		return fmt.Errorf("%s: %w", col, err)
	}
	return nil
}

// CreateEventType inserts a custom (official=false) event type and audits it. The
// name must be a valid canonical key and the payload_schema must be well-formed
// JSON. A duplicate name is ErrEventTypeExists.
func (p *PG) CreateEventType(ctx context.Context, actorID string, spec EventTypeSpec) (*EventType, error) {
	if err := ValidateName("event_type", spec.Name); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEventTypeInvalid, err)
	}
	if err := checkSchemaFragment(spec.PayloadSchema, "payload_schema"); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEventTypeInvalid, err)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin create event type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Cross-registry uniqueness. The ingest registries (property_type, metric_type,
	// event_type) are separate tables with separate uniqueness, so nothing at the
	// schema level stops a name landing in two of them, and such a name resolves to
	// NOTHING at ingest (the snapshot refuses it rather than picking a winner).
	// Refusing at create is the only place the operator can be told, while they
	// still have the name in hand.
	var clash bool
	if err := tx.QueryRow(ctx,
		`select exists (select 1 from property_type where name = $1)
		     or exists (select 1 from metric_type where name = $1)`, spec.Name).Scan(&clash); err != nil {
		return nil, fmt.Errorf("storage: check registry clash for %q: %w", spec.Name, err)
	}
	if clash {
		return nil, ErrEventTypeExists
	}

	// The insert returns the generated id so the audit row can key on the primary
	// key, which survives a later rename, rather than on the name.
	var etID string
	if err := tx.QueryRow(ctx,
		`insert into event_type (name, label, description, payload_schema, official)
		 values ($1, $2, $3, $4, false)
		 returning id`,
		spec.Name, spec.Label, spec.Description, schemaArg(spec.PayloadSchema)).Scan(&etID); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEventTypeExists
		}
		return nil, fmt.Errorf("storage: insert event type %q: %w", spec.Name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "create", "event_type", etID, nil, spec); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit create event type: %w", err)
	}
	return p.GetEventType(ctx, spec.Name)
}

// UpdateEventType patches a custom event type's mutable fields (nil unchanged) and
// audits it. Official event types are read-only; an unknown name is
// ErrEventTypeNotFound.
func (p *PG) UpdateEventType(ctx context.Context, actorID, name string, patch EventTypePatch) (*EventType, error) {
	if err := checkSchemaFragment(patch.PayloadSchema, "payload_schema"); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEventTypeInvalid, err)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin update event type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardEventTypeMutable(ctx, tx, name); err != nil {
		return nil, err
	}
	before, err := registryAuditImage(ctx, tx, "event_type", name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrEventTypeNotFound
		}
		return nil, fmt.Errorf("storage: audit image event_type %q: %w", name, err)
	}
	if _, err := tx.Exec(ctx, `
		update event_type set
			label   = coalesce($2, label),
			description    = coalesce($3, description),
			payload_schema = coalesce($4, payload_schema)
		where name = $1`,
		name, patch.Label, patch.Description, schemaArg(patch.PayloadSchema)); err != nil {
		return nil, fmt.Errorf("storage: update event type %q: %w", name, err)
	}
	var et EventType
	if err := tx.QueryRow(ctx,
		`select id, name, label, description, payload_schema, official from event_type where name = $1`,
		name).Scan(&et.ID, &et.Name, &et.Label, &et.Description, &et.PayloadSchema, &et.Official); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrEventTypeNotFound
		}
		return nil, fmt.Errorf("storage: reload event type %q: %w", name, err)
	}
	after, err := registryAuditImage(ctx, tx, "event_type", name)
	if err != nil {
		return nil, fmt.Errorf("storage: audit image event_type %q: %w", name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "update", "event_type", et.ID, before, after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit update event type: %w", err)
	}
	return &et, nil
}

// DeleteEventType removes a custom event type and audits it. Official event types
// are read-only.
func (p *PG) DeleteEventType(ctx context.Context, actorID, name string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin delete event type: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := guardEventTypeMutable(ctx, tx, name); err != nil {
		return err
	}
	before, err := registryAuditImage(ctx, tx, "event_type", name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEventTypeNotFound
		}
		return fmt.Errorf("storage: audit image event_type %q: %w", name, err)
	}
	if _, err := tx.Exec(ctx, `delete from event_type where name = $1`, name); err != nil {
		return fmt.Errorf("storage: delete event type %q: %w", name, err)
	}
	if err := writeAuditRes(ctx, tx, actorID, "delete", "event_type", auditImageID(before), before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit delete event type: %w", err)
	}
	return nil
}
