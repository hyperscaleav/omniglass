package node

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
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
	r := newNodeReport(time.Minute, t0)
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
	r := newNodeReport(time.Minute, t0)
	for i := range 3 {
		r.dispatch(dispatchWindow{passes: 4, passMax: 2 * time.Second}, t0.Add(time.Duration(i)*30*time.Second))
	}
	got := readings(r.take(t0.Add(time.Minute)))
	if _, ok := got[writerclass.TickOverrunCount]; ok {
		t.Fatalf("a punctual dispatcher reported overruns: %v", got)
	}
	if got[writerclass.TickDuration] != 2000 {
		t.Fatalf("tick-duration = %v ms, want 2000", got[writerclass.TickDuration])
	}
}

func TestNodeReportReadsNoTickDurationForAWindowWithNoPass(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	r := newNodeReport(time.Minute, t0)
	r.dispatch(dispatchWindow{}, t0)
	if got := readings(r.take(t0)); len(got) != 0 {
		t.Fatalf("an empty window read %v", got)
	}
}

func TestNodeReportEmitsEachWindowedCountOncePerWindowAndNoZeroNoise(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	r := newNodeReport(time.Minute, t0)

	windowed := []string{writerclass.TickOverrunCount, writerclass.ScheduleSkipCount, writerclass.SpoolDropCount, writerclass.PublishFailureCount}

	// Inside the window: counts accumulate, none is emitted.
	r.dispatch(dispatchWindow{overruns: 1, skips: 2}, t0)
	r.spool(spoolFigures{batches: 4, drops: 3, failures: 1}, t0)
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
	r.spool(spoolFigures{drops: 2}, t0.Add(time.Minute))
	end := r.take(t0.Add(time.Minute))
	got := readings(end)
	want := map[string]float64{
		writerclass.TickOverrunCount:    1,
		writerclass.ScheduleSkipCount:   3,
		writerclass.SpoolDropCount:      5,
		writerclass.PublishFailureCount: 1,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
		if _, dup := got[k+"#dup"]; dup {
			t.Errorf("%s emitted twice in one window", k)
		}
	}

	// A quiet window emits no windowed count at all, not a zero.
	quiet := readings(r.take(t0.Add(2 * time.Minute)))
	for _, k := range windowed {
		if _, ok := quiet[k]; ok {
			t.Errorf("a quiet window emitted %s = %v", k, quiet[k])
		}
	}
}

func TestNodeReportSamplesTheEachTickFigures(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	r := newNodeReport(time.Minute, t0)
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
	r := newNodeReport(time.Minute, t0)
	r.dispatch(dispatchWindow{passes: 1, passMax: time.Millisecond}, t0)
	got := readings(r.take(t0))
	for _, k := range []string{writerclass.ScheduleLateness, writerclass.ScheduleGapExcess} {
		if _, ok := got[k]; ok {
			t.Errorf("a tick that ran no task read %s", k)
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
	r := newNodeReport(time.Minute, t0)
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
