package storage

import (
	"context"
	"fmt"
	"time"
)

// EventOccurrence is one observed log-kind occurrence to persist. It shares the
// owner-arc shape of a datapoint (OwnerKind picks the arc column, OwnerID is the
// estate address). Message carries a log's text (string_value); Attributes carries
// its structured payload (json_value), nil when absent.
type EventOccurrence struct {
	OwnerKind  string
	OwnerID    string
	Key        string
	Instance   string
	Message    string
	Attributes []byte
	Source     string
	TS         time.Time
}

// Event is a stored occurrence row (read side).
type Event struct {
	ID         int64
	TS         time.Time
	OwnerKind  string
	Key        string
	Instance   string
	Message    string
	Attributes []byte
	Provenance string
	Source     string
}

// InsertEvents writes observed occurrence rows in one transaction. Each row sets
// exactly its owner arc column (the CHECK enforces the rest) and provenance
// observed. Callers apply reject-not-project (collection.Registry) before calling;
// this is the durable write. Mirrors InsertMetricDatapoints.
func (p *PG) InsertEvents(ctx context.Context, evs []EventOccurrence) error {
	if len(evs) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin insert events: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, ev := range evs {
		col, err := ownerColumn(ev.OwnerKind)
		if err != nil {
			return fmt.Errorf("storage: event %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
		ts := ev.TS
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		// attributes is jsonb: pass the raw JSON as text (nil stays SQL NULL) so
		// pgx does not encode a []byte as bytea.
		var attrs any
		if len(ev.Attributes) > 0 {
			attrs = string(ev.Attributes)
		}
		sql := fmt.Sprintf(`insert into event (ts, owner_kind, %s, property_id, instance, message, attributes, provenance, source)
			values ($1, $2, $3, (select id from property where name = $4), $5, $6, $7, 'observed', $8)`, col)
		// The arc points at the primary key, so the owner reference resolves to a
		// uuid before it is stored. A node still stores its name until the
		// collection tier converts.
		arc, err := p.ownerArcValue(ctx, tx, ev.OwnerKind, ev.OwnerID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, sql, ts, ev.OwnerKind, arc, ev.Key, ev.Instance, ev.Message, attrs, ev.Source); err != nil {
			return fmt.Errorf("storage: insert event %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit insert events: %w", err)
	}
	return nil
}

// ListComponentEvents returns a component's recent occurrences, newest first,
// bounded by since and limit. Read helper for the component event log panel.
func (p *PG) ListComponentEvents(ctx context.Context, componentName string, since time.Time, limit int) ([]Event, error) {
	rows, err := p.pool.Query(ctx, `
		select id, ts, owner_kind,
			(select p.name from property p where p.id = event.property_id), instance, message, attributes, provenance, source
		from event
		where component_id = (select id from component where name = $1) and ts >= $2
		order by ts desc
		limit $3`, componentName, since, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: list events %s: %w", componentName, err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TS, &e.OwnerKind, &e.Key, &e.Instance, &e.Message, &e.Attributes, &e.Provenance, &e.Source); err != nil {
			return nil, fmt.Errorf("storage: scan event %s: %w", componentName, err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate events %s: %w", componentName, err)
	}
	return out, nil
}
