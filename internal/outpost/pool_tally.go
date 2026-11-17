package node

import "time"

// poolTally is a worker pool's busy time over a window (#1686): the sum, over
// its workers, of the time each spent running a job inside the window. A job
// that spans a window's end counts its time before the end in that window and
// the rest in the next, so a pool held busy through a window reads all of it.
// Pure: the caller stamps every start, end and take, so the clock stays at the
// edge.
type poolTally struct {
	from    time.Time     // the window's start
	workers int           // the pool's workers, its worker time per second of window
	busy    time.Duration // the window's time from the jobs that ended in it
	running int           // jobs in flight
	lead    time.Duration // over the jobs in flight, the sum of each one's start in the window past from
}

// poolWindow is a pool's busy worker time over a window and the worker time it
// had, the window's length times its workers: busy over capacity is its busy
// fraction (platformmetric.BusyFraction).
type poolWindow struct {
	busy, capacity time.Duration
}

// add merges o into w, busy time to busy time and worker time to worker time,
// so windows on different worker counts merge to the share of the time the
// pool actually had.
func (w *poolWindow) add(o poolWindow) {
	w.busy += o.busy
	w.capacity += o.capacity
}

// open starts the first window at at, on workers workers.
func (p *poolTally) open(at time.Time, workers int) { p.from, p.workers = at, workers }

// start counts a job starting at at, and returns the stamp its end passes back.
func (p *poolTally) start(at time.Time) time.Time {
	p.running++
	p.lead += at.Sub(p.from)
	return at
}

// end counts the job started at started ending at at: its time inside the
// current window.
func (p *poolTally) end(started, at time.Time) {
	from := max(started.Sub(p.from), 0)
	p.lead -= from
	p.running--
	p.busy += max(at.Sub(p.from)-from, 0)
}

// take returns the window ending at at, the jobs still in flight counted to
// at, and starts the next window there.
func (p *poolTally) take(at time.Time) poolWindow {
	wall := max(at.Sub(p.from), 0)
	w := poolWindow{busy: p.busy + time.Duration(p.running)*wall - p.lead, capacity: time.Duration(p.workers) * wall}
	p.from, p.busy, p.lead = at, 0, 0
	return w
}
