package node

import (
	"testing"

	"github.com/nats-io/nats.go"
)

// The spool's figures for the node's self-report (#1625): its depth each
// tick, and the batches it dropped and the publishes that failed since it last
// reported, each counted once.
func TestSpoolReportsItsDepthDropsAndFailures(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{down: true}
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", pub.publish)
	s.maxBatches = 3
	queueTagged(t, s, "b1")
	waitIdle(t, s) // not acked: one failure, the spool holds
	for _, tag := range []string{"b2", "b3", "b4", "b5"} {
		queueTagged(t, s, tag)
	}
	got := s.takeFigures()
	if got != (spoolFigures{batches: 3, drops: 2, failures: 1}) {
		t.Fatalf("figures = %+v, want 3 queued, 2 dropped at the cap, 1 publish not acked", got)
	}
	if again := s.takeFigures(); again != (spoolFigures{batches: 3}) {
		t.Fatalf("second take = %+v, want the depth alone: each drop and failure counts once", again)
	}
	pub.setDown(false)
	tick(t, s)
	if after := s.takeFigures(); after != (spoolFigures{}) {
		t.Fatalf("after shipping = %+v, want an empty spool and nothing new", after)
	}
}

func TestSpoolCountsABatchTheBusRefusesAsAFailureNotADrop(t *testing.T) {
	t.Parallel()
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", func(string, []byte, string) (ackWait, error) {
		return nil, nats.ErrMaxPayload
	})
	queueTagged(t, s, "big")
	waitIdle(t, s)
	if got := s.takeFigures(); got != (spoolFigures{failures: 1}) {
		t.Fatalf("figures = %+v, want one failure and nothing dropped as full", got)
	}
}
