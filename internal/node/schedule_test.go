package node

import (
	"slices"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// #1634: a failed poll retries on a backoff of its own: a fixed base,
// doubling on each consecutive failure and capped at the poll's interval, so
// a 24h poll that fails during a reboot retries in a second, not a day. The
// schedule has no heartbeat to read: the node's tick no longer sets any
// task's cadence.
func TestABackoffStartsAtItsBaseDoublesAndCapsAtTheInterval(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	waits := func(every time.Duration, n int) []time.Duration {
		s := newPollSchedule()
		now := t0
		var out []time.Duration
		for i := 0; i < n; i++ {
			s.failed("p", every, now)
			next, ok := s.nextOf("p")
			if !ok {
				t.Fatalf("failure %d booked no retry", i)
			}
			out = append(out, next.Sub(now))
			now = next
		}
		return out
	}
	day := 24 * time.Hour
	got := waits(day, 6)
	want := []time.Duration{backoffBase, 2 * backoffBase, 4 * backoffBase, 8 * backoffBase, 16 * backoffBase, 32 * backoffBase}
	if !slices.Equal(got, want) {
		t.Fatalf("a failing 24h poll retried after %v, want %v", got, want)
	}
	if backoffBase != time.Second {
		t.Fatalf("the backoff base is %v, want 1s", backoffBase)
	}
	if got := waits(4*time.Second, 5); !slices.Equal(got, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second, 4 * time.Second}) {
		t.Fatalf("a failing 4s poll retried after %v, want the backoff capped at its interval", got)
	}
	// A success ends the streak: the next failure starts at the base again.
	s := newPollSchedule()
	s.failed("p", day, t0)
	s.failed("p", day, t0)
	s.succeeded("p")
	s.failed("p", day, t0)
	if next, _ := s.nextOf("p"); next.Sub(t0) != backoffBase {
		t.Fatalf("after a success a failure waits %v, want the base", next.Sub(t0))
	}
	// A Retry-After floors the wait, never past the interval.
	s.failedAtLeast("q", time.Minute, t0, 30*time.Second)
	if next, _ := s.nextOf("q"); next.Sub(t0) != 30*time.Second {
		t.Fatalf("a Retry-After of 30s retried after %v", next.Sub(t0))
	}
	s.failedAtLeast("r", time.Minute, t0, time.Hour)
	if next, _ := s.nextOf("r"); next.Sub(t0) != time.Minute {
		t.Fatalf("a Retry-After past the interval retried after %v, want the interval", next.Sub(t0))
	}
}

// A poll whose endpoint's connection fingerprint changes (an address, port or
// secret edited in place, #1061) is due at once: rebook reports the move and
// drops the booking and the failure backoff built up against inputs that are
// gone. An unchanged or first-seen fingerprint keeps them.
func TestPollScheduleRunsAtOnceWhenTheFingerprintMoves(t *testing.T) {
	t.Parallel()
	s := newPollSchedule()
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	every := time.Minute
	if s.rebook("p1", "fp-wrong-port") {
		t.Fatal("a first-seen fingerprint read as moved")
	}
	for i := 0; i < 5; i++ {
		s.failed("p1", every, now)
	}
	if s.rebook("p1", "fp-wrong-port") {
		t.Fatal("an unchanged fingerprint read as moved")
	}
	if _, ok := s.nextOf("p1"); !ok {
		t.Fatal("an unchanged fingerprint released the backoff")
	}
	if !s.rebook("p1", "fp-fixed-port") {
		t.Fatal("a moved fingerprint did not read as moved")
	}
	if _, ok := s.nextOf("p1"); ok {
		t.Fatal("a moved fingerprint kept the old booking")
	}
	s.failed("p1", every, now)
	if next, _ := s.nextOf("p1"); next.Sub(now) != backoffBase {
		t.Fatalf("a moved fingerprint kept the old backoff: the next failure waits %v", next.Sub(now))
	}
}

// A refused poll (#1049) is released once its session reads up again, and
// only then; retain forgets every task that left the worklist.
func TestARefusedPollIsReleasedWhenItsSessionIsUp(t *testing.T) {
	t.Parallel()
	s := newPollSchedule()
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	s.book("p", now.Add(time.Minute))
	s.refusedRun("p")
	if got := s.refusedIDs(); len(got) != 1 || got[0] != "p" {
		t.Fatalf("refused ids %v, want [p]", got)
	}
	if s.releaseRefused("p", false) {
		t.Fatal("a refused poll was released while its session is down")
	}
	if !s.releaseRefused("p", true) {
		t.Fatal("a refused poll was not released when its session read up")
	}
	if s.releaseRefused("p", true) {
		t.Fatal("a released poll was released twice")
	}
	s.book("gone", now)
	s.retain(map[string]bool{"p": true})
	if _, ok := s.nextOf("gone"); ok {
		t.Fatal("a task that left the worklist kept its booking")
	}
}

// TestForgetOccurrencesLandsThemAgain: after a spool overflow the run loop
// forgets what each poll landed, so an unchanged event or log line lands
// again rather than staying lost with the batch the spool dropped.
func TestForgetOccurrencesLandsThemAgain(t *testing.T) {
	t.Parallel()
	s := newPollSchedule()
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
