package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// StateDatapointEvent is one observed state verdict to persist. OwnerKind picks
// the arc; OwnerID is the estate address (component/system/location/node name);
// Instance discriminates many verdicts of one key on one owner (the interface
// name for interface.reachable). Value is the categorical text (up/down), the
// mirror of MetricDatapointEvent but with a value domain, not a number.
type StateDatapointEvent struct {
	OwnerKind string
	OwnerID   string
	Key       string
	Instance  string
	Value     string
	Source    string
	TS        time.Time
}

// StateDatapoint is a stored observed/derived state row (read side).
type StateDatapoint struct {
	TS         time.Time
	OwnerKind  string
	Key        string
	Instance   string
	Value      string
	Provenance string
	Source     string
}

// InsertStateDatapoints writes observed state rows in one transaction. Each row
// sets exactly its owner arc column (the CHECK enforces the rest) and provenance
// observed. Callers apply reject-not-project (collection.Registry) and the
// transition-only guard before calling; this is the durable write.
func (p *PG) InsertStateDatapoints(ctx context.Context, evs []StateDatapointEvent) error {
	if len(evs) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin insert state datapoints: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, ev := range evs {
		col, err := ownerColumn(ev.OwnerKind)
		if err != nil {
			return fmt.Errorf("storage: state datapoint %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
		ts := ev.TS
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		sql := fmt.Sprintf(`insert into state_datapoint (ts, owner_kind, %s, key, instance, value, provenance, source)
			values ($1, $2, %s, $4, $5, $6, 'observed', $7)`, col, ownerArcExprN(ev.OwnerKind, 3))
		if _, err := tx.Exec(ctx, sql, ts, ev.OwnerKind, ev.OwnerID, ev.Key, ev.Instance, ev.Value, ev.Source); err != nil {
			return fmt.Errorf("storage: insert state datapoint %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit insert state datapoints: %w", err)
	}
	return nil
}

// LatestState returns the most recent state row for a component series (key +
// instance), or nil if none. It backs the ingest-side transition guard (skip a
// write whose value equals the latest stored value) and the reachability panel.
//
// ts orders it, because an observed series carries the OBSERVATION time and a
// late arrival must not displace a newer reading. id breaks the tie: a poll cycle
// stamping several rows in the same instant would otherwise resolve to an
// arbitrary one, and the transition guard would compare against a row that is not
// the current value.
func (p *PG) LatestState(ctx context.Context, componentName, key, instance string) (*StateDatapoint, error) {
	var dp StateDatapoint
	err := p.pool.QueryRow(ctx, `
		select ts, owner_kind, key, instance, value, provenance, source
		from state_datapoint
		where component_id = (select id from component where name = $1) and key = $2 and instance = $3
		order by ts desc, id desc
		limit 1`, componentName, key, instance).Scan(&dp.TS, &dp.OwnerKind, &dp.Key, &dp.Instance, &dp.Value, &dp.Provenance, &dp.Source)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("storage: latest state %s/%s[%s]: %w", componentName, key, instance, err)
	}
	return &dp, nil
}

// StateTransitions returns a component series' state rows at or after since,
// ordered oldest-first: the ordered flip sequence the availability strip reads
// (each row is one transition, since the write path is transition-only). A zero
// since returns the whole series.
func (p *PG) StateTransitions(ctx context.Context, componentName, key, instance string, since time.Time) ([]StateDatapoint, error) {
	rows, err := p.pool.Query(ctx, `
		select ts, owner_kind, key, instance, value, provenance, source
		from state_datapoint
		where component_id = (select id from component where name = $1) and key = $2 and instance = $3 and ts >= $4
		order by ts asc`, componentName, key, instance, since)
	if err != nil {
		return nil, fmt.Errorf("storage: state transitions %s/%s[%s]: %w", componentName, key, instance, err)
	}
	defer rows.Close()
	var out []StateDatapoint
	for rows.Next() {
		var dp StateDatapoint
		if err := rows.Scan(&dp.TS, &dp.OwnerKind, &dp.Key, &dp.Instance, &dp.Value, &dp.Provenance, &dp.Source); err != nil {
			return nil, fmt.Errorf("storage: scan state transition %s/%s[%s]: %w", componentName, key, instance, err)
		}
		out = append(out, dp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate state transitions %s/%s[%s]: %w", componentName, key, instance, err)
	}
	return out, nil
}
