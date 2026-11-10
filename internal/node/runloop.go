package node

import (
	"context"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// runLoop is the node's run-loop goroutine (#1636, ADR-1637): the one owner
// of the session maps, so a reconcile, a borrowed session and a housekeeping
// pass never race. Each kind of work has its own clock, a channel, so a test
// drives the loop with clocks it owns and a nil channel never fires:
//
//   - a worklist reconciles sessions the moment it arrives, so a held
//     endpoint added, removed or re-keyed does not wait for a tick;
//   - reconcile is the safety net, for a gate or a retry that changed
//     between worklists;
//   - heartbeat runs the heartbeat's own work (liveness, the spool, the
//     tasks and commands);
//   - housekeep flushes what the sessions accumulated (unclaimed lines,
//     pushes into the collection report) and publishes the report when due.
//
// Every handler runs on this goroutine; none may block on another.
type runLoop struct {
	worklists <-chan collection.WorklistReply
	jobs      <-chan runJob
	heartbeat <-chan time.Time
	reconcile <-chan time.Time
	housekeep <-chan time.Time
	sessions  *sessionManager

	onJob       func(runJob)
	onHeartbeat func(collection.WorklistReply)
	onHousekeep func(collection.WorklistReply)
}

// run serves the loop until ctx ends and returns the last worklist it took.
func (l *runLoop) run(ctx context.Context, wl collection.WorklistReply) collection.WorklistReply {
	for {
		select {
		case <-ctx.Done():
			return wl
		case job := <-l.jobs:
			if l.onJob != nil {
				l.onJob(job)
			}
		case next := <-l.worklists:
			// A failed pull sends nothing, so the last good worklist stays.
			wl = next
			l.reconcileSessions(wl)
		case <-l.reconcile:
			l.reconcileSessions(wl)
		case <-l.heartbeat:
			if l.onHeartbeat != nil {
				l.onHeartbeat(wl)
			}
		case <-l.housekeep:
			if l.onHousekeep != nil {
				l.onHousekeep(wl)
			}
		}
	}
}

// reconcileSessions holds, recycles or tears down sessions and datagram
// sockets to match wl.
func (l *runLoop) reconcileSessions(wl collection.WorklistReply) {
	if l.sessions != nil {
		l.sessions.reconcile(wl.Tasks)
	}
}

// housekeepEvery is the housekeeping interval: the report window, so a
// window's report is published when it is due, and never longer than the
// heartbeat, so unclaimed lines are not held longer than they were when the
// heartbeat flushed them.
func housekeepEvery(heartbeat, report time.Duration) time.Duration {
	if report > 0 && report < heartbeat {
		return report
	}
	return heartbeat
}
