package storage

import (
	"context"
	"fmt"
)

// The recompute's seams for the external test package, the same shim pattern
// ExportGenerateName established: both live in _test.go files, so they exist
// only in the test binary and ship in nothing.
//
// They exist because the ordering contract #670 is about cannot be observed
// from outside. The lock order is the visit order of an unexported query, and
// nothing the gateway returns reports it: a recompute's only observable is the
// health rows it writes, and those do not show the lock order, because the
// locations are rolled up bottom-up only after every lock is taken (#1562).
// Asserting the property from a public read would mean asserting it does not
// hold, which is not a test.

// ExportLocationsOver runs locationsOver over the given system and location
// ids and returns the location ids IN THE ORDER the recompute would lock them.
// Read-only: the transaction is always rolled back.
func (p *PG) ExportLocationsOver(ctx context.Context, systemIDs, namedIDs []string) ([]string, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: begin export locations over: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	refs, _, err := p.locationsOver(ctx, tx, exportRefs(systemIDs), exportRefs(namedIDs))
	if err != nil {
		return nil, err
	}
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.ID
	}
	return out, nil
}

// ExportRecompute runs one whole recompute in its own committed transaction,
// seeded the way the two production trigger shapes seed it: named locations
// alone (recomputeMovedLocation) or a system plus a named location
// (recomputeMovedSystem). It is what lets a test drive two real recomputes
// concurrently over chains that overlap.
func (p *PG) ExportRecompute(ctx context.Context, systemIDs, namedIDs []string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin export recompute: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := p.recomputeChain(ctx, tx, nil, exportRefs(systemIDs), exportRefs(namedIDs)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func exportRefs(ids []string) []ownerRef {
	out := make([]ownerRef, len(ids))
	for i, id := range ids {
		out[i] = ownerRef{ID: id}
	}
	return out
}

// ExportRecomputeStaffing runs, in its own committed transaction, exactly the
// recompute AssignRole and UnassignRole run: the component and the system it
// staffs, named. It is what lets the scale probe time the recompute alone,
// without the staffing write around it.
func (p *PG) ExportRecomputeStaffing(ctx context.Context, componentID, systemID string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("storage: begin export recompute staffing: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := p.recomputeChain(ctx, tx, exportRefs([]string{componentID}), exportRefs([]string{systemID}), nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
