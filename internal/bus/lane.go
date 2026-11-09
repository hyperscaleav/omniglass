package bus

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// The ingest lane's own account of itself (#1517). The telemetry consumer is
// one lane: one durable with one message in flight at a time, so the fraction
// of wall time the handler holds it is the lane's utilisation, and the mean
// time a batch holds it is the lane's service time. Both come from a tally the
// handler keeps on every message it sees, whatever the outcome (landed,
// terminated or failed and left for redelivery): the lane was busy either way.
// The server has no metrics endpoint, so the tally is logged as a running
// total every laneLogEvery, in the line the scale lab's harness reads
// (internal/labfootprint, the consumer lane busy row), and read in process by
// LaneTally.

// laneLogEvery is how often the running tally is logged, when it moved.
var laneLogEvery = 30 * time.Second

// laneTally is the consumer lane's running total: batches handled and the
// nanoseconds the handler held the lane for them.
type laneTally struct {
	batches atomic.Int64
	busy    atomic.Int64
}

// observe records one handled message that held the lane for d.
func (t *laneTally) observe(d time.Duration) {
	t.batches.Add(1)
	t.busy.Add(int64(d))
}

// LaneTally is the consumer lane's running total since the server started:
// the batches its handler saw and the time it held the lane for them. The
// server has no metrics endpoint, so this and the periodic "ingest lane" log
// line are what a later one reads.
type LaneTally struct {
	Batches int64
	Busy    time.Duration
}

// LaneTally reads the consumer lane's running total.
func (s *Server) LaneTally() LaneTally {
	return LaneTally{Batches: s.lane.batches.Load(), Busy: time.Duration(s.lane.busy.Load())}
}

// startLaneLog logs the lane's running tally every laneLogEvery, only while
// the count moves, so an idle server stays quiet. The totals are cumulative:
// a reader takes the difference between two lines, which is why the line
// carries milliseconds of busy time rather than a fraction.
func (s *Server) startLaneLog() {
	ctx, cancel := context.WithCancel(context.Background())
	s.stopLaneLog = cancel
	s.sweepDone.Add(1)
	go func() {
		defer s.sweepDone.Done()
		t := time.NewTicker(laneLogEvery)
		defer t.Stop()
		var logged int64
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				tally := s.LaneTally()
				if tally.Batches == logged {
					continue
				}
				logged = tally.Batches
				slog.Info("ingest lane", "facility", "ingest", "batches", tally.Batches, "busy_ms", tally.Busy.Milliseconds())
			}
		}
	}()
}
