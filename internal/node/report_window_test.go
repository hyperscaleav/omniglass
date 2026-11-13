package node

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// lateTakes is how late each take lands after its window's end: the run loop
// takes a window on the first tick at or past its end, so up to a tick late.
var lateTakes = []time.Duration{0, 1900 * time.Millisecond, 500 * time.Millisecond, 1999 * time.Millisecond, 0, time.Second, 1500 * time.Millisecond, 1900 * time.Millisecond}

// A collection report window ends on its grid however late the tick that
// takes it (#1779): each ends exactly a window after the last, so a fleet's
// reports land once a minute and a five-minute read holds five of them,
// never four or six.
func TestACollectionReportWindowEndsOnItsGridHoweverLateItIsTaken(t *testing.T) {
	t.Parallel()
	r := newCollectionReport(time.Minute, 0, t0)
	for k, late := range lateTakes {
		end := t0.Add(time.Duration(k+1) * time.Minute)
		if r.due(end.Add(-time.Nanosecond)) {
			t.Fatalf("window %d is due before its grid end %s", k+1, end)
		}
		if !r.due(end) {
			t.Fatalf("window %d is not due at its grid end %s", k+1, end)
		}
		r.take(end.Add(late), nil)
	}
}

// A take more than a window late (a stalled loop) starts the next window at
// the next slot after now: no burst of back-to-back windows catching up.
func TestACollectionReportTakenAfterAStallResumesAtTheNextSlot(t *testing.T) {
	t.Parallel()
	r := newCollectionReport(time.Minute, 0, t0)
	r.take(t0.Add(4*time.Minute+30*time.Second), nil)
	next := t0.Add(5 * time.Minute)
	if r.due(next.Add(-time.Nanosecond)) || !r.due(next) {
		t.Fatalf("after a stall the next window does not end at the next slot %s", next)
	}
}

// The node's account of itself closes its windowed counts on the same grid.
func TestNodeReportWindowEndsOnItsGridHoweverLateItIsTaken(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	r := newNodeReport(time.Minute, 0, t0)
	for k, late := range lateTakes {
		end := t0.Add(time.Duration(k+1) * time.Minute)
		if _, ok := readings(r.take(end.Add(-time.Nanosecond)))[writerclass.TickOverrunCount]; ok {
			t.Fatalf("window %d closed before its grid end %s", k+1, end)
		}
		if _, ok := readings(r.take(end.Add(late)))[writerclass.TickOverrunCount]; !ok {
			t.Fatalf("window %d did not close at its grid end %s", k+1, end)
		}
	}
}

// A node's name places its grid: two nodes close their windows at different
// offsets into the minute, so a fleet's reports spread instead of landing
// together, and a node that restarts keeps the offset it had.
func TestAReportWindowsPhaseFollowsTheNodesName(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_800_000_000, 0).Add(17 * time.Second)
	a := newReportWindow(time.Minute, phaseSeed("node-a"), start)
	b := newReportWindow(time.Minute, phaseSeed("node-b"), start)
	if a.end.Sub(a.end.Truncate(time.Minute)) == b.end.Sub(b.end.Truncate(time.Minute)) {
		t.Fatalf("node-a and node-b close at the same offset %v", a.end.Sub(a.end.Truncate(time.Minute)))
	}
	restarted := newReportWindow(time.Minute, phaseSeed("node-a"), start.Add(7*time.Minute+23*time.Second))
	if d := restarted.end.Sub(a.end); d%time.Minute != 0 {
		t.Fatalf("a restart moved node-a's grid by %v", d%time.Minute)
	}
	if a.end.Sub(start) > time.Minute || !a.end.After(start) {
		t.Fatalf("the first window runs %v, want within one length", a.end.Sub(start))
	}
}

// A changed length re-grids from the window in progress; the same length,
// which every heartbeat repeats, leaves the window alone.
func TestAReportWindowsLengthChangeRegridsFromItsStart(t *testing.T) {
	t.Parallel()
	t0 := time.Unix(1_800_000_000, 0)
	w := newReportWindow(time.Minute, 0, t0)
	w.close(t0.Add(time.Minute))
	end := w.end
	w.setEvery(time.Minute)
	if !w.end.Equal(end) {
		t.Fatalf("an unchanged length moved the end from %v to %v", end, w.end)
	}
	w.setEvery(5 * time.Minute)
	if want := t0.Add(5 * time.Minute); !w.end.Equal(want) {
		t.Fatalf("a five-minute length ends at %v, want %v", w.end, want)
	}
}
