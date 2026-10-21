package storage

import "context"

// SetCalcServeHook installs a hook the calc drain calls at each stage of
// serving a mark ("taken": the mark is deleted in the serving transaction,
// before the compute), so a test can hold a serve mid-transaction or fail
// it. Declared in a _test.go file: it exists only in the test binary.
func (p *PG) SetCalcServeHook(h func(stage string) error) { p.calcHook = h }

// WriteMetricHeld is InsertMetricSamples with a pause before the commit: the
// batch has marked and written, and holds its locks until hold returns.
func (p *PG) WriteMetricHeld(ctx context.Context, evs []MetricSampleWrite, hold func()) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := p.writeMetricSamples(ctx, tx, evs, nil); err != nil {
		return err
	}
	hold()
	return tx.Commit(ctx)
}
