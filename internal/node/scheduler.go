package node

import (
	"container/heap"
	"context"
	"hash/fnv"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// DefaultProbeEvery is how often each reachability probe runs when the node
// is given no interval of its own (#1634): its own timer, never the
// heartbeat.
const DefaultProbeEvery = 30 * time.Second

// DefaultPollWorkers is how many tasks a node runs at once when it is given
// no worker count (#1634).
const DefaultPollWorkers = 16

// The dispatcher (#1634) runs each poll and probe on its own interval, from
// a timer of its own, not on the heartbeat tick. Every task sits once in a
// due-time heap; the run loop sleeps until the earliest due and calls pass,
// which hands each due task to a lane pool (one lane per endpoint, a fixed
// set of workers) and books its next due on the task's phase grid. A task
// still in flight at its next due is skipped and counted, never queued
// behind itself. A poll the endpoint's gate holds is decided here, in the
// pass, so it never takes a lane: counted held once per due, released the
// moment the gate opens. The heartbeat keeps the node's housekeeping and
// sets no task's cadence.

// phaseOf is where on its interval a task's slots fall: a function of its
// id alone, so a restarted node keeps each task's slots and a fleet of tasks
// spreads across the interval rather than firing together.
func phaseOf(id string, every time.Duration) time.Duration {
	if every <= 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return time.Duration(h.Sum64() % uint64(every))
}

// slotAfter is the first slot of the grid (every apart, offset phase from
// the Unix epoch) strictly after x.
func slotAfter(x time.Time, phase, every time.Duration) time.Time {
	n := x.UnixNano()
	e := int64(every)
	base := n - posmod(n-int64(phase), e)
	return time.Unix(0, base+e).In(x.Location())
}

func posmod(a, m int64) int64 {
	r := a % m
	if r < 0 {
		r += m
	}
	return r
}

// missedSlots is how many slots of the grid fall in (at, now]: the dues a
// late pass or a long run passed over.
func missedSlots(at, now time.Time, phase, every time.Duration) int {
	first := slotAfter(at, phase, every)
	if first.After(now) {
		return 0
	}
	return 1 + int(now.Sub(first)/every)
}

// nextBooking is the slot a run due at due and started at now books next:
// the first slot at least half an interval past the due, so a first run just
// before a slot does not run twice in quick succession, and never a slot
// already behind now, so a late pass runs once, never a catch-up burst.
func nextBooking(due, now time.Time, phase, every time.Duration) time.Time {
	from := due.Add(every / 2)
	if now.After(from) {
		from = now
	}
	return slotAfter(from, phase, every)
}

// dispatchConfig is what a dispatcher runs on.
type dispatchConfig struct {
	workers    int
	heartbeat  time.Duration // a poll that declares no interval runs at it
	probeEvery time.Duration // zero is DefaultProbeEvery
	clock      func() time.Time
	sched      *pollSchedule
	sessions   *sessionManager
	report     *collectionReport
	work       func(context.Context, collection.TaskSpec)
}

// dispatchWindow is the dispatcher's account of itself since the window was
// last taken: the figures the heartbeat tick used to carry.
type dispatchWindow struct {
	passes   int           // dispatch passes run
	passMax  time.Duration // tick-duration: the longest pass
	overruns int           // tick-overrun-count: dues that met their run in flight
	skips    int           // schedule-skip-count: dues passed over, overruns and late passes
	lateMax  time.Duration // the latest a task was dispatched past its due
	busyMax  int           // worker-busy-count: most workers busy at once
	queueMax int           // dispatch-queue-depth: most dispatches waiting at once
}

type dueEntry struct {
	id  string
	at  time.Time
	idx int
	// fresh marks a due that is no slot of the grid (a new task, a moved
	// fingerprint): no slot before it was missed.
	fresh bool
	// extra marks a run off the schedule (a held poll released, a refused
	// one released, a retry ahead of its slot): it runs, but is no due.
	extra bool
	// slot is the grid slot the task's last pass booked.
	slot time.Time
}

type dueHeap []*dueEntry

func (h dueHeap) Len() int { return len(h) }
func (h dueHeap) Less(i, j int) bool {
	if !h[i].at.Equal(h[j].at) {
		return h[i].at.Before(h[j].at)
	}
	return h[i].id < h[j].id
}
func (h dueHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].idx, h[j].idx = i, j
}
func (h *dueHeap) Push(x any) {
	e := x.(*dueEntry)
	e.idx = len(*h)
	*h = append(*h, e)
}
func (h *dueHeap) Pop() any {
	old := *h
	e := old[len(old)-1]
	*h = old[:len(old)-1]
	return e
}

type dispatcher struct {
	cfg  dispatchConfig
	pool *lanePool
	// wake asks the run loop for a pass now: a run finished, a gate moved.
	wake chan struct{}

	mu       sync.Mutex
	due      dueHeap
	entries  map[string]*dueEntry
	tasks    map[string]collection.TaskSpec
	inflight map[string]bool
	held     map[string]string // held poll id to its endpoint
	window   dispatchWindow
}

func newDispatcher(cfg dispatchConfig) *dispatcher {
	if cfg.workers <= 0 {
		cfg.workers = DefaultPollWorkers
	}
	if cfg.probeEvery <= 0 {
		cfg.probeEvery = DefaultProbeEvery
	}
	if cfg.clock == nil {
		cfg.clock = time.Now
	}
	if cfg.sched == nil {
		cfg.sched = newPollSchedule()
	}
	return &dispatcher{cfg: cfg, pool: newLanePool(cfg.workers), wake: make(chan struct{}, 1),
		entries: map[string]*dueEntry{}, tasks: map[string]collection.TaskSpec{},
		inflight: map[string]bool{}, held: map[string]string{}}
}

// interval is how often a task runs: a driver poll at its declared
// schedule, at the heartbeat when it declares none; a reachability probe at
// the node's probe interval.
func (d *dispatcher) interval(t collection.TaskSpec) time.Duration {
	if driver.IsBaked(t.Spec) {
		if every := pollInterval(t); every > 0 {
			return every
		}
		if d.cfg.heartbeat > 0 {
			return d.cfg.heartbeat
		}
		return DefaultProbeEvery
	}
	return d.cfg.probeEvery
}

// laneKey is the lane a task runs on: its endpoint, else the task alone.
func laneKey(t collection.TaskSpec) string {
	if t.EndpointID != "" {
		return t.EndpointID
	}
	return t.ID
}

// sync takes a worklist: a new task is due at once, a task gone leaves the
// heap, and a task whose connection fingerprint moved (#1061) is due at once
// (or, in flight, the moment its run ends). A listen task runs no schedule:
// it is what holds its endpoint's session.
func (d *dispatcher) sync(wl collection.WorklistReply, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	present := make(map[string]bool, len(wl.Tasks))
	for _, t := range wl.Tasks {
		if t.Mode != "listen" {
			present[t.ID] = true
		}
	}
	d.cfg.sched.retain(present)
	for id, e := range d.entries {
		if !present[id] {
			heap.Remove(&d.due, e.idx)
			delete(d.entries, id)
			delete(d.tasks, id)
			delete(d.held, id)
		}
	}
	for _, t := range wl.Tasks {
		if !present[t.ID] {
			continue
		}
		d.tasks[t.ID] = t
		moved := d.cfg.sched.rebook(t.ID, t.Fingerprint)
		e, known := d.entries[t.ID]
		if !known {
			e = &dueEntry{id: t.ID, at: now, fresh: true}
			d.entries[t.ID] = e
			heap.Push(&d.due, e)
			continue
		}
		if moved && !d.inflight[t.ID] {
			e.at, e.fresh, e.extra = now, true, false
			heap.Fix(&d.due, e.idx)
		}
	}
}

// pass dispatches every task due at now and returns how many it started.
func (d *dispatcher) pass(ctx context.Context, now time.Time) int {
	began := time.Now()
	d.mu.Lock()
	d.releaseLocked(now)
	var jobs []collection.TaskSpec
	for d.due.Len() > 0 && !d.due[0].at.After(now) {
		e := d.due[0]
		t := d.tasks[e.id]
		every := d.interval(t)
		phase := phaseOf(e.id, every)
		poll := driver.IsBaked(t.Spec)
		if d.inflight[e.id] {
			// The run before is still going: this due is skipped, counted,
			// never queued behind it.
			missed := 1 + missedSlots(e.at, now, phase, every)
			d.window.overruns++
			d.window.skips += missed
			if poll {
				for range missed {
					d.cfg.report.pollDue(t.EndpointID)
					d.cfg.report.pollSkipped(t.EndpointID)
				}
			}
			next := slotAfter(now, phase, every)
			d.cfg.sched.book(e.id, next)
			e.at, e.slot, e.fresh, e.extra = next, next, false, false
			heap.Fix(&d.due, e.idx)
			continue
		}
		if poll && driver.IsHTTP(t.Transport) && d.cfg.sessions.streamPending(t.EndpointID) {
			// The endpoint's stream is making its first attempt under this
			// fingerprint (#1038): the poll waits for its verdict rather
			// than sending the credential beside it.
			e.at = now.Add(time.Second)
			heap.Fix(&d.due, e.idx)
			continue
		}
		if late := now.Sub(e.at); late > d.window.lateMax {
			d.window.lateMax = late
		}
		missed := 0
		if !e.fresh && !e.extra {
			missed = missedSlots(e.at, now, phase, every)
		}
		d.window.skips += missed
		next := nextBooking(e.at, now, phase, every)
		if e.extra && e.slot.After(now) {
			// A run off the schedule keeps the slot its last pass booked.
			next = e.slot
		}
		d.cfg.sched.book(e.id, next)
		extra := e.extra
		e.at, e.slot, e.fresh, e.extra = next, next, false, false
		heap.Fix(&d.due, e.idx)
		if poll {
			for range missed {
				d.cfg.report.pollDue(t.EndpointID)
				d.cfg.report.pollSkipped(t.EndpointID)
			}
			if !extra {
				d.cfg.report.pollDue(t.EndpointID)
			}
			ownRequest := d.cfg.sessions.lookup(t.EndpointID) == nil || driver.IsHTTP(t.Transport)
			_, refused := d.cfg.sessions.refusedAt(t.EndpointID, statelessRung(t))
			if !(refused && ownRequest) {
				if _, holding := d.cfg.sessions.holding(t.EndpointID); holding {
					// The gate holds the endpoint for a reason other than a
					// refusal (#1650): nothing is sent and nothing booked
					// beyond the next due, so it is no failure and starts
					// no backoff. It counts held once per due and runs the
					// moment the gate opens.
					d.cfg.report.heldPoll(t.EndpointID)
					d.held[e.id] = t.EndpointID
					continue
				}
			}
		}
		delete(d.held, e.id)
		d.inflight[e.id] = true
		jobs = append(jobs, t)
	}
	d.mu.Unlock()
	for _, t := range jobs {
		id := t.ID
		d.pool.submit(laneKey(t), laneScheduled, func() {
			d.cfg.work(ctx, t)
			d.finished(id)
		})
	}
	d.mu.Lock()
	d.window.passes++
	if took := time.Since(began); took > d.window.passMax {
		d.window.passMax = took
	}
	d.mu.Unlock()
	return len(jobs)
}

// releaseLocked makes due at once what was waiting on a gate that opened: a
// refused poll whose held session reads up again (#1049), and a held poll
// whose endpoint's gate no longer holds it (#1650). Neither is a due of its
// own.
func (d *dispatcher) releaseLocked(now time.Time) {
	for _, id := range d.cfg.sched.refusedIDs() {
		t, ok := d.tasks[id]
		if !ok || driver.IsHTTP(t.Transport) {
			// An http poll is its own request: an up stream beside it says
			// nothing of what refused it (#1038).
			continue
		}
		if d.cfg.sessions.isUp(t.EndpointID) && d.cfg.sched.releaseRefused(id, true) {
			d.runNowLocked(id, now)
		}
	}
	for id, ep := range d.held {
		if _, holding := d.cfg.sessions.holding(ep); !holding {
			delete(d.held, id)
			d.runNowLocked(id, now)
		}
	}
}

func (d *dispatcher) runNowLocked(id string, now time.Time) {
	e, ok := d.entries[id]
	if !ok || d.inflight[id] {
		return
	}
	e.at, e.fresh, e.extra = now, true, true
	heap.Fix(&d.due, e.idx)
}

// finished ends a task's run: its next due is what the run booked (a retry
// on a backoff, else the slot its pass booked), or now when nothing is
// booked (its fingerprint moved while it ran). It wakes the run loop to
// re-arm its timer.
func (d *dispatcher) finished(id string) {
	d.mu.Lock()
	delete(d.inflight, id)
	if e, ok := d.entries[id]; ok {
		if at, booked := d.cfg.sched.nextOf(id); booked {
			// A retry ahead of its slot is a run off the schedule, no due.
			e.at, e.fresh, e.extra = at, false, at.Before(e.slot)
		} else {
			e.at, e.fresh, e.extra = d.cfg.clock(), true, false
		}
		heap.Fix(&d.due, e.idx)
	}
	d.mu.Unlock()
	d.kick()
}

// kick asks the run loop for a pass now, never blocking.
func (d *dispatcher) kick() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// next is the earliest due, for the run loop's timer.
func (d *dispatcher) next() (time.Time, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.due.Len() == 0 {
		return time.Time{}, false
	}
	return d.due[0].at, true
}

// settle waits for every dispatched run to end.
func (d *dispatcher) settle() { d.pool.wait() }

// takeWindow returns the dispatcher's window and starts the next.
func (d *dispatcher) takeWindow() dispatchWindow {
	d.mu.Lock()
	w := d.window
	d.window = dispatchWindow{}
	d.mu.Unlock()
	w.busyMax, w.queueMax = d.pool.takeWindow()
	return w
}
