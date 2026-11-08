package storagetest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// The pinned database clock (#1567). The storage layer reads one clock, the
// database's: now() in its statements and dbNow in Go. So a test that has to
// run at a chosen instant (just before or just after UTC midnight, the edge a
// day partition, a block fold or a retention cutoff turns on) pins that clock
// rather than waiting for the wall to reach it.
//
// PinClock shadows the database's time functions for one test database: it
// creates them in a schema of their own, each the real one plus a fixed
// offset, and puts that schema ahead of pg_catalog on the database's search
// path. Postgres searches pg_catalog first only when the path does not name
// it, so naming it second lets the shadows win. The clock then reads at when
// PinClock returns and runs on in real time from there, so a test's elapsed
// seconds stay elapsed seconds.
//
// What it does not reach: a column default resolved when its table was made
// (a default of now() names pg_catalog's), the SQL keywords current_timestamp
// and localtimestamp, and time.Now() in Go. A pinned test reads its own times
// from the returned clock, never from time.Now().

// pinnedClockSchema holds the shadowing time functions.
const pinnedClockSchema = "og_test_clock"

// pinnedClockFuncs are the time functions the pin shadows, each with the
// volatility of the pg_catalog function it shadows.
var pinnedClockFuncs = []struct{ name, volatility string }{
	{"now", "stable"},
	{"transaction_timestamp", "stable"},
	{"statement_timestamp", "stable"},
	{"clock_timestamp", "volatile"},
}

// PinClock sets the clock of the test database at dsn to read at, and
// returns that clock for the Go side of the test. Call it before opening any
// connection the test reads time through: the search path is a database
// setting, read when a session starts.
func PinClock(t testing.TB, dsn string, at time.Time) func() time.Time {
	t.Helper()
	ctx := context.Background()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("pin clock: parse dsn: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pin clock: connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var wall time.Time
	if err := conn.QueryRow(ctx, `select pg_catalog.clock_timestamp()`).Scan(&wall); err != nil {
		t.Fatalf("pin clock: read the real clock: %v", err)
	}
	offset := at.Sub(wall)
	stmts := []string{`create schema if not exists ` + pinnedClockSchema}
	for _, f := range pinnedClockFuncs {
		stmts = append(stmts, fmt.Sprintf(`create or replace function %[1]s.%[2]s() returns timestamptz
			language sql %[3]s parallel safe as $$ select pg_catalog.%[2]s() + make_interval(secs => %[4]f) $$`,
			pinnedClockSchema, f.name, f.volatility, offset.Seconds()))
	}
	stmts = append(stmts, `alter database `+pgx.Identifier{cfg.Database}.Sanitize()+
		` set search_path = `+pinnedClockSchema+`, pg_catalog, "$user", public`)
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			t.Fatalf("pin clock: %v", err)
		}
	}
	start := time.Now()
	return func() time.Time { return at.Add(time.Since(start)).UTC() }
}

// Midnights are the instants a test that derives "today" runs at besides the
// real one (#1567): thirty seconds before and after a UTC midnight. The night
// is the one main's test run crossed, a Saturday into a Sunday, so the ISO
// week a block fold seeds from is as long as it gets.
var Midnights = []struct {
	Name string
	At   time.Time
}{
	{"before-midnight", time.Date(2026, 11, 7, 23, 59, 30, 0, time.UTC)},
	{"after-midnight", time.Date(2026, 11, 8, 0, 0, 30, 0, time.UTC)},
}

// AtEachMidnight runs fn as a subtest at the real clock (a zero at) and at
// each of [Midnights]. A test that pins nothing passes at, unread, to the
// fixture that does; fn calls t.Parallel itself when it can run in parallel.
func AtEachMidnight(t *testing.T, fn func(t *testing.T, at time.Time)) {
	t.Helper()
	t.Run("now", func(t *testing.T) { fn(t, time.Time{}) })
	for _, m := range Midnights {
		t.Run(m.Name, func(t *testing.T) { fn(t, m.At) })
	}
}
