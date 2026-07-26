package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// EventType is a registered occurrence key: the "happen" half of the telemetry
// model, the twin of PropertyType (the "know" half). An event is a different shape
// from a datapoint (an occurrence, not a value), so it gets its own registry.
// PayloadSchema is an optional jsonb schema for the occurrence payload. Official
// marks a seed-owned, read-only type. An event type is addressed by its name.
type EventType struct {
	ID            string
	Name          string
	DisplayName   string
	Description   string
	PayloadSchema []byte
	Official      bool
}

// ErrEventTypeNotFound is the miss for a name that resolves to no event type.
var ErrEventTypeNotFound = errors.New("storage: event type not found")

// UpsertEventType installs an official event type, authoritative on conflict (the
// boot-seed bucket): an operator's custom event types (official=false) are keyed by
// a distinct name and untouched. Mirrors UpsertPropertyType.
func (p *PG) UpsertEventType(ctx context.Context, et EventType) error {
	var schema any
	if len(et.PayloadSchema) > 0 {
		schema = string(et.PayloadSchema)
	}
	_, err := p.pool.Exec(ctx, `
		insert into event_type (name, display_name, description, payload_schema, official)
		values ($1, $2, $3, $4, $5)
		on conflict (name) do update set
			display_name = excluded.display_name, description = excluded.description,
			payload_schema = excluded.payload_schema, official = excluded.official`,
		et.Name, et.DisplayName, et.Description, schema, et.Official)
	if err != nil {
		return fmt.Errorf("storage: upsert event type %q: %w", et.Name, err)
	}
	return nil
}

// ListEventTypes returns every registered event type (official and custom). No
// scope.Set: the registry is estate-wide reference data.
func (p *PG) ListEventTypes(ctx context.Context) ([]EventType, error) {
	rows, err := p.pool.Query(ctx, `select id, name, coalesce(display_name, ''), description, payload_schema, official from event_type`)
	if err != nil {
		return nil, fmt.Errorf("storage: list event types: %w", err)
	}
	defer rows.Close()
	var out []EventType
	for rows.Next() {
		var et EventType
		if err := rows.Scan(&et.ID, &et.Name, &et.DisplayName, &et.Description, &et.PayloadSchema, &et.Official); err != nil {
			return nil, fmt.Errorf("storage: scan event type: %w", err)
		}
		out = append(out, et)
	}
	return out, rows.Err()
}

// GetEventType resolves one event type by name, ErrEventTypeNotFound when absent.
func (p *PG) GetEventType(ctx context.Context, name string) (*EventType, error) {
	var et EventType
	err := p.pool.QueryRow(ctx,
		`select id, name, coalesce(display_name, ''), description, payload_schema, official from event_type where name = $1`,
		name).Scan(&et.ID, &et.Name, &et.DisplayName, &et.Description, &et.PayloadSchema, &et.Official)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEventTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get event type %q: %w", name, err)
	}
	return &et, nil
}
