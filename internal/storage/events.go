package storage

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// EventWrite is one occurrence to persist. It shares the owner-arc shape of a
// sample (OwnerKind picks the arc column, OwnerID is the estate address). Key is
// the event_type name. Origin is how the occurrence arrived (caught/caused/derived/
// scheduled); empty defaults to caught (the natively-published path). Message is the
// occurrence's human-readable line; Attributes carries its structured payload (json),
// nil when absent. It is NOT a log line: raw log text lands on the log_line lane
// instead (ADR-0066), and only a derivation rule turns one into an event.
type EventWrite struct {
	OwnerKind  string
	OwnerID    string
	Key        string
	Instance   string
	Origin     string
	Message    string
	Attributes []byte
	Source     string
	TS         time.Time
}

// Event is a stored occurrence row (read side).
type Event struct {
	ID          int64
	TS          time.Time
	OwnerKind   string
	Key         string
	EventTypeID string
	Instance    string
	Origin      string
	Message     string
	Attributes  []byte
	Provenance  string
	Source      string
}

// InsertEvents writes observed occurrence rows in one transaction. Each row sets
// exactly its owner arc column (the CHECK enforces the rest) and provenance
// observed. Callers apply reject-not-project (collection.Registry) before calling;
// this is the durable write. Mirrors InsertMetricSamples.
func (p *PG) InsertEvents(ctx context.Context, evs []EventWrite) error {
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
		origin := ev.Origin
		if origin == "" {
			origin = "caught"
		}
		sql := fmt.Sprintf(`insert into event (ts, owner_kind, %s, event_type_id, instance, origin, message, attributes, provenance, source)
			values ($1, $2, $3, (select id from event_type where name = $4), $5, $6, $7, $8, 'observed', $9)`, col)
		// The arc points at the primary key, so the owner reference resolves to a
		// uuid before it is stored. A node still stores its name until the
		// collection tier converts.
		arc, err := p.ownerArcValue(ctx, tx, ev.OwnerKind, ev.OwnerID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, sql, ts, ev.OwnerKind, arc, ev.Key, ev.Instance, origin, ev.Message, attrs, ev.Source); err != nil {
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
// componentRef is resolved once (name or uuid, ADR-0062); an unknown
// component folds into the same nil-no-error empty result the old inline
// subquery's silent no-match gave (see ListComponentLogs).
func (p *PG) ListComponentEvents(ctx context.Context, componentRef string, since time.Time, limit int) ([]Event, error) {
	c, err := scopedByName(ctx, p.pool, componentConfig, componentRef)
	if errors.Is(err, ErrComponentNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `
		select id, ts, owner_kind,
			(select et.name from event_type et where et.id = event.event_type_id), event.event_type_id, instance, origin, message, attributes, provenance, source
		from event
		where component_id = $1::uuid and ts >= $2
		order by ts desc
		limit $3`, c.ID, since, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: list events %s: %w", componentRef, err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TS, &e.OwnerKind, &e.Key, &e.EventTypeID, &e.Instance, &e.Origin, &e.Message, &e.Attributes, &e.Provenance, &e.Source); err != nil {
			return nil, fmt.Errorf("storage: scan event %s: %w", componentRef, err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate events %s: %w", componentRef, err)
	}
	return out, nil
}
