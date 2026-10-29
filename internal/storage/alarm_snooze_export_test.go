package storage

import "context"

// ExportExpireSnoozes moves every snooze's deadline into the past, so a test
// need not wait out a minute: the rows stay until something ends them.
func (p *PG) ExportExpireSnoozes(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `update alarm_snooze set snoozed_until = now() - interval '1 second'`)
	return err
}
