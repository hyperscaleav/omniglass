package node

import (
	"strconv"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The poll schedule (#822): a driver poll declares how often it runs
// (schedule.every, baked into its task), and the node honors it rather than
// polling on every heartbeat. pollSchedule remembers, per task id, when each
// poll is next due; it lives for the run (a restart polls at its first tick).
// It gates driver polls only: reachability probes run every tick, and a
// listener's pushes are never gated, since they ride the held session as
// they arrive.
type pollSchedule struct {
	heartbeat time.Duration
	next      map[string]time.Time
	backoff   map[string]time.Duration // the last retry wait of a failing poll
	// refused marks a poll whose last run met a terminal refusal (#1049): it
	// waits its interval, unless its session reads up again first.
	refused map[string]bool
	// fingerprint is the connection fingerprint each poll was booked under.
	fingerprint map[string]string
	// occurred is what each poll's events and log lines last landed (#1145),
	// keyed by task, then by emit: a plain poll fires an occurrence when its
	// content changes, not on every run. Node memory alone, so a restart may
	// fire one again.
	occurred map[string]map[string]string
}

// newPollSchedule keeps the schedule for a node ticking every heartbeat,
// which is also where a failed poll's retry backoff starts.
func newPollSchedule(heartbeat time.Duration) *pollSchedule {
	return &pollSchedule{heartbeat: heartbeat, next: map[string]time.Time{}, backoff: map[string]time.Duration{}, refused: map[string]bool{}, fingerprint: map[string]string{}, occurred: map[string]map[string]string{}}
}

// failed rebooks a poll that ran at now and landed nothing (its fetch
// failed): it retries after min(every, max(heartbeat, backoff)), the backoff
// starting at the heartbeat and doubling on each consecutive failure, so a
// 24h poll that fails during a device reboot retries in seconds, not a day,
// and a dead device is never asked more often than every heartbeat.
func (s *pollSchedule) failed(taskID string, every time.Duration, now time.Time) {
	if every <= 0 {
		return
	}
	wait := s.heartbeat
	if prev, ok := s.backoff[taskID]; ok {
		wait = 2 * prev
	}
	if wait < s.heartbeat {
		wait = s.heartbeat
	}
	if wait > every {
		wait = every
	}
	s.backoff[taskID] = wait
	s.next[taskID] = now.Add(wait)
}

// failedAtLeast is failed with a floor on the wait: a component that asked
// for a Retry-After (#902) is not asked again sooner, though never later than
// the poll's own interval.
func (s *pollSchedule) failedAtLeast(taskID string, every time.Duration, now time.Time, atLeast time.Duration) {
	s.failed(taskID, every, now)
	if atLeast <= 0 || every <= 0 {
		return
	}
	if atLeast > every {
		atLeast = every
	}
	if next := now.Add(atLeast); next.After(s.next[taskID]) {
		s.next[taskID] = next
	}
}

// succeeded ends a poll's failing streak; its next run stays booked from
// its due time.
func (s *pollSchedule) succeeded(taskID string) {
	delete(s.backoff, taskID)
	delete(s.refused, taskID)
}

// refusedRun books a poll whose run met a terminal refusal (#1049): no
// backoff, its next run at its interval, unless its session is up again
// first (releaseRefused).
func (s *pollSchedule) refusedRun(taskID string) {
	delete(s.backoff, taskID)
	s.refused[taskID] = true
}

// rebook forgets a poll's booking and failure backoff when its endpoint's
// connection fingerprint moved (#1061): an address, port or secret edited in
// place is new inputs, so the poll runs at the next tick rather than waiting
// out a backoff built up against the old ones.
func (s *pollSchedule) rebook(taskID, fingerprint string) {
	prev, seen := s.fingerprint[taskID]
	s.fingerprint[taskID] = fingerprint
	if !seen || prev == fingerprint {
		return
	}
	delete(s.next, taskID)
	delete(s.backoff, taskID)
	delete(s.refused, taskID)
}

// releaseRefused makes a refused poll due at once when its session is up
// again: a fixed credential lands data at the next tick, not an interval
// later. A poll that was not refused, or whose session is still down or
// rejected, keeps its booking.
func (s *pollSchedule) releaseRefused(taskID string, sessionUp bool) {
	if !s.refused[taskID] || !sessionUp {
		return
	}
	delete(s.refused, taskID)
	delete(s.next, taskID)
}

// nextOf is when a poll is next due (for tests and logs).
func (s *pollSchedule) nextOf(taskID string) (time.Time, bool) {
	t, ok := s.next[taskID]
	return t, ok
}

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
			delete(s.backoff, id)
			delete(s.refused, id)
			delete(s.fingerprint, id)
		}
	}
	for id := range s.occurred {
		if !ids[id] {
			delete(s.occurred, id)
		}
	}
}

// changed holds a poll run's events and log lines to what that task last
// landed (#1145): one whose content (its message, attributes, severity) is
// unchanged since is dropped, so a status page's last error lands when it
// changes rather than every tick. Metrics and properties pass through: the
// property lane has its transition guard, and a metric is a reading each
// run. Pure: commitChanged records what landed.
func (s *pollSchedule) changed(taskID string, dps []collection.Sample) []collection.Sample {
	last := s.occurred[taskID]
	out := dps[:0:0]
	for _, d := range dps {
		if d.Lane == driver.LaneEvent || d.Lane == driver.LaneLog {
			if last != nil && last[occurrenceSlot(d)] == occurrenceContent(d) {
				continue
			}
		}
		out = append(out, d)
	}
	return out
}

// commitChanged records the occurrences a run landed, once published, so a
// run whose publish failed fires them again next time.
func (s *pollSchedule) commitChanged(taskID string, landed []collection.Sample) {
	for _, d := range landed {
		if d.Lane != driver.LaneEvent && d.Lane != driver.LaneLog {
			continue
		}
		if s.occurred[taskID] == nil {
			s.occurred[taskID] = map[string]string{}
		}
		s.occurred[taskID][occurrenceSlot(d)] = occurrenceContent(d)
	}
}

// occurrenceSlot names the emit an occurrence came from: its index in the
// poll's function, since two log lines share a name ("") and two events may
// share a type.
func occurrenceSlot(d collection.Sample) string { return d.Lane + "\x00" + strconv.Itoa(d.Emit) }

// occurrenceContent is what an occurrence says, its time aside.
func occurrenceContent(d collection.Sample) string {
	return d.Text + "\x00" + string(d.Attributes) + "\x00" + d.Severity + "\x00" + d.Facility
}
