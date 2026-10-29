package storage

import "context"

// ExportExpireSnoozes moves every snooze's deadline into the past, so a test
// need not wait out a minute: the rows stay until something ends them.
func (p *PG) ExportExpireSnoozes(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `update alarm_snooze set snoozed_until = now() - interval '1 second'`)
	return err
}

// ExportRaiseHeld raises an alarm through the shared raise seam inside a
// transaction it leaves open, so a test can race a snooze against a
// transition whose notifications are judged but not yet committed. The
// returned func commits it.
func (p *PG) ExportRaiseHeld(ctx context.Context, componentName string, spec AlarmSpec) (func() error, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	c, err := scopedByName(ctx, tx, componentConfig, componentName)
	if err == nil {
		_, err = p.raiseAlarmTx(ctx, tx, "", c, spec, nil)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return func() error { return tx.Commit(ctx) }, nil
}
