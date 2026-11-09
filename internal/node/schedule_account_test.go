package node

import (
	"testing"
	"time"
)

// #1625: due reports the time a run was due and the whole intervals it
// skipped, rather than rebooking a late task silently.
func TestPollScheduleDueReportsDueTimeAndSkippedIntervals(t *testing.T) {
	t.Parallel()
	hb := 30 * time.Second
	every := time.Minute
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	booked := t0.Add(every)
	for _, tc := range []struct {
		name    string
		now     time.Time
		run     bool
		skipped int
		next    time.Time
	}{
		{"on time", booked, true, 0, booked.Add(every)},
		{"half a heartbeat early", booked.Add(-hb / 2), true, 0, booked.Add(every)},
		{"more than half a heartbeat early", booked.Add(-hb/2 - time.Millisecond), false, 0, booked},
		{"one interval missed", booked.Add(every), true, 1, booked.Add(2 * every)},
		{"three intervals missed", booked.Add(3*every + time.Second), true, 3, booked.Add(3*every + time.Second + every)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newPollSchedule(hb)
			if first := s.due("p", every, t0); !first.run || first.skipped != 0 {
				t.Fatalf("a new task: %+v, want a run with nothing skipped", first)
			}
			got := s.due("p", every, tc.now)
			if got.run != tc.run || got.skipped != tc.skipped {
				t.Fatalf("due at %v: run %v skipped %d, want run %v skipped %d", tc.now.Sub(booked), got.run, got.skipped, tc.run, tc.skipped)
			}
			if tc.run && !got.at.Equal(booked) {
				t.Fatalf("due time %v, want the booked %v", got.at, booked)
			}
			if next, _ := s.nextOf("p"); !next.Equal(tc.next) {
				t.Fatalf("next booked %v, want %v", next.Sub(t0), tc.next.Sub(t0))
			}
		})
	}
	// A task new to the schedule, or one with no interval, is due on the
	// tick: no booked time, nothing skipped.
	s := newPollSchedule(hb)
	if d := s.due("new", every, t0); !d.run || !d.at.IsZero() || d.skipped != 0 {
		t.Fatalf("a new task: %+v, want a run with no booked time", d)
	}
	if d := s.due("every-tick", 0, t0); !d.run || !d.at.IsZero() || d.skipped != 0 {
		t.Fatalf("an unscheduled task: %+v, want a run with no booked time", d)
	}
}

// tickOf drives one heartbeat tick of a schedule over the given tasks, each
// starting at the tick, and returns the tick's figures.
func tickOf(s *pollSchedule, tick time.Time, tasks map[string]time.Duration) tickFigures {
	s.beginTick(tick)
	for id, every := range tasks {
		if d := s.due(id, every, tick); d.run {
			s.ran(id, every, d, tick)
		}
	}
	return s.takeTick()
}

// #1625: the gap accounting makes heartbeat rounding visible. A 45 s poll on
// a 30 s heartbeat runs at 30 s and 60 s gaps, 15 s off its interval every
// run; a 30 s poll on a 30 s heartbeat reads zero.
func TestScheduleGapExcessShowsHeartbeatRounding(t *testing.T) {
	t.Parallel()
	hb := 30 * time.Second
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		every time.Duration
		want  time.Duration
	}{
		{"45 s poll on a 30 s heartbeat", 45 * time.Second, 15 * time.Second},
		{"30 s poll on a 30 s heartbeat", 30 * time.Second, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newPollSchedule(hb)
			if f := tickOf(s, t0, map[string]time.Duration{"p": tc.every}); !f.ran || f.gapExcess != 0 || f.lateness != 0 {
				t.Fatalf("first run %+v, want a run with no gap to measure", f)
			}
			runs := 0
			for i := 1; i <= 12; i++ {
				f := tickOf(s, t0.Add(time.Duration(i)*hb), map[string]time.Duration{"p": tc.every})
				if !f.ran {
					continue
				}
				runs++
				if f.gapExcess != tc.want {
					t.Fatalf("tick %d: gap excess %v, want %v", i, f.gapExcess, tc.want)
				}
				if f.lateness != 0 {
					t.Fatalf("tick %d: lateness %v on a punctual tick", i, f.lateness)
				}
			}
			if runs == 0 {
				t.Fatal("the task never ran again")
			}
		})
	}
}

// #1625: lateness is the start a run actually took against its due time,
// stamped per task, so a task reached late in a slow pass reads late though
// the tick fired on time; a task with no interval is due at the tick.
func TestScheduleLatenessIsTheWorstRunOfTheTick(t *testing.T) {
	t.Parallel()
	hb := 30 * time.Second
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	s := newPollSchedule(hb)
	tickOf(s, t0, map[string]time.Duration{"a": time.Minute, "b": 0})

	tick := t0.Add(time.Minute)
	s.beginTick(tick)
	// a is reached 4 s into the pass, b (every tick) 9 s in.
	if d := s.due("a", time.Minute, tick.Add(4*time.Second)); d.run {
		s.ran("a", time.Minute, d, tick.Add(4*time.Second))
	}
	if d := s.due("b", 0, tick.Add(9*time.Second)); d.run {
		s.ran("b", 0, d, tick.Add(9*time.Second))
	}
	f := s.takeTick()
	if f.lateness != 9*time.Second {
		t.Fatalf("lateness %v, want the worst run's 9s", f.lateness)
	}
	// b's previous run was at t0, 69 s before, on a 30 s heartbeat: one
	// tick's interval apart would be 30 s.
	if f.gapExcess != 39*time.Second {
		t.Fatalf("gap excess %v, want 39s", f.gapExcess)
	}
	// The figures are the tick's: the next tick starts from zero.
	if f := s.takeTick(); f.ran || f.lateness != 0 || f.gapExcess != 0 {
		t.Fatalf("a tick with no run reads %+v", f)
	}
}

// #1625: the skipped intervals accumulate until taken, the report window's
// count.
func TestScheduleSkipsAccumulate(t *testing.T) {
	t.Parallel()
	hb := 30 * time.Second
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	s := newPollSchedule(hb)
	tickOf(s, t0, map[string]time.Duration{"p": time.Minute})
	f := tickOf(s, t0.Add(4*time.Minute), map[string]time.Duration{"p": time.Minute})
	if f.skipped != 3 {
		t.Fatalf("skipped %d, want 3 whole intervals", f.skipped)
	}
	if f.lateness != 3*time.Minute {
		t.Fatalf("lateness %v, want 3m", f.lateness)
	}
}
