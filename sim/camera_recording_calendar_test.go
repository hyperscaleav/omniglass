package sim

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// The PTZ camera and the recording service follow the lab calendar (#2775).
// The camera frames the people actually in its room and sits at its home
// preset between meetings; the recording service carries a recording for a
// seeded share of the meetings held in the rooms it covers. Every reading is
// derived from the calendar through the device's clock; nothing consults
// Omniglass.

var gazeRoom = calendar.Room{Site: "hq", Building: "tower", Room: "r101"}

func gazeKnobs() calendar.Knobs {
	k := calTestKnobs()
	k.BookingRate, k.NoShowRate, k.WalkInRate = 0.7, 0.25, 0.4
	k.Hours = calendar.Hours{Start: 8, End: 18}
	k.RecordingRate = 0.5
	return k
}

func gazeCal(seed int64, k calendar.Knobs) *calendar.Calendar {
	return calendar.New(seed, func(calendar.Room) (calendar.Knobs, bool) { return k, true })
}

// calDevice is one device of the profile in room on a pinned clock, over the
// shipped catalog, with the calendar and the world installed on its engine.
func calDevice(t *testing.T, profileName, hostname string, room calendar.Room, cal *calendar.Calendar, w *world.World, at *time.Time) *Device {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	e.SetCalendar(cal)
	e.SetWorld(w)
	snap := DeviceSnapshot{DeviceID: hostname, Hostname: hostname, LoopbackIP: "127.0.0.1", ProfileName: profileName, Version: 1,
		SiteCode: room.Site, Building: room.Building, Room: room.Room}
	if err := e.LoadDevices([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	d.SetClock(pinned(at))
	return d
}

func num(t *testing.T, d *Device, path string) float64 {
	t.Helper()
	switch v := d.ReadPath(path).(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		t.Fatalf("%s = %v (%T), want a number", path, v, v)
		return 0
	}
}

// framedMag is the profile's framing rule: a tighter shot for fewer people,
// the widest for a full room, 1 to 4 times.
func framedMag(people int) float64 {
	return math.Round(math.Min(4, math.Max(1, 4.5-0.3*float64(people)))*10) / 10
}

// Over a simulated day, the camera's presence and people count agree with
// the calendar at every sampled minute, it is framed on the room's group
// through a meeting and at its home preset before the first and after each
// one; the day holds a booked meeting, a walk-in and a no-show.
func TestGazeFramesTheRoomsActualAttendeesOverADay(t *testing.T) {
	k := gazeKnobs()
	const seed = 21
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	cal := gazeCal(seed, k)
	var booked, walkIn, noShow int
	for _, e := range cal.Entries(gazeRoom, day, day.Add(24*time.Hour)) {
		switch {
		case e.Meeting.Outcome == calendar.OutcomeNoShow:
			noShow++
		case e.Meeting.Origin == calendar.OriginWalkIn:
			walkIn++
		default:
			booked++
		}
	}
	if booked == 0 || walkIn == 0 || noShow == 0 {
		t.Fatalf("the pinned day holds %d booked, %d walk-in, %d no-show; pick a seed with all three", booked, walkIn, noShow)
	}

	at := day
	d := calDevice(t, "kestrel-gaze-p2", "cam-1", gazeRoom, cal, nil, &at)
	var framedMinutes, walkInSeen, noShowMinutes int
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		d.TickBehavior("room_meeting")
		people := 0
		if e, ok := cal.Now(gazeRoom, at); ok {
			if e.Meeting.Outcome == calendar.OutcomeNoShow {
				noShowMinutes++
			} else {
				people = e.Meeting.Actual
				if e.Meeting.Origin == calendar.OriginWalkIn {
					walkInSeen++
				}
			}
		}
		if got := int(num(t, d, "status.camera.people")); got != people {
			t.Fatalf("%s: %d people framed, the calendar says %d", at.Format("15:04"), got, people)
		}
		if got := fmt.Sprint(d.ReadPath("status.camera.presence")); got != fmt.Sprint(people > 0) {
			t.Fatalf("%s: presence %s with %d people", at.Format("15:04"), got, people)
		}
		pan, tilt := num(t, d, "status.camera.pan"), num(t, d, "status.camera.tilt")
		if people > 0 {
			framedMinutes++
			if got := num(t, d, "status.camera.magnification"); got != framedMag(people) {
				t.Fatalf("%s: magnification %v for %d people, want %v", at.Format("15:04"), got, people, framedMag(people))
			}
			if d.ReadPath("status.camera.tracking") != "tracking" || num(t, d, "status.camera.preset") != 0 {
				t.Fatalf("%s: not tracking in a meeting: %v", at.Format("15:04"), d.State()["status"])
			}
			continue
		}
		// Empty (between meetings, or a no-show): home, as defaults say.
		if pan != 0 || tilt != 0 || num(t, d, "status.camera.magnification") != 1 || num(t, d, "status.camera.preset") != 0 {
			t.Fatalf("%s: not at home with nobody there: pan %v tilt %v mag %v preset %v", at.Format("15:04"), pan, tilt,
				d.ReadPath("status.camera.magnification"), d.ReadPath("status.camera.preset"))
		}
	}
	if framedMinutes == 0 || walkInSeen == 0 || noShowMinutes == 0 {
		t.Fatalf("the day exercised %d framed minutes, %d walk-in, %d no-show", framedMinutes, walkInSeen, noShowMinutes)
	}
}

// A room whose every booking is skipped is never framed: presence stays
// false, the camera stays where an operator left it.
func TestGazeIsUnchangedByANoShow(t *testing.T) {
	k := gazeKnobs()
	k.BookingRate, k.NoShowRate, k.WalkInRate = 1, 1, 0
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	at := day
	d := calDevice(t, "kestrel-gaze-p2", "cam-1", gazeRoom, gazeCal(5, k), nil, &at)
	d.TickBehavior("room_meeting")
	setPath(t, d, "status.camera.preset", 2)
	setPath(t, d, "status.camera.tracking", "manual")
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		d.TickBehavior("room_meeting")
		if d.ReadPath("status.camera.presence") != false || num(t, d, "status.camera.people") != 0 {
			t.Fatalf("%s: presence in a room of no-shows", at.Format("15:04"))
		}
		if num(t, d, "status.camera.preset") != 2 || d.ReadPath("status.camera.tracking") != "manual" {
			t.Fatalf("%s: a no-show moved the camera", at.Format("15:04"))
		}
	}
}

// An operator's framing holds through the meeting; the next meeting's
// start or the end of this one is the edge that moves the camera.
func TestGazeKeepsAnOperatorsFramingUntilTheNextEdge(t *testing.T) {
	k := gazeKnobs()
	k.NoShowRate, k.WalkInRate = 0, 0
	cal := gazeCal(8, k)
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	var m calendar.Entry
	for _, e := range cal.Entries(gazeRoom, day, day.Add(24*time.Hour)) {
		m = e
		break
	}
	at := m.Start.Add(time.Minute)
	d := calDevice(t, "kestrel-gaze-p2", "cam-1", gazeRoom, cal, nil, &at)
	d.TickBehavior("room_meeting")
	if d.ReadPath("status.camera.tracking") != "tracking" {
		t.Fatal("not framed at the meeting")
	}
	setPath(t, d, "status.camera.preset", 3)
	setPath(t, d, "status.camera.tracking", "manual")
	setPath(t, d, "status.camera.pan", 12.0)
	for at = m.Start.Add(2 * time.Minute); at.Before(m.End); at = at.Add(30 * time.Second) {
		d.TickBehavior("room_meeting")
		if num(t, d, "status.camera.preset") != 3 || num(t, d, "status.camera.pan") != 12 {
			t.Fatalf("%s: the operator's framing was overridden mid-meeting", at.Format("15:04:05"))
		}
	}
	at = m.End.Add(time.Second)
	d.TickBehavior("room_meeting")
	if num(t, d, "status.camera.preset") != 0 || num(t, d, "status.camera.pan") != 0 || d.ReadPath("status.camera.tracking") != "tracking" {
		t.Fatalf("after the meeting the camera is not home: %v", d.State()["status"])
	}
}

// Tracking follows a presenter only while someone is there: an empty room's
// camera holds home; a camera with no calendar drifts as it always did.
func TestGazeTracksOnlyWhileSomeoneIsThere(t *testing.T) {
	k := gazeKnobs()
	k.NoShowRate = 0
	cal := gazeCal(8, k)
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	m := cal.Entries(gazeRoom, day, day.Add(24*time.Hour))[0]
	at := m.Start.Add(-time.Hour)
	d := calDevice(t, "kestrel-gaze-p2", "cam-1", gazeRoom, cal, nil, &at)
	for i := 0; i < 30; i++ {
		d.TickBehavior("room_meeting")
		d.TickBehavior("tracking_drift")
		if num(t, d, "status.camera.pan") != 0 || num(t, d, "status.camera.tilt") != 0 {
			t.Fatalf("an empty room's camera drifted: %v", d.State()["status"])
		}
	}
	at = m.Start.Add(time.Minute)
	moved := false
	for i := 0; i < 30; i++ {
		d.TickBehavior("room_meeting")
		d.TickBehavior("tracking_drift")
		if num(t, d, "status.camera.pan") != 0 {
			moved = true
		}
	}
	if !moved {
		t.Fatal("a camera in a meeting never tracked")
	}

	n := calDevice(t, "kestrel-gaze-p2", "cam-2", gazeRoom, nil, nil, &at)
	for i := 0; i < 30; i++ {
		n.TickBehavior("room_meeting")
		n.TickBehavior("tracking_drift")
	}
	if num(t, n, "status.camera.pan") == 0 {
		t.Fatal("a camera with no calendar never drifted")
	}
	if n.ReadPath("status.camera.presence") != false {
		t.Fatalf("a camera with no calendar reports presence %v", n.ReadPath("status.camera.presence"))
	}
}

// ---- the recording service ----

func recRooms(n int) []world.Cover {
	var out []world.Cover
	for i := 0; i < n; i++ {
		out = append(out, world.Cover{System: fmt.Sprintf("sys-%d", i), Site: "hq", Building: "tower", Room: fmt.Sprintf("r%d", 100+i)})
	}
	return out
}

func recWorld(k calendar.Knobs, seed int64, covers []world.Cover, service string) (*world.World, *calendar.Calendar) {
	w := &world.World{Version: world.Version, CalendarSeed: seed, Meetings: map[string]calendar.Knobs{"conf": k}}
	var rooms []world.Room
	for _, c := range covers {
		rooms = append(rooms, world.Room{Name: c.Room, Building: c.Building, Standard: "conf", System: c.System})
	}
	w.Sites = []world.Site{{Name: "hq", Rooms: rooms}}
	w.Services = []world.Service{{Name: service, Profile: "recording-service", Covers: covers}}
	return w, w.Calendar()
}

const recQueueWindow = 600 // seconds a finished recording waits to be written

// recWant is the calendar's own answer, computed with no device: how many
// of the covered rooms are recording now, and how many recordings ended in
// the last ten minutes.
func recWant(cal *calendar.Calendar, covers []world.Cover, t time.Time) (inProgress, queued int) {
	for _, c := range covers {
		room := calendar.Room{Site: c.Site, Building: c.Building, Room: c.Room}
		for _, e := range cal.Entries(room, t.Add(-time.Hour), t.Add(time.Hour)) {
			if !e.Meeting.Recorded {
				continue
			}
			if !t.Before(e.Start) && t.Before(e.End) {
				inProgress++
			}
			if !e.End.After(t) && t.Sub(e.End) <= recQueueWindow*time.Second {
				queued++
			}
		}
	}
	return
}

// Over a simulated day the service's recordings in progress and its queue
// agree with the calendar at every sampled minute; a no-show is never among
// them.
func TestRecordingServiceCarriesTheCalendarsRecordingsOverADay(t *testing.T) {
	k := gazeKnobs()
	const seed = 77
	covers := recRooms(6)
	w, cal := recWorld(k, seed, covers, "rec-1")
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	for _, c := range covers {
		for _, e := range cal.Entries(calendar.Room{Site: c.Site, Building: c.Building, Room: c.Room}, day, day.Add(24*time.Hour)) {
			if e.Meeting.Outcome == calendar.OutcomeNoShow && e.Meeting.Recorded {
				t.Fatal("a no-show is recorded")
			}
		}
	}
	at := day
	d := calDevice(t, "recording-service", "rec-1", calendar.Room{}, cal, w, &at)
	var peak, queuedSeen int
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		d.TickBehavior("room_recordings")
		wantIn, wantQ := recWant(cal, covers, at)
		if got := int(num(t, d, "status.recordings.in_progress")); got != wantIn {
			t.Fatalf("%s: %d recordings in progress, the calendar says %d", at.Format("15:04"), got, wantIn)
		}
		if got := int(num(t, d, "status.recordings.queue_depth")); got != wantQ {
			t.Fatalf("%s: queue %d, the calendar says %d", at.Format("15:04"), got, wantQ)
		}
		peak, queuedSeen = max(peak, wantIn), max(queuedSeen, wantQ)
	}
	if peak == 0 || queuedSeen == 0 {
		t.Fatalf("the day never recorded (peak %d) or queued (%d)", peak, queuedSeen)
	}
}

// A room of no-shows leaves the service idle even at a recording rate of 1,
// and a rate of 0 records nothing at all.
func TestRecordingServiceHoldsNoRecordingForANoShowOrAZeroRate(t *testing.T) {
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	for name, mod := range map[string]func(*calendar.Knobs){
		"no-shows":  func(k *calendar.Knobs) { k.BookingRate, k.NoShowRate, k.WalkInRate, k.RecordingRate = 1, 1, 0, 1 },
		"zero rate": func(k *calendar.Knobs) { k.RecordingRate = 0 },
	} {
		k := gazeKnobs()
		mod(&k)
		covers := recRooms(4)
		w, cal := recWorld(k, 3, covers, "rec-1")
		at := day
		d := calDevice(t, "recording-service", "rec-1", calendar.Room{}, cal, w, &at)
		for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
			d.TickBehavior("room_recordings")
			if in, q := num(t, d, "status.recordings.in_progress"), num(t, d, "status.recordings.queue_depth"); in != 0 || q != 0 {
				t.Fatalf("%s %s: %v in progress, queue %v", name, at.Format("15:04"), in, q)
			}
		}
	}
}

// Over a simulated month the share of held meetings the service records
// holds near the knob's rate, and the service agrees with the calendar at
// every sampled twenty minutes.
func TestRecordingServiceShareHoldsOverAMonth(t *testing.T) {
	k := gazeKnobs()
	k.RecordingRate = 0.3
	covers := recRooms(3)
	w, cal := recWorld(k, 12, covers, "rec-1")
	month := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	end := month.AddDate(0, 0, 30)
	var held, recorded int
	for _, c := range covers {
		for _, e := range cal.Entries(calendar.Room{Site: c.Site, Building: c.Building, Room: c.Room}, month, end) {
			if e.Meeting.Outcome == calendar.OutcomeNoShow {
				continue
			}
			held++
			if e.Meeting.Recorded {
				recorded++
			}
		}
	}
	if held < 300 {
		t.Fatalf("only %d held meetings in a month", held)
	}
	if got := float64(recorded) / float64(held); math.Abs(got-k.RecordingRate) > 0.06 {
		t.Fatalf("%d of %d held meetings recorded (%.3f), want near %v", recorded, held, got, k.RecordingRate)
	}
	at := month
	d := calDevice(t, "recording-service", "rec-1", calendar.Room{}, cal, w, &at)
	for at = month; at.Before(end); at = at.Add(20 * time.Minute) {
		d.TickBehavior("room_recordings")
		wantIn, _ := recWant(cal, covers, at)
		if got := int(num(t, d, "status.recordings.in_progress")); got != wantIn {
			t.Fatalf("%s: %d in progress, the calendar says %d", at.Format("Jan 2 15:04"), got, wantIn)
		}
	}
}

// A service that covers no rooms (no world) keeps its old drift: the
// counters still move on their own and the calendar is not consulted.
func TestRecordingServiceWithNoRoomsKeepsItsOwnDrift(t *testing.T) {
	at := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	d := calDevice(t, "recording-service", "rec-1", calendar.Room{}, gazeCal(1, gazeKnobs()), nil, &at)
	moved := false
	start := num(t, d, "status.recordings.in_progress")
	for i := 0; i < 40; i++ {
		d.TickBehavior("room_recordings")
		if num(t, d, "status.recordings.in_progress") != start {
			moved = true
		}
	}
	if !moved {
		t.Fatal("a service covering no rooms stopped drifting")
	}
}

// A recorder that sits in a room (no rooms it covers) records that room.
func TestRecordingServiceInARoomRecordsItsOwnRoom(t *testing.T) {
	k := gazeKnobs()
	k.RecordingRate = 0.6
	const seed = 31
	cal := gazeCal(seed, k)
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	at := day
	d := calDevice(t, "recording-service", "rec-1", gazeRoom, cal, nil, &at)
	var recordedMinutes, queuedMinutes int
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		d.TickBehavior("room_recordings")
		wantIn, wantQ := 0, 0
		for _, e := range cal.Entries(gazeRoom, at.Add(-time.Hour), at.Add(time.Hour)) {
			if !e.Meeting.Recorded {
				continue
			}
			if !at.Before(e.Start) && at.Before(e.End) {
				wantIn = 1
			}
			if !e.End.After(at) && at.Sub(e.End) <= recQueueWindow*time.Second {
				wantQ = 1
			}
		}
		if got := int(num(t, d, "status.recordings.in_progress")); got != wantIn {
			t.Fatalf("%s: %d in progress, the calendar says %d", at.Format("15:04"), got, wantIn)
		}
		if got := int(num(t, d, "status.recordings.queue_depth")); got != wantQ {
			t.Fatalf("%s: queue %d, the calendar says %d", at.Format("15:04"), got, wantQ)
		}
		recordedMinutes += wantIn
		queuedMinutes += wantQ
	}
	if recordedMinutes == 0 || queuedMinutes == 0 {
		t.Fatalf("the day recorded %d minutes and queued %d", recordedMinutes, queuedMinutes)
	}
}

// Covering rooms with no calendar behind them is no calendar: the service
// drifts exactly as before.
func TestRecordingServiceCoveringRoomsWithNoCalendarKeepsItsDrift(t *testing.T) {
	at := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	w, _ := recWorld(gazeKnobs(), 1, recRooms(3), "rec-1")
	d := calDevice(t, "recording-service", "rec-1", calendar.Room{}, nil, w, &at)
	moved := false
	start := num(t, d, "status.recordings.in_progress")
	for i := 0; i < 40; i++ {
		d.TickBehavior("room_recordings")
		moved = moved || num(t, d, "status.recordings.in_progress") != start
	}
	if !moved {
		t.Fatal("with no calendar the service stopped drifting")
	}
}
