package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/db"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// backfillUpSQL reads the product_type_backfill migration's up section
// straight from the embedded migration source (db.FS), so the test drives the
// exact statements the migration applies rather than a hand-copied
// approximation that could drift from it.
func backfillUpSQL(t *testing.T) string {
	t.Helper()
	raw, err := db.FS.ReadFile("migrations/20260807113000_product_type_backfill.sql")
	if err != nil {
		t.Fatalf("read backfill migration: %v", err)
	}
	up, _, ok := strings.Cut(string(raw), "-- migrate:down")
	if !ok {
		t.Fatalf("backfill migration missing '-- migrate:down' marker")
	}
	return strings.TrimPrefix(up, "-- migrate:up")
}

// TestBackfillIdempotent proves the product_type_backfill migration's SQL is
// safe to run more than once: the generic component_type/product rows
// ON CONFLICT DO NOTHING rather than duplicate, and the UPDATE guards
// (kind = 'vm', component_type_id/product_id IS NULL) leave nothing to touch
// on a second pass. storagetest.NewDSN already migrates to head, so the
// backfill has run once via the real migration by the time this test opens
// its connection; running its SQL text again (twice, for good measure) must
// change nothing.
func TestBackfillIdempotent(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	conn := connectDSN(t, dsn)
	up := backfillUpSQL(t)

	countGenerics := func(table string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx,
			`select count(*) from `+table+` where name in ('generic-device', 'generic-app', 'generic-service')`).Scan(&n); err != nil {
			t.Fatalf("count %s generics: %v", table, err)
		}
		return n
	}

	if got := countGenerics("component_type"); got != 3 {
		t.Fatalf("baseline generic component_types = %d, want 3 (from the real migration)", got)
	}
	if got := countGenerics("product"); got != 3 {
		t.Fatalf("baseline generic products = %d, want 3 (from the real migration)", got)
	}

	// Nothing is pending on a fully-migrated database (the floor's NOT NULL and
	// narrowed kind CHECK are already active), so re-running the backfill is
	// exactly the no-op property under test.
	if _, err := conn.Exec(ctx, up); err != nil {
		t.Fatalf("first re-run of backfill SQL: %v", err)
	}
	if got := countGenerics("component_type"); got != 3 {
		t.Fatalf("generic component_types after 1st re-run = %d, want 3 (no duplicates)", got)
	}
	if got := countGenerics("product"); got != 3 {
		t.Fatalf("generic products after 1st re-run = %d, want 3 (no duplicates)", got)
	}

	if _, err := conn.Exec(ctx, up); err != nil {
		t.Fatalf("second re-run of backfill SQL: %v", err)
	}
	if got := countGenerics("component_type"); got != 3 {
		t.Fatalf("generic component_types after 2nd re-run = %d, want 3 (no duplicates)", got)
	}
	if got := countGenerics("product"); got != 3 {
		t.Fatalf("generic products after 2nd re-run = %d, want 3 (no duplicates)", got)
	}
}
