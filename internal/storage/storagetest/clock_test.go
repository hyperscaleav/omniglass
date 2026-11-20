package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestPinClockMovesTheDatabaseClock: after PinClock, a new session's now(),
// clock_timestamp() and the Go clock it returns all read the pinned instant
// (to within the test's own run time), and pg_catalog's own now() still reads
// the real time.
func TestPinClockMovesTheDatabaseClock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := NewDSN(t)
	at := time.Date(2026, 11, 7, 23, 59, 30, 0, time.UTC)
	clock := PinClock(t, dsn, at)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var now, wall, catalog time.Time
	if err := conn.QueryRow(ctx, `select now(), clock_timestamp(), pg_catalog.now()`).Scan(&now, &wall, &catalog); err != nil {
		t.Fatal(err)
	}
	near := func(name string, got time.Time) {
		t.Helper()
		if d := got.Sub(at); d < 0 || d > time.Minute {
			t.Errorf("%s = %s, want within a minute after %s", name, got.UTC().Format(time.RFC3339Nano), at.Format(time.RFC3339))
		}
	}
	near("now()", now)
	near("clock_timestamp()", wall)
	near("the Go clock", clock())
	if d := time.Since(catalog); d < -time.Minute || d > time.Minute {
		t.Errorf("pg_catalog.now() = %s, want the real time", catalog.UTC().Format(time.RFC3339Nano))
	}
}

// TestStopClockHoldsTheDatabaseClock: after StopClock, a session reads the
// stopped instant exactly, in two statements a moment apart, and moving the
// stopped clock moves what a new session reads.
func TestStopClockHoldsTheDatabaseClock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := NewDSN(t)
	for _, at := range []time.Time{time.Date(2026, 11, 7, 23, 59, 30, 0, time.UTC), time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)} {
		clock := StopClock(t, dsn, at)
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		var first, now, wall time.Time
		if err := conn.QueryRow(ctx, `select now()`).Scan(&first); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
		if err := conn.QueryRow(ctx, `select now(), clock_timestamp()`).Scan(&now, &wall); err != nil {
			t.Fatal(err)
		}
		_ = conn.Close(ctx)
		for name, got := range map[string]time.Time{"the first now()": first, "now()": now, "clock_timestamp()": wall, "the Go clock": clock()} {
			if !got.Equal(at) {
				t.Errorf("%s = %s, want the stopped %s", name, got.UTC().Format(time.RFC3339Nano), at.Format(time.RFC3339))
			}
		}
	}
}

// TestAProvisionedDatabaseCarriesYesterday: the template is rolled when it is
// built, so a cloned database has a metric day for yesterday as well as the
// days the migration made. Without it, a binary whose template was built
// minutes after UTC midnight hands its tests no partition for a row planted an
// hour back, and the raw insert fails with 23514 (#1567).
func TestAProvisionedDatabaseCarriesYesterday(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	conn := connect(t, ctx, NewDSN(t))
	var n int
	if err := conn.QueryRow(ctx, `
		select count(*) from pg_inherits i
		join pg_class c on c.oid = i.inhrelid
		where i.inhparent = 'metric'::regclass
		  and c.relname = 'metric_p' || to_char((now() - interval '1 day') at time zone 'utc', 'YYYYMMDD')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("yesterday's metric day in a provisioned database: %d partitions, want 1", n)
	}
}
