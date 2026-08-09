// Package storagetest provides the shared real-Postgres test harness. Every
// integration test runs against an ephemeral, fully isolated database: one
// container is started lazily per test binary (sync.Once), and each NewDB call
// creates a fresh database, so tests never share mutable state and never
// collide on a host port.
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
//
// There is no in-memory double. The doctrine is: do not mock the database.
package storagetest

import (
	"context"
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
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// templateName is the database the migration chain is applied to once per test
// binary. Every test database is a copy of it.
const templateName = "og_template"

var (
	startOnce sync.Once
	ctr       *tcpostgres.PostgresContainer // shared container, terminated by Main
	adminDSN  string                        // DSN to the container's default db, for CREATE DATABASE
	startErr  error
	dbCounter atomic.Int64

	templateOnce sync.Once
	templateErr  error
)

// startContainer starts one ephemeral Postgres container and returns it with
// the admin DSN to its default database. It is the capability primitive behind
// the shared harness: the single place that talks to Docker, isolated so the
// start-and-terminate lifecycle is directly testable.
func startContainer(ctx context.Context) (*tcpostgres.PostgresContainer, string, error) {
	c, err := tcpostgres.Run(ctx, "postgres:18",
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("omniglass"),
		tcpostgres.WithPassword("omniglass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		return nil, "", err
	}
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = testcontainers.TerminateContainer(c)
		return nil, "", err
	}
	return c, dsn, nil
}

func ensureContainer() {
	startOnce.Do(func() {
		// Escape hatch for environments without a Docker daemon (some CI runners,
		// sandboxes): OMNIGLASS_TEST_ADMIN_DSN points the harness at an already
		// running Postgres (its default/admin database), and NewDSN creates and
		// migrates a fresh, isolated database per test on it exactly as it does on
		// the container. Unset (the default), the harness starts an ephemeral
		// testcontainer, so nothing changes for a normal `make test` with Docker.
		if dsn := config.Get("OMNIGLASS_TEST_ADMIN_DSN"); dsn != "" {
			adminDSN = dsn
			return
		}
		ctr, adminDSN, startErr = startContainer(context.Background())
	})
}

// ensureTemplate starts the container and builds the template database once
// per test binary, following the ensureContainer idiom: the work happens under
// a sync.Once and the outcome is read from templateErr.
func ensureTemplate() {
	templateOnce.Do(func() {
		ensureContainer()
		if startErr != nil {
			return // NewDSN reports startErr; nothing to build against
		}
		templateErr = buildTemplate(context.Background())
	})
}

// buildTemplate applies the whole migration chain to the template database and
// then seals it. It is the single expensive step of the harness, paid once per
// test binary instead of once per test.
func buildTemplate(ctx context.Context) error {
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return fmt.Errorf("admin connect: %w", err)
	}
	// The OMNIGLASS_TEST_ADMIN_DSN escape hatch points the harness at a
	// long-lived Postgres, where a template from an earlier run can still be
	// present and built from a different migration set. Recreate rather than
	// reuse: a stale template is a wrong schema for every test in the binary.
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

// dropTemplate removes a template database left by an earlier run, if there is
// one. Postgres refuses to drop a database while its template flag is set, and
// the flag can only be cleared with connections allowed, so the seal is undone
// before the drop.
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
func NewDSN(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("storage: skipped under -short (Postgres testcontainer)")
	}
	ensureTemplate()
	if startErr != nil {
		t.Fatalf("start postgres container: %v", startErr)
	}
	if templateErr != nil {
		t.Fatalf("build template database: %v", templateErr)
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
func NewDB(t *testing.T) storage.Gateway {
	t.Helper()
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, NewDSN(t))
	if err != nil {
		t.Fatalf("storage.NewPG: %v", err)
	}
	t.Cleanup(gw.Close)
	return gw
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
func Main(m *testing.M) int {
	code := m.Run()
	if err := testcontainers.TerminateContainer(ctr); err != nil {
		fmt.Fprintf(os.Stderr, "storagetest: terminate container: %v\n", err)
	}
	return code
}
