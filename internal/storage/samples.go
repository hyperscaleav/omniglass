package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// MetricSampleWrite is one observed metric to persist. OwnerKind picks the
// arc; OwnerID is the estate address (component/system/location/node name).
// Instance discriminates many values of one key on one owner (default "").
type MetricSampleWrite struct {
	OwnerKind string
	OwnerID   string
	Key       string
	Instance  string
	Value     float64
	Source    string
	TS        time.Time
}

// MetricSample is a stored observed/derived metric row (read side).
type MetricSample struct {
	TS         time.Time
	OwnerKind  string
	Key        string
	Instance   string
	Value      float64
	Provenance string
	Source     string
}

// ErrUnknownOwnerKind guards the owner-arc column mapping.
var ErrUnknownOwnerKind = errors.New("storage: unknown sample owner_kind")

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

// InsertMetricSamples writes observed metric rows in one transaction. Each
// row sets exactly its owner arc column (the CHECK enforces the rest) and
// provenance observed. Callers apply reject-not-project (collection.Registry)
// before calling; this is the durable write.
func (p *PG) InsertMetricSamples(ctx context.Context, evs []MetricSampleWrite) error {
	if len(evs) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin insert samples: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, ev := range evs {
		col, err := ownerColumn(ev.OwnerKind)
		if err != nil {
			return fmt.Errorf("storage: sample %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
		ts := ev.TS
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		// The arc stores the owner's primary key. Resolving here rather than in the
		// insert means an unknown owner is a named error instead of a NULL that
		// trips the arc CHECK opaquely.
		arc, err := p.ownerArcValue(ctx, tx, ev.OwnerKind, ev.OwnerID)
		if err != nil {
			return fmt.Errorf("storage: sample %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
		sql := fmt.Sprintf(`insert into metric (ts, owner_kind, %s, metric_type_id, instance, value, provenance, source)
			values ($1, $2, $3, (select id from metric_type where name = $4), $5, $6, 'observed', $7)`, col)
		if _, err := tx.Exec(ctx, sql, ts, ev.OwnerKind, arc, ev.Key, ev.Instance, ev.Value, ev.Source); err != nil {
			return fmt.Errorf("storage: insert sample %s/%s: %w", ev.OwnerID, ev.Key, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit insert samples: %w", err)
	}
	return nil
}

// LatestMetric returns the most recent metric row for a component and key, or
// nil if none. Read helper for the component reachability panel and tests.
// componentRef is resolved once (name or uuid, ADR-0062) rather than left for
// the query to re-derive an id from a name a second row might now share (#627).
// An unknown component folds into the same nil-no-error "nothing yet" result
// the old inline subquery gave (it resolved to no id, which matched no metric
// row): this read backs the ingest-side transition guard
// (bus.dedupeProperties's LatestProperty sibling), where any error leaves a
// telemetry message unacked for redelivery, so an unrecognized owner must stay
// silent here rather than start rejecting live ingest traffic that used to be
// silently accepted as "no prior value". ErrAmbiguousName, which could not
// have occurred before scoped uniqueness exists, is not folded in: it is new
// information a caller that reaches this world needs.
func (p *PG) LatestMetric(ctx context.Context, componentRef, key string) (*MetricSample, error) {
	c, err := scopedByName(ctx, p.pool, componentConfig, componentRef)
	if errors.Is(err, ErrComponentNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var dp MetricSample
	err = p.pool.QueryRow(ctx, `
		select ts, owner_kind,
			(select m.name from metric_type m where m.id = metric.metric_type_id), instance, value, provenance, source
		from metric
		where component_id = $1::uuid
		  and metric_type_id = (select id from metric_type where name = $2)
		order by ts desc
		limit 1`, c.ID, key).Scan(&dp.TS, &dp.OwnerKind, &dp.Key, &dp.Instance, &dp.Value, &dp.Provenance, &dp.Source)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("storage: latest metric %s/%s: %w", componentRef, key, err)
	}
	return &dp, nil
}

// LatestMetricInstance returns the most recent metric row for a component series
// (key + instance), or nil if none. The reachability panel's probe metrics
// (tcp-open, icmp-reachable, and their rtt/connect_time companions) are
// per-interface instance, so the layer signals must resolve one interface's
// latest value, not the newest across every interface as LatestMetric does.
func (p *PG) LatestMetricInstance(ctx context.Context, componentRef, key, instance string) (*MetricSample, error) {
	// See LatestMetric: an unknown component folds into nil-no-error, matching
	// the old inline subquery's silent no-match.
	c, err := scopedByName(ctx, p.pool, componentConfig, componentRef)
	if errors.Is(err, ErrComponentNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var dp MetricSample
	err = p.pool.QueryRow(ctx, `
		select ts, owner_kind,
			(select m.name from metric_type m where m.id = metric.metric_type_id), instance, value, provenance, source
		from metric
		where component_id = $1::uuid
		  and metric_type_id = (select id from metric_type where name = $2) and instance = $3
		order by ts desc
		limit 1`, c.ID, key, instance).Scan(&dp.TS, &dp.OwnerKind, &dp.Key, &dp.Instance, &dp.Value, &dp.Provenance, &dp.Source)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("storage: latest metric %s/%s[%s]: %w", componentRef, key, instance, err)
	}
	return &dp, nil
}
