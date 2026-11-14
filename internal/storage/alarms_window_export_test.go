package storage

import (
	"context"
	"fmt"
	"time"
)

// SetAlarmClearedAtOffset updates an alarm's cleared_at timestamp to a specific offset from now.
// Used for testing window-based queries without relying on wall-clock timing.
func (p *PG) SetAlarmClearedAtOffset(ctx context.Context, alarmID string, offset time.Duration) error {
	_, err := p.pool.Exec(ctx, fmt.Sprintf(`update alarm set cleared_at = now() - interval '%v' where id = $1::uuid`, offset), alarmID)
	return err
}
