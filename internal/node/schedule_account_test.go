package node

import (
	"testing"
	"time"
)

// #1625 on the dispatcher (#1634): a poll runs on its own interval, so the
// heartbeat rounding the tick loop showed is gone. A 45 s poll on a 30 s
// heartbeat starts 45 s apart and reads no gap excess and no lateness.
func TestScheduleGapExcessReadsZeroForAPollKeepingItsInterval(t *testing.T) {
	t.Parallel()
	task := everyPoll("p-45", "ep-1", "45s")
	start := aligned(schedT0, task.ID, 45*time.Second)
	h := newSchedHarness(t, start, 4, 30*time.Second)
	h.d.sync(wl(task), start)
	h.runUntil(start.Add(3 * time.Minute))
	w := h.d.takeWindow()
	if w.runs != 5 {
		t.Fatalf("runs %d, want 5 (0s to 180s every 45s)", w.runs)
	}
	if w.gapMax != 0 || w.lateMax != 0 {
		t.Fatalf("gap excess %v, lateness %v, want both zero for a punctual poll", w.gapMax, w.lateMax)
	}
}

// #1625: a pass that reaches a due late reads its lateness, and the gap since
// the task's last start reads the same distance off the interval.
func TestALatePassReadsLatenessAndGapExcess(t *testing.T) {
	t.Parallel()
	task := everyPoll("p-1m", "ep-1", "1m")
	start := aligned(schedT0, task.ID, time.Minute)
	h := newSchedHarness(t, start, 4, 30*time.Second)
	h.d.sync(wl(task), start)
	h.runUntil(start)
	h.d.takeWindow()

	late := start.Add(time.Minute + 4*time.Second)
	h.set(late)
	h.d.pass(t.Context(), late)
	h.d.settle()
	w := h.d.takeWindow()
	if w.runs != 1 || w.lateMax != 4*time.Second || w.gapMax != 4*time.Second {
		t.Fatalf("window %+v, want one run 4s late and 4s off its interval", w)
	}
	if again := h.d.takeWindow(); again.runs != 0 || again.gapMax != 0 || again.lateMax != 0 {
		t.Fatalf("taking the window did not start a new one: %+v", again)
	}
}

// #1625: the whole intervals a late pass passes over count as skips, and the
// gap they leave reads as its excess over the interval.
func TestAPassOverWholeIntervalsCountsTheSkips(t *testing.T) {
	t.Parallel()
	task := everyPoll("p-1m", "ep-1", "1m")
	start := aligned(schedT0, task.ID, time.Minute)
	h := newSchedHarness(t, start, 4, 30*time.Second)
	h.d.sync(wl(task), start)
	h.runUntil(start)

	h.set(start.Add(4 * time.Minute))
	h.d.pass(t.Context(), start.Add(4*time.Minute))
	h.d.settle()
	w := h.d.takeWindow()
	if w.skips != 3 {
		t.Fatalf("skips %d, want 3 whole intervals", w.skips)
	}
	if w.lateMax != 3*time.Minute || w.gapMax != 3*time.Minute {
		t.Fatalf("lateness %v, gap excess %v, want 3m each", w.lateMax, w.gapMax)
	}
}

// A task's first run has no gap to read, and a run off the schedule (a held
// or refused poll released) is no scheduled start.
func TestAFirstRunReadsNoGapExcess(t *testing.T) {
	t.Parallel()
	task := everyPoll("p-1m", "ep-1", "1m")
	h := newSchedHarness(t, schedT0, 4, 30*time.Second)
	h.d.sync(wl(task), schedT0)
	h.d.pass(t.Context(), schedT0)
	h.d.settle()
	if w := h.d.takeWindow(); w.runs != 1 || w.gapMax != 0 {
		t.Fatalf("a first run reads %+v, want one run and no gap excess", w)
	}
}

func TestGapExcessReadsEitherWay(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ gap, every, want time.Duration }{
		{time.Minute, time.Minute, 0},
		{70 * time.Second, time.Minute, 10 * time.Second},
		{50 * time.Second, time.Minute, 10 * time.Second},
	} {
		if got := gapExcess(tc.gap, tc.every); got != tc.want {
			t.Errorf("gapExcess(%v, %v) = %v, want %v", tc.gap, tc.every, got, tc.want)
		}
	}
}
