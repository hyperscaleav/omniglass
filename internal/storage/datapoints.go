package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// MetricDatapointEvent is one observed metric to persist. OwnerKind picks the
// arc; OwnerID is the estate address (component/system/location/node name).
// Instance discriminates many values of one key on one owner (default "").
type MetricDatapointEvent struct {
	OwnerKind string
	OwnerID   string
	Key       string
	Instance  string
	Value     float64
	Source    string
	TS        time.Time
}

// MetricDatapoint is a stored observed/derived metric row (read side).
type MetricDatapoint struct {
	TS         time.Time
	OwnerKind  string
	Key        string
	Instance   string
	Value      float64
	Provenance string
	Source     string
}

// ErrUnknownOwnerKind guards the owner-arc column mapping.
var ErrUnknownOwnerKind = errors.New("storage: unknown datapoint owner_kind")

// ownerColumn maps an owner kind to its arc column, so a bad kind fails in Go
// (an explicit error) rather than as a NULL that trips the CHECK opaquely.
func ownerColumn(kind string) (string, error) {
	switch kind {
	case "component":
		return "component_id", nil
	case "system":
		return "system_id", nil
	case "location":
		return "location_id", nil
	case "node":
		return "node_id", nil
	default:
		return "", ErrUnknownOwnerKind
	}
}

// InsertMetricDatapoints writes observed metric rows in one transaction. Each
// row sets exactly its owner arc column (the CHECK enforces the rest) and
// provenance observed. Callers apply reject-not-project (collection.Registry)
// before calling; this is the durable write.
func (p *PG) InsertMetricDatapoints(ctx context.Context, evs []MetricDatapointEvent) error {
	if len(evs) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin insert datapoints: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, ev := range evs {
		col, err := ownerColumn(ev.OwnerKind)
		if err != nil {
			return fmt.Errorf("storage: datapoint %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
		ts := ev.TS
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		sql := fmt.Sprintf(`insert into metric_datapoint (ts, owner_kind, %s, key, instance, value, provenance, source)
			values ($1, $2, $3, $4, $5, $6, 'observed', $7)`, col)
		if _, err := tx.Exec(ctx, sql, ts, ev.OwnerKind, ev.OwnerID, ev.Key, ev.Instance, ev.Value, ev.Source); err != nil {
			return fmt.Errorf("storage: insert datapoint %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit insert datapoints: %w", err)
	}
	return nil
}

// LatestMetric returns the most recent metric row for a component and key, or
// nil if none. Read helper for the component reachability panel and tests.
func (p *PG) LatestMetric(ctx context.Context, componentName, key string) (*MetricDatapoint, error) {
	var dp MetricDatapoint
	err := p.pool.QueryRow(ctx, `
		select ts, owner_kind, key, instance, value, provenance, source
		from metric_datapoint
		where component_id = $1 and key = $2
		order by ts desc
		limit 1`, componentName, key).Scan(&dp.TS, &dp.OwnerKind, &dp.Key, &dp.Instance, &dp.Value, &dp.Provenance, &dp.Source)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("storage: latest metric %s/%s: %w", componentName, key, err)
	}
	return &dp, nil
}

// LatestMetricInstance returns the most recent metric row for a component series
// (key + instance), or nil if none. The reachability panel's probe metrics
// (tcp.open, icmp.reachable, and their rtt/connect_time companions) are
// per-interface instance, so the layer signals must resolve one interface's
// latest value, not the newest across every interface as LatestMetric does.
func (p *PG) LatestMetricInstance(ctx context.Context, componentName, key, instance string) (*MetricDatapoint, error) {
	var dp MetricDatapoint
	err := p.pool.QueryRow(ctx, `
		select ts, owner_kind, key, instance, value, provenance, source
		from metric_datapoint
		where component_id = $1 and key = $2 and instance = $3
		order by ts desc
		limit 1`, componentName, key, instance).Scan(&dp.TS, &dp.OwnerKind, &dp.Key, &dp.Instance, &dp.Value, &dp.Provenance, &dp.Source)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("storage: latest metric %s/%s[%s]: %w", componentName, key, instance, err)
	}
	return &dp, nil
}
