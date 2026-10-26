package node

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// #822: the node honors a driver poll's declared schedule. A 60s poll on a
// 1s heartbeat runs once per interval, a task new to the worklist runs at
// once, and a task that leaves the worklist is forgotten.
func TestPollScheduleRunsOncePerInterval(t *testing.T) {
	s := newPollSchedule(time.Second)
	t0 := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	var runs []int
	for i := 0; i < 180; i++ {
		if s.due("p60", time.Minute, t0.Add(time.Duration(i)*time.Second)) {
			runs = append(runs, i)
		}
	}
	if len(runs) != 3 || runs[0] != 0 || runs[1] != 60 || runs[2] != 120 {
		t.Fatalf("a 60s poll on a 1s tick ran at seconds %v, want 0, 60, 120", runs)
	}

	// A task that appears mid-run runs at its first tick.
	if !s.due("new", time.Minute, t0.Add(30*time.Second)) {
		t.Fatal("a new task waited for its interval instead of running at once")
	}
	// No declared interval: every tick, as before.
	for i := 0; i < 3; i++ {
		if !s.due("unscheduled", 0, t0.Add(time.Duration(i)*time.Second)) {
			t.Fatal("a task with no interval was gated")
		}
	}
	// A slow tick never bunches: one run however late, then the interval.
	late := newPollSchedule(time.Second)
	late.due("p", time.Minute, t0)
	if !late.due("p", time.Minute, t0.Add(150*time.Second)) || late.due("p", time.Minute, t0.Add(151*time.Second)) {
		t.Fatal("a late tick ran a catch-up burst")
	}

	s.retain(map[string]bool{"new": true})
	if !s.due("p60", time.Minute, t0.Add(130*time.Second)) {
		t.Fatal("a task that left the worklist kept its schedule")
	}
}

// Review of #822: a failed poll retries on a backoff that starts at the
// heartbeat, doubles, and caps at the poll's interval, instead of waiting a
// whole interval (a 24h poll that fails during a reboot must not wait 24h);
// a success books from the due time again.
func TestPollScheduleRetriesAFailureOnABackoff(t *testing.T) {
	hb := time.Second
	s := newPollSchedule(hb)
	t0 := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	every := 24 * time.Hour
	if !s.due("p", every, t0) {
		t.Fatal("a new poll did not run at once")
	}
	s.failed("p", every, t0)
	var retries []time.Duration
	now := t0
	for i := 0; i < 400000 && len(retries) < 20; i++ {
		now = now.Add(hb)
		if s.due("p", every, now) {
			retries = append(retries, now.Sub(t0))
			s.failed("p", every, now)
		}
	}
	if len(retries) == 0 || retries[0] > 2*hb {
		t.Fatalf("the first retry came at %v, want about one heartbeat after the failure", retries)
	}
	for i := 2; i < len(retries); i++ {
		gap, prev := retries[i]-retries[i-1], retries[i-1]-retries[i-2]
		if gap < prev {
			t.Fatalf("retry gaps shrink: %v", retries)
		}
		if gap > every {
			t.Fatalf("a retry gap %v passed the poll's interval", gap)
		}
	}
	// The backoff caps at the interval: a short poll's retries never wait
	// longer than the poll itself.
	short := newPollSchedule(hb)
	short.due("q", 4*time.Second, t0)
	at := t0
	for i := 0; i < 6; i++ {
		short.failed("q", 4*time.Second, at)
		next := at
		for !short.due("q", 4*time.Second, next) {
			next = next.Add(hb)
		}
		if gap := next.Sub(at); gap > 4*time.Second {
			t.Fatalf("retry %d waited %v, past the 4s interval", i, gap)
		}
		at = next
	}
	// A success books from the due time, and the backoff resets.
	s.succeeded("p")
	at, _ = s.nextOf("p")
	if !s.due("p", every, at) {
		t.Fatal("not due at its booked time")
	}
	if next, _ := s.nextOf("p"); next != at.Add(every) {
		t.Fatalf("after a success the next run is %v, want the interval from the due time", next.Sub(at))
	}
}

// Review of #822: the cadence books from the due time, not from the tick
// that ran it, so a tick that lands late does not push every later run back.
func TestPollScheduleDoesNotDrift(t *testing.T) {
	s := newPollSchedule(time.Second)
	t0 := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	s.due("p", time.Minute, t0)
	if !s.due("p", time.Minute, t0.Add(60*time.Second+700*time.Millisecond)) {
		t.Fatal("a slightly late tick did not run the poll")
	}
	if !s.due("p", time.Minute, t0.Add(120*time.Second)) {
		t.Fatal("the run after a late tick drifted: it is not due at t0+120s")
	}
}

// A poll whose endpoint's connection fingerprint changes (an address, port or
// secret edited in place, #1061) runs at the next tick: the failure backoff
// it built up against the old inputs, and its booking, belong to inputs that
// are gone.
func TestPollScheduleRunsAtOnceWhenTheFingerprintMoves(t *testing.T) {
	s := newPollSchedule(2 * time.Second)
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	every := time.Minute
	s.rebook("p1", "fp-wrong-port")
	for i := 0; i < 5; i++ {
		if s.due("p1", every, now) {
			s.failed("p1", every, now)
		}
		now = now.Add(2 * time.Second)
	}
	if s.due("p1", every, now) {
		t.Fatal("a failing poll ran inside its backoff")
	}
	s.rebook("p1", "fp-wrong-port")
	if s.due("p1", every, now) {
		t.Fatal("an unchanged fingerprint released the backoff")
	}
	s.rebook("p1", "fp-fixed-port")
	if !s.due("p1", every, now) {
		t.Fatal("a moved fingerprint did not run the poll at the next tick")
	}
}

// TestForgetOccurrencesLandsThemAgain: after a spool overflow the run loop
// forgets what each poll landed, so an unchanged event or log line lands
// again rather than staying lost with the batch the spool dropped.
func TestForgetOccurrencesLandsThemAgain(t *testing.T) {
	s := newPollSchedule(time.Second)
	ev := []collection.Sample{{Lane: driver.LaneEvent, Name: "input-changed", Text: "hdmi-2", Emit: 0}}
	s.commitChanged("t-1", ev)
	if got := s.changed("t-1", ev); len(got) != 0 {
		t.Fatalf("an unchanged event passed the change memory: %+v", got)
	}
	s.forgetOccurrences()
	if got := s.changed("t-1", ev); len(got) != 1 {
		t.Fatalf("after forgetting, the event did not land again: %+v", got)
	}
}
