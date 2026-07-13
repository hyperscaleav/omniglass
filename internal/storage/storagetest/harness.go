// Package storagetest provides the shared real-Postgres test harness. Every
// integration test runs against an ephemeral, fully isolated database: one
// container is started lazily per test binary (sync.Once), and each NewDB call
// creates and migrates a fresh database, so tests never share mutable state and
// never collide on a host port.
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

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	startOnce sync.Once
	ctr       *tcpostgres.PostgresContainer // shared container, terminated by Main
	adminDSN  string                        // DSN to the container's default db, for CREATE DATABASE
	startErr  error
	dbCounter atomic.Int64
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
		if dsn := os.Getenv("OMNIGLASS_TEST_ADMIN_DSN"); dsn != "" {
			adminDSN = dsn
			return
		}
		ctr, adminDSN, startErr = startContainer(context.Background())
	})
}

// NewDSN returns the DSN of a fresh, migrated, isolated Postgres database.
// Skipped under -short. The database is discarded when the shared container is
// reaped on process exit. Use this when the test needs the raw DSN (e.g. to
// launch the server binary against it).
func NewDSN(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("storage: skipped under -short (Postgres testcontainer)")
	}
	ensureContainer()
	if startErr != nil {
		t.Fatalf("start postgres container: %v", startErr)
	}
	ctx := context.Background()

	dbName := fmt.Sprintf("og_test_%d", dbCounter.Add(1))
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{dbName}.Sanitize()); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create database %s: %v", dbName, err)
	}
	_ = admin.Close(ctx)

	dsn := withDBName(adminDSN, dbName)
	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate %s: %v", dbName, err)
	}
	return dsn
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
