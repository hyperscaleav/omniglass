package storage_test

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestConnectionStatusReadsTheDatabasesOwnSide proves the database-side
// connection read the server's platform metrics take each window (#1980),
// against a real Postgres: connections held count against the limit
// (max_connections less the superuser reserve), a connection left idle in a
// transaction is counted, and the oldest open transaction's age is how long
// it has been open. The container is shared by parallel tests, whose
// connections come and go, so held is checked as at least the connections this
// test holds itself, never against an earlier read.
func TestConnectionStatusReadsTheDatabasesOwnSide(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)

	// Two idle connections, and a third left idle inside a transaction.
	for range 2 {
		c, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close(ctx)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "begin"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "select 1"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)

	after, err := gw.ConnectionStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The three connections above and the gateway's own, the one reading.
	if after.Held < 4 || after.Held > after.Limit {
		t.Errorf("held = %d of %d, want at least the 4 this test holds, within the limit", after.Held, after.Limit)
	}
	if after.Limit < 1 {
		t.Errorf("limit = %d, want max_connections less the superuser reserve", after.Limit)
	}
	if after.IdleInTransaction < 1 {
		t.Errorf("idle in transaction = %d, want the open transaction counted", after.IdleInTransaction)
	}
	if after.OldestTransactionAge < time.Second || after.OldestTransactionAge > time.Minute {
		t.Errorf("oldest transaction age = %v, want the open transaction's ~1.5 s", after.OldestTransactionAge)
	}

	if _, err := conn.Exec(ctx, "rollback"); err != nil {
		t.Fatal(err)
	}
}

// TestConnectionStatusCountsOtherRolesWithoutPgMonitor: a server role that is
// not a member of pg_monitor reads other roles' pg_stat_activity rows with
// their backend_type null, so a filter on it would count only the server's own
// sessions. Held must still count every client: connections opened by another
// role are counted when the gateway reads as a plain role.
func TestConnectionStatusCountsOtherRolesWithoutPgMonitor(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	dsn := storagetest.NewDSN(t)
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)

	// Roles are cluster-wide and the container is shared, so the name is this
	// test's own.
	role := fmt.Sprintf("plain_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, fmt.Sprintf(`create role %s login password 'plain'`, role)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), fmt.Sprintf(`drop role if exists %s`, role))
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(role, "plain")
	gw, err := storage.NewPG(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)

	// Three connections held by the admin role, which the plain role cannot see
	// the backend_type of.
	for range 3 {
		c, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close(ctx)
	}

	st, err := gw.ConnectionStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The three above, the admin connection and the gateway's own.
	if st.Held < 5 {
		t.Errorf("held = %d read as a role without pg_monitor, want at least the 5 open here", st.Held)
	}
}
