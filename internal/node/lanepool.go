package node

import "sync"

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

// lanePool runs work on a fixed number of workers, one job per lane at a
// time (#1634, folding in #1249's lane executor). A lane is an endpoint: two
// tasks on one endpoint never talk to it at once, while two endpoints of one
// component run side by side. A job whose lane is busy waits in its class's
// queue, and a free worker takes the first waiting job whose lane is idle,
// operator work first. The pool keeps the high-water marks of its busy
// workers and its waiting jobs for the dispatch window.
type lanePool struct {
	mu        sync.Mutex
	idle      *sync.Cond
	workers   int
	busy      int
	running   map[string]bool
	queues    [laneClasses][]laneJob
	maxBusy   int
	maxQueued int
}

func newLanePool(workers int) *lanePool {
	if workers <= 0 {
		workers = 1
	}
	p := &lanePool{workers: workers, running: map[string]bool{}}
	p.idle = sync.NewCond(&p.mu)
	return p
}

// submit queues fn on lane key and returns at once.
func (p *lanePool) submit(key string, class laneClass, fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queues[class] = append(p.queues[class], laneJob{key: key, fn: fn})
	p.dispatchLocked()
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
		go p.run(j)
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

func (p *lanePool) run(j laneJob) {
	j.fn()
	p.mu.Lock()
	defer p.mu.Unlock()
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

// takeWindow returns the busy and waiting high-water marks since the last
// call and starts the next window from what is busy and waiting now.
func (p *lanePool) takeWindow() (busy, queued int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	busy, queued = p.maxBusy, p.maxQueued
	p.maxBusy, p.maxQueued = p.busy, p.queuedLocked()
	return busy, queued
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
