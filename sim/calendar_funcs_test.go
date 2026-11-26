package sim

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
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
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	k := calTestKnobs()
	e.SetCalendar(calendar.New(seed, func(calendar.Room) (calendar.Knobs, bool) { return k, true }))
	snap := DeviceSnapshot{DeviceID: "d-" + room.Room, Hostname: "d-" + room.Room, LoopbackIP: "127.0.0.1",
		ProfileName: "boreal-edge-86", Version: 1, SiteCode: room.Site, Building: room.Building, Room: room.Room}
	if err := e.LoadDevices([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	d.SetClock(pinned(at))
	return e, d
}

var probeSeq int

// stampBehavior registers a behavior writing the rendered expr to a probe
// path, ticks it once and returns what it wrote.
func stampBehavior(t *testing.T, d *Device, expr string) any {
	t.Helper()
	probeSeq++
	name := "cal" + string(rune('a'+probeSeq))
	if err := d.AddBehaviorFromProfile(&profile.Behavior{Name: name, Schedule: profile.Schedule{Interval: "60s"},
		Mutations: []profile.Mutation{{Path: "cal_probe", Value: expr}}}); err != nil {
		t.Fatal(err)
	}
	d.SetState(map[string]any{"cal_probe": ""}, 2)
	d.TickBehavior(name)
	return d.ReadPath("cal_probe")
}

// A device's template reads its own room's meeting through its clock, two
// engines of one seed read the same day, and a device in another room reads
// its own.
func TestMeetingFuncsReadTheDevicesRoomThroughItsClock(t *testing.T) {
	room := calendar.Room{Site: "hq", Building: "tower", Room: "r101"}
	other := calendar.Room{Site: "hq", Building: "tower", Room: "r102"}
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	ref := calendar.New(11, func(calendar.Room) (calendar.Knobs, bool) { return calTestKnobs(), true })
	es := ref.Entries(room, day, day.Add(24*time.Hour))
	var e calendar.Entry
	for _, c := range es {
		if c.Meeting.Origin == calendar.OriginBooked {
			e = c
			break
		}
	}
	if e.Meeting == nil {
		t.Fatal("no booked meeting in the day")
	}
	mid := e.Start.Add(e.End.Sub(e.Start) / 2)
	at := mid
	const now = `{{ with meetingNow }}{{ .organizer }}|{{ .outcome }}|{{ .origin }}{{ else }}none{{ end }}`
	want := e.Meeting.Organizer + "|" + string(e.Meeting.Outcome) + "|booked"

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
	otherNow, ok := ref.Now(other, at)
	wantOther := "none"
	if ok {
		wantOther = otherNow.Meeting.Organizer
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
