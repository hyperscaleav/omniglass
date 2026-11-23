package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
)

// The batch creates (#2368, ADR-2368): many rows of one collection in one
// transaction, each the ordinary single create (createLocationTx,
// createSystemTx, createComponentTx) under the caller's own scope with its
// own audit row. Rows run in order, so a row naming a row created earlier in
// the same batch (a location's parent) resolves by name as it would after a
// commit. A refused row refuses the whole batch and a BatchError names it.
// validateOnly runs the same transaction and rolls it back, so a dry run
// refuses exactly what the write would. The batch id the audit rows carry is
// the context's (WithBatch).

// BatchError says which row of a batch refused it: Index is the row's position in
// the request, and Err is the cause its single create returned, so a caller
// maps it as that create maps it.
type BatchError struct {
	Index int
	Err   error
}

func (e *BatchError) Error() string {
	return fmt.Sprintf("requests[%d]: %v", e.Index, e.Err)
}

func (e *BatchError) Unwrap() error { return e.Err }

// batchCreate runs create for each spec, in order, in one transaction, and
// commits unless validateOnly. A refused row is a BatchError at its index.
func batchCreate[S, R any](ctx context.Context, p *PG, what string, specs []S, validateOnly bool,
	create func(context.Context, pgx.Tx, S) (R, error), finish func(context.Context, pgx.Tx) error) ([]R, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin batch create %s: %w", what, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out := make([]R, 0, len(specs))
	for i, spec := range specs {
		r, err := create(ctx, tx, spec)
		if err != nil {
			return nil, &BatchError{Index: i, Err: err}
		}
		out = append(out, r)
	}
	if finish != nil {
		if err := finish(ctx, tx); err != nil {
			return nil, err
		}
	}
	if validateOnly {
		return out, nil // the deferred rollback discards every write
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("storage: commit batch create %s: %w", what, err)
	}
	return out, nil
}

// BatchCreateLocations is CreateLocation for each spec, in order, in one
// transaction: a parent created earlier in the batch resolves by name.
func (p *PG) BatchCreateLocations(ctx context.Context, actorID string, specs []LocationSpec, create scope.Set, validateOnly bool) ([]*Location, error) {
	return batchCreate(ctx, p, "locations", specs, validateOnly, func(ctx context.Context, tx pgx.Tx, s LocationSpec) (*Location, error) {
		return p.createLocationTx(ctx, tx, actorID, s, create)
	}, nil)
}

// BatchCreateSystems is CreateSystem for each spec, in order, in one
// transaction. Health is recomputed once, after the last row, over every
// system the batch created (#915): see healthDeferral.
func (p *PG) BatchCreateSystems(ctx context.Context, actorID string, specs []SystemSpec, create, locationRead scope.Set, validateOnly bool) ([]*System, error) {
	deferred := newHealthDeferral()
	return batchCreate(ctx, p, "systems", specs, validateOnly, func(ctx context.Context, tx pgx.Tx, s SystemSpec) (*System, error) {
		return p.createSystemTx(ctx, tx, actorID, s, create, locationRead, deferred)
	}, func(ctx context.Context, tx pgx.Tx) error {
		if err := deferred.flush(ctx, p, tx); err != nil {
			return fmt.Errorf("storage: batch create systems health: %w", err)
		}
		return nil
	})
}

// BatchCreateComponents is CreateComponent for each spec, in order, in one
// transaction.
func (p *PG) BatchCreateComponents(ctx context.Context, actorID string, specs []ComponentSpec, create, locationRead, systemRead, systemUpdate scope.Set, validateOnly bool) ([]*Component, error) {
	return batchCreate(ctx, p, "components", specs, validateOnly, func(ctx context.Context, tx pgx.Tx, s ComponentSpec) (*Component, error) {
		return p.createComponentTx(ctx, tx, actorID, s, create, locationRead, systemRead, systemUpdate)
	}, nil)
}
