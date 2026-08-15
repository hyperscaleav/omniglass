package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestLocationNameRuleArrivesClaimingNothing is the acceptance behavior
// "existing locations keep their names and their pen stays false", proven
// against the migration rather than against the gateway.
//
// It matters because the generator this slice adds is the first thing that can
// rename a location the platform owns. A name_rule backfilled onto the shipped
// types, or a pen or an ordinal claimed for an existing row, would make the
// first :move or reclassify rename real fleet: exactly the failure the two
// name-pen migrations before this one each argued against, one slice apart.
// "No backfill" is not something the absence of a file can demonstrate, since
// the column's DEFAULT is what an existing row actually gets, so this stands
// the database one migration back, writes rows the way an upgraded fleet holds
// them, and migrates forward over them.
//
// The REAL migration file runs, not a copy of its SQL, so the two cannot drift.
func TestLocationNameRuleArrivesClaimingNothing(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, "20260810130000"); err != nil {
		t.Fatalf("roll back below the location name rule: %v", err)
	}
	conn := connectDSN(t, dsn)

	// The registries are seeded by the boot phase rather than by a migration,
	// so this fixture writes the rows an upgraded fleet would already hold.
	// The column is `display_name` at this point in the chain: #613's rename
	// (20260814100000_label_is_the_column.sql) is above the version this test
	// stands the database at, so writing `label` here would fail on a column
	// that does not exist yet.
	mustExec(t, conn, `insert into location_type (name, display_name) values ('floor', 'Floor')`)
	mustExec(t, conn, `insert into location (name, display_name_generated, location_type)
	                   values ($1, false, (select id from location_type where name = 'floor'))`, "ground")

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward over the pre-rule rows: %v", err)
	}

	var (
		name    string
		pen     bool
		ordinal *int
	)
	if err := conn.QueryRow(ctx, `select name, name_generated, ordinal from location where name = $1`, "ground").Scan(&name, &pen, &ordinal); err != nil {
		t.Fatalf("read the location: %v", err)
	}
	if name != "ground" {
		t.Errorf("location name after the migration = %q, want ground: the migration renames nothing", name)
	}
	if pen {
		t.Error("an existing location reports name_generated = true after the migration, want false: its name was typed by an operator, and claiming it would let the first :move rename it")
	}
	if ordinal != nil {
		t.Errorf("an existing location carries ordinal %d after the migration, want absent: there is no number the platform owns for a name it did not pick", *ordinal)
	}

	// And the type does not start generating either. A backfilled rule would
	// turn every later nameless create on an upgraded fleet into a generated
	// name the operator never opted into.
	var rule *string
	if err := conn.QueryRow(ctx, `select name_rule::text from location_type where name = 'floor'`).Scan(&rule); err != nil {
		t.Fatalf("read the type rule: %v", err)
	}
	if rule != nil {
		t.Errorf("an existing location_type carries name_rule %s after the migration, want absent: opting a type in is an operator's act", *rule)
	}

	// The shape floor is idempotent and refuses a non-object, which is what
	// keeps a hand-written row from making the decoder the first thing to
	// notice.
	if _, err := conn.Exec(ctx, `update location_type set name_rule = '"positional"'::jsonb where name = 'floor'`); err == nil {
		t.Error("a scalar name_rule was accepted, want the shape check to refuse it")
	}
	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward a second time: %v", err)
	}
}
