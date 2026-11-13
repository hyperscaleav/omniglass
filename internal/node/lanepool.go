package node

import (
	"sync"
	"time"
)

// laneClass orders the work waiting on a lane (#1634): an operator's run
// (a run now, a command) goes ahead of the scheduled tasks queued on the
// same endpoint.
type laneClass int

const (
	laneOperator laneClass = iota
	laneScheduled
	laneClasses
)

// laneJob is one piece of work for a lane.
type laneJob struct {
	key string
	fn  func()
}

// operatorQueueCap bounds the operator work waiting across every lane
// (#1249): an offer past it is refused and counted, never queued without
// bound behind a slow device.
const operatorQueueCap = 64

// lanePool runs work on a fixed number of workers, one job per lane at a
// time (#1634, #1249's endpoint lane). A lane is an endpoint: two tasks on
// one endpoint never talk to it at once, while two endpoints of one
// component run side by side. A job whose lane is busy waits in its class's
// queue, and a free worker takes the first waiting job whose lane is idle,
// operator work first; a job in flight is never preempted. The pool keeps
// the high-water marks of its busy workers and its waiting jobs, and its
// operator offers against the ones refused, for the dispatch window.
type lanePool struct {
	mu        sync.Mutex
	idle      *sync.Cond
	workers   int
	busy      int
	running   map[string]bool
	queues    [laneClasses][]laneJob
	maxBusy   int
	maxQueued int
	offers    int
	refusals  int
	// clock stamps each job's start and end for tally, the pool's busy
	// worker time over the window (#1686).
	clock func() time.Time
	tally poolTally
}

// laneWindow is the pool's share of a dispatch window: the busy and waiting
// high-water marks, the operator offers made against those refused, and the
// worker time spent busy over the window's length on its workers.
type laneWindow struct {
	busy, queued    int
	offers, refused int
	time            poolWindow
	workers         int
}

// newLanePool is a pool of workers that stamps its jobs by clock, time.Now
// when nil.
func newLanePool(workers int, clock func() time.Time) *lanePool {
	if workers <= 0 {
		workers = 1
	}
	if clock == nil {
		clock = time.Now
	}
	p := &lanePool{workers: workers, running: map[string]bool{}, clock: clock}
	p.idle = sync.NewCond(&p.mu)
	p.tally.open(clock())
	return p
}

// submit queues fn on lane key and returns at once.
func (p *lanePool) submit(key string, class laneClass, fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queues[class] = append(p.queues[class], laneJob{key: key, fn: fn})
	p.dispatchLocked()
}

// offer queues fn on lane key as submit does, unless it is operator work
// and the operator queue is at its cap: then it is refused, counted, and
// false. A nil pool (a node run with no dispatcher) runs fn at once on its
// own goroutine.
func (p *lanePool) offer(key string, class laneClass, fn func()) bool {
	if p == nil {
		go fn()
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if class == laneOperator {
		p.offers++
		if len(p.queues[laneOperator]) >= operatorQueueCap {
			p.refusals++
			return false
		}
	}
	p.queues[class] = append(p.queues[class], laneJob{key: key, fn: fn})
	p.dispatchLocked()
	return true
}

// do runs fn on lane key and returns once it ran: an operator's command
// waits for its endpoint's lane like any other work on it.
func (p *lanePool) do(key string, class laneClass, fn func()) {
	done := make(chan struct{})
	p.submit(key, class, func() {
		defer close(done)
		fn()
	})
	<-done
}

// dispatchLocked starts waiting jobs while a worker is free and a waiting
// job's lane is idle, then notes the window's high-water marks.
func (p *lanePool) dispatchLocked() {
	for p.busy < p.workers {
		j, ok := p.takeLocked()
		if !ok {
			break
		}
		p.busy++
		p.running[j.key] = true
		go p.run(j, p.tally.start(p.clock()))
	}
	p.maxBusy = max(p.maxBusy, p.busy)
	p.maxQueued = max(p.maxQueued, p.queuedLocked())
}

// takeLocked removes the first waiting job, by class, whose lane is idle.
func (p *lanePool) takeLocked() (laneJob, bool) {
	for c := range p.queues {
		for i, j := range p.queues[c] {
			if p.running[j.key] {
				continue
			}
			p.queues[c] = append(p.queues[c][:i:i], p.queues[c][i+1:]...)
			return j, true
		}
	}
	return laneJob{}, false
}

func (p *lanePool) run(j laneJob, started time.Time) {
	j.fn()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tally.end(started, p.clock())
	p.busy--
	delete(p.running, j.key)
	p.dispatchLocked()
	if p.busy == 0 && p.queuedLocked() == 0 {
		p.idle.Broadcast()
	}
}

func (p *lanePool) queuedLocked() int {
	n := 0
	for _, q := range p.queues {
		n += len(q)
	}
	return n
}

// queued is how many jobs wait for a worker or their lane.
func (p *lanePool) queued() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.queuedLocked()
}

// takeWindow returns the window since the last call and starts the next
// from what is busy and waiting now.
func (p *lanePool) takeWindow() laneWindow {
	p.mu.Lock()
	defer p.mu.Unlock()
	w := laneWindow{busy: p.maxBusy, queued: p.maxQueued, offers: p.offers, refused: p.refusals,
		time: p.tally.take(p.clock()), workers: p.workers}
	p.maxBusy, p.maxQueued = p.busy, p.queuedLocked()
	p.offers, p.refusals = 0, 0
	return w
}

// wait returns once no job runs or waits: a node's shutdown, and a test
// settling the work a pass dispatched.
func (p *lanePool) wait() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for p.busy > 0 || p.queuedLocked() > 0 {
		p.idle.Wait()
	}
}
