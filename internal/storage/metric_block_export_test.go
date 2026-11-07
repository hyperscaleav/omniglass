package storage

import (
	"context"
	"time"
)

// SetMetricBlockMarks stands both block folds' marks (#1528) at through, a
// Monday, with no series of its day or week folded, so a test starts the
// folds where its samples are.
func (p *PG) SetMetricBlockMarks(ctx context.Context, through time.Time) error {
	return setMetricBlockMarks(ctx, p.pool, through)
}

// MetricBlockMarks reads the block folds' marks (#1528): the day fold's day
// and series cursor and the week fold's, ok false before the first pass.
func (p *PG) MetricBlockMarks(ctx context.Context) (blocked time.Time, blockedSeries int64, week time.Time, weekSeries int64, ok bool, err error) {
	m, ok, err := metricBlockMarks(ctx, p.pool, false)
	return m.blocked, m.blockedSeries, m.week, m.weekSeries, ok, err
}

// BlockMergeLockTimeout is how long a late batch waits for the block lock
// before it gives up (#1546).
const BlockMergeLockTimeout = blockMergeLockTimeout
