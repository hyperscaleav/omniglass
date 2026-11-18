package storage

import (
	"context"

	"github.com/hyperscaleav/omniglass/internal/tail"
)

// ExportBeginSampleWrite opens a sample write the way sampleWriteOnce does,
// holding a pooled connection until its commit, which passes the tail's
// fence: for a test that parks a writer between the two.
func (p *PG) ExportBeginSampleWrite(ctx context.Context) (commit func(rows []tail.Row) error, err error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return func(rows []tail.Row) error {
		return p.tail.Commit(func() error { return tx.Commit(ctx) }, rows)
	}, nil
}
