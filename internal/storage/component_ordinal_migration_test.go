package storage_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestComponentOrdinalBackfill proves the one-time backfill
// (20260810091000_component_ordinal_backfill.sql) against rows the migration
// set can never produce on its own: the harness migrates an empty database,
// and every component the gateway writes from here on records its ordinal at
// mint time, so running the migrations forward would exercise this UPDATE
// against an empty table and prove nothing.
//
// It stands the database one migration back (the ordinal column exists, the
// backfill has not run), writes the four shapes an upgraded fleet can hold,
// and then migrates forward over them. The REAL migration file runs, not a
// copy of its SQL, so the two cannot drift.
func TestComponentOrdinalBackfill(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, "20260810091000"); err != nil {
		t.Fatalf("roll back below the ordinal backfill: %v", err)
	}
	conn := connectDSN(t, dsn)

	insert := `insert into component (name, name_generated, product_id)
	           values ($1, $2, (select id from product where name = 'generic-device'))`
	rows := []struct {
		name      string
		generated bool
		want      string // the ordinal expected after the backfill, "" for NULL
		why       string
	}{
		{"display-3", true, "3", "a platform-owned name carries its ordinal in its digits"},
		{"orig-stem-12", true, "12", "a multi-digit ordinal, and a stem that itself contains a hyphen"},
		{"front-desk", true, "", "platform-owned but no trailing ordinal to read: left absent, never fabricated"},
		{"display-1", false, "", "operator-typed: the platform owns no number here whatever the digits say"},
		{"legacy-1234567890", true, "", "a digit run too long to be an ordinal this generator minted"},
	}
	for _, r := range rows {
		mustExec(t, conn, insert, r.name, r.generated)
	}

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward over the pre-ordinal rows: %v", err)
	}
	for _, r := range rows {
		if got := ordinalOf(t, conn, r.name); got != r.want {
			t.Errorf("ordinal of %q after backfill = %s, want %s (%s)", r.name, quoted(got), quoted(r.want), r.why)
		}
	}

	// Idempotent: rolling the backfill back (its down is deliberately a no-op,
	// since the column migration's own down takes these values with it) and
	// running it a second time over already-backfilled data changes nothing.
	// The "ordinal is null" guard is what makes that true, and this runs the
	// real file again rather than asserting the guard exists by reading it.
	if err := migrate.RollbackBelow(dsn, "20260810091000"); err != nil {
		t.Fatalf("roll the backfill back for the second run: %v", err)
	}
	mustExec(t, conn, `update component set ordinal = 99 where name = 'display-3'`)
	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("second run of the backfill: %v", err)
	}
	if got := ordinalOf(t, conn, "display-3"); got != "99" {
		t.Errorf("ordinal of display-3 after a second backfill run = %s, want 99 unchanged: the backfill is not idempotent", quoted(got))
	}
	for _, r := range rows[1:] {
		if got := ordinalOf(t, conn, r.name); got != r.want {
			t.Errorf("ordinal of %q after a second backfill run = %s, want %s unchanged", r.name, quoted(got), quoted(r.want))
		}
	}
}

// ordinalOf reads one component's ordinal as a string, "" for NULL, so a test
// can compare absence and value in one expression.
func ordinalOf(t *testing.T, conn interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, name string,
) string {
	t.Helper()
	var got *int
	if err := conn.QueryRow(context.Background(), `select ordinal from component where name = $1`, name).Scan(&got); err != nil {
		t.Fatalf("read ordinal of %q: %v", name, err)
	}
	if got == nil {
		return ""
	}
	return strconv.Itoa(*got)
}

func quoted(s string) string {
	if s == "" {
		return "NULL"
	}
	return s
}
