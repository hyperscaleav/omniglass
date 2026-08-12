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
	// A location's ordinal is the same non-event. The column itself arrives one
	// migration later, with the generator that fills it (#687, and see
	// TestLocationNameRuleArrivesClaimingNothing, which holds THAT migration to
	// the same bar); what this asserts is that migrating the whole way forward
	// leaves an existing location's number absent, exactly as it leaves the
	// system's.
	//
	// This assertion used to read "location has no ordinal column at all",
	// which was the honest statement of #686's own restraint (a column no
	// writer can fill is a fact waiting to be read wrongly) and became false
	// the moment the writer landed. migrate.Run has no bounded form, so it
	// always runs every migration; the surviving invariant is the value, not
	// the column's absence.
	if err := conn.QueryRow(ctx, `select ordinal from location where name = $1`, "legacy-room").Scan(&ordinal); err != nil {
		t.Fatalf("read the location ordinal: %v", err)
	}
	if ordinal != nil {
		t.Errorf("an existing location carries ordinal %d after the migrations, want absent", *ordinal)
	}
}
