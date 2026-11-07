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
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
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
