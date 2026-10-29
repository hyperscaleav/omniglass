package node

import (
	"sort"
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
// endpoint per window however fast a driver polls or a device pushes. Pure:
// the run loop feeds it and publishes what it takes.
type collectionReport struct {
	every     time.Duration
	since     time.Time
	endpoints map[string]*endpointTally
}

// endpointTally is one endpoint's window so far.
type endpointTally struct {
	runs      int
	failures  int
	took      time.Duration
	held      bool
	claimed   int
	unclaimed int
}

// endpointReport is one endpoint's rows for a window, under the task they
// publish under.
type endpointReport struct {
	task    string
	samples []collection.Sample
}

func newCollectionReport(every time.Duration, now time.Time) *collectionReport {
	if every <= 0 {
		every = defaultReportEvery
	}
	return &collectionReport{every: every, since: now, endpoints: map[string]*endpointTally{}}
}

// setEvery applies the window the worklist carries, from the window in
// progress; none (zero) keeps the window the node has.
func (r *collectionReport) setEvery(every time.Duration) {
	if every > 0 {
		r.every = every
	}
}

func (r *collectionReport) tally(endpoint string) *endpointTally {
	e := r.endpoints[endpoint]
	if e == nil {
		e = &endpointTally{}
		r.endpoints[endpoint] = e
	}
	return e
}

// poll records one driver poll run of endpoint: how long it took and whether
// it failed (faulted and landed nothing, as the scheduler books a failure).
func (r *collectionReport) poll(endpoint string, took time.Duration, failed bool) {
	if r == nil {
		return
	}
	e := r.tally(endpoint)
	e.runs++
	e.took += took
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
	r.tally(endpoint).failures++
}

// pushes records what a held endpoint's listeners claimed and did not since
// the last call; a held endpoint reports its claimed count every window, zero
// included.
func (r *collectionReport) pushes(endpoint string, claimed, unclaimed int) {
	if r == nil {
		return
	}
	e := r.tally(endpoint)
	e.held = true
	e.claimed += claimed
	e.unclaimed += unclaimed
}

// due reports a window that has run its length.
func (r *collectionReport) due(now time.Time) bool { return !now.Before(r.since.Add(r.every)) }

// take returns the window's rows, each stamped at now, the window's end, and
// starts the next window there. Each endpoint's rows publish under the task
// tasks names for it, the task the node holds now: a driver revision
// mid-window replaces an endpoint's task ids, and a row under a deleted one
// would be refused as an orphan. An endpoint tasks does not name (deleted, or
// moved to another node) reports nothing. A mean duration lands only for an
// endpoint that polled, a failure or unclaimed count only when nonzero, and a
// claimed count for every held endpoint. Ordered by task, so a publish order
// holds still.
func (r *collectionReport) take(now time.Time, tasks map[string]string) []endpointReport {
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
		if e.failures > 0 {
			metric(probekeys.PollFailureCount, float64(e.failures))
		}
		if e.held {
			metric(probekeys.PushMessageCount, float64(e.claimed))
		}
		if e.unclaimed > 0 {
			metric(probekeys.PushUnclaimedCount, float64(e.unclaimed))
		}
		if len(dps) > 0 {
			out = append(out, endpointReport{task: task, samples: dps})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].task < out[j].task })
	r.endpoints = map[string]*endpointTally{}
	r.since = now
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
