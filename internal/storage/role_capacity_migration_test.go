package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestDuplicateStaffingPreflightRaises proves the #626 one-role-per-component
// migration (20260807140000_role_capacity_and_positions.sql) refuses to
// upgrade an estate that already has a component filling two roles in one
// system, rather than aborting mid-migration on an unnamed 23505: stand the
// database at the schema immediately before that migration, write the
// violating pair directly, and migrate forward over it.
func TestDuplicateStaffingPreflightRaises(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, "20260807140000"); err != nil {
		t.Fatalf("rollback below the role capacity migration: %v", err)
	}
	conn := connectDSN(t, dsn)
	ctx := context.Background()

	var sysID, compID, roleAID, roleBID string
	if err := conn.QueryRow(ctx, `insert into system (name) values ('dup-sys') returning id`).Scan(&sysID); err != nil {
		t.Fatalf("system: %v", err)
	}
	if err := conn.QueryRow(ctx,
		`insert into component (name, product_id) values ('dup-comp', (select id from product where name = 'generic-device')) returning id`,
	).Scan(&compID); err != nil {
		t.Fatalf("component: %v", err)
	}
	if err := conn.QueryRow(ctx,
		`insert into system_role (owner_kind, system_id, name, display_name) values ('system', $1, 'role-a', 'Role A') returning id`,
		sysID).Scan(&roleAID); err != nil {
		t.Fatalf("role a: %v", err)
	}
	if err := conn.QueryRow(ctx,
		`insert into system_role (owner_kind, system_id, name, display_name) values ('system', $1, 'role-b', 'Role B') returning id`,
		sysID).Scan(&roleBID); err != nil {
		t.Fatalf("role b: %v", err)
	}
	mustExec(t, conn, `insert into system_role_assignment (role_id, component_id, system_id) values ($1, $2, $3)`, roleAID, compID, sysID)
	mustExec(t, conn, `insert into system_role_assignment (role_id, component_id, system_id) values ($1, $2, $3)`, roleBID, compID, sysID)

	err := migrate.Run(dsn)
	if err == nil {
		t.Fatal("migrate over a component filling two roles in one system succeeded, want a loud refusal")
	}
	if !strings.Contains(err.Error(), "dup-sys/dup-comp") {
		t.Errorf("refusal error = %v, want it to name dup-sys/dup-comp", err)
	}
}

// backfillSQL is copied verbatim from the UPDATE statement in
// db/migrations/20260807143000_assignment_position_backfill.sql, extracted
// so the two cannot drift silently: if the migration's SQL ever changes,
// this constant must change with it or the test below stops proving what
// actually runs during an upgrade.
const backfillSQL = `
with numbered as (
  select id, row_number() over (partition by system_id, role_id order by created_at, id) as n
  from system_role_assignment
  where position is null
)
update system_role_assignment sra
   set position = numbered.n
  from numbered
 where sra.id = numbered.id`

// TestPositionBackfillIdempotent proves the backfill statement itself: the
// storagetest harness migrates an empty database (the boot seed creates zero
// role assignments and devseed's fixtures are written by AssignRole AFTER
// migration completes), so running the migration set forward can never
// exercise this UPDATE against a non-empty table. This test stands the
// database at the schema right after the position column exists but before
// the floor migration enforces NOT NULL, writes three assignment rows with
// NULL position in a known creation order, and runs the extracted SQL twice
// directly: positions 1, 2, 3 after the first run, unchanged after the
// second.
func TestPositionBackfillIdempotent(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, "20260807146000"); err != nil {
		t.Fatalf("rollback below the position floor: %v", err)
	}
	conn := connectDSN(t, dsn)
	ctx := context.Background()

	var sysID, roleID string
	if err := conn.QueryRow(ctx, `insert into system (name) values ('backfill-sys') returning id`).Scan(&sysID); err != nil {
		t.Fatalf("system: %v", err)
	}
	if err := conn.QueryRow(ctx,
		`insert into system_role (owner_kind, system_id, name, display_name) values ('system', $1, 'table-mic', 'Table Mic') returning id`,
		sysID).Scan(&roleID); err != nil {
		t.Fatalf("role: %v", err)
	}

	var assignmentIDs []string
	for _, name := range []string{"one", "two", "three"} {
		var compID, assignID string
		if err := conn.QueryRow(ctx,
			`insert into component (name, product_id) values ($1, (select id from product where name = 'generic-device')) returning id`,
			name).Scan(&compID); err != nil {
			t.Fatalf("component %s: %v", name, err)
		}
		// created_at defaults to now() (transaction start time in a single
		// exec), so successive inserts here still tie; id (uuidv7) is the
		// tie-break, and separate Exec calls issue strictly increasing
		// uuidv7 ids in the order they run.
		if err := conn.QueryRow(ctx,
			`insert into system_role_assignment (role_id, component_id, system_id) values ($1, $2, $3) returning id`,
			roleID, compID, sysID).Scan(&assignID); err != nil {
			t.Fatalf("assignment %s: %v", name, err)
		}
		assignmentIDs = append(assignmentIDs, assignID)
	}

	positionsByID := func() map[string]int {
		rows, err := conn.Query(ctx, `select id, position from system_role_assignment where system_id = $1`, sysID)
		if err != nil {
			t.Fatalf("read positions: %v", err)
		}
		defer rows.Close()
		out := map[string]int{}
		for rows.Next() {
			var id string
			var pos *int
			if err := rows.Scan(&id, &pos); err != nil {
				t.Fatalf("scan position: %v", err)
			}
			if pos != nil {
				out[id] = *pos
			}
		}
		return out
	}

	mustExec(t, conn, backfillSQL)
	got := positionsByID()
	for i, id := range assignmentIDs {
		want := i + 1
		if got[id] != want {
			t.Errorf("assignment %d position = %v, want %d (creation order)", i, got[id], want)
		}
	}

	// Re-running must change nothing: the "where position is null" guard
	// makes the second pass a no-op.
	mustExec(t, conn, backfillSQL)
	gotAgain := positionsByID()
	for i, id := range assignmentIDs {
		if gotAgain[id] != got[id] {
			t.Errorf("assignment %d position after a second backfill = %v, want unchanged at %v", i, gotAgain[id], got[id])
		}
	}
}
