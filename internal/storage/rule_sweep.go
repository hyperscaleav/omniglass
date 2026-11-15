package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// StampRuleSweep records, on the database's clock, that the server whose
// platform component is componentID has just completed an event rule sweep
// (#1986). The stamp is what a second server reads as this one's sweep age
// (ServerSweepAges), and the database's clock is the one both read, so a
// skewed host clock never ages a healthy peer.
func (p *PG) StampRuleSweep(ctx context.Context, componentID string) error {
	if _, err := p.pool.Exec(ctx, `update platform_server set rule_swept_at = now() where platform_component_id = $1::uuid`, componentID); err != nil {
		return fmt.Errorf("storage: stamp rule sweep: %w", err)
	}
	return nil
}

// ReleaseRuleSweep clears a server's stamp as it stops cleanly, so a peer
// does not judge a server that was told to stop. A server that dies keeps its
// stamp, which is how a peer knows.
func (p *PG) ReleaseRuleSweep(ctx context.Context, componentID string) error {
	if _, err := p.pool.Exec(ctx, `update platform_server set rule_swept_at = null where platform_component_id = $1::uuid`, componentID); err != nil {
		return fmt.Errorf("storage: release rule sweep: %w", err)
	}
	return nil
}

// ServerSweepAge is how long ago a server's event rule sweep last completed,
// by the database's clock, on that server's platform component.
type ServerSweepAge struct {
	ComponentID string
	Age         time.Duration
}

// ServerSweepAges reads every server that holds a stamp, in component order:
// the peer sweep timer's input. A server that has not swept yet, or stopped
// cleanly, holds none and is absent. A platform read: no scope, it names no
// row.
func (p *PG) ServerSweepAges(ctx context.Context) ([]ServerSweepAge, error) {
	rows, err := p.pool.Query(ctx, `
		select platform_component_id::text, extract(epoch from now() - rule_swept_at)::float8
		from platform_server
		where rule_swept_at is not null
		order by platform_component_id`)
	if err != nil {
		return nil, fmt.Errorf("storage: read server sweep ages: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ServerSweepAge, error) {
		var a ServerSweepAge
		var secs float64
		err := r.Scan(&a.ComponentID, &secs)
		a.Age = time.Duration(max(secs, 0) * float64(time.Second))
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("storage: read server sweep ages: %w", err)
	}
	return out, nil
}
