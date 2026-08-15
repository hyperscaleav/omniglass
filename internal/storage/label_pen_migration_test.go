package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestLabelPenBackfill proves the one-time backfill
// (20260810110000_label_pen_backfill.sql) against rows the migration set can
// never produce on its own, the same shape TestComponentOrdinalBackfill uses:
// it stands the database one migration back (the pen column exists, the
// backfill has not run), writes the shapes an upgraded estate holds, then
// migrates forward over them. The REAL migration file runs, not a copy of its
// SQL, so the two cannot drift and deleting a statement from the file turns
// this red.
//
// The finding this closes: the pen defaults false, and without a backfill every
// row that predates the column stays inert forever. The stamp returns at its
// first line when the pen is false, and #685's bulk recompute cannot repair it
// either, since a recompute only touches rows the platform already owns. On an
// upgraded estate the whole feature would apply to rows created after the
// upgrade and to nothing else.
func TestLabelPenBackfill(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, "20260810110000"); err != nil {
		t.Fatalf("roll back below the label pen backfill: %v", err)
	}
	conn := connectDSN(t, dsn)
	ctx := context.Background()

	// A location_type to hang a location on: the seeded ones are boot-seed
	// content, and no seed has run against this database.
	// The column is `display_name` at this point in the chain: #613's rename
	// (20260814100000_label_is_the_column.sql) is above the version this test
	// stands the database at, so writing `label` here would fail on a column
	// that does not exist yet.
	mustExec(t, conn, `insert into location_type (name, official, display_name, icon, allowed_parent_types)
	                   values ('room', false, 'Room', 'door-open', '{root}')`)

	rows := []struct {
		table, name string
		label       any
		want        bool
		why         string
	}{
		{"component", "legacy-null", nil, true, "no label at all: nothing to protect, and the majority case"},
		{"component", "legacy-blank", "", true, "the empty string an old clear wrote is the same answer as NULL"},
		{"component", "legacy-typed", "Front of House", false, "the operator typed this one"},
		{"system", "sys-null", nil, true, "same rule on the system tier"},
		{"system", "sys-typed", "Main Room AV", false, "same rule on the system tier"},
		{"location", "loc-null", nil, true, "same rule on the location tier"},
		{"location", "loc-typed", "Level 3 East", false, "same rule on the location tier"},
	}
	for _, r := range rows {
		switch r.table {
		case "component":
			mustExec(t, conn, `insert into component (name, display_name, display_name_generated, product_id)
			                   values ($1, $2, false, (select id from product where name = 'generic-device'))`, r.name, r.label)
		case "system":
			mustExec(t, conn, `insert into system (name, display_name, display_name_generated) values ($1, $2, false)`, r.name, r.label)
		case "location":
			mustExec(t, conn, `insert into location (name, display_name, display_name_generated, location_type)
			                   values ($1, $2, false, (select id from location_type where name = 'room'))`, r.name, r.label)
		}
	}

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward over the pre-pen rows: %v", err)
	}
	// `label_generated` and not `display_name_generated`: the read happens AFTER
	// the migrate-forward above, which carries #613's rename with it. The writes
	// before it use the old name because they run at the older version.
	penOf := func(table, name string) bool {
		var pen bool
		if err := conn.QueryRow(ctx, `select label_generated from `+table+` where name = $1`, name).Scan(&pen); err != nil {
			t.Fatalf("read pen of %s %q: %v", table, name, err)
		}
		return pen
	}
	for _, r := range rows {
		if got := penOf(r.table, r.name); got != r.want {
			t.Errorf("pen of %s %q after backfill = %v, want %v (%s)", r.table, r.name, got, r.want, r.why)
		}
	}

	// Idempotent: a second forward run (its down is a no-op, since the column
	// migration's own down takes these values with it) changes nothing, and in
	// particular does not reach the rows an operator owns.
	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward a second time: %v", err)
	}
	for _, r := range rows {
		if got := penOf(r.table, r.name); got != r.want {
			t.Errorf("pen of %s %q after a second run = %v, want %v", r.table, r.name, got, r.want)
		}
	}
}
