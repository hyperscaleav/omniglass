package node

import "time"

// The poll schedule (#822): a driver poll declares how often it runs
// (schedule.every, baked into its task), and the node honors it rather than
// polling on every heartbeat. pollSchedule remembers, per task id, when each
// poll is next due; it lives for the run (a restart polls at its first tick).
// It gates driver polls only: reachability probes run every tick, and a
// listener's pushes are never gated, since they ride the held session as
// they arrive.
type pollSchedule struct {
	next map[string]time.Time
}

func newPollSchedule() *pollSchedule { return &pollSchedule{next: map[string]time.Time{}} }

// due reports whether the task runs at now, and if so books its next run. A
// task with no interval runs every tick. A task new to the schedule runs at
// once. The next run is booked from the due time, not from now, so a tick's
// jitter does not drift the cadence; a tick so late it missed whole
// intervals runs once and books from now, never a catch-up burst.
func (s *pollSchedule) due(taskID string, every time.Duration, now time.Time) bool {
	if every <= 0 {
		return true
	}
	at, booked := s.next[taskID]
	if booked && now.Before(at) {
		return false
	}
	next := now.Add(every)
	if booked && now.Sub(at) < every {
		next = at.Add(every)
	}
	s.next[taskID] = next
	return true
}

// retain forgets tasks that left the worklist, so the map does not grow
// for the life of the run and a task that returns runs at once.
func (s *pollSchedule) retain(ids map[string]bool) {
	for id := range s.next {
		if !ids[id] {
			delete(s.next, id)
		}
	}
}
