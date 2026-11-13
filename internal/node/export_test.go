package node

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/jackc/pgx/v5"
)

// HopRig is a node's telemetry spool over a real bus and a real Postgres, as
// the enrolled node edge-1 holding the poll task t-1, for the tests in
// package node_test: they read what it landed through packages that import
// this one (#1692). While it is down every publish fails as an outage
// fails it, so the spool holds its batches and, at its cap, overflows.
type HopRig struct {
	Conn *pgx.Conn

	spool *telemetrySpool
	mu    sync.Mutex
	down  bool
}

// NewHopRig is a rig whose spool holds at most maxBatches batches.
func NewHopRig(t *testing.T, maxBatches int) *HopRig {
	t.Helper()
	f := newSpoolFixture(t)
	nc, err := connectBus(f.srv.ClientURL(), "edge-1", "token-1", "edge-1")
	if err != nil {
		t.Fatalf("connect as the node: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := newNodeJetStream(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	real := jetStreamPublish(js)
	r := &HopRig{Conn: f.conn}
	r.spool = newTelemetrySpool(collection.TelemetrySubject("edge-1"), "edge-1", func(subject string, data []byte, id string) (ackWait, error) {
		r.mu.Lock()
		down := r.down
		r.mu.Unlock()
		if down {
			return answered(errors.New("the bus is unreachable")), nil
		}
		return real(subject, data, id)
	})
	r.spool.maxBatches = maxBatches
	return r
}

// SetDown takes the bus away from the spool, or gives it back.
func (r *HopRig) SetDown(down bool) {
	r.mu.Lock()
	r.down = down
	r.mu.Unlock()
}

// Tick is one tick of t-1: one tcp-connect-time sample of value at ts, added
// and flushed into a batch of its own, then the shipper left to stop.
func (r *HopRig) Tick(t *testing.T, value float64, ts time.Time) {
	t.Helper()
	if err := r.spool.add(groupFor("t-1", []collection.Sample{{Name: "tcp-connect-time", Value: value, TS: ts}})); err != nil {
		t.Fatalf("add the sample: %v", err)
	}
	r.spool.flush()
	waitIdle(t, r.spool)
}

// Retry is the run loop's retry tick.
func (r *HopRig) Retry(t *testing.T) {
	t.Helper()
	tick(t, r.spool)
}

// Pending is the batches the spool still holds.
func (r *HopRig) Pending() int { return r.spool.pending() }
