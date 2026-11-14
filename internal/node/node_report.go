package node

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/delivery"
	"github.com/hyperscaleav/omniglass/internal/platformmetric"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// nodeReport is the node's account of itself (#1625): how long the
// dispatcher's passes take and the dues it could not meet (#1634), how late
// its tasks run against their intervals, how deep its spool is and what it
// delivers, the sessions it holds, and what its process costs. They are platform metrics
// on the node's own platform component, sent in the node-self block of the
// batch the tick publishes; the server lands them under the platform writer.
//
// Two shapes, per the key table. The each-tick figures are one observation
// per sample, taken every heartbeat tick. The windowed counts (dues met in
// flight, skipped intervals, failed command pulls, and the spool's delivery
// per telemetry kind, #1659, and the dispatcher's schedule hop, runs due
// against runs made per kind under the instance schedule/{kind}, #1693) are
// tallied over the node's report window, the collection report's
// (report.go), and emitted once when it ends, every window, a quiet window's
// as zero: a count written only when nonzero would leave the last event's
// count standing as the series' latest value long after it. The window sits
// on the node's grid (#1779), the collection report's, and its first runs
// from the node's start to its first slot and writes too. Pure: the run loop
// feeds it and sends what it takes.
type nodeReport struct {
	win reportWindow

	overruns int
	skipped  int
	// pool is the lane pool's busy worker time against the worker time it
	// had over the report window's dispatch windows: worker-busy-fraction
	// (#1686).
	pool poolWindow
	// flow is the items the spool settled in the window, per kind: in for
	// every item a batch carried once it was acked or lost, out for the
	// acked (#1659).
	flow delivery.Counts
	// schedule is the dispatcher's schedule hop over the window (#1693):
	// runs due in, runs made out, per kind, written under schedule/{kind}.
	schedule delivery.Counts
	// pullFailures is the command queue pulls that failed in the window
	// (#1626): no answer from the bus, or one the node could not read.
	// Atomic because the command cycle runs off the heartbeat goroutine
	// (#1634) while the heartbeat takes the window.
	pullFailures atomic.Int64
	// pulls is the worklist pulls in the window (#1868), tallied from the
	// pull loop's own goroutine (#1577).
	pulls worklistPulls

	// rt is the runtime sampler the server shares, so the node's CPU time
	// reads as the time between two of its ticks.
	rt platformmetric.Runtime

	samples []*ogv1.MetricSample
}

// spoolFigures is what the spool reports to one tick: its queue depth now,
// and the items its batches settled since it last reported.
type spoolFigures struct {
	batches int
	flow    delivery.Counts
}

// newNodeReport starts a report whose windows sit on the grid seed places,
// the collection report's (#1779).
func newNodeReport(every time.Duration, seed uint64, now time.Time) *nodeReport {
	if every <= 0 {
		every = defaultReportEvery
	}
	return &nodeReport{win: newReportWindow(every, seed, now)}
}

// setEvery applies the report window the worklist carries; none keeps the
// window the node has.
func (r *nodeReport) setEvery(every time.Duration) {
	if r != nil {
		r.win.setEvery(every)
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
	r.pool.add(w.pool)
	r.schedule.Merge(w.schedule)
	if w.runs > 0 {
		r.sample(writerclass.ScheduleLateness, platformmetric.Millis(w.lateMax), at, "")
		r.sample(writerclass.ScheduleGapExcess, platformmetric.Millis(w.gapMax), at, "")
	}
}

// spool records the spool's depth now and tallies its delivery.
func (r *nodeReport) spool(f spoolFigures, at time.Time) {
	if r == nil {
		return
	}
	r.sample(writerclass.SpoolBatchCount, float64(f.batches), at, "")
	r.flow.Merge(f.flow)
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

// worklistPulled tallies one worklist pull toward the window, refused when
// the bus gave no worklist the node could read, so it kept its last one.
func (r *nodeReport) worklistPulled(refused bool) {
	if r != nil {
		r.pulls.add(refused)
	}
}

// worklistPulls tallies the worklist pulls of a window. A mutex rather than
// atomics, so a window's attempts and refusals are taken together.
type worklistPulls struct {
	mu          sync.Mutex
	attempts    int
	refused     int
	lastRefused bool
}

func (p *worklistPulls) add(refused bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.attempts++
	if refused {
		p.refused++
	}
	p.lastRefused = refused
}

// lossRatio returns the window's share of refused pulls and starts the next
// window. A window with no pull carries the last pull's outcome, so a node
// backed off between refused pulls still reads 1.
func (p *worklistPulls) lossRatio() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	attempts, refused := p.attempts, p.refused
	p.attempts, p.refused = 0, 0
	if attempts == 0 {
		if p.lastRefused {
			return 1
		}
		return 0
	}
	return float64(refused) / float64(attempts)
}

// worklist records the size of a worklist the node just installed.
func (r *nodeReport) worklist(tasks int, at time.Time) {
	if r == nil {
		return
	}
	r.sample(writerclass.WorklistTaskCount, float64(tasks), at, "")
}

// take returns what the report holds: the each-tick samples since the last
// take, and, once the window has reached its grid end, the window's counts,
// zero included, stamped at now, the next window starting at that end.
func (r *nodeReport) take(now time.Time) []*ogv1.MetricSample {
	if r == nil {
		return nil
	}
	if r.win.due(now) {
		for _, c := range []struct {
			key string
			n   int
		}{
			{writerclass.TickOverrunCount, r.overruns},
			{writerclass.ScheduleSkipCount, r.skipped},
			{writerclass.CommandPullFailureCount, int(r.pullFailures.Swap(0))},
		} {
			r.sample(c.key, float64(c.n), now, "")
		}
		r.sample(writerclass.WorklistPullLossRatio, r.pulls.lossRatio(), now, "")
		// One pool serves every kind of work today, so the fraction is
		// the lane pool's (#1686); per-kind pools get an instance each.
		r.sample(writerclass.WorkerBusyFraction, platformmetric.BusyFraction(r.pool.busy, r.pool.capacity), now, "lane")
		for _, rd := range r.flow.Readings() {
			r.sample(rd.Key, rd.Value, now, rd.Instance)
		}
		for _, rd := range r.schedule.HopReadings(delivery.Schedule) {
			r.sample(rd.Key, rd.Value, now, rd.Instance)
		}
		r.overruns, r.skipped, r.flow, r.schedule = 0, 0, delivery.Counts{}, delivery.Counts{}
		r.pool = poolWindow{}
		r.win.close(now)
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
