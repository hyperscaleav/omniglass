package behave_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim/behave"
)

func TestContentSharingFollowsTheRoomsStretchAndHoldsAnOperatorsChange(t *testing.T) {
	call := behave.CallSignals{Active: true}
	for _, tc := range []struct {
		name        string
		hms         string
		prev        behave.ShareState
		call        behave.CallSignals
		want, wantP bool
	}{
		{"before the stretch: not sharing", "08:35:00", behave.ShareState{}, call, false, false},
		{"the stretch opens: sharing", "08:39:00", behave.ShareState{}, call, true, true},
		{"mid stretch: sharing carries on", "08:45:00", behave.ShareState{Sharing: true, Prev: true}, call, true, true},
		{"an operator's stop holds mid stretch", "08:45:00", behave.ShareState{Sharing: false, Prev: true}, call, false, true},
		{"the stretch closes: stops", "08:50:00", behave.ShareState{Sharing: true, Prev: true}, call, false, false},
		{"an operator's share holds until the stretch next changes", "08:35:00", behave.ShareState{Sharing: true, Prev: false}, call, true, false},
		{"a meeting too short to share never does", "12:50:00", behave.ShareState{}, call, false, false},
		{"impaired: still reads the stretch beneath", "08:45:00", behave.ShareState{Sharing: true, Prev: true}, call, true, true},
		{"no call: off", "08:45:00", behave.ShareState{Sharing: true, Prev: true}, behave.CallSignals{}, false, true},
	} {
		got := behave.ContentSharing(tc.prev, roomAt(tc.hms), tc.call, behave.DefaultShare, never{t})
		if got.Sharing != tc.want || got.Prev != tc.wantP {
			t.Errorf("%s: sharing %v prev %v, want %v %v", tc.name, got.Sharing, got.Prev, tc.want, tc.wantP)
		}
	}
}

func TestContentSharingWithNoCalendarOrADialedCallDrawsItsOwn(t *testing.T) {
	flip, stay := &stub{chance: true}, &stub{chance: false}
	call := behave.CallSignals{Active: true}
	if got := behave.ContentSharing(behave.ShareState{}, noCalendar, call, behave.DefaultShare, flip); !got.Sharing {
		t.Error("a drawn flip did not share")
	}
	if got := behave.ContentSharing(behave.ShareState{Sharing: true}, noCalendar, call, behave.DefaultShare, stay); !got.Sharing {
		t.Error("no flip dropped sharing")
	}
	if got := behave.ContentSharing(behave.ShareState{Prev: true}, roomAt("08:45:00"), behave.CallSignals{Active: true, Dialed: true}, behave.DefaultShare, flip); !got.Sharing {
		t.Error("a dialed call in a calendar room did not draw its own")
	}
	// the calendar's reading is still remembered through a dialed call
	if got := behave.ContentSharing(behave.ShareState{}, roomAt("08:45:00"), behave.CallSignals{Active: true, Dialed: true}, behave.DefaultShare, stay); !got.Prev {
		t.Error("the calendar's sharing was not read")
	}
}

func TestContentSharingHoldsIdleOnlyWhereTheDeviceHoldsIt(t *testing.T) {
	idle := behave.CallSignals{}
	hold := behave.ShareParams{ToggleChance: 0.05, HoldIdle: true}
	if got := behave.ContentSharing(behave.ShareState{Sharing: true}, noCalendar, idle, hold, never{t}); !got.Sharing {
		t.Error("a holding device dropped sharing outside a call")
	}
	if got := behave.ContentSharing(behave.ShareState{Sharing: true}, noCalendar, behave.CallSignals{Ended: true}, hold, never{t}); got.Sharing {
		t.Error("a holding device kept sharing the tick its call ended")
	}
}

func TestContentSourceKeepsOrDrawsOnce(t *testing.T) {
	sources := []string{"hdmi1", "hdmi2", "usbc", "wireless"}
	if got := behave.ContentSource("usbc", false, sources, never{t}); got != "" {
		t.Errorf("not sharing: %q, want none", got)
	}
	if got := behave.ContentSource("usbc", true, sources, never{t}); got != "usbc" {
		t.Errorf("sharing on usbc: %q", got)
	}
	if got := behave.ContentSource("", true, sources, &stub{}); got != "hdmi1" {
		t.Errorf("first draw: %q, want hdmi1", got)
	}
	if got := behave.ContentSource("", true, sources, &stub{pickHi: true}); got != "wireless" {
		t.Errorf("last draw: %q, want wireless", got)
	}
}

func TestPeopleCountReadsTheCalendarsAttendees(t *testing.T) {
	for _, tc := range []struct {
		name string
		hms  string
		rng  behave.Source
		want int
		ev   string
	}{
		{"booked: who came", "08:30:00", &stub{chance: false}, 3, "increased"},
		{"booked: one stepped out", "08:30:00", &stub{chance: true}, 2, "increased"},
		{"walk-in of one: nobody to miss, no draw", "09:34:00", never{t}, 1, "increased"},
		{"back to back: the second's two", "11:02:00", &stub{chance: false}, 2, "increased"},
		{"impaired: the room is still counted", "08:45:00", &stub{chance: false}, 3, "increased"},
		{"between meetings: empty, no draw", "09:20:00", never{t}, 0, "steady"},
		{"no-show: empty, no draw", "13:50:00", never{t}, 0, "steady"},
	} {
		got := behave.PeopleCount(behave.OccupancyState{}, roomAt(tc.hms), 0, behave.DefaultOccupancy, tc.rng)
		if got.Count != tc.want || got.LastEvent != tc.ev || got.Vacant != (tc.want == 0) {
			t.Errorf("%s: %+v, want %d %s", tc.name, got, tc.want, tc.ev)
		}
	}
	if got := behave.PeopleCount(behave.OccupancyState{Count: 5}, roomAt("09:20:00"), 0, behave.DefaultOccupancy, never{t}); got.LastEvent != "decreased" || got.Prev != 5 {
		t.Errorf("a room that emptied: %+v", got)
	}
}

func TestPeopleCountWithNoCalendarWalksOrCountsTheCall(t *testing.T) {
	mid := &stub{frac: 0.5} // a step of zero
	if got := behave.PeopleCount(behave.OccupancyState{Count: 6}, noCalendar, 0, behave.DefaultOccupancy, mid); got.Count != 6 {
		t.Errorf("a still walk moved to %d", got.Count)
	}
	if got := behave.PeopleCount(behave.OccupancyState{Count: 13}, noCalendar, 0, behave.DefaultOccupancy, &stub{frac: 1}); got.Count != 14 {
		t.Errorf("the walk went to %d, want 14 (the cap)", got.Count)
	}
	if got := behave.PeopleCount(behave.OccupancyState{Count: 1}, noCalendar, 0, behave.DefaultOccupancy, &stub{frac: 0}); got.Count != 0 {
		t.Errorf("the walk went to %d, want 0 (the floor)", got.Count)
	}
	if got := behave.PeopleCount(behave.OccupancyState{}, noCalendar, 4, behave.CallCount, never{t}); got.Count != 4 {
		t.Errorf("a bar in a call of 4 counted %d", got.Count)
	}
	if got := behave.PeopleCount(behave.OccupancyState{Count: 4}, noCalendar, 0, behave.CallCount, never{t}); got.Count != 0 {
		t.Errorf("an idle bar counted %d", got.Count)
	}
}
