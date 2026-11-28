package behave_test

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// stepPower runs the device through the day from standby, every 30 seconds
// like its profile, and returns what it read at each instant it is asked about.
func stepPower(t *testing.T, until string, mid func(hms string, s *behave.PowerState)) map[string]behave.PowerState {
	t.Helper()
	cal := pinnedCalendar()
	s := behave.PowerState{Power: "standby"}
	out := map[string]behave.PowerState{}
	for tm := day.Add(7 * time.Hour); !tm.After(at(until)); tm = tm.Add(30 * time.Second) {
		s, _ = behave.PowerFollowsRoom(s, cal.StateAt(room, tm), tm, behave.DefaultPower)
		hms := tm.Format("15:04:05")
		if mid != nil {
			mid(hms, &s)
		}
		out[hms] = s
	}
	return out
}

func TestPowerFollowsTheRoomOverThePinnedDay(t *testing.T) {
	for _, tc := range []struct {
		name string
		hms  string
		want string
	}{
		{"before the day, asleep", "07:30:00", "standby"},
		{"booked: awake in the wake lead", "08:27:00", "on"},
		{"booked: awake in the meeting", "08:30:00", "on"},
		{"sharing: awake", "08:40:00", "on"},
		{"impaired: still reads the meeting beneath", "08:45:00", "on"},
		{"cooldown: still awake", "09:03:00", "on"},
		{"idle between meetings: asleep", "09:10:00", "standby"},
		{"walk-in: no lead, still asleep a minute before", "09:31:30", "standby"},
		{"walk-in: awake at its start", "09:33:00", "on"},
		{"back to back: awake through the gap", "10:59:00", "on"},
		{"back to back: awake in the second", "11:05:00", "on"},
		{"asleep after the cooldown", "11:21:00", "standby"},
		{"booked, short: awake", "12:50:00", "on"},
		{"no-show: nothing wakes it", "13:50:00", "standby"},
	} {
		got := stepPower(t, tc.hms, nil)[tc.hms].Power
		if got != tc.want {
			t.Errorf("%s (%s): power %q, want %q", tc.name, tc.hms, got, tc.want)
		}
	}
}

func TestPowerWakesOnTheEdgeAndReportsIt(t *testing.T) {
	cal := pinnedCalendar()
	for _, tc := range []struct {
		name     string
		hms      string
		prev     behave.PowerState
		wantPow  string
		wantWoke bool
	}{
		{"booked wakes in its lead", "08:26:10", behave.PowerState{Power: "standby", Known: true}, "on", true},
		{"already awake: no second edge", "08:30:00", behave.PowerState{Power: "on", Busy: true, Awake: true, Known: true}, "on", false},
		{"an operator's change mid-meeting holds", "08:30:00", behave.PowerState{Power: "off", Busy: true, Awake: true, Known: true}, "off", false},
		{"back to back: the cooldown was awake, no edge", "11:01:00", behave.PowerState{Power: "off", Awake: true, Known: true, Until: at("11:02:00").Unix()}, "off", false},
		{"no-show wakes nothing", "13:50:00", behave.PowerState{Power: "standby", Known: true}, "standby", false},
		{"an operator's power on in a no-show holds until its sleep", "13:50:00", behave.PowerState{Power: "on", Known: true}, "standby", false},
	} {
		tm := at(tc.hms)
		got, woke := behave.PowerFollowsRoom(tc.prev, cal.StateAt(room, tm), tm, behave.DefaultPower)
		if got.Power != tc.wantPow || woke != tc.wantWoke {
			t.Errorf("%s: power %q woke %v, want %q %v", tc.name, got.Power, woke, tc.wantPow, tc.wantWoke)
		}
	}
}

func TestPowerHoldsAnOperatorsChangeAcrossAMeeting(t *testing.T) {
	// the operator turns it off at 08:30 and it stays off to the end of the call
	got := stepPower(t, "08:55:00", func(hms string, s *behave.PowerState) {
		if hms == "08:30:00" {
			s.Power = "off"
		}
	})
	if p := got["08:50:00"].Power; p != "off" {
		t.Fatalf("power %q after the operator's off mid-meeting, want off held", p)
	}
}

func TestPowerWithNoCalendarIsLeftAlone(t *testing.T) {
	prev := behave.PowerState{Power: "off", Until: 0}
	got, woke := behave.PowerFollowsRoom(prev, noCalendar, at("10:00:00"), behave.DefaultPower)
	if got.Power != "off" || woke || got.Busy || got.Known {
		t.Fatalf("no calendar moved the device: %+v woke %v", got, woke)
	}
}

func TestInputFollowsTheMeetingOnTheWakeEdgeOnly(t *testing.T) {
	for _, tc := range []struct {
		name, prev string
		woke       bool
		want       string
	}{
		{"wake selects conferencing", "usbc", true, "hdmi1"},
		{"no wake keeps the operator's input", "usbc", false, "usbc"},
	} {
		if got := behave.InputFollowsMeeting(tc.prev, tc.woke, behave.DefaultPower); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRoomPowerRunsTheMacroOnTheEdgeOnly(t *testing.T) {
	cal := pinnedCalendar()
	s := behave.ControlState{RoomPower: "standby", Macro: "none"}
	var starts, ends int
	for tm := day.Add(7 * time.Hour); tm.Before(at("14:30:00")); tm = tm.Add(30 * time.Second) {
		rs := cal.StateAt(room, tm)
		next := behave.RoomPower(s, rs, tm, behave.DefaultMacros)
		want := "standby"
		if rs.InUse {
			want = "on"
		}
		if next.RoomPower != want {
			t.Fatalf("%s: room power %q, the room says %q", tm.Format("15:04:05"), next.RoomPower, want)
		}
		if next.Macro != s.Macro {
			if next.MacroAt != tm.Unix() {
				t.Fatalf("%s: macro ran at %d", tm.Format("15:04:05"), next.MacroAt)
			}
			switch next.Macro {
			case "start-meeting":
				starts++
			case "end-meeting":
				ends++
			}
		}
		s = next
	}
	// five runs the room is in use in (the back-to-back pair is one), in order
	if starts != 5 || ends != 5 {
		t.Fatalf("%d starts, %d ends, want 5 and 5", starts, ends)
	}
	if got := behave.RoomPower(behave.ControlState{RoomPower: "on", Macro: "x", MacroAt: 7}, noCalendar, at("10:00:00"), behave.DefaultMacros); got.RoomPower != "on" || got.Macro != "x" || got.MacroAt != 7 {
		t.Fatalf("no calendar moved the controller: %+v", got)
	}
}

var _ = calendar.PhaseIdle
