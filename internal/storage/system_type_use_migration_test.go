package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/db"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The system type names a use (#2067, ADR-2034): the shipped `room` system
// type became `collaboration`. A system type's name is its seed key, and a
// system and each child type hold it by uuid, so without a carry-over the
// boot seed would insert a second row under the new name and leave every
// system typed `room` on an orphan the seed never updates again. The
// migration renames the row in place so its uuid, and everything attached
// through it, comes along.

const systemTypeUseVersion = "20261117120000"

func systemTypeUseUpSQL(t *testing.T) string {
	t.Helper()
	raw, err := db.FS.ReadFile("migrations/" + systemTypeUseVersion + "_system_type_names_a_use.sql")
	if err != nil {
		t.Fatalf("read system type rename migration: %v", err)
	}
	up, _, ok := strings.Cut(string(raw), "-- migrate:down")
	if !ok {
		t.Fatalf("system type rename migration missing '-- migrate:down' marker")
	}
	return strings.TrimPrefix(up, "-- migrate:up")
}

// An install seeded before the rename keeps its systems and its own child
// types on the same row across the upgrade: the migration renames the row,
// the next boot's seed updates it in place, and no `room` row is left behind.
func TestSystemTypeRoomBecomesCollaboration(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	conn := connectDSN(t, dsn)

	// Stand the database as a pre-rename install left it: the row under its
	// old name, a system typed by it, and an operator's own child type.
	var id string
	if err := conn.QueryRow(ctx,
		`update system_type set name = 'room', label = 'Room' where namespace = 'og' and name = 'collaboration' returning id`).Scan(&id); err != nil {
		t.Fatalf("stand collaboration under its old name: %v", err)
	}
	all := scope.Set{All: true}
	typ := "room"
	sys, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "west-collab", SystemTypeID: &typ}, all, all)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx,
		`insert into system_type (name, label, parent_id, official) values ('studio', 'Studio', $1, false)`, id); err != nil {
		t.Fatalf("add an operator child type: %v", err)
	}

	// The upgrade: the migration, then the new binary's boot seed.
	if _, err := conn.Exec(ctx, systemTypeUseUpSQL(t)); err != nil {
		t.Fatalf("apply system type rename: %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := conn.QueryRow(ctx, `select count(*) from system_type where name = 'room'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("system type room survived the upgrade")
	}
	var got, label string
	if err := conn.QueryRow(ctx,
		`select id, label from system_type where namespace = 'og' and name = 'collaboration'`).Scan(&got, &label); err != nil {
		t.Fatalf("system type collaboration missing after the upgrade: %v", err)
	}
	if got != id {
		t.Fatalf("collaboration is a new row (%s), not the renamed one (%s): its systems would be orphaned", got, id)
	}
	if label != "Collaboration" {
		t.Fatalf("the seed left the renamed row labelled %q, want Collaboration", label)
	}
	var typed string
	if err := conn.QueryRow(ctx,
		`select ty.name from system s join system_type ty on ty.id = s.system_type_id where s.id = $1`, sys.ID).Scan(&typed); err != nil {
		t.Fatal(err)
	}
	if typed != "collaboration" {
		t.Fatalf("the system is typed %q after the upgrade, want collaboration", typed)
	}
	var parent string
	if err := conn.QueryRow(ctx,
		`select p.name from system_type c join system_type p on p.id = c.parent_id where c.name = 'studio'`).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent != "collaboration" {
		t.Fatalf("the operator's child type sits under %q after the upgrade, want collaboration", parent)
	}
}

// The migration is a no-op where the new name already exists (a database the
// new seed has already run on), so it can never collide on the name's unique
// key, and re-running it changes nothing.
func TestSystemTypeUseRenameIsIdempotent(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	conn := connectDSN(t, dsn)
	var before string
	if err := conn.QueryRow(ctx, `select string_agg(id::text || name, ',' order by name) from system_type`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := conn.Exec(ctx, systemTypeUseUpSQL(t)); err != nil {
			t.Fatalf("apply system type rename: %v", err)
		}
	}
	var after string
	if err := conn.QueryRow(ctx, `select string_agg(id::text || name, ',' order by name) from system_type`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("the rename changed an already-renamed database:\n%s\n%s", before, after)
	}
}
