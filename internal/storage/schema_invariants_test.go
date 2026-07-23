package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// The #262 and #343 conversions recreated columns to swap a slug key for a uuid,
// and recreating a column silently drops its NOT NULL and every index it carried.
// Most were re-asserted in the same migration; a handful on join/contract tables
// and two hot-path indexes were missed and restored later. This pins them so a
// future column recreation cannot drop one again without turning the suite red.
func TestChurnDroppedConstraintsRestored(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, storagetest.NewDSN(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// A join/contract foreign key with a NULL side is meaningless; each of these
	// lost its NOT NULL when its column was recreated, while the partner column on
	// the same table kept it.
	notNull := []struct{ table, column string }{
		{"product_property", "product_id"},
		{"product_property", "property_id"},
		{"product_capability", "product_id"},
		{"standard_property", "property_id"},
		{"location_type_property", "property_id"},
	}
	for _, c := range notNull {
		var nullable string
		if err := conn.QueryRow(ctx,
			`select is_nullable from information_schema.columns
			 where table_name = $1 and column_name = $2`, c.table, c.column).Scan(&nullable); err != nil {
			t.Fatalf("read %s.%s nullability: %v", c.table, c.column, err)
		}
		if nullable != "NO" {
			t.Errorf("%s.%s is nullable, want NOT NULL (a join/contract foreign key with a NULL side is meaningless)", c.table, c.column)
		}
	}

	// Both indexes existed before the churn and were dropped when their column was
	// recreated; the sibling tables kept theirs.
	indexes := []string{"state_datapoint_owner_idx", "product_capability_capability_idx"}
	for _, idx := range indexes {
		var exists bool
		if err := conn.QueryRow(ctx,
			`select exists (select 1 from pg_indexes where indexname = $1)`, idx).Scan(&exists); err != nil {
			t.Fatalf("check index %s: %v", idx, err)
		}
		if !exists {
			t.Errorf("index %s is missing, want it restored (a hot read path is a sequential scan without it)", idx)
		}
	}
}
