package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/key"
	"github.com/hyperscaleav/omniglass/internal/updatemask"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// EventType is a registered occurrence key: the "happen" half of the telemetry
// model, the twin of PropertyType (the "know" half). An event is a different shape
// from a sample (an occurrence, not a value), so it gets its own registry.
// PayloadSchema is an optional jsonb schema for the occurrence payload. Official
// marks a seed-owned, read-only type. An event type is addressed by its name.
type EventType struct {
	ID string
	// Namespace is who named the type (#318): og, local, or another.
	Namespace     string
	Name          string
	Label         string
	Description   string
	PayloadSchema []byte
	Official      bool
	// WriterClass is who may write this event (ADR-0161): node-probe for
	// collection-failed, collected otherwise (no calc rule outputs an event).
	// Resolved from the name, never stored.
	WriterClass writerclass.Class `json:"-"`
}

// Sentinels for event_type CRUD. Official (seed-owned) event types are read-only.
var (
	ErrEventTypeNotFound = errors.New("storage: event type not found")
	ErrEventTypeExists   = errors.New("storage: event type name already exists")
	ErrEventTypeOfficial = errors.New("storage: official event type is read-only")
	ErrEventTypeInvalid  = errors.New("storage: event type is invalid")
	ErrEventTypeInUse    = errors.New("storage: event type is still referenced")
)

// EventTypeSpec is the create input for a custom event type.
type EventTypeSpec struct {
	Name          string
	Label         string
	Description   string
	PayloadSchema []byte
}

// The event_type fields a PATCH can write, spelled as the wire body spells them,
// because an update_mask names wire fields (ADR-0091).
const (
	EventTypeFieldLabel         = "label"
	EventTypeFieldDescription   = "description"
	EventTypeFieldPayloadSchema = "payload_schema"
)

// EventTypePatchFields is every field an event_type PATCH lets a write set, in
// the order a refusal lists them.
var EventTypePatchFields = []string{EventTypeFieldLabel, EventTypeFieldDescription, EventTypeFieldPayloadSchema}

// EventTypePatch carries the mutable fields of an event-type update; a nil field is
// unchanged. The name is fixed at create. PayloadSchema replaces wholesale when
// non-nil, and, being a nullable object with no empty value to overload, clears
// by being named in Write with no bytes (#1189, ADR-0106's rule).
type EventTypePatch struct {
	Label         *string
	Description   *string
	PayloadSchema []byte
	// Write is the resolved update_mask: the fields this write writes. NIL means
	// the caller sent no mask, read as the implied mask of the fields the patch
	// populated. A NON-NIL empty set names no fields and writes nothing.
	Write updatemask.Fields
}

// Populated is the patch's implied mask: the fields it carries a value for.
// Exported because the API resolves the wire mask against the same patch it is
// about to write, so "populated" is defined once for both layers.
func (p EventTypePatch) Populated() updatemask.Fields {
	f := updatemask.Fields{}
	populatedIf(f, EventTypeFieldLabel, p.Label != nil)
	populatedIf(f, EventTypeFieldDescription, p.Description != nil)
	populatedIf(f, EventTypeFieldPayloadSchema, len(p.PayloadSchema) > 0)
	return f
}

func (p EventTypePatch) writeFields() updatemask.Fields {
	if p.Write != nil {
		return p.Write
	}
	return updatemask.Implied(p.Populated())
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
		insert into event_type (namespace, name, label, description, payload_schema, official)
		values (case when $5 then 'og' else 'local' end, $1, $2, $3, $4, $5)
		on conflict (namespace, name) do update set
			label = excluded.label, description = excluded.description,
			payload_schema = excluded.payload_schema, official = excluded.official`,
		et.Name, labelOrNull(et.Label), et.Description, schema, et.Official)
	if err != nil {
		return fmt.Errorf("storage: upsert event type %q: %w", et.Name, err)
	}
	return nil
}

// ListEventTypes returns every registered event type (official and custom). No
// scope.Set: the registry is fleet-wide reference data.
func (p *PG) ListEventTypes(ctx context.Context) ([]EventType, error) {
	rows, err := p.pool.Query(ctx, eventTypeListSQL)
	if err != nil {
		return nil, fmt.Errorf("storage: list event types: %w", err)
	}
	defer rows.Close()
	var out []EventType
	for rows.Next() {
		var et EventType
		if err := rows.Scan(&et.ID, &et.Name, &et.Label, &et.Description, &et.PayloadSchema, &et.Official, &et.Namespace); err != nil {
			return nil, fmt.Errorf("storage: scan event type: %w", err)
		}
		et.WriterClass = classOf(et.Name, nil)
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
		`select id, name, coalesce(label, ''), description, payload_schema, official, namespace from event_type_in_effect() event_type where name = $1`,
		name).Scan(&et.ID, &et.Name, &et.Label, &et.Description, &et.PayloadSchema, &et.Official, &et.Namespace)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEventTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get event type %q: %w", name, err)
	}
	et.WriterClass = classOf(et.Name, nil)
	return &et, nil
}

// guardEventTypeMutable loads an event type's official flag: ErrEventTypeNotFound if
// absent, ErrEventTypeOfficial if seed-owned. Update and delete call it first.
func guardEventTypeMutable(ctx context.Context, q querier, name string) error {
	if err := RejectAddressForm("event_type", name); err != nil {
		return err
	}
	var official bool
	err := q.QueryRow(ctx, `select official from event_type_in_effect() event_type where name = $1`, name).Scan(&official)
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
	if err := reservedName(spec.Name); err != nil {
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
		`select exists (select 1 from property_type_in_effect() property_type where name = $1)
		     or exists (select 1 from metric_type_in_effect() metric_type where name = $1)`, spec.Name).Scan(&clash); err != nil {
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
		spec.Name, labelOrNull(spec.Label), spec.Description, schemaArg(spec.PayloadSchema)).Scan(&etID); err != nil {
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
	w := patch.writeFields()
	setLabel, labelVal := labelPatch(maskedPtr(w, EventTypeFieldLabel, patch.Label))
	if _, err := tx.Exec(ctx, `
		update event_type set
			label   = case when $5::boolean then $2 else label end,
			description    = coalesce($3, description),
			payload_schema = case when $6::boolean then $4::jsonb else payload_schema end
		where id = (select id from event_type_in_effect() event_type where name = $1)`,
		name, labelVal, maskedPtr(w, EventTypeFieldDescription, patch.Description), schemaArg(patch.PayloadSchema), setLabel,
		w.Has(EventTypeFieldPayloadSchema)); err != nil {
		return nil, fmt.Errorf("storage: update event type %q: %w", name, err)
	}
	var et EventType
	if err := tx.QueryRow(ctx,
		`select id, name, coalesce(label, ''), description, payload_schema, official, namespace from event_type_in_effect() event_type where name = $1`,
		name).Scan(&et.ID, &et.Name, &et.Label, &et.Description, &et.PayloadSchema, &et.Official, &et.Namespace); err != nil {
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
	if _, err := tx.Exec(ctx, `delete from event_type where id = (select id from event_type_in_effect() event_type where name = $1)`, name); err != nil {
		// Recorded events and the event rules that name the type restrict it.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrEventTypeInUse
		}
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
