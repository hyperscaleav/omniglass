package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// EventWrite is one occurrence to persist. It shares the owner-arc shape of a
// sample (OwnerKind picks the arc column, OwnerID is the fleet address). Key is
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
	ID        int64
	TS        time.Time
	OwnerKind string
	// Owner is the owning row's NAME, populated by the system-scoped read
	// (#793), where one list mixes owners; the single-owner reads leave it
	// empty because the caller already knows.
	Owner       string
	Key         string
	EventTypeID string
	Instance    string
	Origin      string
	Message     string
	Attributes  []byte
	Provenance  string
	Source      string
	// DerivedByRule names the event rule that derived the occurrence (#949),
	// by the name it had when the event was written, as the row keeps it in
	// source_rule: a historical record that a rename (#977) does not
	// rewrite, and that outlives the rule; "" for an occurrence no rule
	// derived. AlarmID is the alarm a rule's fire event
	// opened or its clear event resolved, nil for most events.
	DerivedByRule string
	// DerivedByRuleID is the deriving rule's uuid while the rule exists; nil
	// once it is deleted (the name above stays), and for any other event.
	DerivedByRuleID *string
	AlarmID         *string
	// OwnerID is the owning row's uuid, populated beside Owner by the
	// system-scoped read, where names repeat across one list's owners.
	OwnerID string
}

// eventLineageCols reads an event's rule and alarm, the trailing columns
// both event reads scan into Event.DerivedByRule and Event.AlarmID.
const eventLineageCols = `case when event.origin = 'derived' then event.source_rule end, event.derived_by_rule_id::text, event.alarm_id::text`

// InsertEvents writes observed occurrence rows in one transaction, for w: a
// node's lane (NodeWriter) or the push lane (PushWriter). Each row sets
// exactly its owner arc column (the CHECK enforces the rest) and provenance
// observed. Callers apply reject-not-project (collection.Registry) before
// calling; this is the durable write, refusing the whole batch when an event
// type's writer class does not admit w (ADR-0161: collection-failed is the
// node's alone). Mirrors InsertMetricSamples.
func (p *PG) InsertEvents(ctx context.Context, w KeyWriter, evs []EventWrite) error {
	if len(evs) == 0 {
		return nil
	}
	return p.sampleWrite(ctx, "insert events", func(ctx context.Context, tx pgx.Tx) error { return p.writeEvents(ctx, tx, w, evs) })
}

// eventSet is one writer's events in a multi-set write (metricSet).
type eventSet struct {
	w   KeyWriter
	evs []EventWrite
}

// writeEvents is the caught-occurrence write path for one writer, in the
// caller's transaction (writeEventSets).
func (p *PG) writeEvents(ctx context.Context, tx pgx.Tx, w KeyWriter, evs []EventWrite) error {
	return p.writeEventSets(ctx, tx, []eventSet{{w: w, evs: evs}})
}

// writeEventSets is the caught-occurrence write path, in the caller's
// transaction: InsertEvents' own, or an ingest batch's (LandBatch), each set
// held to its event types' writer class under its own writer before anything
// lands, every set sharing one row identity. The rows land in one statement,
// in their order, however many owners and groups carry them (#1582).
func (p *PG) writeEventSets(ctx context.Context, tx pgx.Tx, sets []eventSet) error {
	w, err := sharedWriter(writeEvent, sets, func(s eventSet) KeyWriter { return s.w })
	if err != nil {
		return err
	}
	// The arc points at the primary key, so each owner reference resolves to a
	// uuid before it is stored, and before the writer check holds the row to
	// it.
	var evs []EventWrite
	var arcs []string
	for _, set := range sets {
		setEvs := cleanEventWrites(set.evs)
		rows := make([]keyedWrite, len(setEvs))
		for i, ev := range setEvs {
			if _, err := ownerColumn(ev.OwnerKind); err != nil {
				return fmt.Errorf("storage: event %s/%s: %w", ev.OwnerID, ev.Key, err)
			}
			arc, err := p.ownerArcValue(ctx, tx, ev.OwnerKind, ev.OwnerID)
			if err != nil {
				return err
			}
			arcs = append(arcs, arc)
			rows[i] = keyedWrite{key: ev.Key, instance: ev.Instance, ownerKind: ev.OwnerKind, owner: arc}
		}
		if err := admitWrites(ctx, tx, writeEvent, set.w, rows); err != nil {
			return err
		}
		evs = append(evs, setEvs...)
	}
	if len(evs) == 0 {
		return nil
	}
	clock := sampleClock{q: tx}
	ts := make([]time.Time, len(evs))
	kinds := make([]string, len(evs))
	keys := make([]string, len(evs))
	instances := make([]string, len(evs))
	origins := make([]string, len(evs))
	messages := make([]string, len(evs))
	// attributes is jsonb: the raw JSON travels as text (nil stays SQL NULL)
	// so pgx does not encode a []byte as bytea, and text that is no JSON
	// fails the statement as a data exception (IsRefusedWrite).
	attrs := make([]*string, len(evs))
	sources := make([]string, len(evs))
	for i, ev := range evs {
		at := ev.TS
		if at.IsZero() {
			at = time.Now().UTC()
		}
		if ts[i], err = clock.cap(ctx, ev.Key, at); err != nil {
			return err
		}
		if len(ev.Attributes) > 0 {
			a := string(ev.Attributes)
			attrs[i] = &a
		}
		origins[i] = ev.Origin
		if origins[i] == "" {
			origins[i] = "caught"
		}
		kinds[i], keys[i], instances[i], messages[i], sources[i] = ev.OwnerKind, ev.Key, ev.Instance, ev.Message, ev.Source
	}
	if _, err := tx.Exec(ctx, `insert into event (ts, owner_kind, component_id, system_id, location_id, node_id, event_type_id, instance, origin, message, attributes, provenance, source, source_conduit_id)
		select u.ts, u.kind,
			case when u.kind = 'component' then u.owner::uuid end,
			case when u.kind = 'system' then u.owner::uuid end,
			case when u.kind = 'location' then u.owner::uuid end,
			case when u.kind = 'node' then u.owner::uuid end,
			et.id, u.instance, u.origin, u.message, u.attrs::jsonb, 'observed', u.source, $10::uuid
		from unnest($1::timestamptz[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[], $9::text[])
			with ordinality as u(ts, kind, owner, key, instance, origin, message, attrs, source, ord)
		left join event_type_in_effect() et on et.name = u.key
		order by u.ord`,
		ts, kinds, arcs, keys, instances, origins, messages, attrs, sources, w.sourceConduit()); err != nil {
		named := make([]string, 0, len(evs))
		for _, ev := range evs {
			named = append(named, ev.OwnerID+"/"+ev.Key)
		}
		return fmt.Errorf("storage: insert events %s: %w", strings.Join(named, ", "), err)
	}
	return nil
}

// ListComponentEvents returns a component's recent occurrences, newest first,
// bounded by window and limit. The window is counted back from the DATABASE's
// clock (tsSince, #719); a zero window is every occurrence there is. Read helper for the component event log panel.
// componentRef is resolved once (name or uuid, ADR-0062); an unknown
// component folds into the same nil-no-error empty result the old inline
// subquery's silent no-match gave (see ListComponentLogs).
func (p *PG) ListComponentEvents(ctx context.Context, componentRef string, window time.Duration, limit int) ([]Event, error) {
	c, err := scopedByName(ctx, p.pool, componentConfig, componentRef)
	if errors.Is(err, ErrComponentNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	bound, args := tsSince("ts", window, c.ID, limit)
	rows, err := p.pool.Query(ctx, fmt.Sprintf(`
		select id, ts, owner_kind,
			(select et.name from event_type et where et.id = event.event_type_id), event.event_type_id, instance, origin, message, attributes, provenance, `+sourceRendered("event")+`,
			`+eventLineageCols+`
		from event
		where component_id = $1::uuid and %s
		order by ts desc
		limit $2`, bound), args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list events %s: %w", componentRef, err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var e Event
		var rule *string
		if err := rows.Scan(&e.ID, &e.TS, &e.OwnerKind, &e.Key, &e.EventTypeID, &e.Instance, &e.Origin, &e.Message, &e.Attributes, &e.Provenance, &e.Source,
			&rule, &e.DerivedByRuleID, &e.AlarmID); err != nil {
			return nil, fmt.Errorf("storage: scan event %s: %w", componentRef, err)
		}
		if rule != nil {
			e.DerivedByRule = *rule
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate events %s: %w", componentRef, err)
	}
	return out, nil
}

// ListSystemEvents returns the system's own events AND its members', one list
// newest first (#793): the workspace's Events tab. The caller resolves the
// system in scope first (the non-disclosing-404 pattern every lane read
// shares); membership is the system_member join, so a component shared with
// another system appears in both systems' lists, which is what sharing means.
func (p *PG) ListSystemEvents(ctx context.Context, systemID string, window time.Duration, limit int) ([]Event, error) {
	bound, args := tsSince("ts", window, systemID, limit)
	rows, err := p.pool.Query(ctx, fmt.Sprintf(`
		select event.id, ts, owner_kind,
			coalesce(
				(select s.name from system s where s.id = event.system_id),
				(select c.name from component c where c.id = event.component_id), ''),
			coalesce(event.system_id, event.component_id)::text,
			(select et.name from event_type et where et.id = event.event_type_id), event.event_type_id,
			instance, origin, message, attributes, provenance, `+sourceRendered("event")+`, `+eventLineageCols+`
		from event
		where (event.system_id = $1::uuid
		   or event.component_id in (select m.component_id from system_member m where m.system_id = $1::uuid))
		  and %s
		order by ts desc
		limit $2`, bound), args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list system events %s: %w", systemID, err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var key, typeID, rule *string
		var ownerID *string
		if err := rows.Scan(&e.ID, &e.TS, &e.OwnerKind, &e.Owner, &ownerID, &key, &typeID, &e.Instance, &e.Origin, &e.Message, &e.Attributes, &e.Provenance, &e.Source,
			&rule, &e.DerivedByRuleID, &e.AlarmID); err != nil {
			return nil, fmt.Errorf("storage: scan system event: %w", err)
		}
		if key != nil {
			e.Key = *key
		}
		if typeID != nil {
			e.EventTypeID = *typeID
		}
		if rule != nil {
			e.DerivedByRule = *rule
		}
		if ownerID != nil {
			e.OwnerID = *ownerID
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
