package storage

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The coalesced live stamp's own contract (#943), on a scratch owner and live
// table so it is proven apart from either caller: event_rule_live and
// endpoint_live keep their behavior tests (rule_judged_test.go,
// endpoint_seen_test.go). Internal for the same reason as path_batch_test.go:
// liveStamp is unexported, and HarnessDSN is how an internal test reaches
// Postgres without an import cycle.

var probeLive = liveStamp{table: "probe_live", key: "probe_id", at: "seen_at", owner: "probe"}

// liveStampPool opens a fresh migrated database with a probe owner table and
// its live table, shaped the way event_rule_live and endpoint_live are, and
// returns n probe ids.
func liveStampPool(t *testing.T, n int) (*pgxpool.Pool, []string) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	if HarnessDSN == nil {
		t.Fatal("HarnessDSN is nil: the external TestMain no longer assigns it")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, HarnessDSN(t))
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `create table probe (id uuid primary key);
		create table probe_live (probe_id uuid primary key references probe(id) on delete cascade, seen_at timestamptz not null)`); err != nil {
		t.Fatalf("probe tables: %v", err)
	}
	ids := make([]string, n)
	for i := range ids {
		ids[i] = uuid.NewString()
		if _, err := pool.Exec(ctx, `insert into probe (id) values ($1::uuid)`, ids[i]); err != nil {
			t.Fatalf("probe: %v", err)
		}
	}
	return pool, ids
}

func probeSeen(t *testing.T, pool *pgxpool.Pool, id string) (time.Time, bool) {
	t.Helper()
	var ts time.Time
	err := pool.QueryRow(context.Background(), `select seen_at from probe_live where probe_id = $1::uuid`, id).Scan(&ts)
	if err == pgx.ErrNoRows {
		return time.Time{}, false
	}
	if err != nil {
		t.Fatalf("read stamp: %v", err)
	}
	return ts, true
}

func ageProbe(t *testing.T, pool *pgxpool.Pool, id string) time.Time {
	t.Helper()
	var ts time.Time
	if err := pool.QueryRow(context.Background(), `update probe_live set seen_at = now() - interval '1 minute'
		where probe_id = $1::uuid returning seen_at`, id).Scan(&ts); err != nil {
		t.Fatalf("age: %v", err)
	}
	return ts
}

// Many batches seeing the same new ids at once all succeed: first sight
// inserts ON CONFLICT DO NOTHING, so neither aborts the other, and each id
// ends with exactly one row.
func TestLiveStampConcurrentFirstSight(t *testing.T) {
	pool, ids := liveStampPool(t, 3)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each batch names the ids in its own order: the helper sorts.
			batch := []string{ids[i%3], ids[(i+1)%3], ids[(i+2)%3]}
			errs <- probeLive.stamp(ctx, pool, batch, liveStampEvery)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent first sight: %v", err)
		}
	}
	var rows int
	if err := pool.QueryRow(ctx, `select count(*) from probe_live`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != len(ids) {
		t.Fatalf("got %d stamp rows for %d ids", rows, len(ids))
	}
}

// A stamp within the bound is left alone; one past it moves to now. An id
// with no owner row is skipped, not a foreign-key failure, and an empty batch
// writes nothing.
func TestLiveStampBound(t *testing.T) {
	pool, ids := liveStampPool(t, 2)
	ctx := context.Background()
	if err := probeLive.stamp(ctx, pool, nil, liveStampEvery); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	if _, ok := probeSeen(t, pool, ids[0]); ok {
		t.Fatal("an empty batch stamped an id")
	}
	before := time.Now()
	if err := probeLive.stamp(ctx, pool, []string{ids[0]}, liveStampEvery); err != nil {
		t.Fatalf("first sight: %v", err)
	}
	first, ok := probeSeen(t, pool, ids[0])
	if !ok || first.Before(before.Add(-time.Second)) || first.After(time.Now().Add(time.Second)) {
		t.Fatalf("first sight stamped %v (present %v), want about now", first, ok)
	}

	if err := probeLive.stamp(ctx, pool, []string{ids[0]}, liveStampEvery); err != nil {
		t.Fatalf("restamp: %v", err)
	}
	if got, _ := probeSeen(t, pool, ids[0]); !got.Equal(first) {
		t.Fatalf("a stamp within the bound moved: %v -> %v", first, got)
	}

	aged := ageProbe(t, pool, ids[0])
	if err := probeLive.stamp(ctx, pool, []string{ids[0]}, liveStampEvery); err != nil {
		t.Fatalf("restamp past the bound: %v", err)
	}
	if got, _ := probeSeen(t, pool, ids[0]); !got.After(aged) || time.Since(got) > 10*time.Second {
		t.Fatalf("a stamp past the bound stayed at %v (aged %v)", got, aged)
	}

	gone := uuid.NewString()
	if err := probeLive.stamp(ctx, pool, []string{gone, ids[1]}, liveStampEvery); err != nil {
		t.Fatalf("an id with no owner row failed the batch: %v", err)
	}
	if _, ok := probeSeen(t, pool, gone); ok {
		t.Fatal("an id with no owner row was stamped")
	}
	if _, ok := probeSeen(t, pool, ids[1]); !ok {
		t.Fatal("the owned id beside a missing one was not stamped")
	}
}

// A batch never queues on a stamp: while another transaction holds a stamp
// row past the bound, the stamp skips it and returns at once, stamping the
// rest of its batch.
func TestLiveStampNeverWaits(t *testing.T) {
	pool, ids := liveStampPool(t, 2)
	ctx := context.Background()
	if err := probeLive.stamp(ctx, pool, ids, liveStampEvery); err != nil {
		t.Fatal(err)
	}
	heldAt := ageProbe(t, pool, ids[0])
	freeAt := ageProbe(t, pool, ids[1])
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select 1 from probe_live where probe_id = $1::uuid for update`, ids[0]); err != nil {
		t.Fatal(err)
	}
	quick, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := probeLive.stamp(quick, pool, ids, liveStampEvery); err != nil {
		t.Fatalf("a stamp waited on a row another transaction holds: %v", err)
	}
	if got, _ := probeSeen(t, pool, ids[0]); !got.Equal(heldAt) {
		t.Fatalf("the held row moved: %v -> %v", heldAt, got)
	}
	if got, _ := probeSeen(t, pool, ids[1]); !got.After(freeAt) {
		t.Fatalf("the free row beside the held one stayed at %v", got)
	}
}

// Inside a transaction the stamp is that transaction's clock, the instant a
// caller reading now() in the same transaction saw (the rule evaluator's
// e.now), not the statement's wall time.
func TestLiveStampTransactionClock(t *testing.T) {
	pool, ids := liveStampPool(t, 1)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now, err := dbNow(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := probeLive.stamp(ctx, tx, ids, liveStampEvery); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := probeSeen(t, pool, ids[0]); !got.Equal(now) {
		t.Fatalf("stamped %v inside a transaction whose clock reads %v", got, now)
	}
}
