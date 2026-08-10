// Package storagetest provides the shared real-Postgres test harness. Every
// integration test runs against an ephemeral, fully isolated database: one
// container is started lazily per test binary, and each NewDB call creates a
// fresh database, so tests never share mutable state and never collide on a
// host port.
//
// Both pieces of per-binary work, the container and the template, cache their
// success and never their failure, so a transient hiccup costs the one test
// that hit it rather than every test after it.
//
// The migration chain runs once per test binary, into a template database, and
// each test's database is a CREATE DATABASE ... TEMPLATE copy of it. A copy is
// a file-level clone rather than a replay of every migration, and it carries
// schema_migrations with it, so a provisioned database is indistinguishable
// from a migrated one, including to dbmate. Isolation is unchanged: every test
// still gets its own database.
//
// The container is reclaimed by [Main], which every consuming package must run
// from its TestMain so cleanup happens in-process on normal exit. The
// testcontainers reaper (ryuk) is only a backstop for hard kills; it cannot be
// relied on alone (it is disabled or torn down early in some environments, for
// example Docker Desktop on WSL2), which is why teardown lives in the harness.
// That requirement is enforced rather than documented: [NewDSN] refuses to hand
// out a database to a test binary that is not running under Main.
//
// There is no in-memory double. The doctrine is: do not mock the database.
package storagetest

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/config"
	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// templateName is the database the migration chain is applied to once per test
// binary. Every test database is a copy of it.
const templateName = "og_template"

var (
	ctr       *tcpostgres.PostgresContainer // shared container, terminated by Main
	adminDSN  string                        // DSN to the container's default db, for CREATE DATABASE
	dbCounter atomic.Int64

	containerMu    sync.Mutex
	containerReady bool

	templateMu    sync.Mutex
	templateReady bool

	// mainRan records that this test binary is running under Main, the only
	// thing that reclaims the shared container in-process. Set before m.Run, so
	// every test goroutine it spawns observes it; atomic anyway, because a
	// harness that is itself racy is worse than no harness.
	mainRan atomic.Bool
)

// reclaimGuard refuses to provision a database for a test binary that is not
// running under [Main].
//
// Main is the only thing that terminates the shared container on normal exit,
// and the testcontainers reaper cannot be trusted to finish the job here, so a
// consuming package that forgets its TestMain leaks a Postgres for the life of
// the machine. The package comment has said so since the harness was written,
// and three packages had drifted off it by the time anyone counted the strays:
// a doc comment is not a mechanism.
//
// The check sits exactly on the defect. The call that would otherwise start an
// unreclaimed container is the call that fails, inside the offending package,
// with the fix in the message.
func reclaimGuard() error {
	if mainRan.Load() {
		return nil
	}
	return errors.New("storagetest: this package uses the Postgres harness but does not run its tests " +
		"under storagetest.Main, so the container it starts would never be reclaimed. " +
		"Add a TestMain to the package:\n\n" +
		"\tfunc TestMain(m *testing.M) { os.Exit(storagetest.Main(m)) }")
}

// runPostgres is the harness's one call to Docker. It is a var so the harness's
// own tests can drive the failure shapes real Docker will not produce on
// demand, above all a start that hands back a container ALONGSIDE an error,
// which is the shape that leaks. Everything else, in every consuming package,
// gets runPostgresContainer.
var runPostgres = runPostgresContainer

func runPostgresContainer(ctx context.Context) (*tcpostgres.PostgresContainer, error) {
	return tcpostgres.Run(ctx, "postgres:18",
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("omniglass"),
		tcpostgres.WithPassword("omniglass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
}

// startContainer starts one ephemeral Postgres container and returns it with
// the admin DSN to its default database. It is the capability primitive behind
// the shared harness: the single place that talks to Docker, isolated so the
// start-and-terminate lifecycle is directly testable.
//
// On any error it returns a nil container, having reclaimed whatever it was
// handed. That is a contract its callers depend on, not a detail.
func startContainer(ctx context.Context) (*tcpostgres.PostgresContainer, string, error) {
	c, err := runPostgres(ctx)
	if err != nil {
		// testcontainers returns the container alongside the error when the
		// create succeeded and the start or the wait strategy did not, so the
		// caller can read its logs. Dropping it here leaks a live Postgres, and
		// since a failed start is retried rather than cached, it would leak one
		// per attempt. TerminateContainer is nil-safe, so this covers both shapes.
		_ = testcontainers.TerminateContainer(c)
		return nil, "", err
	}
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = testcontainers.TerminateContainer(c)
		return nil, "", err
	}
	return c, dsn, nil
}

// ensureContainer starts the shared container once per test binary, returning
// the error rather than storing it.
//
// Success is cached; failure deliberately is not, for exactly the reason
// [ensureTemplate] caches only success. A sync.Once here remembered a transient
// start failure for the life of the binary and replayed it into every later
// caller, so one hiccup on a box where the container is known to flap failed
// every remaining test in the package rather than the one that hit it. Worse
// than the cost, it buried the diagnostic under hundreds of identical cached
// errors. Returning the error lets the next test retry. The mutex serializes
// the start so two callers cannot race two containers into existence, which
// would leak whichever one lost.
func ensureContainer() error {
	containerMu.Lock()
	defer containerMu.Unlock()
	if containerReady {
		return nil
	}
	// Escape hatch for environments without a Docker daemon (some CI runners,
	// sandboxes): OMNIGLASS_TEST_ADMIN_DSN points the harness at an already
	// running Postgres (its default/admin database), and NewDSN creates and
	// migrates a fresh, isolated database per test on it exactly as it does on
	// the container. Unset (the default), the harness starts an ephemeral
	// testcontainer, so nothing changes for a normal `make test` with Docker.
	if dsn := config.Get("OMNIGLASS_TEST_ADMIN_DSN"); dsn != "" {
		adminDSN = dsn
		containerReady = true
		return nil
	}
	c, dsn, err := startContainer(context.Background())
	if err != nil {
		// Nothing is recorded on this path, which is what makes the retry safe:
		// startContainer reclaims whatever it was handed before returning an
		// error, so there is no half-started container for ctr to point at.
		return err
	}
	ctr, adminDSN = c, dsn
	containerReady = true
	return nil
}

// ensureTemplate starts the container and builds the template database once per
// test binary, returning the error rather than storing it.
//
// Success is cached; failure deliberately is not, the same rule
// [ensureContainer] follows. A sync.Once here would remember a transient
// failure forever and fail every remaining test in the binary from it, turning
// a one-test blip into a whole-binary wipeout. Before the template existed each
// test connected and migrated on its own, so a hiccup cost exactly one test;
// retrying on the next call keeps that blast radius. The mutex serializes the
// build so t.Parallel tests cannot race two of them.
func ensureTemplate() error {
	templateMu.Lock()
	defer templateMu.Unlock()
	if templateReady {
		return nil
	}
	if err := ensureContainer(); err != nil {
		return fmt.Errorf("start postgres container: %w", err)
	}
	if err := buildTemplate(context.Background()); err != nil {
		return fmt.Errorf("build template database: %w", err)
	}
	templateReady = true
	return nil
}

// buildTemplate applies the whole migration chain to the template database and
// then seals it. It is the single expensive step of the harness, paid once per
// test binary instead of once per test.
func buildTemplate(ctx context.Context) error {
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return fmt.Errorf("admin connect: %w", err)
	}
	// Two ways a template can already be here, both handled by recreating it
	// rather than reusing it. The OMNIGLASS_TEST_ADMIN_DSN escape hatch points
	// the harness at a long-lived Postgres, where a template from an earlier run
	// survives and may carry a different migration set. And because a failed
	// build is retried by the next test rather than cached, this can be a second
	// attempt over a template the first one left half-built, migrated but
	// unsealed, or fully sealed. A stale or partial template is a wrong schema
	// for every test in the binary, so the only safe move is to start over.
	if err := dropTemplate(ctx, admin); err != nil {
		_ = admin.Close(ctx)
		return err
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{templateName}.Sanitize()); err != nil {
		_ = admin.Close(ctx)
		return fmt.Errorf("create template %s: %w", templateName, err)
	}
	if err := admin.Close(ctx); err != nil {
		return fmt.Errorf("close admin connection: %w", err)
	}

	if err := migrate.Run(withDBName(adminDSN, templateName)); err != nil {
		return fmt.Errorf("migrate template %s: %w", templateName, err)
	}
	return sealTemplate(ctx)
}

// sealTemplate closes the template to connections so copying from it can never
// race one. internal/migrate builds a dbmate.DB it never explicitly closes, so
// the harness cannot assume dbmate left no session behind: it terminates
// whatever is still attached and then refuses connections outright. With
// allow_connections false the template cannot be connected to at all, which is
// what makes CREATE DATABASE ... TEMPLATE deterministic. Retrying on "source
// database is being accessed by other users" would only turn a deterministic
// failure into an intermittent one.
func sealTemplate(ctx context.Context) error {
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return fmt.Errorf("admin connect: %w", err)
	}
	defer func() { _ = admin.Close(ctx) }()

	if _, err := admin.Exec(ctx,
		`select pg_terminate_backend(pid) from pg_stat_activity
		  where datname = $1 and pid <> pg_backend_pid()`, templateName); err != nil {
		return fmt.Errorf("terminate backends on %s: %w", templateName, err)
	}
	if _, err := admin.Exec(ctx, "alter database "+pgx.Identifier{templateName}.Sanitize()+
		" with is_template = true allow_connections = false"); err != nil {
		return fmt.Errorf("seal template %s: %w", templateName, err)
	}
	return nil
}

// dropTemplate removes a template database left by an earlier run or by a
// failed build, if there is one. Postgres refuses to drop a database while its
// template flag is set, and a sealed template cannot be connected to at all, so
// the seal is undone first. ALTER DATABASE does not require a connection to its
// target, which is what makes a sealed template droppable.
func dropTemplate(ctx context.Context, admin *pgx.Conn) error {
	var exists bool
	if err := admin.QueryRow(ctx,
		"select exists (select 1 from pg_database where datname = $1)",
		templateName).Scan(&exists); err != nil {
		return fmt.Errorf("look up template %s: %w", templateName, err)
	}
	if !exists {
		return nil
	}
	if _, err := admin.Exec(ctx, "alter database "+pgx.Identifier{templateName}.Sanitize()+
		" with is_template = false allow_connections = true"); err != nil {
		return fmt.Errorf("unseal stale template %s: %w", templateName, err)
	}
	if _, err := admin.Exec(ctx,
		`select pg_terminate_backend(pid) from pg_stat_activity
		  where datname = $1 and pid <> pg_backend_pid()`, templateName); err != nil {
		return fmt.Errorf("terminate backends on stale %s: %w", templateName, err)
	}
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{templateName}.Sanitize()); err != nil {
		return fmt.Errorf("drop stale template %s: %w", templateName, err)
	}
	return nil
}

// NewDSN returns the DSN of a fresh, migrated, isolated Postgres database.
// Skipped under -short. The database is a copy of the per-binary template, so
// it arrives with the full schema and the full schema_migrations history
// without replaying a single migration. It is discarded when the shared
// container is reaped on process exit. Use this when the test needs the raw DSN
// (e.g. to launch the server binary against it).
//
// It refuses outright if the calling package's tests are not running under
// [Main], because the container it would start would then never be reclaimed.
//
// The parameter is testing.TB rather than *testing.T so a BENCHMARK can
// provision an estate to measure against (#651). Nothing else changes: every
// method used here (Helper, Skip, Fatal, Cleanup) is on TB, and a *testing.T
// caller compiles unchanged.
func NewDSN(t testing.TB) string {
	t.Helper()
	// Ahead of the -short skip deliberately. Reclaiming the container is a
	// property of the consuming PACKAGE, true whether or not this particular run
	// provisions anything, so `make test-short` catches a missing TestMain too
	// rather than waiting for the full gate to leak one.
	if err := reclaimGuard(); err != nil {
		t.Fatal(err)
	}
	if testing.Short() {
		t.Skip("storage: skipped under -short (Postgres testcontainer)")
	}
	if err := ensureTemplate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	dbName := fmt.Sprintf("og_test_%d", dbCounter.Add(1))
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	_, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{dbName}.Sanitize()+
		" TEMPLATE "+pgx.Identifier{templateName}.Sanitize())
	_ = admin.Close(ctx)
	if err != nil {
		t.Fatalf("create database %s from template %s: %v", dbName, templateName, err)
	}
	return withDBName(adminDSN, dbName)
}

// NewDB returns a Gateway backed by a fresh, migrated, isolated database.
// Skipped under -short. The gateway is closed on test cleanup.
func NewDB(t testing.TB) storage.Gateway {
	t.Helper()
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, NewDSN(t))
	if err != nil {
		t.Fatalf("storage.NewPG: %v", err)
	}
	t.Cleanup(gw.Close)
	return gw
}

// NewCountingDB returns a Gateway backed by a fresh, migrated, isolated database
// whose pool counts every statement it issues, together with the counter. This
// is the seam a round-trip assertion on a gateway READ needs: the scoped read
// paths query the pool directly (scopedList reaches for p.pool, and the exported
// List methods take a scope.Set, not a querier), so a counter wrapped around
// some argument would observe nothing at all and report a flat, fictional
// number. See [querycount] for that hazard in full; it is the reason this
// constructor exists rather than a wrapper.
//
// The counter is reset before it is returned, so the pool's own connectivity
// ping is not charged to the first measurement. Reset it again between
// measurements: it counts everything the gateway does, fixture writes included.
func NewCountingDB(t testing.TB) (storage.Gateway, *querycount.Counter) {
	t.Helper()
	counter := querycount.New()
	gw, err := storage.NewPG(context.Background(), NewDSN(t), storage.WithQueryTracer(counter))
	if err != nil {
		t.Fatalf("storage.NewPG (counting): %v", err)
	}
	t.Cleanup(gw.Close)
	counter.Reset()
	return gw, counter
}

func withDBName(dsn, db string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	u.Path = "/" + db
	return u.String()
}

// Main runs a package's tests and then terminates the shared Postgres
// container, if one was started. Every package that uses this harness must
// route its tests through Main from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(storagetest.Main(m)) }
//
// This reclaims the container in-process on normal exit, independent of the
// testcontainers reaper. Main returns the exit code to pass to os.Exit.
//
// Running under Main is not advisory: it is what [NewDSN]'s reclaim guard
// checks, and a package that skips it fails on its first provision.
func Main(m *testing.M) int {
	mainRan.Store(true)
	code := m.Run()
	if err := testcontainers.TerminateContainer(ctr); err != nil {
		fmt.Fprintf(os.Stderr, "storagetest: terminate container: %v\n", err)
	}
	return code
}
