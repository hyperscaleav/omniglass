package sim

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/calendar/caltest"
	"github.com/hyperscaleav/omniglass/sim/world"
)

func calTestKnobs() calendar.Knobs {
	return calendar.Knobs{
		BookingRate: 0.6, SlotMix: map[int]float64{30: 1, 60: 1}, NoShowRate: 0.2,
		EndEarlyRate: 0.2, EndLateRate: 0.1, Attendance: calendar.Range{Min: 2, Max: 6}, WalkInRate: 0.2,
	}
}

// calEngine is an engine over the shipped catalog holding one display in the
// given room, its clock pinned, and the lab calendar of the seed.
func calEngine(t *testing.T, seed int64, room calendar.Room, at *time.Time) (*Engine, *Device) {
	t.Helper()
	e := shippedEngine(t)
	k := calTestKnobs()
	e.SetCalendar(calendar.New(seed, func(calendar.Room) (calendar.Knobs, bool) { return k, true }))
	snap := DeviceSnapshot{DeviceID: "d-" + room.Room, Hostname: "d-" + room.Room, LoopbackIP: "127.0.0.1",
		ProfileName: "boreal-edge", Product: "boreal-edge-86", Version: 1, SiteCode: room.Site, Building: room.Building, Room: room.Room}
	if err := e.LoadDevices([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	d.SetClock(pinned(at))
	return e, d
}

var probeSeq int

// A device's template reads its own room's meeting through its clock, two
// engines of one seed read the same day, and a device in another room reads
// its own.
func TestMeetingFuncsReadTheDevicesRoomThroughItsClock(t *testing.T) {
	room := calendar.Room{Site: "hq", Building: "tower", Room: "r101"}
	other := calendar.Room{Site: "hq", Building: "tower", Room: "r102"}
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	ref := calendar.New(11, func(calendar.Room) (calendar.Knobs, bool) { return calTestKnobs(), true })
	es := ref.Entries(room, day, day.Add(24*time.Hour), calendar.MeetingKinds)
	var e calendar.Entry
	for _, c := range es {
		if caltest.M(c).Origin == calendar.OriginBooked {
			e = c
			break
		}
	}
	if !caltest.IsMeeting(e) {
		t.Fatal("no booked meeting in the day")
	}
	mid := e.Start.Add(e.End.Sub(e.Start) / 2)
	at := mid
	const now = `{{ with meetingNow }}{{ .organizer }}|{{ .outcome }}|{{ .origin }}{{ else }}none{{ end }}`
	want := caltest.M(e).Organizer + "|" + string(caltest.M(e).Outcome) + "|booked"

	_, d1 := calEngine(t, 11, room, &at)
	_, d2 := calEngine(t, 11, room, &at)
	if got := stampBehavior(t, d1, now); got != want {
		t.Fatalf("engine one reads %v, want %q", got, want)
	}
	if got := stampBehavior(t, d2, now); got != want {
		t.Fatalf("engine two reads %v, want %q", got, want)
	}

	// meetingNext from before the day's first meeting is that meeting; a
	// template renders an integer as its decimal text.
	first := es[0]
	at = first.Start.Add(-time.Second)
	if got := stampBehavior(t, d1, `{{ with meetingNext }}{{ .start }}{{ end }}`); fmt.Sprint(got) != strconv.FormatInt(first.Start.Unix(), 10) {
		t.Fatalf("meetingNext reads %v, want %d", got, first.Start.Unix())
	}

	// By room key a service reads another room's day.
	at = mid
	otherNow, ok := ref.Now(other, at, calendar.MeetingKinds)
	wantOther := "none"
	if ok {
		wantOther = caltest.M(otherNow).Organizer
	}
	if got := stampBehavior(t, d1, `{{ with roomMeetingNow "hq" "tower" "r102" }}{{ .organizer }}{{ else }}none{{ end }}`); got != wantOther {
		t.Fatalf("roomMeetingNow reads %v, want %q", got, wantOther)
	}
}

func TestMeetingFuncsAreEmptyWithoutACalendar(t *testing.T) {
	at := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	e, d := calEngine(t, 1, calendar.Room{Site: "hq", Building: "tower", Room: "r101"}, &at)
	e.SetCalendar(nil)
	if got := stampBehavior(t, d, `{{ with meetingNow }}busy{{ else }}none{{ end }}`); got != "none" {
		t.Fatalf("a device with no calendar reads %v", got)
	}
}

// A service resolves a system to a room through the world it holds, never
// through Omniglass (#2773): ownerRoom and ownerMeetingNow read the world, a
// system the world does not name resolves to nothing, and coveredRooms lists
// the rooms the service's own world entry speaks for.
func TestServiceFuncsResolveASystemThroughTheWorld(t *testing.T) {
	room := calendar.Room{Site: "hq", Building: "tower", Room: "r102"}
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	ref := calendar.New(11, func(calendar.Room) (calendar.Knobs, bool) { return calTestKnobs(), true })
	var e calendar.Entry
	for _, c := range ref.Entries(room, day, day.Add(24*time.Hour), calendar.MeetingKinds) {
		if caltest.M(c).Origin == calendar.OriginBooked {
			e = c
			break
		}
	}
	if !caltest.IsMeeting(e) {
		t.Fatal("no booked meeting in the day")
	}
	at := e.Start.Add(e.End.Sub(e.Start) / 2)
	eng, d := calEngine(t, 11, calendar.Room{Site: "hq", Building: "tower", Room: "r101"}, &at)
	const resolve = `{{ with ownerRoom "sys-102" }}{{ .site }}/{{ .building }}/{{ .room }}{{ else }}none{{ end }}`
	if got := stampBehavior(t, d, resolve); got != "none" {
		t.Fatalf("with no world a system resolves to %v", got)
	}
	eng.SetWorld(&world.World{Version: world.Version, Sites: []world.Site{{Name: "hq", Rooms: []world.Room{
		{Name: "r101", Building: "tower", System: "sys-101"}, {Name: "r102", Building: "tower", System: "sys-102"}}}},
		Services: []world.Service{{Name: "d-r101", Profile: "x", Covers: []world.Cover{
			{System: "sys-101", Site: "hq", Building: "tower", Room: "r101"}, {System: "sys-102", Site: "hq", Building: "tower", Room: "r102"}}}}})
	if got := stampBehavior(t, d, resolve); got != "hq/tower/r102" {
		t.Fatalf("ownerRoom reads %v", got)
	}
	if got := stampBehavior(t, d, `{{ with ownerRoom "sys-999" }}some{{ else }}none{{ end }}`); got != "none" {
		t.Fatalf("a system the world lacks resolves to %v", got)
	}
	if got := stampBehavior(t, d, `{{ with ownerMeetingNow "sys-102" }}{{ .organizer }}{{ else }}none{{ end }}`); got != caltest.M(e).Organizer {
		t.Fatalf("ownerMeetingNow reads %v, want %q", got, caltest.M(e).Organizer)
	}
	if got := stampBehavior(t, d, `{{ range coveredRooms }}{{ .room }},{{ end }}`); got != "r101,r102," {
		t.Fatalf("coveredRooms reads %v", got)
	}
}
