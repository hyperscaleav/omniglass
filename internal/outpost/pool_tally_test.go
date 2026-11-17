package node

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/platformmetric"
)

// #1686: two workers of a pool of two, each busy for half of a 10 s window,
// read a busy fraction of 0.5.
func TestAPoolTallyReadsTheShareOfWorkerTimeBusy(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	var p poolTally
	p.open(t0, 2)
	a := p.start(t0)
	p.end(a, t0.Add(5*time.Second))
	b := p.start(t0.Add(2 * time.Second))
	p.end(b, t0.Add(7*time.Second))
	w := p.take(t0.Add(10 * time.Second))
	if w.busy != 10*time.Second || w.capacity != 20*time.Second {
		t.Fatalf("busy %v of %v, want 10s of 20s", w.busy, w.capacity)
	}
	if got := platformmetric.BusyFraction(w.busy, w.capacity); got != 0.5 {
		t.Fatalf("busy fraction %v, want 0.5", got)
	}
}

// A run that spans a window's end splits its busy time across both windows:
// the part before the end counts in the first, the rest in the next.
func TestAPoolTallySplitsARunAcrossTheWindowItSpans(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	var p poolTally
	p.open(t0, 1)
	r := p.start(t0.Add(6 * time.Second))
	first := p.take(t0.Add(10 * time.Second))
	p.end(r, t0.Add(13*time.Second))
	second := p.take(t0.Add(20 * time.Second))
	if first.busy != 4*time.Second || second.busy != 3*time.Second {
		t.Fatalf("busy %v then %v, want 4s then 3s", first.busy, second.busy)
	}
	if first.capacity != 10*time.Second || second.capacity != 10*time.Second {
		t.Fatalf("capacities %v and %v, want 10s each", first.capacity, second.capacity)
	}
}

// A run still going when a window is taken counts its time so far in that
// window, and the whole of the next one it stays busy through.
func TestAPoolTallyCountsARunStillInFlight(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	var p poolTally
	p.open(t0, 1)
	p.start(t0.Add(time.Second))
	p.take(t0.Add(10 * time.Second))
	if w := p.take(t0.Add(20 * time.Second)); w.busy != 10*time.Second {
		t.Fatalf("a run busy through the window counted %v, want 10s", w.busy)
	}
}

// A window that ran nothing reads 0 busy time over the pool's whole worker
// time.
func TestAnIdlePoolReadsZero(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	var p poolTally
	p.open(t0, 4)
	if w := p.take(t0.Add(10 * time.Second)); w.busy != 0 || w.capacity != 40*time.Second {
		t.Fatalf("an idle pool read busy %v of %v, want 0 of 40s", w.busy, w.capacity)
	}
}

// Windows add: busy time to busy time and worker time to worker time, so a
// merged window stays the share of the time the pool actually had.
func TestPoolWindowsAdd(t *testing.T) {
	t.Parallel()
	w := poolWindow{busy: time.Second, capacity: 4 * time.Second}
	w.add(poolWindow{busy: 2 * time.Second, capacity: 2 * time.Second})
	if w != (poolWindow{busy: 3 * time.Second, capacity: 6 * time.Second}) {
		t.Fatalf("added window %+v, want 3s of 6s", w)
	}
}
