package node

import (
	"sort"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
)

// defaultReportEvery is the collection report window a node keeps when its
// worklist carries none (an older server): the platform's own default.
const defaultReportEvery = time.Minute

// collectionReport accumulates each endpoint's collection telemetry over a
// report window (#1369): the node's account of its own collection, built-in
// data every endpoint carries whatever its driver, as against the collected
// data a driver declares. Its polls report a mean duration and a failure
// count, its held session's or socket's listeners the messages they claimed
// and did not. Aggregating over a window keeps the cost at a few rows per
// endpoint per window however fast a driver polls or a device pushes. The
// dispatcher, its workers and the run loop feed it from their own goroutines
// (#1634), and the run loop publishes what it takes.
type collectionReport struct {
	mu        sync.Mutex
	win       reportWindow
	endpoints map[string]*endpointTally
}

// endpointTally is one endpoint's window so far.
type endpointTally struct {
	runs     int
	failures int
	took     time.Duration
	held     bool
	// cpu is the thread CPU of the runs whose clock read (#1870), cpuRuns
	// how many: a platform with no thread clock reads none.
	cpu       time.Duration
	cpuRuns   int
	claimed   int
	unclaimed int
	// heldPolls is the due polls the endpoint's gate held (#1650): never
	// sent, so neither a run nor a failure.
	heldPolls int
	// due is the polls that fell due in the window (#1634), held, refused
	// and skipped ones included; skipped is the dues the dispatcher passed
	// over because the poll's last run was still in flight. Together they
	// are the endpoint's schedule loss, skipped over due.
	due     int
	skipped int
	// holdExpiries is the disruptions (#1651) the hold backstop released
	// with no up verdict from the endpoint.
	holdExpiries int
}

// endpointReport is one endpoint's rows for a window, under the task they
// publish under.
type endpointReport struct {
	task    string
	samples []collection.Sample
}

// newCollectionReport starts a report whose windows sit on the grid seed
// places (#1779): the node's phaseSeed, so its reports keep their place
// across a restart and spread from other nodes'.
func newCollectionReport(every time.Duration, seed uint64, now time.Time) *collectionReport {
	if every <= 0 {
		every = defaultReportEvery
	}
	return &collectionReport{win: newReportWindow(every, seed, now), endpoints: map[string]*endpointTally{}}
}

// setEvery applies the window the worklist carries, from the window in
// progress; none (zero) keeps the window the node has.
func (r *collectionReport) setEvery(every time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.win.setEvery(every)
}

// every is the report's window length.
func (r *collectionReport) every() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.win.every
}

func (r *collectionReport) tally(endpoint string) *endpointTally {
	e := r.endpoints[endpoint]
	if e == nil {
		e = &endpointTally{}
		r.endpoints[endpoint] = e
	}
	return e
}

// poll records one driver poll run of endpoint: how long it took, the CPU the
// kernel charged it where that read, and whether it failed (faulted and
// landed nothing, as the scheduler books a failure).
func (r *collectionReport) poll(endpoint string, cost runCost, failed bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.tally(endpoint)
	e.runs++
	e.took += cost.took
	if cost.cpuRead {
		e.cpuRuns++
		e.cpu += cost.cpu
	}
	if failed {
		e.failures++
	}
}

// refused records a due poll the endpoint's gate refused: nothing was sent,
// so it is a failure with no time, and the mean stays the time of the polls
// that ran.
func (r *collectionReport) refused(endpoint string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tally(endpoint).failures++
}

// heldPoll records a due poll the endpoint's gate held for a reason other
// than a refusal (its probe reads it unreachable, #1650): nothing was sent
// and nothing failed, so it is apart from the failures.
func (r *collectionReport) heldPoll(endpoint string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tally(endpoint).heldPolls++
}

// holdExpired records a disruption (#1651) the hold backstop released: the
// endpoint gave no up verdict between its quiet period's end and the hold.
func (r *collectionReport) holdExpired(endpoint string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tally(endpoint).holdExpiries++
}

// pollDue records one poll of endpoint falling due (#1634), whatever then
// became of it.
func (r *collectionReport) pollDue(endpoint string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tally(endpoint).due++
}

// pollSkipped records a due poll of endpoint the dispatcher skipped because
// its last run was still in flight (#1634): counted, never queued.
func (r *collectionReport) pollSkipped(endpoint string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tally(endpoint).skipped++
}

// pushes records what a held endpoint's listeners claimed and did not since
// the last call; a held endpoint reports its claimed count every window, zero
// included.
func (r *collectionReport) pushes(endpoint string, claimed, unclaimed int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.tally(endpoint)
	e.held = true
	e.claimed += claimed
	e.unclaimed += unclaimed
}

// due reports a window that has reached its grid end.
func (r *collectionReport) due(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.win.due(now)
}

// take returns the window's rows, each stamped at now, and starts the next
// window at the grid end of this one. Each endpoint's rows publish under the task
// tasks names for it, the task the node holds now: a driver revision
// mid-window replaces an endpoint's task ids, and a row under a deleted one
// would be refused as an orphan. An endpoint tasks does not name (deleted, or
// moved to another node) reports nothing. A mean duration lands only for an
// endpoint that polled, a mean CPU time only for one whose runs read the
// thread clock, a failure, held, due, skipped, unclaimed or hold-expiry count only when nonzero, and a
// claimed count for every held endpoint. Ordered by task, so a publish order
// holds still.
func (r *collectionReport) take(now time.Time, tasks map[string]string) []endpointReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]endpointReport, 0, len(r.endpoints))
	for endpoint, e := range r.endpoints {
		task, held := tasks[endpoint]
		if !held {
			continue
		}
		var dps []collection.Sample
		metric := func(key string, v float64) {
			dps = append(dps, collection.Sample{Name: key, Value: v, TS: now})
		}
		if e.runs > 0 {
			metric(probekeys.PollDuration, float64(e.took)/float64(e.runs)/float64(time.Millisecond))
		}
		if e.cpuRuns > 0 {
			metric(probekeys.PollCPUTime, float64(e.cpu)/float64(e.cpuRuns)/float64(time.Millisecond))
		}
		if e.failures > 0 {
			metric(probekeys.PollFailureCount, float64(e.failures))
		}
		if e.heldPolls > 0 {
			metric(probekeys.PollHeldCount, float64(e.heldPolls))
		}
		if e.due > 0 {
			metric(probekeys.PollDueCount, float64(e.due))
		}
		if e.skipped > 0 {
			metric(probekeys.PollSkippedCount, float64(e.skipped))
		}
		if e.held {
			metric(probekeys.PushMessageCount, float64(e.claimed))
		}
		if e.unclaimed > 0 {
			metric(probekeys.PushUnclaimedCount, float64(e.unclaimed))
		}
		if e.holdExpiries > 0 {
			metric(probekeys.HoldExpiryCount, float64(e.holdExpiries))
		}
		if len(dps) > 0 {
			out = append(out, endpointReport{task: task, samples: dps})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].task < out[j].task })
	r.endpoints = map[string]*endpointTally{}
	r.win.close(now)
	return out
}

// publishTasks names each endpoint's least task id in the worklist, the task
// its collection report publishes under: any task of an endpoint names its
// component and its endpoint, and the least holds still while the worklist
// does.
func publishTasks(tasks []collection.TaskSpec) map[string]string {
	out := make(map[string]string, len(tasks))
	for _, t := range tasks {
		if cur, ok := out[t.EndpointID]; !ok || t.ID < cur {
			out[t.EndpointID] = t.ID
		}
	}
	return out
}
