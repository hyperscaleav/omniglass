package migrate_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestConcurrentRunsApplyEachMigrationOnce races several migrators at one
// empty database, the way two servers (or a server and a migrate Job) start
// together after an upgrade. Each must return cleanly and every migration must
// be recorded exactly once: the lock makes the later runs wait, and they find
// nothing left to do (#1288).
func TestConcurrentRunsApplyEachMigrationOnce(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackAll(dsn); err != nil {
		t.Fatalf("rollback all: %v", err)
	}

	const runs = 4
	errs := make(chan error, runs)
	start := make(chan struct{})
	for range runs {
		go func() {
			<-start
			errs <- migrate.Run(dsn)
		}()
	}
	close(start)
	for range runs {
		if err := <-errs; err != nil {
			t.Errorf("a concurrent run failed: %v", err)
		}
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var dupes int
	if err := conn.QueryRow(ctx,
		`select count(*) from (select version from schema_migrations group by version having count(*) > 1) d`,
	).Scan(&dupes); err != nil {
		t.Fatalf("count duplicate versions: %v", err)
	}
	if dupes != 0 {
		t.Errorf("%d migrations recorded more than once", dupes)
	}
}

// TestRunGuardsTheSchemaUnderTheLock closes the window #1289 left open: a run
// that waits on the lock must judge the schema as the holder left it, not as
// it was before it waited. The test holds the lock, lets a run queue behind
// it, migrates the database past this binary while the run waits, then
// releases: the run must refuse with ErrSchemaAhead rather than boot.
func TestRunGuardsTheSchemaUnderTheLock(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()

	holder, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect holder: %v", err)
	}
	defer holder.Close(ctx)
	if _, err := holder.Exec(ctx, `select pg_advisory_lock($1)`, migrate.LockKey); err != nil {
		t.Fatalf("take the lock: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- migrate.Run(dsn) }()

	// Wait until the run is queued on the lock (an ungranted advisory lock on
	// LockKey: pg_locks splits the bigint key into classid and objid), so the
	// stamp below lands while it waits rather than before it reached the guard.
	deadline := time.Now().Add(30 * time.Second)
	for {
		select {
		case err := <-done:
			t.Fatalf("run returned %v while another session held the lock; want it to wait", err)
		default:
		}
		var waiting bool
		if err := holder.QueryRow(ctx, `select exists (select 1 from pg_locks where locktype = 'advisory'
			and not granted and classid::bigint = $1 and objid::bigint = $2)`,
			int64(uint32(migrate.LockKey>>32)), int64(uint32(migrate.LockKey)),
		).Scan(&waiting); err != nil {
			t.Fatalf("read pg_locks: %v", err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("run never queued on the migration lock")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if _, err := holder.Exec(ctx, `insert into schema_migrations (version) values ('29991231235959')`); err != nil {
		t.Fatalf("stamp a future migration: %v", err)
	}
	if _, err := holder.Exec(ctx, `select pg_advisory_unlock($1)`, migrate.LockKey); err != nil {
		t.Fatalf("release the lock: %v", err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, migrate.ErrSchemaAhead) {
			t.Fatalf("run after the holder migrated past this binary = %v, want ErrSchemaAhead", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("run never returned after the lock was released")
	}
}
