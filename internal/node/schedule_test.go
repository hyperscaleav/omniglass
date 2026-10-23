package node

import (
	"testing"
	"time"
)

// #822: the node honors a driver poll's declared schedule. A 60s poll on a
// 1s heartbeat runs once per interval, a task new to the worklist runs at
// once, and a task that leaves the worklist is forgotten.
func TestPollScheduleRunsOncePerInterval(t *testing.T) {
	s := newPollSchedule()
	t0 := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	var runs []int
	for i := 0; i < 180; i++ {
		if s.due("p60", time.Minute, t0.Add(time.Duration(i)*time.Second)) {
			runs = append(runs, i)
		}
	}
	if len(runs) != 3 || runs[0] != 0 || runs[1] != 60 || runs[2] != 120 {
		t.Fatalf("a 60s poll on a 1s tick ran at seconds %v, want 0, 60, 120", runs)
	}

	// A task that appears mid-run runs at its first tick.
	if !s.due("new", time.Minute, t0.Add(30*time.Second)) {
		t.Fatal("a new task waited for its interval instead of running at once")
	}
	// No declared interval: every tick, as before.
	for i := 0; i < 3; i++ {
		if !s.due("unscheduled", 0, t0.Add(time.Duration(i)*time.Second)) {
			t.Fatal("a task with no interval was gated")
		}
	}
	// A slow tick never bunches: one run however late, then the interval.
	late := newPollSchedule()
	late.due("p", time.Minute, t0)
	if !late.due("p", time.Minute, t0.Add(150*time.Second)) || late.due("p", time.Minute, t0.Add(151*time.Second)) {
		t.Fatal("a late tick ran a catch-up burst")
	}

	s.retain(map[string]bool{"new": true})
	if !s.due("p60", time.Minute, t0.Add(130*time.Second)) {
		t.Fatal("a task that left the worklist kept its schedule")
	}
}
