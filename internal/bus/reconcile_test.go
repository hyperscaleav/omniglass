package bus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/storage"
)

// stuckStore's reconcile pass blocks until its context ends, and records
// whether that context carried a deadline; its sweeps are counted.
type stuckStore struct {
	fakeStore
	entered     chan struct{}
	once        sync.Once
	hadDeadline atomic.Bool
	sweeps      atomic.Int64
}

func (s *stuckStore) ReconcileEventRules(ctx context.Context, _ *storage.RuleReconcileState) error {
	_, ok := ctx.Deadline()
	s.hadDeadline.Store(ok)
	s.once.Do(func() { close(s.entered) })
	<-ctx.Done()
	return ctx.Err()
}

func (s *stuckStore) SweepEventRules(context.Context, time.Time) error {
	s.sweeps.Add(1)
	return nil
}

// The reconcile pass (#928) never holds up the settlement sweep: it runs on
// a goroutine of its own, under a deadline, and a shutdown ends a pass in
// flight rather than waiting on it. A pass stuck on the database leaves the
// sweep acting on sustains and settlements every tick.
func TestReconcileRunsBesideTheSweep(t *testing.T) {
	st := &stuckStore{entered: make(chan struct{})}
	srv, err := New(Config{Host: "127.0.0.1", Port: -1}, st)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-st.entered:
	case <-time.After(12 * time.Second):
		srv.Shutdown()
		t.Fatal("no reconcile pass began")
	}
	before := st.sweeps.Load()
	deadline := time.Now().Add(2*settlementSweepEvery + time.Second)
	for st.sweeps.Load() == before && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	swept := st.sweeps.Load() > before
	done := make(chan struct{})
	go func() { srv.Shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown waited on a stuck reconcile pass")
	}
	if !swept {
		t.Error("the sweep stopped while a reconcile pass was stuck")
	}
	if !st.hadDeadline.Load() {
		t.Error("the reconcile pass ran with no deadline")
	}
}
