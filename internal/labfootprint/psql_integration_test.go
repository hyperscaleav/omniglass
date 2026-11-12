package labfootprint

import (
	"context"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// psqlHarness is a harness whose database container is a fresh Postgres, as
// a lab's is, and a connection to that database from outside the harness.
func psqlHarness(t *testing.T) (*harness, *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	c, err := tcpostgres.Run(ctx, "postgres:18",
		tcpostgres.WithDatabase("omniglass"),
		tcpostgres.WithUsername("omniglass"),
		tcpostgres.WithPassword("omniglass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)))
	if c != nil {
		t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
	}
	if err != nil {
		t.Fatal(err)
	}
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	dc, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dc.Close() })
	return &harness{cfg: Config{Out: io.Discard}, dc: dc, env: map[string]string{"LAB_DB": c.GetContainerID()}}, conn
}

// sleepers counts the backends still running the test's sleep, waiting a
// few seconds for a cancelled one to go.
func sleepers(t *testing.T, conn *pgx.Conn) int {
	t.Helper()
	var n int
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		if err := conn.QueryRow(context.Background(), `select count(*) from pg_stat_activity
			where query like 'select pg_sleep(30)%' and pid <> pg_backend_pid()`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 || time.Now().After(end) {
			return n
		}
	}
}

// A harness query outlives neither its context nor its deadline, in the
// database as well as in the harness: a measure killed mid-read left its
// query running for hours, starving the ingest it was measuring (#1841).
func TestIntegrationPsqlStopsTheQueryWithItsContext(t *testing.T) {
	t.Parallel()
	h, conn := psqlHarness(t)
	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
	}{
		{"deadline", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 2*time.Second)
		}},
		{"cancel", func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(2*time.Second, cancel)
			return ctx, cancel
		}},
	} {
		ctx, cancel := tc.ctx()
		start := time.Now()
		_, err := h.psql(ctx, "select pg_sleep(30)")
		took := time.Since(start)
		cancel()
		if err == nil {
			t.Fatalf("%s: a query cut short returned no error", tc.name)
		}
		if took > 10*time.Second {
			t.Fatalf("%s: the harness waited %s for a query its context ended at 2s", tc.name, took)
		}
		if n := sleepers(t, conn); n != 0 {
			t.Fatalf("%s: %d backends still run the query after its context ended", tc.name, n)
		}
	}
}

// A query that finishes in time returns its output.
func TestIntegrationPsqlReturnsTheOutput(t *testing.T) {
	t.Parallel()
	h, _ := psqlHarness(t)
	out, err := h.psql(context.Background(), "select 1 + 1")
	if err != nil {
		t.Fatal(err)
	}
	if out != "2\n" {
		t.Fatalf("read %q, want %q", out, "2\n")
	}
}

// The database holds a harness query to its context's deadline itself, so a
// harness that dies without cancelling still leaves nothing running past it.
func TestIntegrationPsqlRunsUnderTheDeadline(t *testing.T) {
	t.Parallel()
	h, _ := psqlHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	out, err := h.psql(ctx, "select setting from pg_settings where name = 'statement_timeout'")
	if err != nil {
		t.Fatal(err)
	}
	ms, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatal(err)
	}
	if ms < 80_000 || ms > 90_000 {
		t.Fatalf("the query ran under statement_timeout %dms, want the context's 90s", ms)
	}
}
