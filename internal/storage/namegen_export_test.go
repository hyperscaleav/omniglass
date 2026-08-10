package storage

import (
	"context"
	"fmt"
)

// The name generator's seam for the external test package, the same shim
// pattern BenchPathsOf established: both live in _test.go files, so they exist
// only in the test binary and ship in nothing.
//
// They exist because the two things #681 has to prove cannot be reached from
// outside. A stem-less allocation has no caller yet (#657 slice 7 is what
// gives a location_type a stem-less name rule), so the only honest way to
// assert it against a real database is to drive the generator directly. And
// the recompute-and-compare invariant has to re-run allocation for a row that
// already exists, which is a read the gateway deliberately does not expose:
// nothing in production wants "what would this row be called".

// ExportGenerateName runs the generator for stem in the given placement
// bucket and returns the name and the ordinal it would allocate, WITHOUT
// writing anything: the transaction is always rolled back, which also
// releases the transaction-scoped advisory lock the generator takes.
//
// excludeID omits one row from the bucket, exactly as every recompute site
// does for its own row (see siblingNamesInScope).
func (p *PG) ExportGenerateName(ctx context.Context, stem string, parentID, locationID, excludeID *string) (string, int, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("storage: begin export generate name: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return generateComponentName(ctx, tx, stem, parentID, locationID, excludeID)
}

// ExportGenerateSystemName is ExportGenerateName on the system tier (#686): it
// runs the real two-step generator (resolve the system_type chain's stem, mint
// the ordinal in the row's placement bucket) and rolls back, so the
// recompute-and-compare invariant can ask "what would this system be called"
// without a gateway read that nothing in production wants.
//
// It takes the system_type id rather than a stem, unlike the component shim
// above, because the whole of a system's mint is derived from that one
// reference: a test that resolved the stem itself would be comparing against a
// restatement of the rule rather than against the rule.
func (p *PG) ExportGenerateSystemName(ctx context.Context, systemTypeID, parentID, locationID, excludeID *string) (string, int, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("storage: begin export generate system name: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return generateNameForSystemType(ctx, tx, systemTypeID, parentID, locationID, excludeID)
}

// ExportStemForProduct resolves the stem a product's component_type chain
// yields, the input the generator mints from. The chain walk is the same one
// generateNameForProduct runs, so a test comparing against it is comparing
// against the real rule rather than a restatement of it.
func (p *PG) ExportStemForProduct(ctx context.Context, productID string) (string, error) {
	typeID, err := componentTypeIDForProduct(ctx, p.pool, productID)
	if err != nil {
		return "", err
	}
	stem, _, _, _, _, err := resolveTypeFacts(ctx, p.pool, typeID)
	return stem, err
}
