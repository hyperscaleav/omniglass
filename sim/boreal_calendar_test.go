package sim

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/calendar/caltest"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// The Boreal displays and projectors follow their room's meetings (#2768):
// the lab calendar says a room is in use, and the panel wakes for it and
// sleeps after it. Every reading is derived from the calendar through the
// device's clock; nothing here consults Omniglass.

const (
	// meetingLead and meetingTail are the profiles' wake-ahead and sleep-after,
	// in seconds: a booked meeting wakes the panel two minutes before it
	// starts and it sleeps five minutes after the meeting ends.
	meetingLead = 120
	meetingTail = 300
)

var roomHQ = calendar.Room{Site: "hq", Building: "tower", Room: "r101"}

func dayKnobs() calendar.Knobs {
	k := calTestKnobs()
	k.BookingRate, k.NoShowRate, k.WalkInRate = 0.7, 0.25, 0.4
	k.Hours = calendar.Hours{Start: 8, End: 18}
	return k
}

// borealEngine is an engine over the shipped catalog with one device of the
// profile in roomHQ on a pinned clock and a calendar of the knobs.
func borealEngine(t *testing.T, profileName string, seed int64, k calendar.Knobs, at *time.Time) *Device {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	e.SetCalendar(calendar.New(seed, func(calendar.Room) (calendar.Knobs, bool) { return k, true }))
	snap := DeviceSnapshot{DeviceID: "d-1", Hostname: "d-1", LoopbackIP: "127.0.0.1", ProfileName: profileName, Version: 1,
		SiteCode: roomHQ.Site, Building: roomHQ.Building, Room: roomHQ.Room}
	if err := e.LoadDevices([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	d.SetClock(pinned(at))
	return d
}

// wantAwake is the calendar's own answer, computed with no device: the panel
// is awake from two minutes before a booked meeting nobody skips (at the
// start for a walk-in) until five minutes after its end; a no-show wakes
// nothing.
func wantAwake(cal *calendar.Calendar, room calendar.Room, t time.Time) bool {
	for _, e := range cal.Entries(room, t.Add(-time.Hour), t.Add(time.Hour), calendar.MeetingKinds) {
		if caltest.M(e).Outcome == calendar.OutcomeNoShow {
			continue
		}
		lead := time.Duration(0)
		if caltest.M(e).Origin == calendar.OriginBooked {
			lead = meetingLead * time.Second
		}
		if !t.Before(e.Start.Add(-lead)) && t.Before(e.End.Add(meetingTail*time.Second)) {
			return true
		}
	}
	return false
}

func calOf(seed int64, k calendar.Knobs) *calendar.Calendar {
	return calendar.New(seed, func(calendar.Room) (calendar.Knobs, bool) { return k, true })
}

func setPath(t *testing.T, d *Device, path string, v any) {
	t.Helper()
	s := d.State()
	cur := s
	parts := splitPath(path)
	for _, p := range parts[:len(parts)-1] {
		cur = cur[p].(map[string]any)
	}
	cur[parts[len(parts)-1]] = v
	d.SetState(s, 99)
}

func splitPath(p string) []string {
	var out []string
	cur := ""
	for _, c := range p {
		if c == '.' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(c)
	}
	return append(out, cur)
}

// Over a simulated day, a display's power agrees with the room's calendar at
// every sampled minute, and the day holds the cases that matter: a booked
// meeting, a walk-in and a no-show.
func TestBorealEdgePowerFollowsTheRoomsMeetingsOverADay(t *testing.T) {
	k := dayKnobs()
	const seed = 21
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	cal := calOf(seed, k)
	var booked, walkIn, noShow int
	for _, e := range cal.Entries(roomHQ, day, day.Add(24*time.Hour), calendar.MeetingKinds) {
		switch {
		case caltest.M(e).Outcome == calendar.OutcomeNoShow:
			noShow++
		case caltest.M(e).Origin == calendar.OriginWalkIn:
			walkIn++
		default:
			booked++
		}
	}
	if booked == 0 || walkIn == 0 || noShow == 0 {
		t.Fatalf("the pinned day holds %d booked, %d walk-in, %d no-show; pick a seed with all three", booked, walkIn, noShow)
	}

	at := day
	d := borealEngine(t, "boreal-edge-86", seed, k, &at)
	var awakeMinutes int
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		d.TickBehavior("room_meeting")
		want, got := "standby", fmt.Sprint(d.ReadPath("status.device.power"))
		if wantAwake(cal, roomHQ, at) {
			want = "on"
			awakeMinutes++
		}
		if got != want {
			t.Fatalf("%s: power %q, the calendar says %q", at.Format("15:04"), got, want)
		}
		if want == "on" && fmt.Sprint(d.ReadPath("status.display.input")) != "hdmi1" {
			t.Fatalf("%s: awake but input %v, want the conferencing input hdmi1", at.Format("15:04"), d.ReadPath("status.display.input"))
		}
	}
	if awakeMinutes == 0 {
		t.Fatal("the display never woke")
	}
}

// A no-show wakes nothing: a room whose every booking is skipped leaves the
// panel asleep and an input an operator chose where it was.
func TestBorealEdgeIsUnchangedByANoShow(t *testing.T) {
	k := dayKnobs()
	k.BookingRate, k.NoShowRate, k.WalkInRate = 1, 1, 0
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	at := day
	d := borealEngine(t, "boreal-edge-86", 5, k, &at)
	d.TickBehavior("room_meeting") // the empty night: standby
	setPath(t, d, "status.display.input", "usbc")
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		d.TickBehavior("room_meeting")
		if p := fmt.Sprint(d.ReadPath("status.device.power")); p != "standby" {
			t.Fatalf("%s: power %q through a no-show, want standby", at.Format("15:04"), p)
		}
	}
	if in := fmt.Sprint(d.ReadPath("status.display.input")); in != "usbc" {
		t.Fatalf("input %q after a day of no-shows, want usbc as set", in)
	}
}

// A device whose room has no calendar is left as it was: nothing to follow.
func TestBorealEdgeWithNoCalendarKeepsItsState(t *testing.T) {
	at := time.Date(2026, 11, 3, 3, 0, 0, 0, time.UTC)
	d := borealEngine(t, "boreal-edge-86", 1, dayKnobs(), &at)
	d.calSrc = func() *calendar.Calendar { return nil }
	d.TickBehavior("room_meeting")
	if p := fmt.Sprint(d.ReadPath("status.device.power")); p != "on" {
		t.Fatalf("power %q with no calendar, want the default on", p)
	}
}

// The projector wakes and sleeps the same way, and its light source's hours
// grow only while it is on: over a simulated week they match the minutes the
// calendar keeps it awake, nothing in a no-show room.
func TestBorealBeamHoursGrowOnlyWhileOnOverAWeek(t *testing.T) {
	k := dayKnobs()
	const seed = 33
	week := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	cal := calOf(seed, k)
	at := week
	d := borealEngine(t, "boreal-beam-340", seed, k, &at)
	start := d.ReadPath("status.projector.light_source_hours").(float64)
	var awake int
	for at = week; at.Before(week.AddDate(0, 0, 7)); at = at.Add(time.Minute) {
		d.TickBehavior("room_meeting")
		before := d.ReadPath("status.projector.light_source_hours").(float64)
		d.TickBehavior("light_source_aging")
		grew := d.ReadPath("status.projector.light_source_hours").(float64) > before
		want := wantAwake(cal, roomHQ, at)
		if want {
			awake++
		}
		if grew != want {
			t.Fatalf("%s: hours grew %v, the calendar says awake %v", at.Format("Mon 15:04"), grew, want)
		}
		if p := fmt.Sprint(d.ReadPath("status.device.power")); (p == "on") != want {
			t.Fatalf("%s: power %q, awake %v", at.Format("Mon 15:04"), p, want)
		}
	}
	if awake == 0 {
		t.Fatal("the projector never woke in a week")
	}
	// The aging tick rounds to four places, so a minute adds 0.0167 of an
	// hour, not 1/60: within half a percent.
	got := d.ReadPath("status.projector.light_source_hours").(float64) - start
	if want := float64(awake) / 60; math.Abs(got-want) > want*0.005 {
		t.Fatalf("a week added %.4f hours, %d awake minutes is %.4f", got, awake, want)
	}

	// A room that holds nothing but no-shows adds none.
	nk := k
	nk.BookingRate, nk.NoShowRate, nk.WalkInRate = 1, 1, 0
	at = week
	n := borealEngine(t, "boreal-beam-340", seed, nk, &at)
	h0 := n.ReadPath("status.projector.light_source_hours").(float64)
	for ; at.Before(week.AddDate(0, 0, 7)); at = at.Add(time.Minute) {
		n.TickBehavior("room_meeting")
		n.TickBehavior("light_source_aging")
	}
	if h1 := n.ReadPath("status.projector.light_source_hours").(float64); h1 != h0 {
		t.Fatalf("a no-show week moved the hours from %v to %v", h0, h1)
	}
}

// A signage player follows the building's day, not the room's meetings: it
// plays through the hours the building is open and stops outside them, with
// every booking on or off.
func TestBorealCastPlaysThroughTheBuildingsHours(t *testing.T) {
	for _, rate := range []float64{0, 1} {
		k := dayKnobs()
		k.BookingRate = rate
		day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
		at := day
		d := borealEngine(t, "boreal-cast-m1", 8, k, &at)
		for at = day; at.Before(day.Add(48 * time.Hour)); at = at.Add(10 * time.Minute) {
			d.TickBehavior("building_day")
			want := "stopped"
			if k.Hours.Open(at) {
				want = "playing"
			}
			if got := fmt.Sprint(d.ReadPath("status.playback.state")); got != want {
				t.Fatalf("booking %v, %s: playback %q, the building says %q", rate, at.Format("Mon 15:04"), got, want)
			}
		}
	}
}

// An operator's pause holds until the building next opens or closes; and a
// player with no hours to read plays on.
func TestBorealCastKeepsAnOperatorsPauseAndPlaysWithNoHours(t *testing.T) {
	k := dayKnobs()
	at := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	d := borealEngine(t, "boreal-cast-m1", 8, k, &at)
	d.TickBehavior("building_day")
	setPath(t, d, "status.playback.state", "paused")
	at = at.Add(time.Hour)
	d.TickBehavior("building_day")
	if got := d.ReadPath("status.playback.state"); got != "paused" {
		t.Fatalf("a pause during open hours became %v", got)
	}
	at = time.Date(2026, 11, 3, 18, 1, 0, 0, time.UTC)
	d.TickBehavior("building_day")
	if got := d.ReadPath("status.playback.state"); got != "stopped" {
		t.Fatalf("after closing: %v, want stopped", got)
	}

	at2 := time.Date(2026, 11, 3, 3, 0, 0, 0, time.UTC)
	n := borealEngine(t, "boreal-cast-m1", 8, k, &at2)
	n.calSrc = func() *calendar.Calendar { return nil }
	n.TickBehavior("building_day")
	if got := n.ReadPath("status.playback.state"); got != "playing" {
		t.Fatalf("with no hours to read the player is %v, want playing", got)
	}
}

// A signage room declares no meetings and so no hours of its own: the
// player reads its building's, the span the building's other rooms keep,
// through the world the simulator holds.
func TestBorealCastReadsItsBuildingsHoursThroughTheWorld(t *testing.T) {
	at := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	k := dayKnobs()
	d := borealEngine(t, "boreal-cast-m1", 8, k, &at)
	d.calSrc = func() *calendar.Calendar { // the sign's own room has no knobs
		return calendar.New(8, func(r calendar.Room) (calendar.Knobs, bool) { return k, r != roomHQ })
	}
	w := &world.World{Version: world.Version, Meetings: map[string]calendar.Knobs{"huddle": k, "boardroom": {Hours: calendar.Hours{Start: 7, End: 20}}},
		Sites: []world.Site{{Name: "hq", Rooms: []world.Room{
			{Name: "r101", Building: "tower", Standard: "sign"},
			{Name: "r102", Building: "tower", Standard: "huddle"},
			{Name: "r103", Building: "tower", Standard: "boardroom"},
			{Name: "r201", Building: "annex", Standard: "huddle"}}}}}
	d.worldSrc = func() *world.World { return w }
	// The tower's rooms span 7 to 20; the annex's 8 to 18 is not its.
	for _, c := range []struct {
		h    int
		want string
	}{{6, "stopped"}, {7, "playing"}, {19, "playing"}, {20, "stopped"}} {
		at = time.Date(2026, 11, 3, c.h, 30, 0, 0, time.UTC)
		d.TickBehavior("building_day")
		if got := d.ReadPath("status.playback.state"); got != c.want {
			t.Fatalf("%02d:30: playback %v, want %s", c.h, got, c.want)
		}
	}
}
