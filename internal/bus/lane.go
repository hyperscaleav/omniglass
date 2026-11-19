package bus

import (
	"context"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// The ingest lanes' own account of themselves (#1517, #1896). Each telemetry
// durable is a lane with one message in flight at a time, so the fraction of
// wall time the handler holds it is the lane's utilisation, and the mean time
// a batch holds it is the lane's service time. Both come from a tally the
// lane keeps on every message it sees, whatever the outcome (landed,
// terminated or failed and left for redelivery): the lane was busy either way.
// Each partition keeps a tally of its own, and the durables outside the
// partitions (the push lane's, and one left draining) share one more, so the
// busiest partition is the one that says whether ingest keeps up. The server
// has no metrics endpoint, so each tally is logged as a running total every
// laneLogEvery, in the line the scale lab's harness reads
// (internal/labfootprint, the consumer lane busy row), and read in process by
// LaneTally and PartitionTallies.

// laneLogEvery is how often the running tallies are logged, when they moved.
var laneLogEvery = 30 * time.Second

// otherLane names the tally of the durables outside the partitions in the
// lane log line.
const otherLane = "other"

// ingestLane is one telemetry durable the server consumes.
type ingestLane struct {
	name string
	cons jetstream.Consumer
	cc   jetstream.ConsumeContext
}

// laneTally is one lane's running total: batches handled and the
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

func (t *laneTally) read() LaneTally {
	return LaneTally{Batches: t.batches.Load(), Busy: time.Duration(t.busy.Load())}
}

// newTallies is a tally for each of k partitions and one more, last, for the
// durables outside them.
func newTallies(k int) []*laneTally {
	ts := make([]*laneTally, k+1)
	for i := range ts {
		ts[i] = &laneTally{}
	}
	return ts
}

// LaneTally is a lane's running total since the server started: the batches
// its handler saw and the time it held the lane for them. The server has no
// metrics endpoint, so this and the periodic "ingest lane" log line are what
// a later one reads.
type LaneTally struct {
	Batches int64
	Busy    time.Duration
}

// LaneTally reads the running total over every lane.
func (s *Server) LaneTally() LaneTally {
	var sum LaneTally
	for _, t := range s.PartitionTallies() {
		sum.Batches += t.Batches
		sum.Busy += t.Busy
	}
	return sum
}

// PartitionTallies reads each partition's running total in order, then the
// total of the durables outside the partitions.
func (s *Server) PartitionTallies() []LaneTally {
	out := make([]LaneTally, len(s.tallies))
	for i, t := range s.tallies {
		out[i] = t.read()
	}
	return out
}

// laneName is how the lane log line names tally i among k partitions.
func laneName(i, k int) string {
	if i == k {
		return otherLane
	}
	return strconv.Itoa(i)
}

// startLaneLog logs each lane's running tally every laneLogEvery, only while
// its count moves, so an idle lane stays quiet. The totals are cumulative: a
// reader takes the difference between two lines of the same partition, which
// is why the line carries milliseconds of busy time rather than a fraction.
func (s *Server) startLaneLog() {
	ctx, cancel := context.WithCancel(context.Background())
	s.stopLaneLog = cancel
	s.sweepDone.Add(1)
	go func() {
		defer s.sweepDone.Done()
		t := time.NewTicker(laneLogEvery)
		defer t.Stop()
		logged := make([]int64, len(s.tallies))
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				for i, tally := range s.PartitionTallies() {
					if tally.Batches == logged[i] {
						continue
					}
					logged[i] = tally.Batches
					slog.Info("ingest lane", "facility", "ingest", "partition", laneName(i, s.partitions),
						"batches", tally.Batches, "busy_ms", tally.Busy.Milliseconds())
				}
			}
		}
	}()
}
