package sim

import (
	"strconv"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/calendar/caltest"
)

// loadRelayIn loads the shipped kestrel-relay-c1 placed in room over the lab
// calendar, its behaviors compiled but not running, so a test ticks them.
func loadRelayIn(t *testing.T, id string, room calendar.Room, seed int64) *Device {
	t.Helper()
	k := calTestKnobs()
	e := shippedEngine(t)
	e.SetCalendar(calendar.New(seed, func(calendar.Room) (calendar.Knobs, bool) { return k, true }))
	snap := DeviceSnapshot{DeviceID: id, Hostname: id, LoopbackIP: "127.0.0.1", ProfileName: "kestrel-relay-c1", Version: 1,
		SiteCode: room.Site, Building: room.Building, Room: room.Room}
	if err := e.LoadDevices([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	return e.Devices()[0]
}

// The Relay C1 codec follows the room's meetings off the lab calendar
// (#2767): over a simulated day on a pinned clock, after every tick its call
// is up exactly when the calendar says somebody is in the room and the video
// bar in the same room says the same, its far-end participants are never
// more than who came, content is shared only inside a call and from one of
// its sources, and none of it is up when the room is empty, a no-show
// included.
func TestTheRelayFollowsTheLabCalendar(t *testing.T) {
	day := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	room, _ := busyDay(t, 7, day)
	relay := loadRelayIn(t, "hq-divisible-conference-1-codec-1", room, 7)
	bar, cal := loadVRoomIn(t, "hq-divisible-conference-1-videobar-1", room, 7)
	at := day
	relay.SetClock(func() time.Time { return at })
	bar.SetClock(func() time.Time { return at })
	sources := map[string]bool{"hdmi1": true, "hdmi2": true, "usbc": true, "wireless": true}
	var active, shared, noShow int
	for i := range 24 * 60 {
		at = day.Add(time.Duration(i) * time.Minute)
		tickByName(t, relay, "call_cycle")
		tickByName(t, bar, "call_cycle")
		e, inRoom := held(cal, room, at)
		on, n := relay.ReadPath("status.call.active"), relay.ReadPath("status.call.participants")
		if on != inRoom {
			t.Fatalf("at %s the codec's call is %v, the calendar has the room %v", at.Format("15:04"), on, inRoom)
		}
		if on != bar.ReadPath("status.call.active") {
			t.Fatalf("at %s the codec's call is %v and the bar's %v, in one room", at.Format("15:04"), on, bar.ReadPath("status.call.active"))
		}
		if now, ok := cal.Now(room, at, calendar.MeetingKinds); ok && caltest.M(now).Outcome == calendar.OutcomeNoShow {
			noShow++
		}
		sharing := relay.ReadPath("status.content.sharing")
		if !inRoom {
			if f, _, _ := toFloat(n); f != 0 || sharing != false || relay.ReadPath("status.content.source") != "" {
				t.Fatalf("at %s the room is empty, yet the codec reads %v participants and content %v from %v", at.Format("15:04"), n, sharing, relay.ReadPath("status.content.source"))
			}
			continue
		}
		active++
		if f, _, _ := toFloat(n); int(f) < 1 || int(f) > caltest.M(e).Actual {
			t.Fatalf("at %s the codec holds %v far-end participants, the meeting had %d come", at.Format("15:04"), n, caltest.M(e).Actual)
		}
		if sharing == true {
			shared++
			if src, _ := relay.ReadPath("status.content.source").(string); !sources[src] {
				t.Fatalf("at %s the codec shares from %q", at.Format("15:04"), src)
			}
		}
	}
	if noShow == 0 || shared == 0 {
		t.Fatalf("the day sampled %d no-show ticks and %d with content shared; want both", noShow, shared)
	}
	t.Logf("in a call %d minutes, sharing content %d of them", active, shared)
}

// A call placed or hung up on the codec's own API holds against the calendar
// (the same yielding the bar has): a dialed call stays until disconnected, a
// hang-up on a meeting is not rejoined, and the next meeting is.
func TestTheRelaysOwnCallsYieldTheCalendar(t *testing.T) {
	day := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	room, entries := busyDay(t, 7, day)
	d := loadRelayIn(t, "hq-divisible-conference-1-codec-1", room, 7)
	cal := calendar.New(7, func(calendar.Room) (calendar.Knobs, bool) { return calTestKnobs(), true })
	var meeting, next calendar.Entry
	for i, e := range entries {
		if caltest.M(e).Outcome == calendar.OutcomeNoShow || e.End.Sub(e.Start) < 25*time.Minute {
			continue
		}
		for _, n := range entries[i+1:] {
			if caltest.M(n).Outcome != calendar.OutcomeNoShow {
				meeting, next = e, n
				break
			}
		}
		if caltest.IsMeeting(next) {
			break
		}
	}
	if !caltest.IsMeeting(meeting) || !caltest.IsMeeting(next) {
		t.Fatal("no two held meetings in the day")
	}
	at := tickUp(meeting.Start.Add(-4 * time.Minute))
	d.SetClock(func() time.Time { return at })
	tickByName(t, d, "call_cycle")
	if d.ReadPath("status.call.active") != false {
		t.Fatal("the codec is in a call before the meeting")
	}
	if res := d.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/call/dial", Body: `{"participants":3}`}); res.Status != 200 {
		t.Fatalf("dial = %+v", res)
	}
	for i := 1; i <= 8; i++ {
		at = tickUp(meeting.Start.Add(-4 * time.Minute)).Add(time.Duration(i) * time.Minute)
		tickByName(t, d, "call_cycle")
	}
	if _, running := held(cal, room, at); !running {
		t.Fatalf("the meeting is not running at %s", at.Format("15:04"))
	}
	if d.ReadPath("status.call.active") != true {
		t.Fatal("a dialed call was ended by the calendar")
	}
	if res := d.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/call/disconnect", Body: `{}`}); res.Status != 200 {
		t.Fatalf("disconnect = %+v", res)
	}
	for i := 1; i <= 2; i++ {
		at = at.Add(time.Duration(i) * time.Minute)
		tickByName(t, d, "call_cycle")
	}
	if _, still := held(cal, room, at); !still || d.ReadPath("status.call.active") != false {
		t.Fatalf("after the hang-up the meeting runs (%v) and the codec is in a call (%v); want it out", still, d.ReadPath("status.call.active"))
	}
	at = tickUp(next.Start)
	tickByName(t, d, "call_cycle")
	if _, running := held(cal, room, at); !running || d.ReadPath("status.call.active") != true {
		t.Fatalf("the codec did not join the next meeting (%s)", strconv.FormatInt(next.Start.Unix(), 10))
	}
}

// With no calendar the codec never takes a call by itself.
func TestTheRelayWithNoCalendarStaysIdle(t *testing.T) {
	e := shippedEngine(t)
	if err := e.LoadDevices([]DeviceSnapshot{{DeviceID: "c", Hostname: "c", LoopbackIP: "127.0.0.1", ProfileName: "kestrel-relay-c1", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	at := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	d.SetClock(func() time.Time { return at })
	for i := range 60 {
		at = at.Add(time.Duration(i) * time.Minute)
		tickByName(t, d, "call_cycle")
	}
	if d.ReadPath("status.call.active") != false {
		t.Fatal("a codec with no calendar is in a call")
	}
}
