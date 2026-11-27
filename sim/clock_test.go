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

// offsetEngine is calEngine's twin on the engine's own clock (#2800): the
// device's clock is not pinned, the engine's is offset.
func offsetEngine(t *testing.T, seed int64, room calendar.Room, offset time.Duration) (*Engine, *Device) {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	e.SetClock(Clock{Offset: offset})
	k := calTestKnobs()
	e.SetCalendar(calendar.New(seed, func(calendar.Room) (calendar.Knobs, bool) { return k, true }))
	snap := DeviceSnapshot{DeviceID: "d-" + room.Room, Hostname: "d-" + room.Room, LoopbackIP: "127.0.0.1",
		ProfileName: "boreal-edge-86", Version: 1, SiteCode: room.Site, Building: room.Building, Room: room.Room}
	if err := e.LoadDevices([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	return e, e.Devices()[0]
}

// A booked meeting of the room, well in the future of real time, and an
// offset that puts the simulated clock inside it.
func meetingAndOffset(t *testing.T, seed int64, room calendar.Room) (calendar.Entry, time.Duration) {
	t.Helper()
	ref := calendar.New(seed, func(calendar.Room) (calendar.Knobs, bool) { return calTestKnobs(), true })
	from := time.Now().Add(72 * time.Hour).Truncate(24 * time.Hour)
	for _, e := range ref.Entries(room, from, from.Add(48*time.Hour)) {
		if e.Meeting.Origin == calendar.OriginBooked && e.Meeting.Outcome != calendar.OutcomeNoShow && e.End.Sub(e.Start) >= 30*time.Minute {
			mid := e.Start.Add(e.End.Sub(e.Start) / 2)
			return e, time.Until(mid)
		}
	}
	t.Fatal("no held booked meeting in two days")
	return calendar.Entry{}, 0
}

// An engine with a clock offset reads every calendar func at real time plus
// the offset: the same answer as an engine whose clock is pinned there, and a
// different one from the engine on real time (#2800).
func TestEngineClockOffsetMovesEveryCalendarFunc(t *testing.T) {
	room := calendar.Room{Site: "hq", Building: "tower", Room: "r101"}
	e, offset := meetingAndOffset(t, 11, room)
	mid := e.Start.Add(e.End.Sub(e.Start) / 2)
	lo, hi := mid.Unix()-3600, mid.Unix()+3600

	at := mid
	_, pinnedDev := calEngine(t, 11, room, &at)
	_, offsetDev := offsetEngine(t, 11, room, offset)
	_, realDev := offsetEngine(t, 11, room, 0)

	for _, expr := range []string{
		`{{ printf "%v" meetingNow }}`,
		`{{ printf "%v" meetingNext }}`,
		`{{ printf "%v" meetingPrev }}`,
		`{{ hasCalendar }}`,
		`{{ roomInUse 120 300 }}`,
		`{{ meetingSharing }}`,
		fmt.Sprintf(`{{ printf "%%v" (meetingsBetween %d %d) }}`, lo, hi),
		`{{ printf "%v" (roomMeetingNow "hq" "tower" "r101") }}`,
		`{{ printf "%v" (roomMeetingNext "hq" "tower" "r101") }}`,
		`{{ printf "%v" (roomMeetingPrev "hq" "tower" "r101") }}`,
		`{{ printf "%v" bookings }}`,
		`{{ printf "%v" (roomBookings "hq" "tower" "r101") }}`,
		`{{ printf "%v" problemsNow }}`,
		`{{ printf "%v" buildingHours }}`,
	} {
		want := fmt.Sprint(stampBehavior(t, pinnedDev, expr))
		if got := fmt.Sprint(stampBehavior(t, offsetDev, expr)); got != want {
			t.Errorf("%s: the offset engine reads %v, an engine pinned there reads %v", expr, got, want)
		}
	}
	inMeeting := `{{ with meetingNow }}{{ .organizer }}{{ else }}none{{ end }}`
	if got := stampBehavior(t, offsetDev, inMeeting); got != e.Meeting.Organizer {
		t.Fatalf("the offset engine's room reads %v, want the meeting's %q", got, e.Meeting.Organizer)
	}
	if got := stampBehavior(t, realDev, inMeeting); got == e.Meeting.Organizer {
		t.Fatalf("the engine on real time reads the meeting three days out")
	}

	// `now` is the offset clock, the same one the calendar funcs read.
	now, _ := strconv.ParseInt(fmt.Sprint(stampBehavior(t, offsetDev, `{{ now }}`)), 10, 64)
	if d := now - mid.Unix(); d < -5 || d > 5 {
		t.Fatalf("now reads %d, want the meeting's middle %d", now, mid.Unix())
	}
}

// The offset moves the simulated world's own time: a route's `now` stamp and
// an uptime anchor read the device's clock, so a reboot or a start stays at
// zero uptime whatever the offset. What Omniglass stamps and the node collects
// on is real time, which the offset never reaches (#2800).
func TestClockOffsetKeepsUptimeAndCollectionStampsReal(t *testing.T) {
	room := calendar.Room{Site: "hq", Building: "tower", Room: "r101"}
	const offset = 72 * time.Hour
	e, d := offsetEngine(t, 11, room, offset)

	if got := e.Now().Sub(time.Now()); got < offset-time.Second || got > offset+time.Second {
		t.Fatalf("the engine's clock runs %v ahead, want %v", got, offset)
	}
	stamp := d.routeFuncs()["now"].(func() int64)()
	if want := time.Now().Add(offset).Unix(); stamp < want-5 || stamp > want+5 {
		t.Fatalf("a route's now stamps %d, want the simulated %d", stamp, want)
	}
	if up := toInt64(d.ReadPath("status.device.uptime")); up < 0 || up > 5 {
		t.Fatalf("a device that has just started reads %ds of uptime on a clock offset by %v", up, offset)
	}

	// The seams that carry a timestamp out of the simulator toward Omniglass
	// stay on real time: a request hit's time, and the observation window.
	if at := hitFromLog(RequestLog{}).At; time.Since(at) > 2*time.Second || time.Since(at) < -2*time.Second {
		t.Fatalf("a hit is stamped %v, want real now", at)
	}
	if st := e.obs.started; time.Since(st) > time.Minute || time.Since(st) < -time.Minute {
		t.Fatalf("the observation window started at %v, want real now", st)
	}
}

// With no clock set an engine reads real time, as it did before the offset.
func TestEngineWithoutAClockReadsRealTime(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	if d := time.Since(e.Now()); d < -time.Second || d > time.Second {
		t.Fatalf("an engine with no clock reads %v from real time", d)
	}
	for _, s := range []string{"", "  "} {
		if off, err := ParseClockOffset(s); err != nil || off != 0 {
			t.Fatalf("ParseClockOffset(%q) = %v, %v", s, off, err)
		}
	}
	if off, err := ParseClockOffset("-90m"); err != nil || off != -90*time.Minute {
		t.Fatalf("ParseClockOffset(-90m) = %v, %v", off, err)
	}
	if _, err := ParseClockOffset("tomorrow"); err == nil {
		t.Fatal("a clock offset that is no duration was read")
	}
}
