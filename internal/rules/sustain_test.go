package rules

import (
	"testing"
	"time"
)

var t0 = time.Unix(1_800_000_000, 0)

func at(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }

// With no sustain an action takes effect at once.
func TestStepWithoutSustain(t *testing.T) {
	next, effect := Step(Streak{}, Fire, at(0), 0, 0)
	if effect != Fire || next.Pending != Hold {
		t.Fatalf("got %v / %+v, want Fire now and no streak", effect, next)
	}
}

// A fire must hold for its sustain: the first sample starts the streak,
// a later one past the sustain fires, and anything else in between breaks it.
func TestStepFireSustain(t *testing.T) {
	s, e := Step(Streak{}, Fire, at(0), 30*time.Second, 0)
	if e != Hold || s.Pending != Fire || !s.Since.Equal(at(0)) {
		t.Fatalf("first fire sample: %v / %+v, want a fire streak since 0", e, s)
	}
	s, e = Step(s, Fire, at(10), 30*time.Second, 0)
	if e != Hold || !s.Since.Equal(at(0)) {
		t.Fatalf("10s in: %v / %+v, want the streak held from 0", e, s)
	}
	if _, e = Step(s, Fire, at(30), 30*time.Second, 0); e != Fire {
		t.Fatalf("30s in: %v, want Fire", e)
	}
	broken, e := Step(s, Hold, at(20), 30*time.Second, 0)
	if e != Hold || broken.Pending != Hold {
		t.Fatalf("a hold mid-streak: %v / %+v, want the streak broken", e, broken)
	}
	flipped, e := Step(s, Clear, at(20), 30*time.Second, 0)
	if e != Clear || flipped.Pending != Hold {
		t.Fatalf("a clear mid fire streak with no clear sustain: %v / %+v, want Clear now", e, flipped)
	}
}

// Due reports whether a pending streak has held for its sustain at a time,
// which is what the sweep asks of a streak no further sample arrives for.
func TestStreakDue(t *testing.T) {
	s := Streak{Pending: Clear, Since: at(0)}
	if s.Due(at(9), 0, 10*time.Second) {
		t.Error("a clear streak 9s old with a 10s sustain read due")
	}
	if !s.Due(at(10), 0, 10*time.Second) {
		t.Error("a clear streak 10s old with a 10s sustain did not read due")
	}
	if (Streak{}).Due(at(100), 0, 0) {
		t.Error("no streak read due")
	}
}

// Step is timed by the server's receipt clock: a continuing action keeps the
// streak's start, and a start that reads later than now (the server clock
// stepped back) restarts at now rather than holding a start in the future
// that would stretch the sustain.
func TestStepTimedByReceipt(t *testing.T) {
	s, _ := Step(Streak{Pending: Fire, Since: at(0)}, Fire, at(5), 30*time.Second, 0)
	if !s.Since.Equal(at(0)) {
		t.Fatalf("a continuing fire moved its start to %v, want %v", s.Since, at(0))
	}
	s, _ = Step(Streak{Pending: Fire, Since: at(60)}, Fire, at(5), 30*time.Second, 0)
	if !s.Since.Equal(at(5)) {
		t.Fatalf("a start ahead of now held at %v, want restarted at %v", s.Since, at(5))
	}
}
