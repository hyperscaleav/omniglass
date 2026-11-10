package bus

import (
	"testing"
	"time"

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
	if got := laneBusy(LaneTally{Busy: 2 * time.Second}, LaneTally{Busy: 5 * time.Second}, 10*time.Second); got != 0.3 {
		t.Fatalf("lane busy = %v, want 0.3", got)
	}
	if got := laneBusy(LaneTally{}, LaneTally{Busy: time.Second}, 0); got != 0 {
		t.Fatalf("lane busy over no wall time = %v, want 0", got)
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
}
