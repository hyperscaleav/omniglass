package platformmetric_test

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/platformmetric"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// bootLock is the advisory lock a server's boot takes to ensure its
// component. Holding it from outside parks every EnsureServerComponent on a
// pool connection, which is how these tests make a slow query and an
// exhausted pool on demand rather than by racing.
const bootLock = `select pg_advisory_lock(hashtextextended('platform boot', 0))`

// seededDSN is a fresh database holding the boot seed, the platform system
// included.
func seededDSN(t *testing.T) string {
	t.Helper()
	return storagetest.NewDSNFrom(t, "platformmetric_seed", func(ctx context.Context, dsn string) error {
		gw, err := storage.NewPG(ctx, dsn)
		if err != nil {
			return err
		}
		defer gw.Close()
		return seed.Run(ctx, gw)
	})
}

// holdBootLock takes the boot lock on its own connection and returns its
// release.
func holdBootLock(t *testing.T, ctx context.Context, dsn string) func() {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("lock connection: %v", err)
	}
	if _, err := conn.Exec(ctx, bootLock); err != nil {
		t.Fatalf("take the boot lock: %v", err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			if _, err := conn.Exec(ctx, `select pg_advisory_unlock_all()`); err != nil {
				t.Errorf("release the boot lock: %v", err)
			}
			_ = conn.Close(ctx)
		})
	}
	t.Cleanup(release)
	return release
}

// waitFor polls cond until it holds or five seconds pass.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// latest is a component's newest sample of key, failing when there is none.
func latest(t *testing.T, ctx context.Context, gw *storage.PG, componentID, key string) float64 {
	t.Helper()
	m, err := gw.LatestMetric(ctx, componentID, key)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	if m == nil {
		t.Fatalf("no %s sample on the server component", key)
	}
	return m.Value
}

// TestPoolContentionLandsAWaitCount: a window in which the pool ran out
// records a nonzero db-pool-wait-count on the server's component, and
// db-pool-max reads the pool's configured cap.
func TestPoolContentionLandsAWaitCount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := seededDSN(t)
	gw, err := storage.NewPG(ctx, dsn+"&pool_max_conns=2")
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	id, err := gw.EnsureServerComponent(ctx, "contended")
	if err != nil {
		t.Fatalf("ensure server component: %v", err)
	}
	rec := platformmetric.NewRecorder(time.Now)
	rp := platformmetric.NewReporter(rec, gw, id, nil, nil, platformmetric.PoolSampler(gw.PoolStat))

	release := holdBootLock(t, ctx, dsn)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if _, err := gw.EnsureServerComponent(ctx, "contended"); err != nil {
				t.Errorf("a parked boot: %v", err)
			}
		})
	}
	waitFor(t, "both pool connections parked on the boot lock", func() bool { return gw.PoolStat().Acquired == 2 })
	wg.Go(func() {
		if err := gw.Ping(ctx); err != nil {
			t.Errorf("ping past the exhausted pool: %v", err)
		}
	})
	waitFor(t, "an acquire to find the pool empty", func() bool { return gw.PoolStat().EmptyAcquireCount > 0 })
	release()
	wg.Wait()

	if err := rp.Report(ctx); err != nil {
		t.Fatalf("report the window: %v", err)
	}
	if v := latest(t, ctx, gw, id, writerclass.DBPoolWaitCount); v < 1 {
		t.Errorf("db-pool-wait-count = %v, want at least 1", v)
	}
	if v := latest(t, ctx, gw, id, writerclass.DBPoolMax); v != 2 {
		t.Errorf("db-pool-max = %v, want the configured cap 2", v)
	}
}

// TestSlowQueryLandsACount: a query through the gateway that runs past the
// lowered threshold logs one WARN, and the window lands a db-slow-query-count
// of 1 on the server's component. The slow query is the boot lock's wait,
// held from outside for longer than the threshold.
func TestSlowQueryLandsACount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := seededDSN(t)
	var buf syncBuffer
	rec := platformmetric.NewRecorder(time.Now)
	tr := storage.NewSlowQueryTracer(slog.New(slog.NewJSONHandler(&buf, nil)), time.Now, func() {
		rec.Count(writerclass.DBSlowQueryCount, "", 1)
	})
	tr.SetThreshold(0)
	gw, err := storage.NewPG(ctx, dsn, storage.WithQueryTracer(tr))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	id, err := gw.EnsureServerComponent(ctx, "slow")
	if err != nil {
		t.Fatalf("ensure server component: %v", err)
	}

	tr.SetThreshold(300 * time.Millisecond)
	release := holdBootLock(t, ctx, dsn)
	done := make(chan error, 1)
	go func() {
		_, err := gw.EnsureServerComponent(ctx, "slow")
		done <- err
	}()
	time.Sleep(600 * time.Millisecond)
	release()
	if err := <-done; err != nil {
		t.Fatalf("the parked boot: %v", err)
	}
	tr.SetThreshold(0)

	warns := strings.Count(buf.String(), `"level":"WARN"`)
	if warns != 1 || !strings.Contains(buf.String(), "pg_advisory_xact_lock") {
		t.Fatalf("slow-query log = %s, want one WARN naming the lock wait", buf.String())
	}
	if err := platformmetric.NewReporter(rec, gw, id, nil, nil).Report(ctx); err != nil {
		t.Fatalf("report the window: %v", err)
	}
	if v := latest(t, ctx, gw, id, writerclass.DBSlowQueryCount); v != 1 {
		t.Errorf("db-slow-query-count = %v, want 1", v)
	}
}

// syncBuffer is a log buffer a pool's connections write at once.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
