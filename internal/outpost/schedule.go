package node

import (
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The poll schedule (#822): a driver poll declares how often it runs
// (schedule.every, baked into its task), and the node honors it. Since #1634
// the dispatcher (scheduler.go) owns when each task is due, on a timer of its
// own rather than the heartbeat tick; pollSchedule remembers, per task id,
// what a run's outcome books on top of that: a failing poll's retry backoff,
// a refused poll (#1049), the connection fingerprint each poll was booked
// under (#1061) and what its events and log lines last landed (#1145). It
// lives for the run and is safe for the dispatcher and its workers to share.
type pollSchedule struct {
	mu      sync.Mutex
	next    map[string]time.Time
	backoff map[string]time.Duration // the last retry wait of a failing poll
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

// backoffBase is where a failing poll's retry wait starts (#1634): a fixed
// second, doubling from there, never the heartbeat.
const backoffBase = time.Second

func newPollSchedule() *pollSchedule {
	return &pollSchedule{next: map[string]time.Time{}, backoff: map[string]time.Duration{}, refused: map[string]bool{}, fingerprint: map[string]string{}, occurred: map[string]map[string]string{}}
}

// failed rebooks a poll that ran at now and landed nothing (its fetch
// failed): it retries after min(every, backoff), the backoff starting at
// backoffBase and doubling on each consecutive failure, so a 24h poll that
// fails during a device reboot retries in seconds, not a day, and a dead
// device is never asked more often than its own interval allows.
func (s *pollSchedule) failed(taskID string, every time.Duration, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failedLocked(taskID, every, now)
}

func (s *pollSchedule) failedLocked(taskID string, every time.Duration, now time.Time) {
	if every <= 0 {
		return
	}
	wait := backoffBase
	if prev, ok := s.backoff[taskID]; ok {
		wait = 2 * prev
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failedLocked(taskID, every, now)
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

// succeeded ends a poll's failing streak; its next run stays where the
// dispatcher booked it.
func (s *pollSchedule) succeeded(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.backoff, taskID)
	delete(s.refused, taskID)
}

// refusedRun books a poll whose run met a terminal refusal (#1049): no
// backoff, its next run at its interval, unless its session is up again
// first (releaseRefused).
func (s *pollSchedule) refusedRun(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.backoff, taskID)
	s.refused[taskID] = true
}

// book records when the dispatcher next runs a task.
func (s *pollSchedule) book(taskID string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next[taskID] = at
}

// rebook records the connection fingerprint a task is booked under and
// reports whether it moved (#1061): an address, port or secret edited in
// place is new inputs, so the booking, backoff and refusal built up against
// the old ones are dropped and the task runs at once. A first-seen or
// unchanged fingerprint keeps them.
func (s *pollSchedule) rebook(taskID, fingerprint string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, seen := s.fingerprint[taskID]
	s.fingerprint[taskID] = fingerprint
	if !seen || prev == fingerprint {
		return false
	}
	delete(s.next, taskID)
	delete(s.backoff, taskID)
	delete(s.refused, taskID)
	return true
}

// releaseRefused reports whether a refused poll is released because its
// session is up again, so a fixed credential lands data at once, not an
// interval later. A poll that was not refused, or whose session is still down
// or rejected, keeps its booking.
func (s *pollSchedule) releaseRefused(taskID string, sessionUp bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.refused[taskID] || !sessionUp {
		return false
	}
	delete(s.refused, taskID)
	delete(s.next, taskID)
	return true
}

// refusedIDs is every refused poll, sorted, for the dispatcher's release
// sweep.
func (s *pollSchedule) refusedIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.refused))
	for id := range s.refused {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// nextOf is when a task is next due, if booked.
func (s *pollSchedule) nextOf(taskID string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.next[taskID]
	return t, ok
}

// retain forgets tasks that left the worklist, so no map grows for the life
// of the run and a task that returns runs at once. Each map is swept by its
// own keys: a task can be remembered without ever being booked.
func (s *pollSchedule) retain(ids map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	forget(s.next, ids)
	forget(s.backoff, ids)
	forget(s.refused, ids)
	forget(s.fingerprint, ids)
	forget(s.occurred, ids)
}

// forget drops every task of m not in ids.
func forget[V any](m map[string]V, ids map[string]bool) {
	for id := range m {
		if !ids[id] {
			delete(m, id)
		}
	}
}

// forgetOccurrences drops what every poll last landed, so each one's next
// run lands its events and log lines again: the run loop's answer to a spool
// overflow that may have dropped the batch that carried them.
func (s *pollSchedule) forgetOccurrences() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.occurred)
}

// changed holds a poll run's events and log lines to what that task last
// landed (#1145): one whose content (its message, attributes, severity) is
// unchanged since is dropped, so a status page's last error lands when it
// changes rather than every tick. Metrics and properties pass through: the
// property lane has its transition guard, and a metric is a reading each
// run. Pure: commitChanged records what landed.
func (s *pollSchedule) changed(taskID string, dps []collection.Sample) []collection.Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
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

// commitChanged records what a run saw, once its landing published, so a run
// whose publish failed fires its occurrences again next time. seen is the
// run's whole sample set, not only what landed: the memory is rebuilt from it,
// so an occurrence the run saw unchanged stays remembered and a slot the run
// did not see is dropped. A function whose identities rotate (#1386) holds
// one run's slots, never every identity it has heard.
func (s *pollSchedule) commitChanged(taskID string, seen []collection.Sample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := map[string]string{}
	for _, d := range seen {
		if d.Lane != driver.LaneEvent && d.Lane != driver.LaneLog {
			continue
		}
		next[occurrenceSlot(d)] = occurrenceContent(d)
	}
	if len(next) == 0 {
		// A run that saw no occurrence (a failed fetch lands nothing) leaves
		// the memory as it was, so the next good run does not fire again
		// what it already landed. The memory stays one run's size.
		return
	}
	s.occurred[taskID] = next
}

// occurrenceSlot names the emit an occurrence came from: its index in the
// poll's function, since two log lines share a name ("") and two events may
// share a type; and, for a function reading records (#1386), the record's
// identity (or, with none, its index), so two rooms' events are remembered
// apart.
func occurrenceSlot(d collection.Sample) string {
	slot := d.Lane + "\x00" + strconv.Itoa(d.Emit)
	if d.Identity != nil {
		return slot + "\x00" + d.Identity.Key + "\x00" + d.Identity.Value
	}
	if d.Record > 0 {
		return slot + "\x00#" + strconv.Itoa(d.Record)
	}
	return slot
}

// occurrenceContent is what an occurrence says, its time aside.
func occurrenceContent(d collection.Sample) string {
	return d.Text + "\x00" + string(d.Attributes) + "\x00" + d.Severity + "\x00" + d.Facility
}
