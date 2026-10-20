package storage_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestStampEndpointsSeen pins the coalesced stamp (#933): a first sight
// inserts, a stamp within the bound is left alone, and two batches seeing a
// new endpoint at once both succeed (neither aborts the other's ingest).
func TestStampEndpointsSeen(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var a, b string
	if err := conn.QueryRow(ctx, `insert into endpoint (name, transport, component, params) values ('a', 'tcp', $1::uuid, '{}'::jsonb) returning id`, comp.ID).Scan(&a); err != nil {
		t.Fatalf("endpoint a: %v", err)
	}
	if err := conn.QueryRow(ctx, `insert into endpoint (name, transport, component, params) values ('b', 'icmp', $1::uuid, '{}'::jsonb) returning id`, comp.ID).Scan(&b); err != nil {
		t.Fatalf("endpoint b: %v", err)
	}

	// Concurrent first sight, many times over: no batch errors.
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- gw.StampEndpointsSeen(ctx, []string{a, b})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent first sight: %v", err)
		}
	}
	stamp := func(id string) time.Time {
		t.Helper()
		var ts time.Time
		if err := conn.QueryRow(ctx, `select last_seen_at from endpoint_live where endpoint_id = $1::uuid`, id).Scan(&ts); err != nil {
			t.Fatalf("read stamp: %v", err)
		}
		return ts
	}
	first := stamp(a)

	// Within the bound: left alone.
	if err := gw.StampEndpointsSeen(ctx, []string{a}); err != nil {
		t.Fatalf("restamp: %v", err)
	}
	if got := stamp(a); !got.Equal(first) {
		t.Fatalf("a stamp within the bound moved: %v -> %v", first, got)
	}
	// Past the bound: moved to now.
	if _, err := conn.Exec(ctx, `update endpoint_live set last_seen_at = now() - interval '1 minute' where endpoint_id = $1::uuid`, a); err != nil {
		t.Fatalf("age: %v", err)
	}
	if err := gw.StampEndpointsSeen(ctx, []string{a}); err != nil {
		t.Fatalf("restamp: %v", err)
	}
	if got := stamp(a); time.Since(got) > 10*time.Second {
		t.Fatalf("a stamp past the bound did not move: %v", got)
	}
}
