package node

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// The run loop's clocks are channels the test owns (#1636): a heartbeat that
// never fires is a clock that never advances, so whatever the loop does to a
// session it does on the worklist's arrival, not on a tick.

// waitUntil polls cond until it holds or the bound passes.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// startLoop runs the loop on its own goroutine and returns the stop that
// cancels it and hands back the worklist it ended on.
func startLoop(t *testing.T, loop *runLoop) func() collection.WorklistReply {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	ended := make(chan collection.WorklistReply, 1)
	go func() { ended <- loop.run(ctx, collection.WorklistReply{}) }()
	return func() collection.WorklistReply {
		cancel()
		return <-ended
	}
}

func TestRunLoopHoldsAndTearsDownASessionWhenItsWorklistArrives(t *testing.T) {
	t.Parallel()
	dialer := &heldFakeDialer{}
	log := &transitionLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, sessionHooks{transition: log.record})
	defer m.closeAll()

	worklists := make(chan collection.WorklistReply)
	beats := make(chan collection.WorklistReply, 8)
	loop := &runLoop{
		worklists:   worklists,
		heartbeat:   make(chan time.Time), // never ticks
		sessions:    m,
		onHeartbeat: func(wl collection.WorklistReply) { beats <- wl },
	}
	stop := startLoop(t, loop)

	worklists <- collection.WorklistReply{Tasks: []collection.TaskSpec{listenTask("l1", "ep-1", "10.0.0.5:51325")}}
	log.waitFor(t, "l1", collection.SessionUp)

	worklists <- collection.WorklistReply{}
	waitUntil(t, "the removed endpoint's connection to close", func() bool {
		conns := dialer.all()
		return len(conns) == 1 && conns[0].isClosed()
	})

	if wl := stop(); len(wl.Tasks) != 0 {
		t.Fatalf("the loop ended on %d tasks, want the last worklist it took", len(wl.Tasks))
	}
	if len(beats) != 0 {
		t.Fatalf("the heartbeat ran %d times with its clock stopped", len(beats))
	}
	if n := len(dialer.all()); n != 1 {
		t.Fatalf("dialed %d times, want one", n)
	}
}

// The periodic reconcile is the safety net for what changed between
// worklists: a session the last reconcile closed is held again on the next
// reconcile tick, without a new worklist.
func TestRunLoopReconcilesOnItsOwnTimer(t *testing.T) {
	t.Parallel()
	dialer := &heldFakeDialer{}
	log := &transitionLog{}
	m := newSessionManager(map[string]collection.SessionDialer{"tcp": dialer}, sessionHooks{transition: log.record})
	defer m.closeAll()

	worklists := make(chan collection.WorklistReply)
	reconcile := make(chan time.Time)
	stop := startLoop(t, &runLoop{worklists: worklists, reconcile: reconcile, sessions: m})
	defer stop()

	worklists <- collection.WorklistReply{Tasks: []collection.TaskSpec{listenTask("l1", "ep-1", "10.0.0.5:51325")}}
	log.waitFor(t, "l1", collection.SessionUp)
	// The device drops the connection; the session redials on its own
	// backoff, and the reconcile tick leaves the held session in place.
	reconcile <- time.Now()
	reconcile <- time.Now()
	if n := len(dialer.all()); n != 1 {
		t.Fatalf("an unchanged worklist reconciled into %d dials, want the one held", n)
	}
}

// Housekeeping (the unclaimed-line flush and the report's push count) runs
// on its own timer against the latest worklist, independent of the heartbeat.
func TestRunLoopHousekeepsOnItsOwnTimer(t *testing.T) {
	t.Parallel()
	worklists := make(chan collection.WorklistReply)
	housekeep := make(chan time.Time)
	kept := make(chan collection.WorklistReply, 1)
	stop := startLoop(t, &runLoop{
		worklists:   worklists,
		heartbeat:   make(chan time.Time),
		housekeep:   housekeep,
		onHousekeep: func(wl collection.WorklistReply) { kept <- wl },
	})
	defer stop()

	worklists <- collection.WorklistReply{Tasks: []collection.TaskSpec{{ID: "p1", Mode: "poll"}}}
	housekeep <- time.Now()
	select {
	case wl := <-kept:
		if len(wl.Tasks) != 1 || wl.Tasks[0].ID != "p1" {
			t.Fatalf("housekeeping ran against %+v, want the worklist that arrived", wl.Tasks)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("housekeeping did not run on its tick")
	}
}

// Housekeeping runs on the report window, so a window's report is published
// when due, and never less often than the heartbeat.
func TestHousekeepEveryIsTheShorterOfTheReportWindowAndTheHeartbeat(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		heartbeat, report, want time.Duration
	}{
		{30 * time.Second, time.Minute, 30 * time.Second},
		{30 * time.Second, 10 * time.Second, 10 * time.Second},
		{30 * time.Second, 30 * time.Second, 30 * time.Second},
		{30 * time.Second, 0, 30 * time.Second},
		{time.Hour, 200 * time.Millisecond, 200 * time.Millisecond},
	} {
		if got := housekeepEvery(c.heartbeat, c.report); got != c.want {
			t.Errorf("housekeepEvery(%s, %s) = %s, want %s", c.heartbeat, c.report, got, c.want)
		}
	}
}
