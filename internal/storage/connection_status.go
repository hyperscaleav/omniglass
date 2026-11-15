package storage

import (
	"context"
	"fmt"
	"time"
)

// ConnectionStatus is the database's own account of its client connections at
// a moment (#1980): the backends held against the limit that admits clients,
// how many sit idle in a transaction, and how long the oldest open transaction
// has been open. The server's pool counts only the connections it opened;
// this counts every client, a migrate, a psql session, a lab measure or a
// second server included.
type ConnectionStatus struct {
	Held                 int64
	Limit                int64
	IdleInTransaction    int64
	OldestTransactionAge time.Duration
}

// ConnectionStatus reads the database's connections in one statement from
// pg_stat_activity. Held counts the client and walsender backends, the ones
// max_connections admits (the checkpointer and the other background workers
// are listed in the view but hold no slot), against max_connections less
// superuser_reserved_connections, the share an ordinary role may take. A role
// without pg_read_all_stats still sees every row counted, but the state and
// transaction start of another role's session read null, so a leaked
// transaction under another role is missed: the server's role should be a
// member of pg_monitor. A platform read: no scope, it names no row.
func (p *PG) ConnectionStatus(ctx context.Context) (ConnectionStatus, error) {
	var st ConnectionStatus
	var age float64
	err := p.pool.QueryRow(ctx, `select count(*),
			current_setting('max_connections')::bigint - current_setting('superuser_reserved_connections')::bigint,
			count(*) filter (where state = 'idle in transaction'),
			coalesce(extract(epoch from now() - min(xact_start)), 0)::float8
		from pg_stat_activity
		where backend_type in ('client backend', 'walsender')`).Scan(&st.Held, &st.Limit, &st.IdleInTransaction, &age)
	if err != nil {
		return ConnectionStatus{}, fmt.Errorf("storage: read connection status: %w", err)
	}
	st.OldestTransactionAge = time.Duration(age * float64(time.Second))
	return st, nil
}
