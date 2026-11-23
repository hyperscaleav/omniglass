package bus

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// partitionStore is a store whose metric partition roll answers with err and
// counts its calls.
type partitionStore struct {
	fakeStore
	err   error
	calls *int
}

func (s partitionStore) EnsureMetricPartitions(context.Context) ([]string, error) {
	*s.calls++
	return nil, s.err
}

// The bus makes sure the metric table has today's partition and the next
// three days' before its ingest starts (#420, #1527), and refuses to start
// when it cannot: ingest over a missing day would only retry until one
// exists.
func TestTheBusChecksTheMetricDaysBeforeIngest(t *testing.T) {
	calls := 0
	srv, err := New(Config{Host: "127.0.0.1", Port: 0}, partitionStore{calls: &calls})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	srv.Shutdown()
	if calls == 0 {
		t.Fatal("the bus started its ingest without checking the metric partitions")
	}
	calls = 0
	_, err = New(Config{Host: "127.0.0.1", Port: 0}, partitionStore{calls: &calls, err: errors.New("lock timeout")})
	if err == nil || !strings.Contains(err.Error(), "metric partitions") {
		t.Fatalf("start over a failed partition check: err %v, want the bus refused naming the partitions", err)
	}
}

// TestTheBusRefusesIngestOverAnUncoveredDay: against a real database, a day
// ahead that a partition covers only half of (the roll sees the day's start
// covered and makes nothing) leaves its second half routing nowhere, so the
// boot refuses to start the consumer, naming the first uncovered instant.
// With the stray partition gone, the boot makes the day whole and starts.
func TestTheBusRefusesIngestOverAnUncoveredDay(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	storagetest.AtEachMidnight(t, busRefusesIngestOverAnUncoveredDay)
}

// stoppedDSN is a fresh test database whose clock is stopped at at (the real
// instant when at is zero), so a run that crosses UTC midnight cannot move
// the day the partitions are rolled for (#2375).
func stoppedDSN(t *testing.T, at time.Time) string {
	t.Helper()
	dsn := storagetest.NewDSN(t)
	if at.IsZero() {
		at = time.Now()
	}
	storagetest.StopClock(t, dsn, at)
	return dsn
}

func busRefusesIngestOverAnUncoveredDay(t *testing.T, at time.Time) {
	ctx := context.Background()
	dsn := stoppedDSN(t, at)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	// The migration rolled the real days; roll the pinned clock's too.
	if _, err := gw.EnsureMetricPartitions(ctx); err != nil {
		t.Fatalf("roll the pinned days: %v", err)
	}

	var today time.Time
	if err := conn.QueryRow(ctx, `select date_trunc('day', now() at time zone 'UTC') at time zone 'UTC'`).Scan(&today); err != nil {
		t.Fatal(err)
	}
	ahead := today.AddDate(0, 0, 3)
	name := "metric_p" + ahead.Format("20060102")
	half := ahead.Add(12 * time.Hour)
	for _, sql := range []string{
		`drop table ` + name,
		`create table metric_half partition of metric for values from ('` + ahead.Format(time.RFC3339) + `') to ('` + half.Format(time.RFC3339) + `')`,
	} {
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	_, err = New(Config{Host: "127.0.0.1", Port: 0}, gw)
	if err == nil || !strings.Contains(err.Error(), "metric partitions") || !strings.Contains(err.Error(), half.Format(time.RFC3339)) {
		t.Fatalf("start over a half-covered day: err %v, want the bus refused naming %s", err, half.Format(time.RFC3339))
	}

	if _, err := conn.Exec(ctx, `drop table metric_half`); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Config{Host: "127.0.0.1", Port: 0}, gw)
	if err != nil {
		t.Fatalf("start once the stray partition is gone: %v", err)
	}
	srv.Shutdown()
	parts, err := gw.MetricPartitions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range parts {
		found = found || p.Name == name && p.From.Equal(ahead) && p.To.Equal(ahead.AddDate(0, 0, 1))
	}
	if !found {
		t.Fatalf("partitions %+v, want %s made whole at boot", parts, name)
	}
}

// TestTheBootRollsTheDaysOfTheClockItReads (#2375): a boot reads the
// database's clock for the day it rolls partitions for. With the clock
// stopped at 23:59:59 a boot covers today through today+3; with it moved to
// 00:00:01 (no sleep) the next boot covers one day further, and the test's
// expectation follows the clock it moved rather than the wall.
func TestTheBootRollsTheDaysOfTheClockItReads(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	before := time.Date(2026, 11, 7, 23, 59, 59, 0, time.UTC)
	storagetest.StopClock(t, dsn, before)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	boot := func() {
		t.Helper()
		srv, err := New(Config{Host: "127.0.0.1", Port: 0}, gw)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		srv.Shutdown()
	}
	has := func(day time.Time) bool {
		t.Helper()
		parts, err := gw.MetricPartitions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range parts {
			if p.Name == "metric_p"+day.Format("20060102") {
				return true
			}
		}
		return false
	}
	day := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	boot()
	if !has(day.AddDate(0, 0, 3)) || has(day.AddDate(0, 0, 4)) {
		t.Fatalf("at %s the boot should cover through %s and no further", before.Format(time.RFC3339), day.AddDate(0, 0, 3).Format("2006-01-02"))
	}
	storagetest.StopClock(t, dsn, before.Add(2*time.Second))
	boot()
	if !has(day.AddDate(0, 0, 4)) {
		t.Fatalf("after midnight the boot should cover through %s", day.AddDate(0, 0, 4).Format("2006-01-02"))
	}
}
