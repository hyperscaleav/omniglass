package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestNamePenSpreadsWithoutClaimingExistingRows proves the half of #686 that is
// a NON-event: the pen arrives on system and location and claims nothing.
//
// It matters because it is the opposite of the label pen's own migration
// (20260810110000), which deliberately DID backfill true, and the difference is
// easy to get wrong in the direction that hurts: a system whose pen defaulted
// true would be re-minted by its first :move, silently renaming a room an
// operator named. "No backfill" is not something the absence of a file can
// demonstrate, since the column's DEFAULT is what an existing row actually
// gets, so this stands the database one migration back, writes rows the way an
// upgraded estate holds them, and migrates forward over them.
//
// The REAL migration file runs, not a copy of its SQL, so the two cannot drift.
func TestNamePenSpreadsWithoutClaimingExistingRows(t *testing.T) {
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, "20260810120000"); err != nil {
		t.Fatalf("roll back below the name pen: %v", err)
	}
	conn := connectDSN(t, dsn)

	// The registries are seeded by the boot phase rather than by a migration, so
	// this fixture writes the one row a location needs to exist at all.
	mustExec(t, conn, `insert into location_type (name, display_name) values ('room', 'Room')`)
	mustExec(t, conn, `insert into system (name, display_name_generated) values ($1, false)`, "legacy-av")
	mustExec(t, conn, `insert into location (name, display_name_generated, location_type)
	                   values ($1, false, (select id from location_type where name = 'room'))`, "legacy-room")

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward over the pre-pen rows: %v", err)
	}

	for _, r := range []struct{ table, name string }{{"system", "legacy-av"}, {"location", "legacy-room"}} {
		var (
			pen  bool
			name string
		)
		if err := conn.QueryRow(ctx, `select name, name_generated from `+r.table+` where name = $1`, r.name).Scan(&name, &pen); err != nil {
			t.Fatalf("read %s %q: %v", r.table, r.name, err)
		}
		if pen {
			t.Errorf("%s %q reports name_generated = true after the migration, want false: an existing name was typed by an operator, and claiming it would let the first :move rename real estate", r.table, r.name)
		}
		if name != r.name {
			t.Errorf("%s name after the migration = %q, want %q: the migration renames nothing", r.table, name, r.name)
		}
	}

	// The system ordinal arrives absent rather than fabricated, the same
	// nullable-is-load-bearing rule the component column follows: there is no
	// number the platform owns for a name it did not pick.
	var ordinal *int
	if err := conn.QueryRow(ctx, `select ordinal from system where name = $1`, "legacy-av").Scan(&ordinal); err != nil {
		t.Fatalf("read the system ordinal: %v", err)
	}
	if ordinal != nil {
		t.Errorf("an existing system carries ordinal %d after the migration, want absent", *ordinal)
	}
	// location gets the pen and NOT an ordinal: there is no mint to allocate one
	// until #687 gives a location_type a name rule, and a column no writer can
	// fill is a fact waiting to be read wrongly.
	var has bool
	if err := conn.QueryRow(ctx, `select exists (
	        select 1 from information_schema.columns
	         where table_name = 'location' and column_name = 'ordinal')`).Scan(&has); err != nil {
		t.Fatalf("check for a location ordinal column: %v", err)
	}
	if has {
		t.Error("location carries an ordinal column with no writer: the generator that fills one is #687's")
	}
}
