package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestCollectionSchema proves the collection migration applies: the six tables
// exist and the metric_datapoint owner-arc CHECK rejects a row whose owner_kind
// does not match its id columns. Skipped under -short (needs a container).
func TestCollectionSchema(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	for _, table := range []string{"node", "interface_type", "interface", "task", "datapoint_type", "metric_datapoint"} {
		var exists bool
		if err := conn.QueryRow(ctx, `select exists (select 1 from information_schema.tables where table_name = $1)`, table).Scan(&exists); err != nil {
			t.Fatalf("probe %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s missing after migrate", table)
		}
	}

	// owner_kind = component but all id columns null violates the owner-arc CHECK.
	_, err = conn.Exec(ctx, `insert into metric_datapoint (owner_kind, key, value, provenance) values ('component', 'tcp.open', 1, 'observed')`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("owner-arc CHECK: want check_violation (23514), got %v", err)
	}
}
