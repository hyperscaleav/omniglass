package storage

import (
	"context"
	"fmt"
)

// DatabaseSize is the database's on-disk bytes, as pg_database_size reads it
// (#1985): the figure the server gauges as db-size each report window. A
// platform read: no scope, it names no row.
func (p *PG) DatabaseSize(ctx context.Context) (int64, error) {
	var n int64
	if err := p.pool.QueryRow(ctx, `select pg_database_size(current_database())`).Scan(&n); err != nil {
		return 0, fmt.Errorf("storage: read database size: %w", err)
	}
	return n, nil
}
