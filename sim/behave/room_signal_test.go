package behave_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim/behave"
)

// The touch panel, the switcher and the extender follow the room's meetings
// (#2833). Each table reads the pinned room day (see pinned_test.go).

func TestPanelWakesAndChangesPageWithTheRoom(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hms   string
		awake bool
		page  string
	}{
		{"before the day", "07:30:00", false, "idle"},
		{"in the wake lead", "08:27:00", true, "idle"},
		{"in the meeting", "08:30:00", true, "in-call"},
		{"sharing", "08:40:00", true, "sharing"},
		{"back from sharing", "08:50:00", true, "in-call"},
		{"cooling down, awake with no call", "09:03:00", true, "idle"},
		{"asleep between meetings", "09:10:00", false, "idle"},
		{"a walk-in", "09:35:00", true, "in-call"},
		{"a no-show holds the slot, nobody wakes it", "13:50:00", false, "idle"},
	} {
		s := behave.PanelFollowsRoom(behave.PanelState{Page: "idle"}, roomAt(tc.hms), behave.DefaultPanel)
		if s.Awake != tc.awake || s.Page != tc.page {
			t.Errorf("%s (%s): awake %v page %q, want %v and %q", tc.name, tc.hms, s.Awake, s.Page, tc.awake, tc.page)
		}
	}
}

func TestPanelInARoomWithNoCalendarIsLeftAsItWas(t *testing.T) {
	prev := behave.PanelState{Awake: true, Page: "sharing"}
	if got := behave.PanelFollowsRoom(prev, noCalendar, behave.DefaultPanel); got != prev {
		t.Fatalf("got %+v, want %+v", got, prev)
	}
}

func TestSwitcherRoutesWhatTheMeetingAsksForOnTheEdgeOnly(t *testing.T) {
	signals := map[string]string{"hdmi1": "present", "hdmi2": "present", "hdmi3": "present", "dp1": "absent"}
	// The meeting's wanted input steps: the codec's feed in a call, the
	// lectern while content is shared, the empty input between meetings.
	s := behave.RouteState{Input: "hdmi1", Signal: "present"}
	run := func(hms string) behave.RouteState {
		s = behave.RouteFollowsRoom(s, roomAt(hms), signals, behave.DefaultRoute)
		return s
	}
	for _, tc := range []struct {
		hms           string
		input, signal string
	}{
		{"07:00:00", "dp1", "absent"},
		{"07:00:30", "dp1", "absent"},
		{"08:28:30", "hdmi2", "present"},
		{"08:39:00", "hdmi1", "present"},
		{"08:49:30", "hdmi2", "present"},
		{"09:00:30", "dp1", "absent"},
	} {
		if got := run(tc.hms); got.Input != tc.input || got.Signal != tc.signal {
			t.Errorf("%s: input %q signal %q, want %q and %q", tc.hms, got.Input, got.Signal, tc.input, tc.signal)
		}
	}
	// An operator routes inside a meeting: it holds until the meeting asks
	// for another input.
	s = behave.RouteState{Input: "hdmi2", Signal: "present", Want: "hdmi2", Prev: "hdmi2"}
	s.Input = "hdmi3"
	if got := run("08:31:00"); got.Input != "hdmi3" || got.Signal != "present" {
		t.Errorf("an operator's route inside a meeting: %+v, want it held", got)
	}
	if got := run("08:39:00"); got.Input != "hdmi1" {
		t.Errorf("the meeting asks for the lectern: %+v", got)
	}
}

func TestSwitcherInARoomWithNoCalendarKeepsItsRouteAndSignal(t *testing.T) {
	prev := behave.RouteState{Input: "hdmi3", Signal: "absent", Want: "", Prev: ""}
	got := behave.RouteFollowsRoom(prev, noCalendar, map[string]string{"hdmi3": "present"}, behave.DefaultRoute)
	if got != prev {
		t.Fatalf("got %+v, want %+v", got, prev)
	}
}

func TestSignalFollowsTheRoutedInput(t *testing.T) {
	signals := map[string]string{"hdmi1": "present", "dp1": "absent"}
	if got := behave.SignalOnInput("present", "dp1", signals); got != "absent" {
		t.Errorf("an unplugged input reads %q", got)
	}
	if got := behave.SignalOnInput("absent", "hdmi1", signals); got != "present" {
		t.Errorf("a plugged input reads %q", got)
	}
	if got := behave.SignalOnInput("present", "hdmi9", signals); got != "present" {
		t.Errorf("an input the switcher has leaves the reading as it was, got %q", got)
	}
}

func TestExtenderCarriesASignalWhileTheMeetingRuns(t *testing.T) {
	for hms, want := range map[string]string{
		"07:30:00": "absent", "08:30:00": "present", "09:10:00": "absent", "09:35:00": "present", "13:50:00": "absent",
	} {
		if got := behave.ExtenderSignal("present", roomAt(hms), behave.DefaultExtender); got != want {
			t.Errorf("%s: %q, want %q", hms, got, want)
		}
	}
	if got := behave.ExtenderSignal("absent", noCalendar, behave.DefaultExtender); got != "absent" {
		t.Errorf("no calendar changes the signal to %q", got)
	}
}
