// Package storage is the Storage Gateway: the single seam over the relational
// backend. The interface exists from day one even though there is exactly one
// implementation, so the rest of the binary depends on the contract and never
// on pgx directly. Swapping or wrapping the backend later (scope injection,
// audit, read replicas) happens behind this interface without rippling into
// call sites.
//
// The walking skeleton's surface is intentionally tiny: open a pool, expose a
// health Ping, and close. Domain reads and writes land on this same interface
// in later slices.
package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Gateway is the only path to the database. Every component that needs durable
// state takes a Gateway, not a *pgxpool.Pool, so the seam stays the single
// chokepoint for cross-cutting concerns (scope, audit) as the system grows.
type Gateway interface {
	// Ping verifies the backend is reachable and responsive. The healthz
	// endpoint reports its result; a non-nil error means the database leg is
	// down even if the process is up.
	Ping(ctx context.Context) error
	// UpsertRole installs or updates an official role by id, the boot-seed
	// phase's write. Idempotent: re-seeding the same role is a no-op update.
	UpsertRole(ctx context.Context, r Role) error
	// Close releases the underlying connection pool. Idempotent at the pool
	// level; call once on shutdown.
	Close()
}

// PG is the Postgres-backed Gateway implementation over a pgx connection pool.
type PG struct {
	pool *pgxpool.Pool
}

// NewPG opens a pgx pool against dsn and verifies connectivity once before
// returning, so a bad DSN or an unreachable database fails fast at boot rather
// than on the first query.
func NewPG(ctx context.Context, dsn string) (*PG, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("storage: ping: %w", err)
	}
	return &PG{pool: pool}, nil
}

// Ping checks backend reachability through the pool.
func (p *PG) Ping(ctx context.Context) error {
	if err := p.pool.Ping(ctx); err != nil {
		return fmt.Errorf("storage: ping: %w", err)
	}
	return nil
}

// Close releases the pool.
func (p *PG) Close() { p.pool.Close() }
