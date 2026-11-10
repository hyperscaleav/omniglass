package storage

import (
	"context"
	"fmt"
	"time"
)

// OutboxStatus is the notification outbox at a moment (#1628): how many rows
// are queued, and how long the oldest due one has waited past its
// next_attempt_at. A queued row in its backoff counts toward Queued but not
// DueAge; an outbox with nothing due has a DueAge of zero.
type OutboxStatus struct {
	Queued int64
	DueAge time.Duration
}

// OutboxStatus reads the outbox's depth and due age in one statement, by the
// predicate DeliverNext takes rows by (status queued, next_attempt_at at or
// before now), over notification_due_idx. A platform read: no scope, it
// names no row.
func (p *PG) OutboxStatus(ctx context.Context) (OutboxStatus, error) {
	var st OutboxStatus
	var age float64
	err := p.pool.QueryRow(ctx, `select count(*),
			coalesce(extract(epoch from now() - min(next_attempt_at) filter (where next_attempt_at <= now())), 0)::float8
		from notification where status = 'queued'`).Scan(&st.Queued, &age)
	if err != nil {
		return OutboxStatus{}, fmt.Errorf("storage: read outbox status: %w", err)
	}
	st.DueAge = time.Duration(age * float64(time.Second))
	return st, nil
}
