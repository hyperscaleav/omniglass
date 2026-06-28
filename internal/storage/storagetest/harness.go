// Package storagetest provides the shared real-Postgres test harness. Every
// integration test runs against an ephemeral, fully isolated database: one
// container is started lazily per test binary (sync.Once) and reaped by ryuk on
// process exit, and each NewDB call creates and migrates a fresh database, so
// tests never share mutable state and never collide on a host port.
//
// There is no in-memory double. The doctrine is: do not mock the database.
package storagetest

import (
	"context"
	"fmt"
	"net/url"
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
	adminDSN  string // DSN to the container's default db, for CREATE DATABASE
	startErr  error
	dbCounter atomic.Int64
)

func ensureContainer() {
	startOnce.Do(func() {
		ctx := context.Background()
		ctr, err := tcpostgres.Run(ctx, "postgres:18",
			tcpostgres.WithDatabase("postgres"),
			tcpostgres.WithUsername("omniglass"),
			tcpostgres.WithPassword("omniglass"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).WithStartupTimeout(60*time.Second)),
		)
		if err != nil {
			startErr = err
			return
		}
		adminDSN, startErr = ctr.ConnectionString(ctx, "sslmode=disable")
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
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
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
