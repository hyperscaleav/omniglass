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

// WriteCalcSamples lands evs as calc rule r's output through the write a calc
// tick lands its output by (writeCalcOutput), each at its own TS: a late or
// recomputed calculated sample, which a tick, dating its output now, does not
// write (#1534).
func (p *PG) WriteCalcSamples(ctx context.Context, r *CalcRule, evs []MetricSampleWrite) error {
	tx, err := p.pool.BeginTx(ctx, trendTxOptions)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for i := range evs {
		evs[i].Source = calcSource
	}
	if err := p.writeMetricSamples(ctx, tx, calcWriter(&sampleLineage{ruleID: r.ID, ruleName: r.Name, version: r.Version}), evs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
