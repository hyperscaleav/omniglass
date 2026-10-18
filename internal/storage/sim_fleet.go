package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ComponentSimNode is the node whose simulator would serve a component (by
// its already-resolved id): the node of its first endpoint, by name, placed
// on one. Empty when no endpoint of the component is placed on a node, so
// nothing simulates it.
func (p *PG) ComponentSimNode(ctx context.Context, componentID string) (string, error) {
	var node string
	err := p.pool.QueryRow(ctx, `select n.name from endpoint i join node n on n.principal_id = i.node_name
		where i.component = $1 order by i.name limit 1`, componentID).Scan(&node)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("storage: sim node of %s: %w", componentID, err)
	}
	return node, nil
}

// SimAddressesInUse is every host an endpoint targets, so a new simulated
// device never takes an address another device already answers on. It
// returns hosts only, never the endpoints, and is the planner's input rather
// than a caller-visible read, so it is unscoped.
func (p *PG) SimAddressesInUse(ctx context.Context) (map[string]bool, error) {
	rows, err := p.pool.Query(ctx, `select distinct params->>'target' from endpoint where params ? 'target'`)
	if err != nil {
		return nil, fmt.Errorf("storage: sim addresses in use: %w", err)
	}
	defer rows.Close()
	used := map[string]bool{}
	for rows.Next() {
		var target string
		if err := rows.Scan(&target); err != nil {
			return nil, fmt.Errorf("storage: scan sim address: %w", err)
		}
		used[TargetHost(target)] = true
	}
	return used, rows.Err()
}

// AuditSimOperation records an operation an operator fired at a simulated
// device (by its already-resolved id). The operation changes the simulated
// device, not the platform's data, so the audit row is the whole write.
func (p *PG) AuditSimOperation(ctx context.Context, actorID, componentID string, op any) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin audit sim operation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := writeAuditRes(ctx, tx, actorID, "fire", "sim_operation", componentID, nil, op); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// simAddressLockKey names the advisory lock that serializes handing out
// simulated addresses.
const simAddressLockKey = "omniglass.sim_address"

// WithSimAddressLock runs fn holding the lock every add of a simulated
// device takes around reading the addresses in use and creating the
// endpoint that claims one, so two adds at once cannot be handed the same
// address. The lock is a session advisory lock on a connection of its own,
// so fn's own writes run on their usual paths.
func (p *PG) WithSimAddressLock(ctx context.Context, fn func() error) error {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("storage: acquire sim address lock: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `select pg_advisory_lock(hashtext($1))`, simAddressLockKey); err != nil {
		return fmt.Errorf("storage: take sim address lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `select pg_advisory_unlock(hashtext($1))`, simAddressLockKey)
	}()
	return fn()
}
