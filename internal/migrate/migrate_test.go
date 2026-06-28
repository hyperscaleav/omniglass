package migrate_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestMigrateRoundTrip proves the embedded migration set applies clean against
// a real Postgres, creates the expected schema, and rolls all the way back
// down. storagetest.NewDSN already ran Run (up); this asserts the table exists,
// then RollbackAll removes it. Skipped under -short.
func TestMigrateRoundTrip(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	if !tableExists(t, ctx, conn, "platform_setting") {
		t.Fatal("platform_setting missing after migrate up")
	}

	if err := migrate.RollbackAll(dsn); err != nil {
		t.Fatalf("rollback all: %v", err)
	}
	if tableExists(t, ctx, conn, "platform_setting") {
		t.Fatal("platform_setting still present after rollback")
	}
}

func tableExists(t *testing.T, ctx context.Context, conn *pgx.Conn, name string) bool {
	t.Helper()
	var exists bool
	err := conn.QueryRow(ctx,
		`select exists (select 1 from information_schema.tables where table_name = $1)`,
		name).Scan(&exists)
	if err != nil {
		t.Fatalf("query table existence: %v", err)
	}
	return exists
}
