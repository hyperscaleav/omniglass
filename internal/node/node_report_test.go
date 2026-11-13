package node

import (
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/platformmetric"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
)

// readings indexes a take by key and instance.
func readings(ms []*ogv1.MetricSample) map[string]float64 {
	out := map[string]float64{}
	for _, m := range ms {
		k := m.GetName()
		if m.GetInstance() != "" {
			k += "/" + m.GetInstance()
		}
		if _, dup := out[k]; dup {
			out[k+"#dup"] = m.GetValue()
		}
		out[k] = m.GetValue()
	}
	return out
}

// #1634: the dispatcher's window feeds the report: the dues it met in
// flight and passed over tally toward the report window, its longest pass is
// the tick duration.
func TestNodeReportTalliesTheDispatchWindows(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	r := newNodeReport(time.Minute, 0, t0)
	r.dispatch(dispatchWindow{passes: 3, passMax: 2 * time.Second, overruns: 1, skips: 2}, t0)
	r.dispatch(dispatchWindow{passes: 1, passMax: time.Millisecond, overruns: 1}, t0.Add(30*time.Second))
	got := readings(r.take(t0.Add(time.Minute)))
	if got[writerclass.TickOverrunCount] != 2 || got[writerclass.ScheduleSkipCount] != 2 {
		t.Fatalf("overruns %v, skips %v, want 2 and 2: %v", got[writerclass.TickOverrunCount], got[writerclass.ScheduleSkipCount], got)
	}
}

func TestNodeReportReadsAPunctualDispatcherAsNoOverrun(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	r := newNodeReport(time.Minute, 0, t0)
	for i := range 3 {
		r.dispatch(dispatchWindow{passes: 4, passMax: 2 * time.Second}, t0.Add(time.Duration(i)*30*time.Second))
	}
	got := readings(r.take(t0.Add(time.Minute)))
	if v, ok := got[writerclass.TickOverrunCount]; !ok || v != 0 {
		t.Fatalf("a punctual dispatcher's window read tick-overrun-count %v (written %v), want 0: %v", v, ok, got)
	}
	if got[writerclass.TickDuration] != 2000 {
		t.Fatalf("tick-duration = %v ms, want 2000", got[writerclass.TickDuration])
	}
}

func TestNodeReportReadsNoTickDurationForAWindowWithNoPass(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	r := newNodeReport(time.Minute, 0, t0)
	r.dispatch(dispatchWindow{}, t0)
	if got := readings(r.take(t0)); len(got) != 0 {
		t.Fatalf("an empty dispatch window read %v", got)
	}
}

func TestNodeReportEmitsEachWindowedCountOncePerWindowZeroWhenQuiet(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	r := newNodeReport(time.Minute, 0, t0)

	windowed := []string{writerclass.TickOverrunCount, writerclass.ScheduleSkipCount}

	// Inside the window: counts accumulate, none is emitted.
	r.dispatch(dispatchWindow{overruns: 1, skips: 2}, t0)
	r.spool(spoolFigures{batches: 4}, t0)
	mid := readings(r.take(t0.Add(30 * time.Second)))
	for _, k := range windowed {
		if _, ok := mid[k]; ok {
			t.Fatalf("%s emitted before its window ended: %v", k, mid)
		}
	}
	if mid[writerclass.SpoolBatchCount] != 4 {
		t.Fatalf("spool-batch-count = %v, want each tick's depth 4", mid[writerclass.SpoolBatchCount])
	}

	// The window ends: each count lands once with the window's whole tally.
	r.dispatch(dispatchWindow{skips: 1}, t0.Add(time.Minute))
	end := r.take(t0.Add(time.Minute))
	got := readings(end)
	want := map[string]float64{
		writerclass.TickOverrunCount:  1,
		writerclass.ScheduleSkipCount: 3,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
		if _, dup := got[k+"#dup"]; dup {
			t.Errorf("%s emitted twice in one window", k)
		}
	}

	// A quiet window emits each windowed count once, as zero.
	quiet := readings(r.take(t0.Add(2 * time.Minute)))
	for _, k := range windowed {
		if v, ok := quiet[k]; !ok || v != 0 {
			t.Errorf("a quiet window read %s = %v (written %v), want 0", k, v, ok)
		}
		if _, dup := quiet[k+"#dup"]; dup {
			t.Errorf("%s emitted twice in a quiet window", k)
		}
	}
}

// TestNodeReportCountsReturnToZeroAfterAQuietWindow: the node's first window
// runs from its start, so it is whole and writes every count, zero included;
// a window with an overrun and a skip follows, then a
// quiet one, and each key's latest value is 0 again at the quiet window's end
// rather than the event's count left standing as current.
func TestNodeReportCountsReturnToZeroAfterAQuietWindow(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	t1, t2, t3 := t0.Add(time.Minute), t0.Add(2*time.Minute), t0.Add(3*time.Minute)
	r := newNodeReport(time.Minute, 0, t0)
	windowed := []string{writerclass.TickOverrunCount, writerclass.ScheduleSkipCount}

	first := readings(r.take(t1))
	for _, k := range windowed {
		if v, ok := first[k]; !ok || v != 0 {
			t.Errorf("the first window read %s = %v (written %v), want 0", k, v, ok)
		}
	}

	r.dispatch(dispatchWindow{passes: 1, passMax: 40 * time.Second, overruns: 1, skips: 1}, t1.Add(time.Second))
	event := r.take(t2)
	quiet := r.take(t3)

	latest := map[string]*ogv1.MetricSample{}
	for _, m := range append(event, quiet...) {
		latest[m.GetName()] = m
	}
	for _, k := range windowed {
		m, ok := latest[k]
		if !ok || !m.GetTs().AsTime().Equal(t3) || m.GetValue() != 0 {
			t.Errorf("%s latest = %v (written %v), want 0 at the quiet window's end %v", k, m, ok, t3)
		}
	}
}

// TestNodeReportEmitsDeliveryPerKindOncePerWindow (#1659): the spool's
// settled items tally over the report window and land once at its end, in,
// out and the loss ratio for every telemetry kind, the kind as the instance:
// a window with drops reads a loss above 0, a kind with no traffic reads 0,
// and a quiet window writes every figure as 0.
func TestNodeReportEmitsDeliveryPerKindOncePerWindow(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	r := newNodeReport(time.Minute, 0, t0)
	key := func(k string, kind delivery.Kind) string { return k + "/" + string(kind) }

	var early delivery.Counts
	early.Delivered(map[delivery.Kind]int{delivery.Metric: 3})
	early.Lost(map[delivery.Kind]int{delivery.Metric: 1})
	r.spool(spoolFigures{batches: 1, flow: early}, t0)
	mid := readings(r.take(t0.Add(30 * time.Second)))
	for _, k := range []string{writerclass.DeliveryInCount, writerclass.DeliveryOutCount, writerclass.DeliveryLossRatio} {
		if _, ok := mid[key(k, delivery.Metric)]; ok {
			t.Fatalf("%s emitted before its window ended: %v", k, mid)
		}
	}

	var late delivery.Counts
	late.Delivered(map[delivery.Kind]int{delivery.Log: 2})
	r.spool(spoolFigures{flow: late}, t0.Add(time.Minute))
	end := readings(r.take(t0.Add(time.Minute)))
	want := map[string]float64{
		key(writerclass.DeliveryInCount, delivery.Metric):     4,
		key(writerclass.DeliveryOutCount, delivery.Metric):    3,
		key(writerclass.DeliveryLossRatio, delivery.Metric):   0.25,
		key(writerclass.DeliveryInCount, delivery.Log):        2,
		key(writerclass.DeliveryOutCount, delivery.Log):       2,
		key(writerclass.DeliveryLossRatio, delivery.Log):      0,
		key(writerclass.DeliveryInCount, delivery.Property):   0,
		key(writerclass.DeliveryLossRatio, delivery.Property): 0,
		key(writerclass.DeliveryInCount, delivery.Event):      0,
		key(writerclass.DeliveryLossRatio, delivery.Event):    0,
	}
	for k, v := range want {
		if g, ok := end[k]; !ok || g != v {
			t.Errorf("%s = %v (written %v), want %v", k, g, ok, v)
		}
		if _, dup := end[k+"#dup"]; dup {
			t.Errorf("%s emitted twice in one window", k)
		}
	}

	quiet := readings(r.take(t0.Add(2 * time.Minute)))
	for _, kind := range delivery.Kinds {
		for _, k := range []string{writerclass.DeliveryInCount, writerclass.DeliveryOutCount, writerclass.DeliveryLossRatio} {
			if v, ok := quiet[key(k, kind)]; !ok || v != 0 {
				t.Errorf("a quiet window read %s = %v (written %v), want 0", key(k, kind), v, ok)
			}
		}
	}
}

func TestNodeReportSamplesTheEachTickFigures(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	r := newNodeReport(time.Minute, 0, t0)
	r.dispatch(dispatchWindow{passes: 2, passMax: 1500 * time.Millisecond, runs: 3, lateMax: 250 * time.Millisecond, gapMax: 15 * time.Second}, t0)
	r.spool(spoolFigures{batches: 0}, t0)
	r.sessions(map[collection.SessionState]int{collection.SessionUp: 3, collection.SessionDown: 1}, t0)
	r.process(platformmetric.Process{Goroutines: 40, HeapBytes: 8 << 20, GCs: 1, LastGCPause: 2 * time.Millisecond}, t0)
	r.worklist(17, t0)
	ms := r.take(t0)
	got := readings(ms)
	want := map[string]float64{
		writerclass.TickDuration:                                                    1500,
		writerclass.ScheduleLateness:                                                250,
		writerclass.ScheduleGapExcess:                                               15000,
		writerclass.SpoolBatchCount:                                                 0,
		writerclass.SessionHeldCount + "/" + string(collection.SessionUp):           3,
		writerclass.SessionHeldCount + "/" + string(collection.SessionDown):         1,
		writerclass.SessionHeldCount + "/" + string(collection.SessionEstablishing): 0,
		writerclass.SessionHeldCount + "/" + string(collection.SessionRecovering):   0,
		writerclass.SessionHeldCount + "/" + string(collection.SessionRejected):     0,
		writerclass.ProcessGoroutineCount:                                           40,
		writerclass.ProcessHeapSize:                                                 8 << 20,
		writerclass.ProcessGCPause:                                                  2,
		writerclass.WorklistTaskCount:                                               17,
	}
	for k, v := range want {
		if g, ok := got[k]; !ok || g != v {
			t.Errorf("%s = %v (present %v), want %v", k, g, ok, v)
		}
	}
	for _, m := range ms {
		if !writerclass.NodeSelf(m.GetName()) {
			t.Errorf("%s is not a declared node-self key", m.GetName())
		}
		if !m.GetTs().AsTime().Equal(t0) {
			t.Errorf("%s stamped %v, want its sampling time", m.GetName(), m.GetTs().AsTime())
		}
		if m.GetInstance() != "" && m.GetName() != writerclass.SessionHeldCount {
			t.Errorf("%s carries instance %q; only session-held-count does", m.GetName(), m.GetInstance())
		}
	}
	if again := r.take(t0.Add(time.Second)); len(again) != 0 {
		t.Fatalf("a take with nothing sampled since emitted %v", again)
	}
}

func TestNodeReportReadsNoScheduleFigureOnAWindowThatRanNothing(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	r := newNodeReport(time.Minute, 0, t0)
	r.dispatch(dispatchWindow{passes: 1, passMax: time.Millisecond}, t0)
	got := readings(r.take(t0))
	for _, k := range []string{writerclass.ScheduleLateness, writerclass.ScheduleGapExcess} {
		if _, ok := got[k]; ok {
			t.Errorf("a window that ran no task read %s", k)
		}
	}
}

func TestNodeReportIsNilSafe(t *testing.T) {
	t.Parallel()
	var r *nodeReport
	r.dispatch(dispatchWindow{passes: 1, runs: 1}, time.Now())
	r.worklist(1, time.Now())
	if got := r.take(time.Now()); got != nil {
		t.Fatalf("a nil report took %v", got)
	}
}

// TestNodeReportReadsCPUThroughTheSharedRuntime: the node reads its process
// through platformmetric.Runtime, the sampler the server shares, so its CPU
// time is the time spent between two ticks and the first tick reads none.
func TestNodeReportReadsCPUThroughTheSharedRuntime(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	r := newNodeReport(time.Minute, 0, t0)
	r.process(platformmetric.Process{CPU: 4 * time.Second, FDs: 12}, t0)
	first := readings(r.take(t0))
	if _, ok := first[writerclass.ProcessCPUSeconds]; ok {
		t.Fatalf("the first tick read cpu %v with nothing to subtract", first)
	}
	if first[writerclass.ProcessFDCount] != 12 {
		t.Errorf("fd count = %v, want 12", first[writerclass.ProcessFDCount])
	}
	r.process(platformmetric.Process{CPU: 5500 * time.Millisecond, FDs: 12}, t0.Add(30*time.Second))
	if got := readings(r.take(t0.Add(30 * time.Second)))[writerclass.ProcessCPUSeconds]; got != 1.5 {
		t.Errorf("cpu = %v, want the 1.5 s spent since the last tick", got)
	}
}

// TestNodeReportCountsAFailedCommandPullOncePerWindow (#1626): every failed
// pull of the command queue is tallied, and the window's tally is sent once
// when it ends; a window with no failure sends zero, so the count returns to
// zero after the window that had it.
func TestNodeReportCountsAFailedCommandPullOncePerWindow(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	r := newNodeReport(time.Minute, 0, t0)
	r.commandPullFailed()
	if mid := readings(r.take(t0.Add(30 * time.Second))); mid[writerclass.CommandPullFailureCount] != 0 {
		t.Fatalf("command-pull-failure-count emitted before its window ended: %v", mid)
	}
	r.commandPullFailed()
	got := readings(r.take(t0.Add(time.Minute)))
	if got[writerclass.CommandPullFailureCount] != 2 {
		t.Errorf("command-pull-failure-count = %v, want the window's 2: %v", got[writerclass.CommandPullFailureCount], got)
	}
	if _, dup := got[writerclass.CommandPullFailureCount+"#dup"]; dup {
		t.Error("command-pull-failure-count emitted twice in one window")
	}
	if quiet := readings(r.take(t0.Add(2 * time.Minute))); quiet[writerclass.CommandPullFailureCount] != 0 {
		t.Errorf("a window with no failed pull sent %v, want 0", quiet)
	} else if _, ok := quiet[writerclass.CommandPullFailureCount]; !ok {
		t.Errorf("a window with no failed pull wrote no command-pull-failure-count: %v", quiet)
	}
	var none *nodeReport
	none.commandPullFailed()
}

// TestNodeReportTalliesAPullFailureFromTheCommandCycleSafely: the command
// cycle runs on its own goroutine beside the heartbeat (#1634), so a failed
// pull tallies while the heartbeat takes the report; under -race neither
// races and no failure is lost.
func TestNodeReportTalliesAPullFailureFromTheCommandCycleSafely(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	r := newNodeReport(time.Minute, 0, t0)
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 100 {
			r.commandPullFailed()
		}
	})
	var total float64
	for i := range 50 {
		total += readings(r.take(t0.Add(time.Duration(i+1) * time.Minute)))[writerclass.CommandPullFailureCount]
	}
	wg.Wait()
	total += readings(r.take(t0.Add(time.Hour)))[writerclass.CommandPullFailureCount]
	if total != 100 {
		t.Fatalf("the windows counted %v failed pulls, want 100", total)
	}
}

// #1693: the dispatcher's schedule hop rides the node-self block beside the
// spool's delivery, under the hop's own instances, schedule/{kind}, once per
// report window, every kind written, a quiet window's as zero; the spool's
// bare-kind series are untouched by it.
func TestNodeReportEmitsTheScheduleHopPerKindOncePerWindow(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	r := newNodeReport(time.Minute, 0, t0)
	var first, second dispatchWindow
	first.schedule.Scheduled([]delivery.Kind{delivery.Metric, delivery.Property}, 2, 1)
	second.schedule.Scheduled([]delivery.Kind{delivery.Metric}, 1, 0)
	r.dispatch(first, t0)
	if _, ok := readings(r.take(t0.Add(30 * time.Second)))[writerclass.DeliveryInCount+"/schedule/metric"]; ok {
		t.Fatal("the schedule hop emitted before its window ended")
	}
	r.dispatch(second, t0.Add(time.Minute))
	end := readings(r.take(t0.Add(time.Minute)))
	want := map[string]float64{
		writerclass.DeliveryInCount + "/schedule/metric":            4,
		writerclass.DeliveryOutCount + "/schedule/metric":           3,
		writerclass.DeliveryLossRatio + "/schedule/metric":          0.25,
		writerclass.DeliveryInCount + "/schedule/property":          3,
		writerclass.DeliveryOutCount + "/schedule/property":         2,
		writerclass.DeliveryInCount + "/schedule/event":             0,
		writerclass.DeliveryLossRatio + "/schedule/log":             0,
		writerclass.DeliveryInCount + "/" + string(delivery.Metric): 0, // the spool's own series
	}
	for k, v := range want {
		if g, ok := end[k]; !ok || g != v {
			t.Errorf("%s = %v (written %v), want %v", k, g, ok, v)
		}
		if _, dup := end[k+"#dup"]; dup {
			t.Errorf("%s emitted twice in one window", k)
		}
	}
	quiet := readings(r.take(t0.Add(2 * time.Minute)))
	for _, kind := range delivery.Kinds {
		k := writerclass.DeliveryInCount + "/" + delivery.HopInstance(delivery.Schedule, kind)
		if v, ok := quiet[k]; !ok || v != 0 {
			t.Errorf("a quiet window read %s = %v (written %v), want 0", k, v, ok)
		}
	}
}
