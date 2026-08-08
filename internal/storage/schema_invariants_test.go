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

	notNull := []struct{ table, column, why string }{
		// Each of these lost its NOT NULL when its column was recreated during
		// the #262/#343 churn, while the partner column on the same table
		// kept it: a join/contract foreign key with a NULL side is
		// meaningless.
		{"product_property", "product_id", "a join/contract foreign key with a NULL side is meaningless"},
		{"product_property", "property_type_id", "a join/contract foreign key with a NULL side is meaningless"},
		{"standard_property", "property_type_id", "a join/contract foreign key with a NULL side is meaningless"},
		{"location_type_property", "property_type_id", "a join/contract foreign key with a NULL side is meaningless"},
		// The position floor (20260807146000_assignment_position_floor.sql,
		// #626): every assignment carries its ordering position within its
		// role, not just those written after the floor landed (the
		// preceding backfill fills every existing row first). Not a foreign
		// key: an ordering integer with no NULL reading at all, since
		// "unpositioned" is exactly the state the backfill exists to close
		// out before the floor lands.
		{"system_role_assignment", "position", "an assignment's ordering position has no meaning when unset"},
		// role_choice and choice_alternate (20260807150000_role_choices_and_alternates.sql,
		// #626): new tables, not churn, but the same guard is the cheapest
		// place to pin that a fresh column recreation never drops these.
		{"choice_alternate", "choice_id", "an alternate with no choice cannot be grouped by anything"},
		{"choice_alternate", "position", "the tie-break internal/health.Choice.Active reads has no meaning when unset"},
		{"role_choice", "owner_kind", "a choice with no declared owner kind cannot resolve its arc"},
		{"role_choice", "name", "an unnamed choice cannot be addressed"},
	}
	for _, c := range notNull {
		var nullable string
		if err := conn.QueryRow(ctx,
			`select is_nullable from information_schema.columns
			 where table_name = $1 and column_name = $2`, c.table, c.column).Scan(&nullable); err != nil {
			t.Fatalf("read %s.%s nullability: %v", c.table, c.column, err)
		}
		if nullable != "NO" {
			t.Errorf("%s.%s is nullable, want NOT NULL (%s)", c.table, c.column, c.why)
		}
	}

	// This index existed before the churn and was dropped when its column was
	// recreated; the sibling tables kept theirs.
	indexes := []string{"property_owner_idx"}
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
