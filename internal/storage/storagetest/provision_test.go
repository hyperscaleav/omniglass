package storagetest

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hyperscaleav/omniglass/db"
	"github.com/jackc/pgx/v5"
)

// wantTemplatePrefix is what the harness must build its template database
// under. Spelled out here rather than read from templateName so the test states
// the contract instead of restating the implementation.
//
// It is a PREFIX rather than the whole name since #662, and the change IS the
// contract: a fixed `og_template` was safe only because every binary had a
// Postgres to itself, and on the OMNIGLASS_TEST_ADMIN_DSN hatch (an
// already-running Postgres, for environments with no Docker daemon) it let one
// binary drop and rebuild the template another was copying from. The name now
// carries this process's pid and a random half, so a test can state the prefix
// and that exactly one database matches it, which is the property, but not the
// whole literal.
var wantTemplatePrefix = fmt.Sprintf("og_tmpl_%d_", os.Getpid())

// oneTemplateOfThisProcess is the template this binary built, found by the
// prefix above, and it fails if the count is anything but one: a second match
// would mean the harness minted two templates for one process, which is the
// half of the naming that a prefix assertion alone would not catch.
func oneTemplateOfThisProcess(t *testing.T, ctx context.Context, admin *pgx.Conn) string {
	t.Helper()
	rows, err := admin.Query(ctx,
		`select datname from pg_database where datname like $1 || '%' order by datname`, wantTemplatePrefix)
	if err != nil {
		t.Fatalf("list this process's templates: %v", err)
	}
	defer rows.Close()
	var found []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan template name: %v", err)
		}
		found = append(found, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate template names: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("databases named %s* = %v, want exactly one: the template is per process", wantTemplatePrefix, found)
	}
	return found[0]
}

// TestProvisionIsolatesWrites is the property the harness exists to provide:
// one test's rows are invisible to the next test's database. Provisioning from
// a template rather than replaying migrations is the change most likely to
// break it, either by handing two tests the same database or by letting a
// write reach the template every later database is copied from, so the write
// happens between the two provisions and the second database must not see it.
func TestProvisionIsolatesWrites(t *testing.T) {
	ctx := context.Background()
	first := NewDSN(t)

	const probe = "isolation-probe-649"
	firstConn := connect(t, ctx, first)
	if _, err := firstConn.Exec(ctx,
		`insert into location_type (name, label) values ($1, $1)`, probe); err != nil {
		t.Fatalf("insert probe row: %v", err)
	}

	// Provision AFTER the write, so a write that leaked into the template would
	// be copied into this database.
	second := NewDSN(t)
	if first == second {
		t.Fatalf("two provisions returned the same DSN %q; databases are not isolated", first)
	}
	secondConn := connect(t, ctx, second)

	if got := probeCount(t, ctx, firstConn, probe); got != 1 {
		t.Errorf("probe row count in the database it was written to = %d, want 1", got)
	}
	if got := probeCount(t, ctx, secondConn, probe); got != 0 {
		t.Errorf("probe row count in a freshly provisioned database = %d, want 0", got)
	}
}

// TestProvisionCarriesFullSchema proves a provisioned database is
// indistinguishable from a migrated one, including to dbmate: every embedded
// migration is recorded as applied. A copy that silently landed an empty or
// partial schema turns this red, and so does a template built from a stale
// migration set. The expected count is derived from the embedded migration set
// rather than hand-written, so it cannot drift as migrations land.
func TestProvisionCarriesFullSchema(t *testing.T) {
	ctx := context.Background()
	conn := connect(t, ctx, NewDSN(t))

	entries, err := db.FS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	want := len(entries)
	if want == 0 {
		t.Fatal("embedded migration set is empty; the test would assert nothing")
	}

	var got int
	if err := conn.QueryRow(ctx, `select count(*) from schema_migrations`).Scan(&got); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if got != want {
		t.Errorf("schema_migrations rows = %d, want %d (one per embedded migration)", got, want)
	}
}

// TestTemplateRefusesConnections is the direct regression test for the
// hardening that makes CREATE DATABASE ... TEMPLATE deterministic. internal/
// migrate builds a dbmate.DB it never explicitly closes, so the harness must
// terminate stray backends and then set allow_connections = false rather than
// assume dbmate left nothing behind. A template that still accepts connections
// copies correctly only by timing, which is the flake this closes.
func TestTemplateRefusesConnections(t *testing.T) {
	ctx := context.Background()
	NewDSN(t) // force the container and the template into existence

	admin := connect(t, ctx, adminDSN)
	name := oneTemplateOfThisProcess(t, ctx, admin)
	var isTemplate, allowConn bool
	err := admin.QueryRow(ctx,
		`select datistemplate, datallowconn from pg_database where datname = $1`,
		name).Scan(&isTemplate, &allowConn)
	if err != nil {
		t.Fatalf("look up template database %s: %v", name, err)
	}
	if !isTemplate {
		t.Errorf("%s datistemplate = false, want true", name)
	}
	if allowConn {
		t.Errorf("%s datallowconn = true, want false: a connectable template can race a copy", name)
	}
}

// TestProvisionUnderRepetition provisions in a tight loop, which is how the
// suite actually uses the harness: hundreds of back-to-back copies from one
// template. Every one must succeed. Without the hardening above this is where
// "source database is being accessed by other users" surfaces.
func TestProvisionUnderRepetition(t *testing.T) {
	ctx := context.Background()
	const rounds = 20
	seen := make(map[string]bool, rounds)
	for i := range rounds {
		dsn := NewDSN(t)
		if seen[dsn] {
			t.Fatalf("round %d reused DSN %q", i, dsn)
		}
		seen[dsn] = true
		conn := connect(t, ctx, dsn)
		var n int
		if err := conn.QueryRow(ctx, `select count(*) from schema_migrations`).Scan(&n); err != nil {
			t.Fatalf("round %d: count schema_migrations: %v", i, err)
		}
		if n == 0 {
			t.Fatalf("round %d: provisioned database has no applied migrations", i)
		}
	}
}

// TestTemplateBuildFailureIsNotCached pins the blast radius of a transient
// failure. Building the template is per-binary work, so caching its error would
// fail every remaining test in the binary from one hiccup: the harness has been
// seen to hit a refused admin connection when the container flaps, and before
// the template existed that cost exactly one test, because each test connected
// and migrated on its own. Only success may be cached.
//
// The failure is forced the way the real one arrives, by pointing the harness
// at an endpoint that refuses connections. This mutates harness state, so it
// restores it and must not run alongside a t.Parallel test.
func TestTemplateBuildFailureIsNotCached(t *testing.T) {
	if testing.Short() {
		t.Skip("storagetest: skipped under -short (Postgres testcontainer)")
	}
	NewDSN(t) // container up, template built and sealed

	good := adminDSN
	t.Cleanup(func() {
		// Leave the harness usable for later tests whatever happens below. A
		// false templateReady only costs the next caller one rebuild.
		adminDSN = good
		templateReady = false
	})

	// Nothing listens on port 1, so the admin connect is refused outright.
	adminDSN = "postgres://omniglass:omniglass@127.0.0.1:1/postgres?sslmode=disable"
	templateReady = false

	if err := ensureTemplate(); err == nil {
		t.Fatal("ensureTemplate returned nil against an endpoint that refuses connections")
	}
	if templateReady {
		t.Fatal("a failed build marked the template ready")
	}

	// The property under test: the very next call retries rather than replaying
	// the stored error. This also drives the retry over a template the previous
	// successful build left sealed, which is only droppable because the seal is
	// undone first.
	adminDSN = good
	if err := ensureTemplate(); err != nil {
		t.Fatalf("ensureTemplate did not recover after a transient failure: %v", err)
	}
	if !templateReady {
		t.Fatal("a successful rebuild did not mark the template ready")
	}
	recovered := context.Background()
	conn := connect(t, recovered, NewDSN(t))
	var n int
	if err := conn.QueryRow(recovered, `select count(*) from schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("count schema_migrations after recovery: %v", err)
	}
	if n == 0 {
		t.Fatal("the rebuilt template provisioned a database with no applied migrations")
	}
}

func connect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func probeCount(t *testing.T, ctx context.Context, conn *pgx.Conn, name string) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(ctx,
		`select count(*) from location_type where name = $1`, name).Scan(&n); err != nil {
		t.Fatalf("count probe rows: %v", err)
	}
	return n
}
