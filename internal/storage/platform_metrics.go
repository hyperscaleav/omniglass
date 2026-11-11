package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// PlatformMetric is one observation Omniglass makes of one of its own parts
// (#1094, first written by #1625): a platform key's value on a platform
// component, at a time. The instance discriminates where the key does, and
// is empty otherwise. Source is empty for the process's own measure; the dev
// seed names itself when it lays down history for a capture (#1790), so a
// seeded series stays apart from the measured one.
type PlatformMetric struct {
	ComponentID string
	Key         string
	Instance    string
	Value       float64
	TS          time.Time
	Source      string
}

// RecordPlatformMetrics lands the process's own observations as the platform
// components' metric series, observed, written by the process (the platform
// writer class), so the gateway refuses a key the process does not write. It
// is the one write every server-side platform measure goes through: a node's
// check-in age today, the server's own figures and a node's command queue in
// the slices after (#1626, #1627).
//
// Each component's rows land in a transaction of their own, as
// RecordWebhookCounts does, so a refusal costs only its own rows: a component
// gone since its rows were read (a node deleted between the read and the
// write) is dropped, and every other component's rows still land. Any other
// failure is returned, after the rest have been tried.
func (p *PG) RecordPlatformMetrics(ctx context.Context, rows []PlatformMetric) error {
	byComponent := map[string][]MetricSampleWrite{}
	var order []string
	for _, r := range rows {
		if _, seen := byComponent[r.ComponentID]; !seen {
			order = append(order, r.ComponentID)
		}
		byComponent[r.ComponentID] = append(byComponent[r.ComponentID], MetricSampleWrite{OwnerKind: "component",
			OwnerID: r.ComponentID, Key: r.Key, Instance: r.Instance, Value: r.Value, Source: r.Source, TS: r.TS})
	}
	var errs []error
	for _, id := range order {
		writes := byComponent[id]
		err := p.sampleWrite(ctx, "record platform metrics", func(ctx context.Context, tx pgx.Tx) error {
			return p.writeMetricSamples(ctx, tx, gatewayWriter(writerclass.Process), writes)
		})
		if err != nil && !errors.Is(err, ErrComponentNotFound) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// NodeCheckin is one claimed node's last word (#1625): its platform
// component and the time it last spoke, its last heartbeat or, before its
// first, its claim. The check-in age is the time since.
type NodeCheckin struct {
	Node        string
	ComponentID string
	Since       time.Time
}

// NodeCheckins reads every claimed node that has a platform component, in
// name order: the check-in age timer's input. A node never claimed has not
// begun to speak, so its silence has not begun either, and is absent; a node
// with no platform component (registered before the boot seed made the
// platform) has nowhere to land its age until the next boot adopts it.
func (p *PG) NodeCheckins(ctx context.Context) ([]NodeCheckin, error) {
	rows, err := p.pool.Query(ctx, `
		select name, platform_component_id::text, coalesce(last_heartbeat_at, enrolled_at)
		from node
		where enrolled_at is not null and platform_component_id is not null
		order by name`)
	if err != nil {
		return nil, fmt.Errorf("storage: read node checkins: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (NodeCheckin, error) {
		var c NodeCheckin
		err := r.Scan(&c.Node, &c.ComponentID, &c.Since)
		return c, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read node checkins: %w", err)
	}
	return out, nil
}

// RecordServerIdentity writes the server's identity on its platform
// component (#1644): the build it runs (server-version) and the instant this
// boot started it (server-started-at, RFC 3339 in UTC, a string because the
// property lane has no timestamp type). Observed, written by the process (the
// platform writer class), so no node, push or operator can write either.
// Transition-only: a restart moves the start time forward, and the version
// lands again only when the build changes. A component gone since the boot
// ensured it is ErrComponentNotFound, which the boot logs and does not fail
// on, the rule RecordPlatformMetrics keeps.
func (p *PG) RecordServerIdentity(ctx context.Context, componentID, version string, startedAt time.Time) error {
	at := startedAt.UTC()
	writes := []PropertySampleWrite{
		{OwnerKind: "component", OwnerID: componentID, Key: writerclass.ServerVersion, Value: version, TS: at},
		{OwnerKind: "component", OwnerID: componentID, Key: writerclass.ServerStartedAt, Value: at.Format(time.RFC3339Nano), TS: at},
	}
	return p.sampleWrite(ctx, "record server identity", func(ctx context.Context, tx pgx.Tx) error {
		return p.writePropertySamples(ctx, tx, gatewayWriter(writerclass.Process), writes, true)
	})
}
