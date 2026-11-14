package bus

import (
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/platformmetric"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// flushed is a recorder's window read back by key and instance.
func flushed(rec *platformmetric.Recorder) map[[2]string]float64 {
	out := map[[2]string]float64{}
	for _, m := range rec.Flush("server") {
		out[[2]string{m.Key, m.Instance}] = m.Value
	}
	return out
}

// TestLagObserverMeanAndMax proves the ingest lag of one committed batch on a
// fake clock: the window mean is over the batch's newest and oldest sample,
// the max is its oldest, and every sample counts toward throughput, a group's
// and the batch's own lanes alike, an untimed one included.
func TestLagObserverMeanAndMax(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	at := func(ago time.Duration) *timestamppb.Timestamp { return timestamppb.New(now.Add(-ago)) }
	ev := &ogv1.TelemetryBatch{
		Metrics: []*ogv1.MetricSample{{Name: "node-tick-duration", Ts: at(4 * time.Second)}},
		Groups: []*ogv1.TaskGroup{
			{TaskId: "t-1", Ts: at(10 * time.Second), Metrics: []*ogv1.MetricSample{
				{Name: "icmp-rtt-avg"}, // takes the group's ts
				{Name: "icmp-loss", Ts: at(2 * time.Second)},
			}},
			{TaskId: "t-2", Properties: []*ogv1.PropertySample{{Name: "power"}}}, // no ts anywhere: lands at ingest time
			{TaskId: "t-3", Events: []*ogv1.EventSample{{Name: "link-down", Ts: at(6 * time.Second)}}},
		},
	}
	rec := platformmetric.NewRecorder(func() time.Time { return now })
	observeLanding(pipelineMetrics{rec}, now, batchLanding(ev))

	got := flushed(rec)
	want := map[[2]string]float64{
		{writerclass.IngestLag, ""}:         6, // mean of newest (2 s) and oldest (10 s)
		{writerclass.IngestLagMax, ""}:      10,
		{writerclass.IngestSampleCount, ""}: 5,
	}
	if len(got) != len(want) {
		t.Fatalf("flushed %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%v = %v, want %v", k, got[k], v)
		}
	}
}

// TestLagObserverEmptyBatchObservesNothing proves a batch with no samples (a
// self-log tick) reads no lag and no throughput: nothing landed to be late.
func TestLagObserverEmptyBatchObservesNothing(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	rec := platformmetric.NewRecorder(func() time.Time { return now })
	observeLanding(pipelineMetrics{rec}, now, batchLanding(&ogv1.TelemetryBatch{Ts: timestamppb.New(now)}))
	if got := rec.Flush("server"); len(got) != 0 {
		t.Fatalf("an empty batch observed %+v, want nothing", got)
	}
}

// TestLagObserverUntimedBatchCountsWithoutLag proves samples with no ts on the
// sample, its group or the batch still count as throughput, but read no lag:
// the insert stamps them at ingest, so they were never late.
func TestLagObserverUntimedBatchCountsWithoutLag(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	rec := platformmetric.NewRecorder(func() time.Time { return now })
	ev := &ogv1.TelemetryBatch{Metrics: []*ogv1.MetricSample{{Name: "a"}, {Name: "b"}}}
	observeLanding(pipelineMetrics{rec}, now, batchLanding(ev))
	got := flushed(rec)
	if len(got) != 1 || got[[2]string{writerclass.IngestSampleCount, ""}] != 2 {
		t.Fatalf("flushed %v, want only ingest-sample-count 2", got)
	}
}

// TestWorklistDeltasArePerWindow proves the worklist outcome counts flush as
// each window's difference, not the server's running total: the second
// window reads only what happened in it, and an outcome that did not move
// says nothing.
func TestWorklistDeltasArePerWindow(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	rec := platformmetric.NewRecorder(func() time.Time { return now })
	cur := WorklistCounts{Served: 10, Busy: 2}
	sample := worklistSampler(func() WorklistCounts { return cur })

	cur = WorklistCounts{Served: 14, Busy: 2, ReadFailed: 1}
	sample(pipelineMetrics{rec})
	first := flushed(rec)
	if first[[2]string{writerclass.BusWorklistPullCount, "served"}] != 4 ||
		first[[2]string{writerclass.BusWorklistPullCount, "read-failed"}] != 1 {
		t.Fatalf("first window %v, want served 4 and read-failed 1", first)
	}
	if _, ok := first[[2]string{writerclass.BusWorklistPullCount, "busy"}]; ok {
		t.Fatalf("first window read busy %v, which did not move", first)
	}

	cur = WorklistCounts{Served: 21, Busy: 5, ReadFailed: 1}
	sample(pipelineMetrics{rec})
	second := flushed(rec)
	if second[[2]string{writerclass.BusWorklistPullCount, "served"}] != 7 ||
		second[[2]string{writerclass.BusWorklistPullCount, "busy"}] != 3 {
		t.Fatalf("second window %v, want served 7 and busy 3 (the window's, not the running total)", second)
	}
	if v, ok := second[[2]string{writerclass.BusWorklistPullCount, "read-failed"}]; !ok || v != 0 {
		t.Fatalf("second window read-failed = %v (%v), want its one closing zero", v, ok)
	}
}

// TestWorklistDeltasNeverNegative proves a count that reads lower than the
// last (a restarted server's fresh tally) is no window's activity, not a
// negative count.
func TestWorklistDeltasNeverNegative(t *testing.T) {
	d := worklistDeltas(WorklistCounts{Served: 9}, WorklistCounts{Served: 3})
	if len(d) != 0 {
		t.Fatalf("deltas %+v, want none", d)
	}
}

// TestLaneBusyIsTheWindowFraction proves the lane busy figure is the busy
// time the window added over the window's wall time.
func TestLaneBusyIsTheWindowFraction(t *testing.T) {
	if got := laneBusy([]LaneTally{{Busy: 2 * time.Second}}, []LaneTally{{Busy: 5 * time.Second}}, 1, 10*time.Second); got != 0.3 {
		t.Fatalf("lane busy = %v, want 0.3", got)
	}
	if got := laneBusy([]LaneTally{{}}, []LaneTally{{Busy: time.Second}}, 1, 0); got != 0 {
		t.Fatalf("lane busy over no wall time = %v, want 0", got)
	}
}

// TestLaneBusyIsTheBusiestPartition proves that with lanes side by side the
// figure is the busiest one's window, not their sum or mean: ingest keeps up
// only while every lane does (#1896).
func TestLaneBusyIsTheBusiestPartition(t *testing.T) {
	prev := []LaneTally{{Busy: time.Second}, {Busy: 4 * time.Second}, {}}
	cur := []LaneTally{{Busy: 3 * time.Second}, {Busy: 11 * time.Second}, {Busy: time.Second}}
	if got := laneBusy(prev, cur, 3, 10*time.Second); got != 0.7 {
		t.Fatalf("lane busy = %v, want 0.7 (the second lane's)", got)
	}
}

// TestLaneBusyLeavesOutTheOtherTally proves the figure reads the partitions
// alone: the tally past them sums the push durable and every partition a
// larger count left draining, several consumers at once, so its busy time is
// no one lane's and can pass the window's wall time.
func TestLaneBusyLeavesOutTheOtherTally(t *testing.T) {
	prev := []LaneTally{{}, {}, {}}
	cur := []LaneTally{{Busy: 2 * time.Second}, {Busy: time.Second}, {Busy: 25 * time.Second}}
	if got := laneBusy(prev, cur, 2, 10*time.Second); got != 0.2 {
		t.Fatalf("lane busy = %v, want 0.2 (the first partition's, not the other tally's)", got)
	}
}

// TestTheAckFloorIsBelowTheFirstHeldBatch proves the work queue's floor
// reads from the stream: below its first held batch, at its head once empty,
// and 0 before anything was stored.
func TestTheAckFloorIsBelowTheFirstHeldBatch(t *testing.T) {
	for _, c := range []struct{ first, want uint64 }{{0, 0}, {1, 0}, {42, 41}} {
		if got := ackFloor(c.first); got != c.want {
			t.Errorf("ackFloor(%d) = %d, want %d", c.first, got, c.want)
		}
	}
}

// TestPipelineMetricsZeroValueRecordsNothing proves a Server built without a
// recorder (most in-package fixtures) instruments as a no-op rather than a
// nil dereference.
func TestPipelineMetricsZeroValueRecordsNothing(t *testing.T) {
	var m pipelineMetrics
	m.count(writerclass.IngestBatchCount, "", 1)
	m.mean(writerclass.IngestLag, "", 1)
	m.max(writerclass.IngestLagMax, "", 1)
	m.gauge(writerclass.BusConsumerPending, "", 1)
	m.failure(writerclass.SweepFailureCount, "", true)
}

// TestPushTurnCountsPublishesRefusalsAndWait (#1746): every push lane turn
// counts toward push-lane-publish-count, a refused one toward
// push-lane-refusal-count as well, so refusals read as a ratio where zero is
// healthy, and every turn's wait folds into the window's p99 and max, in
// milliseconds, a refusal's included: it waited the whole bound.
func TestPushTurnCountsPublishesRefusalsAndWait(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	rec := platformmetric.NewRecorder(func() time.Time { return now })
	m := pipelineMetrics{rec}
	for range 3 {
		m.pushTurn(delivery.Turn{Wait: 2 * time.Millisecond})
	}
	m.pushTurn(delivery.Turn{Wait: 5 * time.Second, Refused: true})

	got := flushed(rec)
	if got[[2]string{writerclass.PushLanePublishCount, ""}] != 4 || got[[2]string{writerclass.PushLaneRefusalCount, ""}] != 1 {
		t.Fatalf("flushed %v, want four publishes, one refused", got)
	}
	if got[[2]string{writerclass.PushLaneTurnWaitMax, ""}] != 5000 {
		t.Fatalf("turn wait max = %v, want the refusal's 5000 ms", got[[2]string{writerclass.PushLaneTurnWaitMax, ""}])
	}
	if p99 := got[[2]string{writerclass.PushLaneTurnWaitP99, ""}]; p99 < 5000*7/8 || p99 > 5000*9/8 {
		t.Fatalf("turn wait p99 = %v, want about the refusal's 5000 ms", p99)
	}
	if len(got) != 4 {
		t.Fatalf("flushed %v, want only the four push lane keys", got)
	}

	rec.Flush("server")
	m.pushTurn(delivery.Turn{Wait: 3 * time.Second, Abandoned: true})
	got = flushed(rec)
	if _, ok := got[[2]string{writerclass.PushLanePublishCount, ""}]; ok {
		t.Fatalf("flushed %v, want an abandoned wait counted as no publish: it never took the turn", got)
	}
	if _, ok := got[[2]string{writerclass.PushLaneRefusalCount, ""}]; ok {
		t.Fatalf("flushed %v, want an abandoned wait counted as no refusal: the lane never answered it 503", got)
	}
	if got[[2]string{writerclass.PushLaneTurnWaitMax, ""}] != 3000 {
		t.Fatalf("turn wait max = %v, want the abandoned 3000 ms: a pusher that gave up waiting still waited", got[[2]string{writerclass.PushLaneTurnWaitMax, ""}])
	}
	if p99 := got[[2]string{writerclass.PushLaneTurnWaitP99, ""}]; p99 < 3000*7/8 || p99 > 3000*9/8 {
		t.Fatalf("turn wait p99 = %v, want about the abandoned 3000 ms", p99)
	}

	rec.Flush("server")
	m.pushTurn(delivery.Turn{Wait: time.Millisecond})
	if n, ok := flushed(rec)[[2]string{writerclass.PushLaneRefusalCount, ""}]; !ok || n != 0 {
		t.Fatalf("a window of publishes and no refusal reads refusals %v (landed %v), want a 0 that landed: the ratio reads healthy, not unreported", n, ok)
	}
}

// TestFailureCountsLandAZeroBesideTheirAttempts (#1769): a bus request, a
// sweep and a telemetry ack each count a failure on every attempt, 1 when it
// failed and 0 when it did not, so a healthy window's failures land beside
// their attempts as a 0 and the pair reads "0 / n" rather than a failure
// figure never reported.
func TestFailureCountsLandAZeroBesideTheirAttempts(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	rec := platformmetric.NewRecorder(func() time.Time { return now })
	s := &Server{metrics: pipelineMetrics{rec}}

	s.request(requestHeartbeat, nil)
	s.request(requestHeartbeat, nil)
	s.request(requestCommandPull, errors.New("db down"))
	_ = s.sweep(writerclass.SweepEventRule, func() error { return nil })
	_ = s.sweep(writerclass.SweepCommandSettlement, func() error { return errors.New("db down") })
	s.ack(&fakeMsg{})
	s.ack(&fakeMsg{})

	got := flushed(rec)
	for key, want := range map[[2]string]float64{
		{writerclass.BusRequestCount, requestHeartbeat}:                     2,
		{writerclass.BusRequestFailureCount, requestHeartbeat}:              0,
		{writerclass.BusRequestCount, requestCommandPull}:                   1,
		{writerclass.BusRequestFailureCount, requestCommandPull}:            1,
		{writerclass.SweepCount, writerclass.SweepEventRule}:                1,
		{writerclass.SweepFailureCount, writerclass.SweepEventRule}:         0,
		{writerclass.SweepCount, writerclass.SweepCommandSettlement}:        1,
		{writerclass.SweepFailureCount, writerclass.SweepCommandSettlement}: 1,
		{writerclass.BusTelemetryAckFailureCount, ""}:                       0,
	} {
		if n, ok := got[key]; !ok || n != want {
			t.Errorf("%s{%s} = %v (landed %v), want %v", key[0], key[1], n, ok, want)
		}
	}

	rec.Flush("server")
	s.ack(&fakeMsg{ackErr: errors.New("ack timed out")})
	if n := flushed(rec)[[2]string{writerclass.BusTelemetryAckFailureCount, ""}]; n != 1 {
		t.Fatalf("an unconfirmed ack counts %v, want 1", n)
	}
}
