package node

import (
	"sync/atomic"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/platformmetric"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// nodeReport is the node's account of itself (#1625): how long the
// dispatcher's passes take and the dues it could not meet, how late its
// tasks run against their intervals, how deep its spool is and what it drops, the
// sessions it holds, and what its process costs. They are platform metrics
// on the node's own platform component, sent in the node-self block of the
// batch the tick publishes; the server lands them under the platform writer.
//
// Two shapes, per the key table. The each-tick figures are one observation
// per sample, taken every heartbeat tick. The windowed counts (dues met in
// flight, skipped intervals, spool drops, publish failures, failed command
// pulls) are tallied over the node's
// report window, the collection report's (report.go), and emitted once when
// it ends, every window, a quiet window's as zero: a count written only when
// nonzero would leave the last event's count standing as the series' latest
// value long after it. The first window runs from the node's start, so it is
// whole and writes too. Pure: the run loop feeds it and sends what it takes.
type nodeReport struct {
	every time.Duration
	since time.Time

	overruns int
	skipped  int
	drops    int
	failures int
	// pullFailures is the command queue pulls that failed in the window
	// (#1626): no answer from the bus, or one the node could not read. The
	// command cycle runs off the heartbeat (#1634), so it tallies atomically.
	pullFailures atomic.Int64

	// rt is the runtime sampler the server shares, so the node's CPU time
	// reads as the time between two of its ticks.
	rt platformmetric.Runtime

	samples []*ogv1.MetricSample
}

// spoolFigures is what the spool reports to one tick: its queue depth now,
// and the batches it dropped and the publishes that failed since it last
// reported.
type spoolFigures struct {
	batches  int
	drops    int
	failures int
}

func newNodeReport(every time.Duration, now time.Time) *nodeReport {
	if every <= 0 {
		every = defaultReportEvery
	}
	return &nodeReport{every: every, since: now}
}

// setEvery applies the report window the worklist carries; none keeps the
// window the node has.
func (r *nodeReport) setEvery(every time.Duration) {
	if r != nil && every > 0 {
		r.every = every
	}
}

func (r *nodeReport) sample(key string, v float64, at time.Time, instance string) {
	r.samples = append(r.samples, &ogv1.MetricSample{Name: key, Value: v, Ts: timestamppb.New(at.UTC()), Instance: instance})
}

// dispatch records the dispatcher's window just ended (#1634): its longest
// pass when it ran one, the latest dispatch and the worst gap excess when a
// task ran on the schedule, and the dues it met in flight or passed over
// toward the report window.
func (r *nodeReport) dispatch(w dispatchWindow, at time.Time) {
	if r == nil {
		return
	}
	if w.passes > 0 {
		r.sample(writerclass.TickDuration, platformmetric.Millis(w.passMax), at, "")
	}
	r.overruns += w.overruns
	r.skipped += w.skips
	if w.runs > 0 {
		r.sample(writerclass.ScheduleLateness, platformmetric.Millis(w.lateMax), at, "")
		r.sample(writerclass.ScheduleGapExcess, platformmetric.Millis(w.gapMax), at, "")
	}
}

// spool records the spool's depth now and tallies its drops and failures.
func (r *nodeReport) spool(f spoolFigures, at time.Time) {
	if r == nil {
		return
	}
	r.sample(writerclass.SpoolBatchCount, float64(f.batches), at, "")
	r.drops += f.drops
	r.failures += f.failures
}

// sessions records the sessions the node holds, one sample per state, a
// state with none included, so a state that empties reads zero rather than
// its last count.
func (r *nodeReport) sessions(held map[collection.SessionState]int, at time.Time) {
	if r == nil {
		return
	}
	for _, st := range collection.SessionStates {
		r.sample(writerclass.SessionHeldCount, float64(held[st]), at, string(st))
	}
}

// process records what the node's process costs now.
func (r *nodeReport) process(p platformmetric.Process, at time.Time) {
	if r == nil {
		return
	}
	for _, rd := range r.rt.Readings(p) {
		r.sample(rd.Key, rd.Value, at, "")
	}
}

// commandPullFailed tallies one failed pull of the command queue toward the
// window.
func (r *nodeReport) commandPullFailed() {
	if r != nil {
		r.pullFailures.Add(1)
	}
}

// worklist records the size of a worklist the node just installed.
func (r *nodeReport) worklist(tasks int, at time.Time) {
	if r == nil {
		return
	}
	r.sample(writerclass.WorklistTaskCount, float64(tasks), at, "")
}

// take returns what the report holds: the each-tick samples since the last
// take, and, once the window has run its length, the window's counts, zero
// included, stamped at now, the window's end, the next window starting there.
func (r *nodeReport) take(now time.Time) []*ogv1.MetricSample {
	if r == nil {
		return nil
	}
	if !now.Before(r.since.Add(r.every)) {
		for _, c := range []struct {
			key string
			n   int
		}{
			{writerclass.TickOverrunCount, r.overruns},
			{writerclass.ScheduleSkipCount, r.skipped},
			{writerclass.SpoolDropCount, r.drops},
			{writerclass.PublishFailureCount, r.failures},
			{writerclass.CommandPullFailureCount, int(r.pullFailures.Swap(0))},
		} {
			r.sample(c.key, float64(c.n), now, "")
		}
		r.overruns, r.skipped, r.drops, r.failures = 0, 0, 0, 0
		r.since = now
	}
	out := r.samples
	r.samples = nil
	return out
}

// sessionCounts is how many sessions the node holds in each state.
func (m *sessionManager) sessionCounts() map[collection.SessionState]int {
	out := map[collection.SessionState]int{}
	if m == nil {
		return out
	}
	m.mapsMu.RLock()
	defer m.mapsMu.RUnlock()
	for _, h := range m.held {
		st, _ := h.sess.State()
		out[st]++
	}
	return out
}

// account closes a heartbeat tick: it records the dispatcher's window, the
// spool's figures, the sessions held and the process, and takes what the
// report has to send.
func (r *nodeReport) account(w dispatchWindow, spool *telemetrySpool, sessions *sessionManager) []*ogv1.MetricSample {
	if r == nil {
		return nil
	}
	now := time.Now()
	r.dispatch(w, now)
	r.spool(spool.takeFigures(), now)
	r.sessions(sessions.sessionCounts(), now)
	r.process(platformmetric.ReadProcess(), now)
	return r.take(now)
}
