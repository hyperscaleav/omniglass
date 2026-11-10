package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ReceiptWindow is one node's telemetry receipts around a closed-loop
// window [start, end], read over [from, next]: every receipt landed in that
// span, and of those, how many landed before start (the margin) and how
// many after end.
type ReceiptWindow struct {
	Landed   int64
	Margin   int64
	AfterEnd int64
}

// ClosedLoopPollTarget is one enabled poll task's endpoint with a component,
// by the node that holds it. Every is a driver poll's declared schedule
// ("0s" when it declares none) and empty for a reachability probe, which
// runs at its node's probe interval.
type ClosedLoopPollTarget struct {
	Node        string
	ComponentID string
	Transport   string
	Every       string
}

// CountTelemetryReceipts counts the receipts whose batch id begins with
// batchPrefix (a node's telemetry subject and a space) landed in [from,
// next], split by start and end. starts_with keeps the prefix's own
// characters literal, so a node named edge-1 never counts edge-10's.
func (p *PG) CountTelemetryReceipts(ctx context.Context, batchPrefix string, from, start, end, next time.Time) (ReceiptWindow, error) {
	var w ReceiptWindow
	err := p.pool.QueryRow(ctx, `select count(*),
			count(*) filter (where landed_at < $3),
			count(*) filter (where landed_at > $4)
		from telemetry_receipt
		where starts_with(batch_id, $1) and landed_at >= $2 and landed_at <= $5`,
		batchPrefix, from, start, end, next).Scan(&w.Landed, &w.Margin, &w.AfterEnd)
	if err != nil {
		return ReceiptWindow{}, fmt.Errorf("storage: count telemetry receipts: %w", err)
	}
	return w, nil
}

// ListClosedLoopPollTargets lists every enabled poll task's endpoint with a
// component, by node, component and transport. The driver test is
// driver.IsBaked's, and the join the node's worklist read.
func (p *PG) ListClosedLoopPollTargets(ctx context.Context) ([]ClosedLoopPollTarget, error) {
	rows, err := p.pool.Query(ctx, `select n.name, i.component::text, i.transport,
			case when coalesce(t.spec->>'driver', '') <> '' then coalesce(t.spec->'schedule'->>'every', '0s') else '' end
		from task t
		join endpoint i on i.id = t.endpoint_id
		join node n on n.principal_id = i.node_name
		where t.enabled and t.mode = 'poll' and i.component is not null
		order by 1, 2, 3`)
	if err != nil {
		return nil, fmt.Errorf("storage: list closed-loop poll targets: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ClosedLoopPollTarget, error) {
		var t ClosedLoopPollTarget
		return t, r.Scan(&t.Node, &t.ComponentID, &t.Transport, &t.Every)
	})
	if err != nil {
		return nil, fmt.Errorf("storage: list closed-loop poll targets: %w", err)
	}
	return out, nil
}
