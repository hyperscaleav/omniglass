package bus_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// landOrderStore records each landing the consumer starts and the order batches
// finish landing in, by the tag their log line carries.
type landOrderStore struct {
	*storage.PG
	mu       sync.Mutex
	attempts int
	landed   []string
}

func (s *landOrderStore) LandBatch(ctx context.Context, b storage.IngestBatch) (storage.LandResult, error) {
	s.mu.Lock()
	s.attempts++
	s.mu.Unlock()
	res, err := s.PG.LandBatch(ctx, b)
	if err == nil && res.Landed && len(b.LogLines) > 0 {
		s.mu.Lock()
		s.landed = append(s.landed, b.LogLines[0].Message)
		s.mu.Unlock()
	}
	return res, err
}

func (s *landOrderStore) snapshot() (attempts int, landed []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts, append([]string(nil), s.landed...)
}

// TestALateBatchBehindAFoldIsHeldByItsHeartbeat: a late batch waiting on the
// block lock for several times the stream's ack wait (and past the five
// deliveries the wait would otherwise spend) is delivered once, not once per
// ack wait; the publisher's next batch, behind it on the one ack-pending slot,
// is not delivered until it lands, and lands after it (ADR-1529 decision 9,
// ADR-0311). Real Postgres and JetStream. One server's handler runs a batch
// at a time, so without the heartbeat the redeliveries queue behind the first
// and are each landed as a no-op once it returns (8 landings here, not 2); the
// order breaks only where another server takes the slot once the deliveries
// are spent, which the heartbeat prevents by never letting them be spent.
func TestALateBatchBehindAFoldIsHeldByItsHeartbeat(t *testing.T) {
	const ackWait, hold = 400 * time.Millisecond, 3 * time.Second // hold is 7.5 ack waits, past 5 deliveries
	restore := bus.SetTelemetryAckWait(ackWait)
	defer restore()

	f := newDeliveryFixture(t)
	// An announced mark ahead of the batches makes their metric a late sample,
	// whose merge takes the block lock shared.
	if _, err := f.conn.Exec(f.ctx, `insert into metric_trend_mark (announced_through, compacted_through)
		values (now() + interval '1 hour', now() - interval '1 hour')`); err != nil {
		t.Fatalf("set the trend mark: %v", err)
	}
	store := &landOrderStore{PG: f.gw}
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, store)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()

	fold, err := f.conn.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fold.Rollback(context.WithoutCancel(f.ctx)) }()
	if _, err := fold.Exec(f.ctx, `select pg_advisory_xact_lock(hashtextextended('metric_block:fold', 0))`); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for _, tag := range []string{"held", "next"} {
		if err := srv.PublishTelemetry(f.ctx, pushBatch(tag)); err != nil {
			t.Fatalf("publish %s: %v", tag, err)
		}
	}
	time.Sleep(hold - time.Since(start))
	if attempts, landed := store.snapshot(); attempts != 1 || len(landed) != 0 {
		t.Fatalf("after %s behind the fold the consumer started %d landings and finished %v, want the one batch waiting, started once and the next not delivered",
			time.Since(start).Round(time.Millisecond), attempts, landed)
	}
	if err := fold.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}

	f.waitLanded(t, "held")
	f.waitLanded(t, "next")
	f.waitDrained(t, srv)
	if attempts, landed := store.snapshot(); attempts != 2 || len(landed) != 2 || landed[0] != "held" || landed[1] != "next" {
		t.Errorf("the consumer started %d landings and finished %v, want one landing each and held before next", attempts, landed)
	}
	f.assertOnce(t, "held", 2)
	f.assertOnce(t, "next", 2)
}
