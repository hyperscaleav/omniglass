package bus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// drainStore counts calc drains, records whether each ran under a deadline,
// and blocks one pass until its context ends.
type drainStore struct {
	fakeStore
	drains      atomic.Int64
	hadDeadline atomic.Bool
	limit       atomic.Int64
	block       chan struct{}
	once        sync.Once
}

func (s *drainStore) DrainCalcPending(ctx context.Context, limit int) (int, error) {
	_, ok := ctx.Deadline()
	s.hadDeadline.Store(ok)
	s.limit.Store(int64(limit))
	if s.drains.Add(1) == 3 {
		s.once.Do(func() { close(s.block) })
		<-ctx.Done()
		return 0, ctx.Err()
	}
	return 0, nil
}

// The calc tick (#996) drains pending calc marks on its own goroutine every
// calcTickEvery, each pass under a deadline and a bounded batch, and a
// shutdown ends a pass in flight rather than waiting on it.
func TestCalcTickDrains(t *testing.T) {
	st := &drainStore{block: make(chan struct{})}
	srv, err := New(Config{Host: "127.0.0.1", Port: -1}, st)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-st.block:
	case <-time.After(5*calcTickEvery + 2*time.Second):
		srv.Shutdown()
		t.Fatalf("%d drains in %s, want a pass every %s", st.drains.Load(), 5*calcTickEvery, calcTickEvery)
	}
	done := make(chan struct{})
	go func() { srv.Shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown waited on a calc pass in flight")
	}
	if !st.hadDeadline.Load() {
		t.Error("a calc pass ran with no deadline")
	}
	if got := st.limit.Load(); got != calcTickBatch {
		t.Errorf("a pass drained up to %d, want calcTickBatch %d", got, calcTickBatch)
	}
}
