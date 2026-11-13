package node

import (
	"testing"
	"time"
)

// #1686: two workers of a pool of two, each busy for half of a 10 s window,
// read a busy fraction of 0.5.
func TestAPoolTallyReadsTheShareOfWorkerTimeBusy(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	var p poolTally
	p.open(t0)
	a := p.start(t0)
	p.end(a, t0.Add(5*time.Second))
	b := p.start(t0.Add(2 * time.Second))
	p.end(b, t0.Add(7*time.Second))
	w := p.take(t0.Add(10 * time.Second))
	if w.busy != 10*time.Second || w.wall != 10*time.Second {
		t.Fatalf("busy %v over %v, want 10s over 10s", w.busy, w.wall)
	}
	if got := busyFraction(w.busy, w.wall, 2); got != 0.5 {
		t.Fatalf("busy fraction %v, want 0.5", got)
	}
}

// A run that spans a window's end splits its busy time across both windows:
// the part before the end counts in the first, the rest in the next.
func TestAPoolTallySplitsARunAcrossTheWindowItSpans(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	var p poolTally
	p.open(t0)
	r := p.start(t0.Add(6 * time.Second))
	first := p.take(t0.Add(10 * time.Second))
	p.end(r, t0.Add(13*time.Second))
	second := p.take(t0.Add(20 * time.Second))
	if first.busy != 4*time.Second || second.busy != 3*time.Second {
		t.Fatalf("busy %v then %v, want 4s then 3s", first.busy, second.busy)
	}
	if first.wall != 10*time.Second || second.wall != 10*time.Second {
		t.Fatalf("walls %v and %v, want 10s each", first.wall, second.wall)
	}
}

// A run still going when a window is taken counts its time so far in that
// window, and the whole of the next one it stays busy through.
func TestAPoolTallyCountsARunStillInFlight(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	var p poolTally
	p.open(t0)
	p.start(t0.Add(time.Second))
	p.take(t0.Add(10 * time.Second))
	if w := p.take(t0.Add(20 * time.Second)); w.busy != 10*time.Second {
		t.Fatalf("a run busy through the window counted %v, want 10s", w.busy)
	}
}

// A window that ran nothing reads 0, and so does one with no length or no
// workers, never a division by zero.
func TestAnIdlePoolReadsZero(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	var p poolTally
	p.open(t0)
	w := p.take(t0.Add(10 * time.Second))
	for _, c := range []struct {
		busy, wall time.Duration
		workers    int
	}{{w.busy, w.wall, 4}, {0, 0, 4}, {time.Second, time.Second, 0}} {
		if got := busyFraction(c.busy, c.wall, c.workers); got != 0 {
			t.Errorf("busy %v over %v with %d workers read %v, want 0", c.busy, c.wall, c.workers, got)
		}
	}
}
