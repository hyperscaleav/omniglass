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
// It gates driver polls at their own interval and reachability probes at the
// node's probe interval (#1273), every tick when that is unset; a listener's
// pushes are never gated, since they ride the held session as they arrive.
type pollSchedule struct {
	heartbeat time.Duration
	// probeEvery is how often each reachability probe runs (#1273). Zero is
	// every tick: the heartbeat, as before the interval existed.
	probeEvery time.Duration
	next       map[string]time.Time
	backoff    map[string]time.Duration // the last retry wait of a failing poll
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
	// last is each task's previous actual start (#1625), what its next run's
	// gap is measured from. Node memory alone, like next.
	last map[string]time.Time
	// tick is the current tick's schedule figures (#1625), taken once the
	// tick's tasks ran; tickStart is when that tick fired, the due time of a
	// run with no booking of its own.
	tick      tickFigures
	tickStart time.Time
}

// dueRun is due's answer (#1625): whether the task runs, the time it was
// booked to run (zero for a task new to the schedule or with no interval,
// which is due on the tick), and the whole intervals it skipped past that
// time, rebooked from now rather than run as a catch-up burst.
type dueRun struct {
	run     bool
	at      time.Time
	skipped int
}

// tickFigures is one tick's account of its schedule (#1625): the worst
// lateness (a run's actual start after its due time) and the worst gap
// excess (how far a run's gap since the task's previous run strayed from
// its interval, early or late) over the tick's runs, and the whole intervals
// skipped. ran is whether any task ran, since a tick with none has no
// lateness or gap to report rather than a zero one.
type tickFigures struct {
	ran       bool
	lateness  time.Duration
	gapExcess time.Duration
	skipped   int
}

// newPollSchedule keeps the schedule for a node ticking every heartbeat,
// which is also where a failed poll's retry backoff starts.
func newPollSchedule(heartbeat time.Duration) *pollSchedule {
	return &pollSchedule{heartbeat: heartbeat, next: map[string]time.Time{}, backoff: map[string]time.Duration{}, refused: map[string]bool{}, fingerprint: map[string]string{}, occurred: map[string]map[string]string{}, last: map[string]time.Time{}}
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
// intervals runs once and books from now, never a catch-up burst, and says
// how many it skipped (#1625). A task is due on the tick within half a
// heartbeat of its due time (#1273): ticks are a heartbeat apart but judged
// after variable work, so a tick that reached the task a few milliseconds
// sooner than the one that booked it would otherwise skip it, and an
// interval equal to the heartbeat would double.
func (s *pollSchedule) due(taskID string, every time.Duration, now time.Time) dueRun {
	if every <= 0 {
		return dueRun{run: true}
	}
	at, booked := s.next[taskID]
	if booked && now.Add(s.heartbeat/2).Before(at) {
		return dueRun{}
	}
	d := dueRun{run: true}
	next := now.Add(every)
	if booked {
		d.at = at
		if late := now.Sub(at); late < every {
			next = at.Add(every)
		} else {
			d.skipped = int(late / every)
		}
	}
	s.next[taskID] = next
	return d
}

// beginTick starts a tick's schedule account (#1625): the tick fired at
// tick, the due time of every run booked on no time of its own.
func (s *pollSchedule) beginTick(tick time.Time) {
	s.tick = tickFigures{}
	s.tickStart = tick
}

// ran accounts a run due reported, started at start (#1625): its lateness
// against its due time (the tick, for a run with no booking), its gap since
// the task's previous start against its interval (the heartbeat, for a task
// with none, which runs every tick), and the intervals it skipped. A run
// early inside the half-heartbeat window is not late.
func (s *pollSchedule) ran(taskID string, every time.Duration, d dueRun, start time.Time) {
	due := d.at
	if due.IsZero() {
		due = s.tickStart
	}
	s.tick.ran = true
	if late := start.Sub(due); late > s.tick.lateness {
		s.tick.lateness = late
	}
	if every <= 0 {
		every = s.heartbeat
	}
	if prev, ok := s.last[taskID]; ok {
		excess := start.Sub(prev) - every
		if excess < 0 {
			excess = -excess
		}
		if excess > s.tick.gapExcess {
			s.tick.gapExcess = excess
		}
	}
	s.last[taskID] = start
	s.tick.skipped += d.skipped
}

// takeTick hands over the tick's schedule figures and starts the next from
// zero.
func (s *pollSchedule) takeTick() tickFigures {
	f := s.tick
	s.tick = tickFigures{}
	return f
}

// retain forgets tasks that left the worklist, so no map grows for the life
// of the run and a task that returns runs at once. Each map is swept by its
// own keys: a task can be remembered without ever being booked (a probe at
// the heartbeat, a poll with no schedule).
func (s *pollSchedule) retain(ids map[string]bool) {
	forget(s.next, ids)
	forget(s.backoff, ids)
	forget(s.refused, ids)
	forget(s.fingerprint, ids)
	forget(s.occurred, ids)
	forget(s.last, ids)
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
	clear(s.occurred)
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

// commitChanged records what a run saw, once its landing published, so a run
// whose publish failed fires its occurrences again next time. seen is the
// run's whole sample set, not only what landed: the memory is rebuilt from it,
// so an occurrence the run saw unchanged stays remembered and a slot the run
// did not see is dropped. A function whose identities rotate (#1386) holds
// one run's slots, never every identity it has heard.
func (s *pollSchedule) commitChanged(taskID string, seen []collection.Sample) {
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
