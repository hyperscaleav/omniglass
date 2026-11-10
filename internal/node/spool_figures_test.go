package node

import (
	"io"
	"log/slog"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"github.com/nats-io/nats.go"
)

// flowOf indexes a take's delivery readings by key and kind.
func flowOf(f spoolFigures) map[string]float64 {
	out := map[string]float64{}
	for _, r := range f.flow.Readings() {
		out[r.Key+"/"+r.Instance] = r.Value
	}
	return out
}

func wantFlow(t *testing.T, f spoolFigures, kind delivery.Kind, in, out, loss float64) {
	t.Helper()
	got := flowOf(f)
	k := "/" + string(kind)
	if got[writerclass.DeliveryInCount+k] != in || got[writerclass.DeliveryOutCount+k] != out || got[writerclass.DeliveryLossRatio+k] != loss {
		t.Fatalf("%s in %v out %v loss %v, want %v, %v, %v", kind,
			got[writerclass.DeliveryInCount+k], got[writerclass.DeliveryOutCount+k], got[writerclass.DeliveryLossRatio+k], in, out, loss)
	}
}

// The spool's part of the node's self-report (#1625, #1659): its depth each
// tick, and its delivery: the items each batch carried counted once it
// settles, in and out when the stream acks it, in alone when it is dropped at
// a cap. A batch held for a retry has not settled, so it counts in neither.
func TestSpoolCountsADroppedBatchInAndNotOut(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{down: true}
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", pub.publish)
	s.maxBatches = 3
	queueTagged(t, s, "b1")
	waitIdle(t, s) // not acked: the spool holds, nothing settled
	for _, tag := range []string{"b2", "b3", "b4", "b5"} {
		queueTagged(t, s, tag)
	}
	got := s.takeFigures()
	if got.batches != 3 {
		t.Fatalf("depth = %d, want 3", got.batches)
	}
	wantFlow(t, got, delivery.Metric, 2, 0, 1)

	again := s.takeFigures()
	if again.batches != 3 {
		t.Fatalf("second depth = %d, want 3", again.batches)
	}
	wantFlow(t, again, delivery.Metric, 0, 0, 0)

	pub.setDown(false)
	tick(t, s)
	after := s.takeFigures()
	if after.batches != 0 {
		t.Fatalf("after shipping depth = %d, want 0", after.batches)
	}
	wantFlow(t, after, delivery.Metric, 3, 3, 0)
}

// TestSpoolCountsAHeldBatchOnceWhenItShips: a publish the stream did not ack
// holds the batch for the next tick, not loss. Once acked it counts once in
// and once out, so the retry reads 0 loss.
func TestSpoolCountsAHeldBatchOnceWhenItShips(t *testing.T) {
	t.Parallel()
	pub := &stubPublisher{down: true}
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", pub.publish)
	queueTagged(t, s, "b1")
	waitIdle(t, s)
	tick(t, s) // a second attempt, still not acked
	wantFlow(t, s.takeFigures(), delivery.Metric, 0, 0, 0)
	pub.setDown(false)
	tick(t, s)
	wantFlow(t, s.takeFigures(), delivery.Metric, 1, 1, 0)
	if pub.attemptCount() < 3 {
		t.Fatalf("attempts = %d, want the batch sent at least three times", pub.attemptCount())
	}
}

func TestSpoolCountsABatchTheBusRefusesAsLost(t *testing.T) {
	t.Parallel()
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", func(string, []byte, string) (ackWait, error) {
		return nil, nats.ErrMaxPayload
	})
	queueTagged(t, s, "big")
	waitIdle(t, s)
	got := s.takeFigures()
	if got.batches != 0 {
		t.Fatalf("depth = %d, want the refused batch gone", got.batches)
	}
	wantFlow(t, got, delivery.Metric, 1, 0, 1)
}

// TestSpoolCountsAGroupItCannotEncodeAsLost: a group that fails to encode
// never reaches a batch, so its items went in and none came out.
func TestSpoolCountsAGroupItCannotEncodeAsLost(t *testing.T) {
	t.Parallel()
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", (&stubPublisher{}).publish)
	bad := &ogv1.TaskGroup{TaskId: "\xff", Metrics: []*ogv1.MetricSample{{Name: "a", Value: 1}, {Name: "b", Value: 2}}}
	if err := s.add(bad); err == nil {
		t.Fatal("a group whose task id is not UTF-8 encoded")
	}
	wantFlow(t, s.takeFigures(), delivery.Metric, 2, 0, 1)
}

// TestSpoolCountsTheSelfLogLinesTheBufferDroppedAsLost: a self-log line the
// bounded buffer dropped never reached a batch, so the node hop's figures
// count it in and not out (#1692), the same loss the lab's node row reads.
func TestSpoolCountsTheSelfLogLinesTheBufferDroppedAsLost(t *testing.T) {
	t.Parallel()
	s := newTelemetrySpool("og.v1.telemetry.edge", "edge", (&stubPublisher{}).publish)
	sink := newLogSink(slog.NewTextHandler(io.Discard, nil))
	logger := slog.New(sink)
	for i := 0; i < maxBufferedLogs+3; i++ {
		logger.Info("chatty", "i", i)
	}
	publishSelf(s, "edge", sink, nil)
	waitIdle(t, s)
	got := flowOf(s.takeFigures())
	k := "/" + string(delivery.Log)
	in, out := got[writerclass.DeliveryInCount+k], got[writerclass.DeliveryOutCount+k]
	if out == 0 || in-out != 3 {
		t.Fatalf("log in %v out %v, want the 3 lines the buffer dropped in and not out", in, out)
	}
}
