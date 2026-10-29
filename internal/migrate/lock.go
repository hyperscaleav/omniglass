package migrate

import (
	"context"
	"fmt"

	"github.com/amacneil/dbmate/v2/pkg/dbmate"
)

// LockKey is the Postgres advisory lock a migration run holds from its schema
// guard through its last migration, so concurrent runs against one database
// take turns (#1288). The value spells "omni" in ASCII; it differs from
// dbmate's own key, which a run also takes on its migrating connection.
const LockKey int64 = 0x6f6d6e69

// withLock runs fn while this session holds LockKey, waiting up to the run's
// MigrationLockTimeout for another holder. The guard needs it: dbmate's own
// lock covers only Migrate, so without this a run that waited would have
// judged the schema before the holder moved it (#1289's window). The lock is
// session-level and lives on one pinned connection; closing it releases the
// lock even if the explicit unlock never runs. If that connection drops part
// way, dbmate's lock (UseMigrationLock) still keeps two Migrates apart.
func withLock(mate *dbmate.DB, fn func() error) error {
	drv, err := mate.Driver()
	if err != nil {
		return err
	}
	sqlDB, err := drv.Open()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	ctx := context.Background()
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	// lock_timeout bounds the wait (zero would wait forever); reset afterwards
	// so it does not linger on the session.
	timeout := max(mate.MigrationLockTimeout.Milliseconds(), 1)
	q := fmt.Sprintf("set lock_timeout = %d; select pg_advisory_lock(%d); reset lock_timeout", timeout, LockKey)
	if _, err := conn.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("waiting for another migration run to finish: %w", err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, "select pg_advisory_unlock($1)", LockKey) }()

	mate.UseMigrationLock = true
	return fn()
}
