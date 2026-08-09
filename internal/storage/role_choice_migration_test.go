package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestChoiceAlternatePositionDeferrableRepairsOldSchema is the fix-round
// regression for the second blocking finding: 20260807150000 was edited in
// place, within the same development session, to make
// choice_alternate_position_key deferrable, which is safe for a chain that
// had not yet applied that version but leaves any database that already had
// stuck on the old, non-deferrable shape forever (dbmate keys on the
// version, and 20260807150000's own `create table if not exists` makes a
// forced re-run a no-op). SeedRoleChoice unconditionally defers that named
// constraint every boot, and Postgres errors deferring a named constraint
// that is not itself deferrable, so such a database cannot boot at all.
//
// This stands a database at the schema immediately before
// 20260807160000_choice_alternate_position_deferrable_repair.sql (which, on
// a fresh chain, finds the constraint already deferrable and does nothing),
// manually reverts the constraint to the OLD non-deferrable shape to
// simulate a database that applied 20260807150000 before the repair
// existed, confirms that state is genuinely broken the way the finding
// describes, then migrates forward over it and confirms the repair fixes
// it.
func TestChoiceAlternatePositionDeferrableRepairsOldSchema(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, "20260807160000"); err != nil {
		t.Fatalf("rollback below the deferrable repair: %v", err)
	}
	conn := connectDSN(t, dsn)
	ctx := context.Background()

	// Simulate the old, non-deferrable shape: revert what 20260807150000
	// already applies deferrable on this chain, standing in for a database
	// that applied that version before this session's in-place edit.
	mustExec(t, conn, `alter table choice_alternate drop constraint choice_alternate_position_key`)
	mustExec(t, conn, `alter table choice_alternate add constraint choice_alternate_position_key unique (choice_id, position)`)

	// Confirm the simulated state is genuinely the failure SeedRoleChoice
	// hits: deferring a non-deferrable named constraint errors, inside a
	// transaction so the failed attempt does not need its own cleanup.
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(ctx, `set constraints choice_alternate_position_key deferred`); err == nil {
		t.Fatal("SET CONSTRAINTS against the simulated old schema succeeded, want it to fail " +
			"(otherwise this test is not reproducing the bug)")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback sanity-check tx: %v", err)
	}

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward over the simulated old schema: %v", err)
	}

	tx2, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx2.Rollback(ctx) }()
	if _, err := tx2.Exec(ctx, `set constraints choice_alternate_position_key deferred`); err != nil {
		t.Fatalf("SET CONSTRAINTS after the repair migration = %v, want success", err)
	}
}
