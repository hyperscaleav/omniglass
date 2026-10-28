package migrate_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestRunRefusesASchemaNewerThanTheBinary stands a database up the way build
// N+1 would leave it (one applied migration past everything this binary
// embeds) and proves build N refuses it, naming both versions, instead of
// booting on a schema it does not know (#1289).
func TestRunRefusesASchemaNewerThanTheBinary(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()

	// At its own head the binary migrates as today.
	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("run at head: %v", err)
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var head string
	if err := conn.QueryRow(ctx, `select max(version) from schema_migrations`).Scan(&head); err != nil {
		t.Fatalf("read head: %v", err)
	}
	const future = "29991231235959"
	if _, err := conn.Exec(ctx, `insert into schema_migrations (version) values ($1)`, future); err != nil {
		t.Fatalf("stamp a future migration: %v", err)
	}

	err = migrate.Run(dsn)
	if !errors.Is(err, migrate.ErrSchemaAhead) {
		t.Fatalf("run on a newer schema = %v, want ErrSchemaAhead", err)
	}
	for _, v := range []string{future, head} {
		if !strings.Contains(err.Error(), v) {
			t.Errorf("refusal %q does not name %s", err, v)
		}
	}
}

// TestRunMigratesADatabaseBehindTheBinary is the other half of the guard: a
// database one migration behind is brought forward, not refused.
func TestRunMigratesADatabaseBehindTheBinary(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackOne(dsn); err != nil {
		t.Fatalf("rollback one: %v", err)
	}
	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("run behind head: %v", err)
	}
}
