package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestNodeEnrollmentSchema proves the additive enrollment migration applies:
// node gains enrollment_token and enrolled_at. Skipped under -short.
func TestNodeEnrollmentSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	for _, col := range []string{"enrollment_token", "enrolled_at"} {
		var exists bool
		if err := conn.QueryRow(ctx,
			`select exists (select 1 from information_schema.columns where table_name = 'node' and column_name = $1)`,
			col).Scan(&exists); err != nil {
			t.Fatalf("probe node.%s: %v", col, err)
		}
		if !exists {
			t.Errorf("column node.%s missing after migrate", col)
		}
	}
}
