package node

import (
	"hash/fnv"
	"time"
)

// reportWindow is a report's window on the node's grid (#1779): its ends
// fall on slots one window apart from the Unix epoch, offset by a phase from the
// node's seed, the way the scheduler places a task's polls. A window closes
// at its grid end however late the tick that takes it, so a report written
// "every minute" lands once a minute and never drifts later by the tick's
// lateness, and a fleet's nodes spread their reports over the window rather
// than sending together. The first window runs from the node's start to its
// first slot, so it is short.
type reportWindow struct {
	every time.Duration
	seed  uint64
	start time.Time
	end   time.Time
}

// phaseSeed is the seed a name's phase on any interval comes from: a
// function of the name alone, so a restarted node keeps its phase.
func phaseSeed(name string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return h.Sum64()
}

func newReportWindow(every time.Duration, seed uint64, now time.Time) reportWindow {
	w := reportWindow{every: every, seed: seed, start: now}
	w.end = w.slotAfter(now)
	return w
}

func (w reportWindow) slotAfter(x time.Time) time.Time {
	return slotAfter(x, time.Duration(w.seed%uint64(w.every)), w.every)
}

// due reports a window that has reached its grid end.
func (w reportWindow) due(now time.Time) bool {
	return !now.Before(w.end)
}

// close starts the next window at the end of the one just taken, or, when
// the take came more than a window late, at the next slot after now, so a
// stall never sends a run of back-to-back windows.
func (w *reportWindow) close(now time.Time) {
	w.start = w.end
	w.end = w.end.Add(w.every)
	if !now.Before(w.end) {
		w.start = now
		w.end = w.slotAfter(now)
	}
}

// setEvery moves the window in progress onto the grid of a new interval;
// none (zero) or the same interval keeps it.
func (w *reportWindow) setEvery(every time.Duration) {
	if every <= 0 || every == w.every {
		return
	}
	w.every = every
	w.end = w.slotAfter(w.start)
}
