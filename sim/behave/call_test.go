package behave_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
)

var (
	barParams = func() behave.CallParams {
		p := behave.DefaultCall
		p.Standalone = &behave.DefaultStandalone
		p.DialEnds = true
		return p
	}()
	relayParams = func() behave.CallParams { p := behave.DefaultCall; p.ClearIDOnEnd = true; return p }()
	quiet       = &stub{chance: false}
)

func start(hms string) int64 { return at(hms).Unix() }

func TestCallStartsTurnsAndEndsWithTheRoomsMeeting(t *testing.T) {
	cal := pinnedCalendar()
	for _, tc := range []struct {
		name      string
		hms       string
		prev      behave.CallState
		wantEvent behave.CallEvent
		wantID    string
		wantN     [2]int // participants, inclusive range
		wantOn    bool
	}{
		{"booked starts with half to all of who came", "08:30:00", behave.CallState{}, behave.EventStart, fmt.Sprintf("c%d", start("08:28:07")), [2]int{2, 3}, true},
		{"walk-in starts", "09:34:00", behave.CallState{}, behave.EventStart, fmt.Sprintf("c%d", start("09:32:41")), [2]int{1, 1}, true},
		{"back to back: the second starts after the first ended", "11:01:00", behave.CallState{ID: "c-old"}, behave.EventStart, fmt.Sprintf("c%d", start("11:00:58")), [2]int{1, 2}, true},
		{"no-show is no call", "13:50:00", behave.CallState{}, behave.EventNone, "", [2]int{0, 0}, false},
		{"a running call carries on", "08:35:00", behave.CallState{Active: true, ID: fmt.Sprintf("c%d", start("08:28:07")), Participants: 3}, behave.EventNone, fmt.Sprintf("c%d", start("08:28:07")), [2]int{3, 3}, true},
		{"the meeting ending ends the call", "09:00:30", behave.CallState{Active: true, ID: fmt.Sprintf("c%d", start("08:28:07")), Participants: 3}, behave.EventEnd, fmt.Sprintf("c%d", start("08:28:07")), [2]int{0, 0}, false},
		{"another meeting under a running call turns it", "09:34:00", behave.CallState{Active: true, ID: fmt.Sprintf("c%d", start("08:28:07")), Participants: 3}, behave.EventTurn, fmt.Sprintf("c%d", start("09:32:41")), [2]int{1, 1}, true},
		{"impaired: the call carries on beneath it", "08:45:00", behave.CallState{Active: true, ID: fmt.Sprintf("c%d", start("08:28:07")), Participants: 3}, behave.EventNone, fmt.Sprintf("c%d", start("08:28:07")), [2]int{3, 3}, true},
	} {
		for name, p := range map[string]behave.CallParams{"bar": barParams, "relay": relayParams} {
			s := tc.prev
			if name == "relay" && s.Active {
				s.ID = tc.prev.ID
			}
			got := behave.FollowsCall(s, cal.StateAt(room, at(tc.hms)), at(tc.hms), p, quiet)
			if got.Event != tc.wantEvent || got.Active != tc.wantOn || got.Participants < tc.wantN[0] || got.Participants > tc.wantN[1] {
				t.Errorf("%s/%s: event %q active %v peers %d, want %q %v %v", tc.name, name, got.Event, got.Active, got.Participants, tc.wantEvent, tc.wantOn, tc.wantN)
			}
			if tc.wantEvent == behave.EventEnd && name == "relay" && got.ID != "" {
				t.Errorf("%s/relay: id %q after the end, want it cleared", tc.name, got.ID)
			}
			if tc.wantID != "" && tc.wantEvent != behave.EventEnd && got.ID != tc.wantID {
				t.Errorf("%s/%s: id %q, want %q", tc.name, name, got.ID, tc.wantID)
			}
		}
	}
}

func TestCallPeersJoinAndLeaveWithinWhoCame(t *testing.T) {
	cal := pinnedCalendar()
	rs := cal.StateAt(room, at("10:20:00")) // booked, 6 attend
	s := behave.FollowsCall(behave.CallState{}, rs, at("10:20:00"), barParams, quiet)
	if !s.Active || s.WantN != 6 {
		t.Fatalf("call %+v, want a call for 6", s)
	}
	rng := behave.NewSource("d-peers")
	sawJoin, sawLeave := false, false
	for i := 0; i < 2000; i++ {
		tm := at("10:20:00").Add(time.Duration(i) * time.Second)
		if tm.After(at("10:50:00")) {
			break
		}
		rs = cal.StateAt(room, tm)
		s = behave.FollowsCall(s, rs, tm, barParams, rng)
		switch s.Event {
		case behave.EventJoin:
			sawJoin = true
		case behave.EventLeave:
			sawLeave = true
		case behave.EventNone:
		default:
			t.Fatalf("%s: event %q inside a steady meeting", tm.Format("15:04:05"), s.Event)
		}
		if s.Participants < 1 || s.Participants > 6 {
			t.Fatalf("%s: %d in the call, want 1 to 6", tm.Format("15:04:05"), s.Participants)
		}
	}
	if !sawJoin || !sawLeave {
		t.Fatalf("join %v leave %v over half an hour of ticks", sawJoin, sawLeave)
	}
}

func TestCallDialedAndHungUpAreNotRejoined(t *testing.T) {
	cal := pinnedCalendar()
	rs := cal.StateAt(room, at("10:20:00"))
	meeting := fmt.Sprintf("c%d", start("10:03:55"))

	// a dialed call: the meeting does not start one over it, and the dial lasts its length
	dialed := behave.CallState{Dialed: true, DialLength: 3, Active: true, ID: "c-dial", Participants: 4, Cal: true}
	s := behave.FollowsCall(dialed, rs, at("10:20:00"), barParams, quiet)
	if s.Event == behave.EventTurn || s.Skip != start("10:03:55") {
		t.Fatalf("dialed call: event %q skip %d, want no turn and the meeting skipped", s.Event, s.Skip)
	}
	for i := 0; i < 3 && s.Active; i++ {
		s = behave.FollowsCall(s, rs, at("10:20:00"), barParams, quiet)
	}
	if s.Active || s.Dialed {
		t.Fatalf("the dial outlived its length: %+v", s)
	}
	// the skipped meeting stays skipped after the dial ends
	if s = behave.FollowsCall(s, rs, at("10:21:00"), barParams, quiet); s.Event != behave.EventNone || s.Want != "" {
		t.Fatalf("rejoined a skipped meeting: event %q want %q", s.Event, s.Want)
	}

	// a hang-up skips the running meeting the same way
	hung := behave.FollowsCall(behave.CallState{Hungup: true, Cal: true}, rs, at("10:20:00"), barParams, quiet)
	if hung.Event != behave.EventNone || hung.Hungup || hung.Want != "" {
		t.Fatalf("hung up: %+v, want the meeting skipped and the hang-up consumed", hung)
	}
	// a meeting that is not the skipped one starts as usual
	next := behave.FollowsCall(hung, cal.StateAt(room, at("12:35:00")), at("12:35:00"), barParams, quiet)
	if next.Event != behave.EventStart || next.ID == meeting {
		t.Fatalf("the next meeting: event %q id %q", next.Event, next.ID)
	}
}

func TestCallWithNoCalendarFollowsItsOwnScheduleOrNone(t *testing.T) {
	// a bar keeps its own day: nothing running, it plans; booked, it starts when its slot comes
	now := at("10:07:00")
	s := behave.FollowsCall(behave.CallState{}, noCalendar, now, barParams, quiet)
	if s.Active || s.Cal {
		t.Fatalf("a bar with nothing planned started a call: %+v", s)
	}
	planner := &stub{chance: true, frac: 0.5} // books, a middle-length slot
	s = behave.PlanOwnSchedule(s, now, behave.DefaultStandalone, planner)
	if !s.Plan || !s.Booked || s.StartAt == 0 || s.EndAt <= s.StartAt || s.Slot != 15 && s.Slot != 30 && s.Slot != 60 {
		t.Fatalf("a free bar did not book its slot: %+v", s)
	}
	if s.Boundary%900 != 0 && s.Boundary%1800 != 0 {
		t.Fatalf("boundary %d is not on a slot grid", s.Boundary)
	}
	started := behave.FollowsCall(s, noCalendar, time.Unix(s.StartAt, 0), barParams, quiet)
	if started.Event != behave.EventStart || !started.Active || started.ID != "c9001" || started.Participants != 2 {
		t.Fatalf("at its start: %+v, want its own call c9001 of 2", started)
	}
	ended := behave.FollowsCall(started, noCalendar, time.Unix(s.EndAt, 0), barParams, quiet)
	if ended.Event != behave.EventEnd || ended.Active || ended.Participants != 0 {
		t.Fatalf("at its end: %+v", ended)
	}

	// a device that reads the calendar alone does nothing without one, and never draws
	r := behave.FollowsCall(behave.CallState{}, noCalendar, now, relayParams, &stub{chance: false})
	if r.Event != behave.EventNone || r.Active {
		t.Fatalf("a relay with no calendar called: %+v", r)
	}
}

func TestCallLatchesTheCalendarOnceSeen(t *testing.T) {
	cal := pinnedCalendar()
	s := behave.FollowsCall(behave.CallState{}, cal.StateAt(room, at("09:10:00")), at("09:10:00"), barParams, quiet)
	if !s.Cal {
		t.Fatal("the bar did not latch a calendar it saw")
	}
	s = behave.FollowsCall(s, noCalendar, at("09:11:00"), barParams, quiet)
	if !s.Cal {
		t.Fatal("the bar let go of the calendar when it went away")
	}
}

func TestNextSlotIsTheNextWallClockBoundary(t *testing.T) {
	for _, tc := range []struct {
		t       int64
		minutes int
		want    int64
	}{{0, 30, 1800}, {1799, 30, 1800}, {1800, 30, 3600}, {800, 15, 900}} {
		if got := behave.NextSlot(tc.t, tc.minutes); got != tc.want {
			t.Errorf("NextSlot(%d, %d) = %d, want %d", tc.t, tc.minutes, got, tc.want)
		}
	}
}

var _ = calendar.PhaseIdle
