package node

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
)

// reportValues flattens a report's rows to task -> key -> value, failing on a
// key landed twice for one task (a series carried twice in one window).
func reportValues(t *testing.T, rows []endpointReport) map[string]map[string]float64 {
	t.Helper()
	out := map[string]map[string]float64{}
	for _, r := range rows {
		if out[r.task] != nil {
			t.Fatalf("task %s reported twice in one window", r.task)
		}
		out[r.task] = map[string]float64{}
		for _, s := range r.samples {
			if _, dup := out[r.task][s.Name]; dup {
				t.Fatalf("task %s carries %s twice", r.task, s.Name)
			}
			if s.IsText || s.Lane != "" {
				t.Fatalf("%s is not a metric sample", s.Name)
			}
			out[r.task][s.Name] = s.Value
		}
	}
	return out
}

var t0 = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

// A window's polls report as one mean duration per endpoint, in
// milliseconds, every run counted, and a failure count only when a run
// failed. The endpoint's several polls share its series, so the rows land
// under one of its tasks.
func TestAReportWindowAveragesAnEndpointsPolls(t *testing.T) {
	r := newCollectionReport(time.Minute, t0)
	r.poll("ep-bar", 40*time.Millisecond, false)
	r.poll("ep-bar", 20*time.Millisecond, false)
	r.poll("ep-bar", 90*time.Millisecond, true)
	r.poll("ep-display", 12*time.Millisecond, false)

	if r.due(t0.Add(59 * time.Second)) {
		t.Fatal("the window is due before it has run its length")
	}
	if !r.due(t0.Add(time.Minute)) {
		t.Fatal("the window is not due once it has run its length")
	}
	got := reportValues(t, r.take(t0.Add(time.Minute), map[string]string{"ep-bar": "task-audio", "ep-display": "task-scalars"}))
	want := map[string]map[string]float64{
		"task-audio":   {probekeys.PollDuration: 50, probekeys.PollFailureCount: 1},
		"task-scalars": {probekeys.PollDuration: 12},
	}
	if len(got) != len(want) {
		t.Fatalf("report = %v, want %v", got, want)
	}
	for task, keys := range want {
		for k, v := range keys {
			if got[task][k] != v {
				t.Errorf("%s %s = %v, want %v", task, k, got[task][k], v)
			}
		}
		if len(got[task]) != len(keys) {
			t.Errorf("%s carries %v, want only %v", task, got[task], keys)
		}
	}
}

// A held endpoint's pushes report every window, a quiet one as zero, so the
// endpoint's series is continuous and silence shows; its unclaimed messages
// report only when there were some. An endpoint seen in one window and not
// the next reports nothing in the next: nothing ran there and nothing is held.
func TestAReportWindowCountsAHeldEndpointsPushes(t *testing.T) {
	r := newCollectionReport(time.Minute, t0)
	r.pushes("ep-panel", 3, 0)
	r.pushes("ep-panel", 2, 1)
	r.pushes("ep-quiet", 0, 0)
	tasks := map[string]string{"ep-panel": "task-listen", "ep-quiet": "task-quiet"}

	got := reportValues(t, r.take(t0.Add(time.Minute), tasks))
	if got["task-listen"][probekeys.PushMessageCount] != 5 || got["task-listen"][probekeys.PushUnclaimedCount] != 1 {
		t.Errorf("task-listen = %v, want 5 claimed and 1 unclaimed", got["task-listen"])
	}
	quiet, ok := got["task-quiet"]
	if !ok || len(quiet) != 1 || quiet[probekeys.PushMessageCount] != 0 {
		t.Errorf("task-quiet = %v (reported %v), want push-message-count 0 and nothing else", quiet, ok)
	}

	if rows := r.take(t0.Add(2*time.Minute), tasks); len(rows) != 0 {
		t.Errorf("an empty window reported %v", rows)
	}
}

// Every row of a window is stamped at the window's end, and the next window
// starts there, so the windows tile time with no gap or overlap.
func TestAReportWindowStampsItsEndAndStartsTheNext(t *testing.T) {
	r := newCollectionReport(time.Minute, t0)
	r.poll("ep", time.Millisecond, false)
	end := t0.Add(61 * time.Second)
	for _, row := range r.take(end, map[string]string{"ep": "task"}) {
		for _, s := range row.samples {
			if !s.TS.Equal(end) {
				t.Errorf("%s stamped %v, want the window's end %v", s.Name, s.TS, end)
			}
		}
	}
	if r.due(end.Add(59 * time.Second)) {
		t.Error("the next window is due before its own length from the last one's end")
	}
	if !r.due(end.Add(time.Minute)) {
		t.Error("the next window is not due a length after the last one's end")
	}
}

// The worklist carries the platform's window. A new window applies from the
// window in progress (it is due by the new length), and a window the worklist
// does not carry (an older server) keeps the node's own default.
func TestAReportWindowFollowsTheWorklist(t *testing.T) {
	r := newCollectionReport(time.Minute, t0)
	r.setEvery(0)
	if r.due(t0.Add(30*time.Second)) || !r.due(t0.Add(time.Minute)) {
		t.Error("a window the worklist does not carry changed the default")
	}
	r.setEvery(20 * time.Second)
	if !r.due(t0.Add(20 * time.Second)) {
		t.Error("a shorter window from the worklist is not due at its own length")
	}
}

// The rows publish under a task the node holds now, chosen when the window
// is taken: a driver revision mid-window replaces an endpoint's task ids,
// and a row under a deleted id would be refused as an orphan, raising the
// node's stuck-task signal for nothing. An endpoint with no task left (it
// was deleted, or moved to another node) reports nothing.
func TestAReportWindowPublishesUnderATaskTheNodeHoldsNow(t *testing.T) {
	r := newCollectionReport(time.Minute, t0)
	r.poll("ep-bar", 30*time.Millisecond, false)
	r.poll("ep-gone", 30*time.Millisecond, false)
	got := reportValues(t, r.take(t0.Add(time.Minute), map[string]string{"ep-bar": "task-revised"}))
	if _, ok := got["task-revised"]; !ok || len(got) != 1 {
		t.Fatalf("report = %v, want the bar's rows under its current task alone", got)
	}
}

// A poll the endpoint's gate refused sent nothing, yet it was due and did
// not run: a failure, as the scheduler books it, counted every window the
// gate holds, while the mean time stays the time of the polls that ran.
func TestARefusedPollIsAFailureWithoutATime(t *testing.T) {
	r := newCollectionReport(time.Minute, t0)
	r.poll("ep", 40*time.Millisecond, false)
	r.refused("ep")
	r.refused("ep")
	got := reportValues(t, r.take(t0.Add(time.Minute), map[string]string{"ep": "task"}))["task"]
	if got[probekeys.PollDuration] != 40 || got[probekeys.PollFailureCount] != 2 {
		t.Fatalf("report = %v, want a 40 ms mean and 2 failures", got)
	}
	r.refused("ep")
	got = reportValues(t, r.take(t0.Add(2*time.Minute), map[string]string{"ep": "task"}))["task"]
	if _, timed := got[probekeys.PollDuration]; timed || got[probekeys.PollFailureCount] != 1 {
		t.Fatalf("a window of refusals alone = %v, want 1 failure and no time", got)
	}
}

// publishTasks picks each endpoint's least task id from the worklist: any
// task of an endpoint names its component and its endpoint, and the least
// holds still while the worklist does.
func TestPublishTasksPicksEachEndpointsLeastTask(t *testing.T) {
	got := publishTasks([]collection.TaskSpec{
		{ID: "t-9", EndpointID: "ep-1"}, {ID: "t-2", EndpointID: "ep-1"}, {ID: "t-5", EndpointID: "ep-2"},
	})
	if len(got) != 2 || got["ep-1"] != "t-2" || got["ep-2"] != "t-5" {
		t.Fatalf("publishTasks = %v, want ep-1 t-2 and ep-2 t-5", got)
	}
}
