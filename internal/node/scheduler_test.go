package node

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
)

// everyPoll is a driver poll of endpoint on a declared interval.
func everyPoll(id, endpoint, every string) collection.TaskSpec {
	return collection.TaskSpec{ID: id, Mode: "poll", Transport: "snmp", EndpointID: endpoint, EndpointName: "snmp",
		EndpointParams: json.RawMessage(`{"target":"10.20.4.40:161"}`),
		Spec: json.RawMessage(fmt.Sprintf(`{"driver": "edge", "function": "scalars", "schedule": {"every": %q},
			"request": {"get": ["1.3.6.1.2.1.1.1.0"]},
			"emits": [{"name": "model-number", "lane": "property", "extract": {"oid": "1.3.6.1.2.1.1.1.0"}}]}`, every)),
		Secrets: map[string]map[string]string{"community": {"community": "lab-public"}}}
}

// schedT0 is an arbitrary wall time the scheduler tests start near.
var schedT0 = time.Date(2026, 11, 9, 14, 3, 0, 0, time.UTC)

// aligned is the first time at or after t on id's phase grid for every, so
// a test can start where the task's steady slots fall.
func aligned(t time.Time, id string, every time.Duration) time.Time {
	return slotAfter(t.Add(-time.Nanosecond), phaseOf(id, every), every)
}

// schedHarness drives a dispatcher on a fake clock, its work a fake runner
// that records when each task started and, when gated, blocks until released.
type schedHarness struct {
	t      *testing.T
	d      *dispatcher
	report *collectionReport

	mu      sync.Mutex
	now     time.Time
	runs    map[string][]time.Time
	spans   map[string][][2]time.Time // real start and end of each run
	gate    chan struct{}             // nil runs at once; else each run waits on it
	started chan string
}

func newSchedHarness(t *testing.T, start time.Time, workers int, heartbeat time.Duration) *schedHarness {
	h := &schedHarness{t: t, now: start, runs: map[string][]time.Time{}, spans: map[string][][2]time.Time{},
		started: make(chan string, 256), report: newCollectionReport(time.Hour, start)}
	h.d = newDispatcher(dispatchConfig{workers: workers, heartbeat: heartbeat, clock: h.clock,
		sched: newPollSchedule(), report: h.report, work: h.work})
	t.Cleanup(h.d.pool.wait)
	return h
}

func (h *schedHarness) clock() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.now
}

func (h *schedHarness) set(at time.Time) {
	h.mu.Lock()
	h.now = at
	h.mu.Unlock()
}

func (h *schedHarness) work(_ context.Context, task collection.TaskSpec) {
	began := time.Now()
	h.mu.Lock()
	h.runs[task.ID] = append(h.runs[task.ID], h.now)
	gate := h.gate
	h.mu.Unlock()
	h.started <- task.ID
	if gate != nil {
		<-gate
	}
	h.mu.Lock()
	h.spans[task.ID] = append(h.spans[task.ID], [2]time.Time{began, time.Now()})
	h.mu.Unlock()
}

// block makes every run from here on wait until the returned func releases it.
func (h *schedHarness) block() func() {
	gate := make(chan struct{})
	h.mu.Lock()
	h.gate = gate
	h.mu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			h.mu.Lock()
			h.gate = nil
			h.mu.Unlock()
			close(gate)
		})
	}
	h.t.Cleanup(release)
	return release
}

// awaitStarts waits for n runs to have started.
func (h *schedHarness) awaitStarts(n int) []string {
	h.t.Helper()
	var ids []string
	for len(ids) < n {
		select {
		case id := <-h.started:
			ids = append(ids, id)
		case <-time.After(5 * time.Second):
			h.t.Fatalf("%d runs started, want %d", len(ids), n)
		}
	}
	return ids
}

// runUntil fires the dispatcher at each next due time, as the run loop's
// scheduler timer does, through end; nothing ticks between dues.
func (h *schedHarness) runUntil(end time.Time) {
	h.t.Helper()
	for {
		at, ok := h.d.next()
		if !ok || at.After(end) {
			return
		}
		h.set(at)
		h.d.pass(h.t.Context(), at)
		h.d.settle()
	}
}

// offsets is when each run of id started, from start.
func (h *schedHarness) offsets(id string, start time.Time) []time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []time.Duration
	for _, at := range h.runs[id] {
		out = append(out, at.Sub(start))
	}
	return out
}

func wl(tasks ...collection.TaskSpec) collection.WorklistReply {
	return collection.WorklistReply{Tasks: tasks}
}

// #1634: a poll runs on its own interval from a timer, not on the heartbeat:
// a 10s poll on a 30s heartbeat runs every 10s, and a 45s poll at 45s, not at
// the 60s heartbeat after it.
func TestEachPollRunsOnItsOwnIntervalNotTheHeartbeat(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		every string
		until time.Duration
		want  []time.Duration
	}{
		{"10s", 30 * time.Second, []time.Duration{0, 10 * time.Second, 20 * time.Second, 30 * time.Second}},
		{"45s", 60 * time.Second, []time.Duration{0, 45 * time.Second}},
	} {
		t.Run(tc.every, func(t *testing.T) {
			t.Parallel()
			task := everyPoll("p-"+tc.every, "ep-1", tc.every)
			every, _ := time.ParseDuration(tc.every)
			start := aligned(schedT0, task.ID, every)
			h := newSchedHarness(t, start, 4, 30*time.Second)
			h.d.sync(wl(task), start)
			h.runUntil(start.Add(tc.until))
			if got := h.offsets(task.ID, start); !slices.Equal(got, tc.want) {
				t.Fatalf("a %s poll on a 30s heartbeat ran at %v, want %v", tc.every, got, tc.want)
			}
		})
	}
}

// A run still in flight when its next due arrives is skipped, never queued
// behind itself: the window counts one schedule skip and one overrun, the
// endpoint one skipped poll of two due, and no second run starts until the
// due after.
func TestAPollStillRunningAtItsNextDueIsSkippedNotQueued(t *testing.T) {
	t.Parallel()
	task := everyPoll("p-slow", "ep-1", "10s")
	start := aligned(schedT0, task.ID, 10*time.Second)
	h := newSchedHarness(t, start, 4, 30*time.Second)
	release := h.block()
	h.d.sync(wl(task), start)
	h.d.pass(t.Context(), start)
	h.awaitStarts(1)

	h.set(start.Add(10 * time.Second))
	if n := h.d.pass(t.Context(), start.Add(10*time.Second)); n != 0 {
		t.Fatalf("a due poll still in flight dispatched %d runs, want it skipped", n)
	}
	if at, _ := h.d.next(); !at.Equal(start.Add(20 * time.Second)) {
		t.Fatalf("after the skip the poll is next due at %v, want the due after (+20s)", at.Sub(start))
	}
	h.set(start.Add(15 * time.Second))
	release()
	h.d.settle()
	if at, _ := h.d.next(); !at.Equal(start.Add(20 * time.Second)) {
		t.Fatalf("the slow run's end moved the next due to %v, want +20s", at.Sub(start))
	}
	h.runUntil(start.Add(20 * time.Second))
	if got := h.offsets(task.ID, start); !slices.Equal(got, []time.Duration{0, 20 * time.Second}) {
		t.Fatalf("the slow poll ran at %v, want 0s and 20s", got)
	}
	w := h.d.takeWindow()
	if w.skips != 1 {
		t.Fatalf("schedule skips = %d, want 1 (missed=1)", w.skips)
	}
	got := map[string]float64{}
	for _, rep := range h.report.take(start.Add(20*time.Second), map[string]string{"ep-1": task.ID}) {
		for _, s := range rep.samples {
			got[s.Name] = s.Value
		}
	}
	if got[probekeys.PollDueCount] != 3 || got[probekeys.PollSkippedCount] != 1 {
		t.Fatalf("the endpoint's window = %v, want 3 due (0s, 10s, 20s) and 1 skipped", got)
	}
}

// The skipped in-flight run counts once into the window's overruns, and a
// dispatch pass reports how long it took: the figures the heartbeat tick used
// to carry, with no ticker behind them.
func TestASkippedRunCountsOneOverrunAndAPassReportsItsDuration(t *testing.T) {
	t.Parallel()
	task := everyPoll("p-slow", "ep-1", "10s")
	start := aligned(schedT0, task.ID, 10*time.Second)
	h := newSchedHarness(t, start, 4, 30*time.Second)
	release := h.block()
	h.d.sync(wl(task), start)
	h.d.pass(t.Context(), start)
	h.awaitStarts(1)
	h.set(start.Add(10 * time.Second))
	h.d.pass(t.Context(), start.Add(10*time.Second))
	release()
	h.d.settle()
	w := h.d.takeWindow()
	if w.overruns != 1 {
		t.Fatalf("tick-overrun-count = %d, want the one skipped in-flight run", w.overruns)
	}
	if w.passes != 2 || w.passMax <= 0 {
		t.Fatalf("passes %d, tick-duration %v: want 2 passes and their longest duration", w.passes, w.passMax)
	}
	if again := h.d.takeWindow(); again.overruns != 0 || again.passes != 0 {
		t.Fatalf("taking the window did not start a new one: %+v", again)
	}
}

// Two tasks on one endpoint never run at once (the endpoint's lane), while
// two endpoints of one component do.
func TestTwoTasksOnOneEndpointNeverOverlap(t *testing.T) {
	t.Parallel()
	a, b := everyPoll("p-a", "ep-1", "10s"), everyPoll("p-b", "ep-1", "10s")
	c := everyPoll("p-c", "ep-2", "10s") // the same component's other endpoint
	h := newSchedHarness(t, schedT0, 4, 30*time.Second)
	release := h.block()
	h.d.sync(wl(a, b, c), schedT0)
	if n := h.d.pass(t.Context(), schedT0); n != 3 {
		t.Fatalf("a pass over three new tasks dispatched %d", n)
	}
	first := h.awaitStarts(2)
	if !slices.Contains(first, c.ID) {
		t.Fatalf("the second endpoint's poll did not run beside the first's: %v", first)
	}
	select {
	case id := <-h.started:
		t.Fatalf("%s started while its endpoint's lane was busy", id)
	case <-time.After(50 * time.Millisecond):
	}
	release()
	h.d.settle()
	h.mu.Lock()
	defer h.mu.Unlock()
	sa, sb, sc := h.spans[a.ID][0], h.spans[b.ID][0], h.spans[c.ID][0]
	if sa[0].Before(sb[1]) && sb[0].Before(sa[1]) {
		t.Fatalf("two tasks on ep-1 overlapped: %v and %v", sa, sb)
	}
	ep1 := sa
	if sb[0].Before(sa[0]) {
		ep1 = sb
	}
	if !(ep1[0].Before(sc[1]) && sc[0].Before(ep1[1])) {
		t.Fatalf("two endpoints of one component did not overlap: %v and %v", ep1, sc)
	}
}

// An on-demand run on a busy endpoint waits for its lane, then starts ahead
// of the scheduled tasks queued on that endpoint.
func TestAnOperatorRunWaitsForItsLaneAndGoesFirst(t *testing.T) {
	t.Parallel()
	p := newLanePool(4)
	t.Cleanup(p.wait)
	gate := make(chan struct{})
	var mu sync.Mutex
	var order []string
	ran := func(name string, wait bool) func() {
		return func() {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			if wait {
				<-gate
			}
		}
	}
	p.submit("ep-1", laneScheduled, ran("running", true))
	p.submit("ep-1", laneScheduled, ran("queued", false))
	done := make(chan struct{})
	go func() {
		p.do("ep-1", laneOperator, ran("operator", false))
		close(done)
	}()
	waitFor(t, func() bool { return p.queued() == 2 }, "the operator run queued")
	mu.Lock()
	if !slices.Equal(order, []string{"running"}) {
		t.Fatalf("ran %v while the lane was busy", order)
	}
	mu.Unlock()
	close(gate)
	<-done
	p.wait()
	if !slices.Equal(order, []string{"running", "operator", "queued"}) {
		t.Fatalf("lane order %v, want the operator run ahead of the queued scheduled one", order)
	}
}

// A first run happens at once; later dues fall on the task's phase, which is
// a function of its id alone, so a restarted node keeps each task's slots and
// a fleet of tasks spreads across the interval rather than firing together.
func TestAFirstRunIsAtOnceAndLaterDuesKeepAStablePhase(t *testing.T) {
	t.Parallel()
	every := 10 * time.Second
	task := everyPoll("p-phase", "ep-1", "10s")
	onPhase := func(at time.Time) bool {
		return time.Duration(at.UnixNano())%every == phaseOf(task.ID, every)
	}
	for _, start := range []time.Time{schedT0.Add(3300 * time.Millisecond), schedT0.Add(47 * time.Second)} {
		h := newSchedHarness(t, start, 4, 30*time.Second) // a fresh node: a restart
		h.d.sync(wl(task), start)
		h.d.pass(t.Context(), start)
		h.d.settle()
		if got := h.offsets(task.ID, start); !slices.Equal(got, []time.Duration{0}) {
			t.Fatalf("a new task ran at %v, want at once", got)
		}
		next, _ := h.d.next()
		if !onPhase(next) || next.Sub(start) < every/2 || next.Sub(start) > every+every/2 {
			t.Fatalf("started at %v the next due is %v later, off the task's phase %v", start, next.Sub(start), phaseOf(task.ID, every))
		}
	}
	phases := map[time.Duration]bool{}
	for i := 0; i < 20; i++ {
		phases[phaseOf(fmt.Sprintf("t-%d", i), time.Minute)] = true
	}
	if len(phases) < 15 {
		t.Fatalf("20 task ids share %d phases of a minute, want them spread", len(phases))
	}
}

// With 4 workers and 10 due tasks on 10 components behind a blocking runner,
// the window's high-water marks read 4 workers busy and 6 dispatches queued.
func TestTheWindowReportsBusyWorkersAndQueuedDispatches(t *testing.T) {
	t.Parallel()
	h := newSchedHarness(t, schedT0, 4, 30*time.Second)
	release := h.block()
	var tasks []collection.TaskSpec
	for i := 0; i < 10; i++ {
		tasks = append(tasks, everyPoll(fmt.Sprintf("p-%d", i), fmt.Sprintf("ep-%d", i), "10s"))
	}
	h.d.sync(wl(tasks...), schedT0)
	h.d.pass(t.Context(), schedT0)
	h.awaitStarts(4)
	release()
	h.d.settle()
	w := h.d.takeWindow()
	if w.busyMax != 4 || w.queueMax != 6 {
		t.Fatalf("worker-busy-count %d, dispatch-queue-depth %d, want 4 and 6", w.busyMax, w.queueMax)
	}
}
