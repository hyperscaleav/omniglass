package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A service principal's identifier (#563). The column is the username analogue
// for kind=service: the only operator-visible handle the row has, denormalized
// as bare text into the audit trail and into an alarm's acknowledgement. So it
// is a `name`, and it is unique for the same reason `human.username` is.
//
// Both tests write the profile rows directly. That is not a shortcut around the
// gateway: there is no create path for a service principal anywhere in the
// platform, which is itself why the uniqueness question had never been forced.

// TestAServicePrincipalReadsBackItsName is the round trip through both principal
// read paths: the single-row load and the batched directory load, which are
// separate implementations of the same fact (#671) and drift apart if only one
// is driven.
func TestAServicePrincipalReadsBackItsName(t *testing.T) {
	gw, dsn := secretGateway(t)
	ctx := context.Background()
	id := insertServicePrincipal(t, dsn, "ingest-bot")

	pr, err := gw.GetPrincipal(ctx, id, scope.Set{All: true})
	if err != nil {
		t.Fatalf("get principal: %v", err)
	}
	if pr.Service == nil {
		t.Fatal("a service principal came back with no service profile")
	}
	if pr.Service.Name != "ingest-bot" {
		t.Errorf("the single-row read names the service %q, want %q", pr.Service.Name, "ingest-bot")
	}

	prs, err := gw.ListPrincipals(ctx, scope.Set{All: true}, false)
	if err != nil {
		t.Fatalf("list principals: %v", err)
	}
	var found *storage.Principal
	for i := range prs {
		if prs[i].ID == id {
			found = &prs[i]
		}
	}
	if found == nil {
		t.Fatal("the service principal is missing from the directory")
	}
	if found.Service == nil || found.Service.Name != "ingest-bot" {
		t.Errorf("the batched directory read names the service %+v, want %q", found.Service, "ingest-bot")
	}
}

// TestTwoServicePrincipalsCannotShareAName is the uniqueness floor. The string
// is written into audit_log as bare text so the trail survives a purge, and into
// an alarm's acknowledgement on every read; a duplicate makes both of those
// unresolvable after the fact, with no uuid left beside the text to tell them
// apart.
func TestTwoServicePrincipalsCannotShareAName(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	insertServicePrincipal(t, dsn, "ingest-bot")

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var pid string
	if err := conn.QueryRow(ctx,
		`insert into principal (kind) values ('service') returning id`).Scan(&pid); err != nil {
		t.Fatalf("insert principal: %v", err)
	}
	_, err = conn.Exec(ctx, `insert into service (principal_id, name) values ($1, 'ingest-bot')`, pid)
	if err == nil {
		t.Fatal("two service principals took the same name: the identifier does not identify")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("the second insert failed with %v, want a unique violation (23505)", err)
	}

	// A DIFFERENT name is still fine, so the constraint is on the column and not
	// on the table.
	if _, err := conn.Exec(ctx,
		`insert into service (principal_id, name) values ($1, 'ingest-bot-2')`, pid); err != nil {
		t.Fatalf("a second service account under its own name was refused: %v", err)
	}
}

// insertServicePrincipal creates a service principal and its profile row and
// returns the principal id.
func insertServicePrincipal(t *testing.T, dsn, name string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var pid string
	if err := conn.QueryRow(ctx,
		`insert into principal (kind) values ('service') returning id`).Scan(&pid); err != nil {
		t.Fatalf("insert principal: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`insert into service (principal_id, name) values ($1, $2)`, pid, name); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	return pid
}

// dedupeSQL is copied verbatim from the UPDATE in
// db/migrations/20260813171000_service_name_dedupe.sql, extracted so the two
// cannot drift silently: if the migration's SQL changes, this constant must
// change with it or the test below stops proving what actually runs during an
// upgrade.
const dedupeSQL = `
UPDATE service s
   SET name = s.name || '-' || s.principal_id::text
 WHERE EXISTS (
         SELECT 1 FROM service other
          WHERE other.name = s.name
            AND other.principal_id < s.principal_id
       )`

// TestServiceNameDedupeKeepsTheOldestAndIsIdempotent proves the backfill that
// stands between the rename and the uniqueness floor.
//
// The harness migrates an empty database, so running the chain forward can
// never exercise this UPDATE against duplicates: the acceptance behaviour
// "existing rows migrate with no operator action" would otherwise be a claim
// with nothing behind it. This stands the schema right after the rename and
// before the floor, writes three service accounts sharing one name in a known
// creation order, and runs the extracted SQL twice.
func TestServiceNameDedupeKeepsTheOldestAndIsIdempotent(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, "20260813172000"); err != nil {
		t.Fatalf("rollback below the service name floor: %v", err)
	}
	conn := connectDSN(t, dsn)
	ctx := context.Background()

	// Three under one name plus one under another, so the sweep is shown to
	// leave an unduplicated row alone as well as to disambiguate a duplicated
	// one. Separate Execs issue strictly increasing uuidv7 ids, so insertion
	// order IS creation order.
	var dupes []string
	for range 3 {
		var pid string
		if err := conn.QueryRow(ctx,
			`insert into principal (kind) values ('service') returning id`).Scan(&pid); err != nil {
			t.Fatalf("insert principal: %v", err)
		}
		mustExec(t, conn, `insert into service (principal_id, name) values ($1, 'ingest')`, pid)
		dupes = append(dupes, pid)
	}
	var lone string
	if err := conn.QueryRow(ctx,
		`insert into principal (kind) values ('service') returning id`).Scan(&lone); err != nil {
		t.Fatalf("insert principal: %v", err)
	}
	mustExec(t, conn, `insert into service (principal_id, name) values ($1, 'lonely')`, lone)

	nameOf := func(pid string) string {
		var n string
		if err := conn.QueryRow(ctx, `select name from service where principal_id = $1`, pid).Scan(&n); err != nil {
			t.Fatalf("read name: %v", err)
		}
		return n
	}

	mustExec(t, conn, dedupeSQL)
	if got := nameOf(dupes[0]); got != "ingest" {
		t.Errorf("the oldest duplicate is now called %q, want %q: the account an operator has been calling by that name for longest is the one that keeps it", got, "ingest")
	}
	for i, pid := range dupes[1:] {
		want := "ingest-" + pid
		if got := nameOf(pid); got != want {
			t.Errorf("duplicate %d is now called %q, want %q", i+1, got, want)
		}
	}
	if got := nameOf(lone); got != "lonely" {
		t.Errorf("an unduplicated account was renamed to %q; the sweep touches only what collides", got)
	}

	// The floor can now land, which is the whole point of running the backfill
	// first, and it is the strongest statement that the disambiguation worked.
	mustExec(t, conn, `alter table service add constraint service_name_key unique (name)`)

	// And a re-run changes nothing: there are no duplicates left to find.
	before := map[string]string{}
	for _, pid := range append(append([]string{}, dupes...), lone) {
		before[pid] = nameOf(pid)
	}
	mustExec(t, conn, dedupeSQL)
	for pid, was := range before {
		if now := nameOf(pid); now != was {
			t.Errorf("a second run renamed %s from %q to %q; the backfill is not idempotent", pid, was, now)
		}
	}
}
