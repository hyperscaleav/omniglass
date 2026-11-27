package behave_test

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

func TestRecordingsCountTheRecordedMeetingAndItsQueue(t *testing.T) {
	for _, tc := range []struct {
		name    string
		hms     string
		in, que int
	}{
		{"a recorded booked meeting runs", "10:30:00", 1, 0},
		{"unrecorded booked meeting", "08:45:00", 0, 0},
		{"a recorded one just ended: queued", "10:58:30", 0, 1},
		{"back to back: the next (unrecorded) runs while the last is still queued", "11:03:00", 0, 1},
		{"the queue is a tail: gone after ten minutes", "11:09:00", 0, 0},
		{"a recorded walk-in", "12:10:00", 1, 0},
		{"its queue", "12:20:00", 0, 1},
		{"a no-show is never recorded", "13:50:00", 0, 0},
		{"between meetings", "09:20:00", 0, 0},
	} {
		rs := roomAt(tc.hms)
		own := behave.Recordings(behave.RecordingCounts{}, nil, rs, behave.DefaultRecordings, never{t})
		cov := behave.Recordings(behave.RecordingCounts{}, []behave.RecordingRoom{{Name: "a", State: rs}}, calendar.RoomState{}, behave.DefaultRecordings, never{t})
		for who, got := range map[string]behave.RecordingCounts{"own room": own, "covered": cov} {
			if got.InProgress != tc.in || got.QueueDepth != tc.que {
				t.Errorf("%s (%s): %+v, want %d recording and %d queued", tc.name, who, got, tc.in, tc.que)
			}
		}
	}
}

func TestRecordingsSumTheCoveredRoomsAndListEachOne(t *testing.T) {
	rooms := []behave.RecordingRoom{{Name: "a", State: roomAt("10:30:00")}, {Name: "b", State: roomAt("10:30:00")}, {Name: "c", State: roomAt("09:20:00")}}
	got := behave.Recordings(behave.RecordingCounts{}, rooms, calendar.RoomState{}, behave.DefaultRecordings, never{t})
	if got.InProgress != 2 || got.QueueDepth != 0 {
		t.Errorf("two recording rooms: %+v", got)
	}
	per := behave.PerRoom(append(rooms, behave.RecordingRoom{Name: "d", State: roomAt("10:58:30")}))
	want := []behave.RoomRecording{{"a", 1, 0}, {"b", 1, 0}, {"c", 0, 0}, {"d", 0, 1}}
	for i := range want {
		if per[i] != want[i] {
			t.Errorf("room %d: %+v, want %+v", i, per[i], want[i])
		}
	}
}

func TestRecordingsWithNoCalendarDriftEachCounterOnce(t *testing.T) {
	rng := &stub{frac: 1} // a full step up each
	got := behave.Recordings(behave.RecordingCounts{InProgress: 11, QueueDepth: 39}, nil, noCalendar, behave.DefaultRecordings, rng)
	if got.InProgress != 12 || got.QueueDepth != 40 || rng.draws != 2 {
		t.Fatalf("%+v after %d draws, want 12 and 40 (the caps) after 2", got, rng.draws)
	}
	// a covered room with no calendar drifts too
	got = behave.Recordings(behave.RecordingCounts{}, []behave.RecordingRoom{{Name: "a", State: noCalendar}}, noCalendar, behave.DefaultRecordings, &stub{frac: 0})
	if got.InProgress != 0 || got.QueueDepth != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestBuildingDayPlaysInsideTheHoursAndStopsOutside(t *testing.T) {
	open, shut, none := behave.HoursReading{Known: true, Open: true}, behave.HoursReading{Known: true}, behave.HoursReading{}
	p := behave.DefaultSign
	for _, tc := range []struct {
		name  string
		prev  behave.SignState
		hours behave.HoursReading
		want  behave.SignState
	}{
		{"opening plays from the first item", behave.SignState{Playback: "stopped", Index: 2, Content: "Today's events"}, open, behave.SignState{Open: true, Playback: "playing", Index: 0, Content: "Welcome loop"}},
		{"open: the playlist's own advance is left alone", behave.SignState{Open: true, Playback: "playing", Index: 2}, open, behave.SignState{Open: true, Playback: "playing", Index: 2, Content: "Today's events"}},
		{"an operator's pause holds while open", behave.SignState{Open: true, Playback: "paused", Index: 1}, open, behave.SignState{Open: true, Playback: "paused", Index: 1, Content: "Wayfinding"}},
		{"closing stops it where it was", behave.SignState{Open: true, Playback: "playing", Index: 3}, shut, behave.SignState{Playback: "stopped", Index: 3, Content: "Safety notice"}},
		{"closed stays stopped", behave.SignState{Playback: "stopped", Index: 3}, shut, behave.SignState{Playback: "stopped", Index: 3, Content: "Safety notice"}},
		{"an operator's play while closed holds until opening", behave.SignState{Playback: "playing", Index: 1}, shut, behave.SignState{Playback: "playing", Index: 1, Content: "Wayfinding"}},
		{"no hours to read: plays on", behave.SignState{Open: true, Playback: "playing", Index: 2}, none, behave.SignState{Open: true, Playback: "playing", Index: 2, Content: "Today's events"}},
		{"an index past the playlist reads the last", behave.SignState{Playback: "stopped", Index: 9}, shut, behave.SignState{Playback: "stopped", Index: 9, Content: "Safety notice"}},
	} {
		if got := behave.BuildingDay(tc.prev, tc.hours, p); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestReadBuildingHoursIsOpenInsideTheSpanOfWholeHours(t *testing.T) {
	h := calendar.Hours{Start: 8, End: 14}
	for _, tc := range []struct {
		hms   string
		known bool
		want  behave.HoursReading
	}{
		{"07:59:59", true, behave.HoursReading{Known: true}},
		{"08:00:00", true, behave.HoursReading{Known: true, Open: true}},
		{"13:59:59", true, behave.HoursReading{Known: true, Open: true}},
		{"14:00:00", true, behave.HoursReading{Known: true}},
		{"10:00:00", false, behave.HoursReading{}},
	} {
		if got := behave.ReadBuildingHours(h, tc.known, at(tc.hms)); got != tc.want {
			t.Errorf("%s known %v: %+v, want %+v", tc.hms, tc.known, got, tc.want)
		}
	}
}

func TestBuildingSpanIsTheWidestHoursOfTheBuildingsRooms(t *testing.T) {
	knobs := func(s, e int) calendar.Knobs {
		k := pinnedKnobs()
		k.Hours = calendar.Hours{Start: s, End: e}
		return k
	}
	w := &world.World{Version: world.Version, Meetings: map[string]calendar.Knobs{"short": knobs(9, 12), "long": knobs(6, 18), "other": knobs(0, 24)},
		Sites: []world.Site{{Name: "hq", Rooms: []world.Room{
			{Name: "r1", Building: "tower", Standard: "short"}, {Name: "r2", Building: "tower", Standard: "long"},
			{Name: "r3", Building: "annex", Standard: "other"}, {Name: "sign", Building: "tower", Standard: "signage"}}},
			{Name: "elsewhere", Rooms: []world.Room{{Name: "x", Building: "tower", Standard: "other"}}}}}
	cal := pinnedCalendar()
	for _, tc := range []struct {
		name string
		w    *world.World
		cal  *calendar.Calendar
		room calendar.Room
		want calendar.Hours
		ok   bool
	}{
		{"the widest of the building's rooms, a signage room included", w, nil, calendar.Room{Site: "hq", Building: "tower", Room: "sign"}, calendar.Hours{Start: 6, End: 18}, true},
		{"another building has its own", w, nil, calendar.Room{Site: "hq", Building: "annex", Room: "r3"}, calendar.Hours{Start: 0, End: 24}, true},
		{"no world: the room's own through the calendar", nil, cal, room, calendar.Hours{Start: 8, End: 14}, true},
		{"nothing declared", nil, nil, room, calendar.Hours{}, false},
		{"a world naming no knobs for the building falls to the calendar", w, cal, calendar.Room{Site: "hq", Building: "none", Room: "q"}, calendar.Hours{Start: 8, End: 14}, true},
	} {
		got, ok := behave.BuildingSpan(tc.w, tc.cal, tc.room)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("%s: %+v %v, want %+v %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

var _ = time.Second
