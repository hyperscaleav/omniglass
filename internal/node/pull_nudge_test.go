package node

import (
	"context"
	"testing"
	"time"
)

// A nudge (#1945) ends the wait for the next worklist pull early.
func TestANudgeEndsThePullWaitEarly(t *testing.T) {
	t.Parallel()
	nudges := make(chan struct{}, 1)
	nudge(nudges)
	start := time.Now()
	if !waitPull(context.Background(), time.Hour, nudges, start) {
		t.Fatal("the wait reported a cancelled context")
	}
	if took := time.Since(start); took > 100*time.Millisecond {
		t.Fatalf("a nudge took %v to end an hour's wait", took)
	}
}

// A nudge inside the gap after the last pull waits out the gap, so a burst of
// changes cannot pull faster than one per gap.
func TestANudgeInsideTheGapWaitsItOut(t *testing.T) {
	t.Parallel()
	nudges := make(chan struct{}, 1)
	nudge(nudges)
	start := time.Now()
	earliest := start.Add(150 * time.Millisecond)
	if !waitPull(context.Background(), time.Hour, nudges, earliest) {
		t.Fatal("the wait reported a cancelled context")
	}
	took := time.Since(start)
	if took < 150*time.Millisecond || took > time.Second {
		t.Fatalf("a nudge inside the gap ended the wait after %v, want the gap's 150ms", took)
	}
}

// With no nudge the scheduled pull still comes.
func TestThePullWaitEndsOnSchedule(t *testing.T) {
	t.Parallel()
	if !waitPull(context.Background(), 10*time.Millisecond, make(chan struct{}, 1), time.Now()) {
		t.Fatal("the wait reported a cancelled context")
	}
}

// An ended context ends the wait, nudged or not, reporting false.
func TestThePullWaitEndsWithItsContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitPull(ctx, time.Hour, make(chan struct{}, 1), time.Now()) {
		t.Fatal("a cancelled wait reported its interval elapsed")
	}
	nudges := make(chan struct{}, 1)
	nudge(nudges)
	if waitPull(ctx, time.Hour, nudges, time.Now().Add(time.Hour)) {
		t.Fatal("a cancelled wait inside the gap reported its interval elapsed")
	}
}

// Nudges coalesce: a hundred arriving before the pull loop looks leave one
// pending, and the sender never blocks.
func TestNudgesCoalesce(t *testing.T) {
	t.Parallel()
	nudges := make(chan struct{}, 1)
	for range 100 {
		nudge(nudges)
	}
	if len(nudges) != 1 {
		t.Fatalf("%d nudges pending, want 1", len(nudges))
	}
}
