package sim

import (
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// The room phase (#2797): every device in a room reads one derived state, the
// wake lead, cooldown tail and sharing stretch come from the standard's knobs,
// and a room's state is computed once per second however many devices read it.
// The phase timeline itself is hand-checked in sim/calendar (phase_test.go);
// these tests hold the engine side: the template funcs, the memo, the knobs
// reaching the readers, the impairment, and the no-calendar fallback.

// phaseDayKnobs and phaseDaySeed are the pinned room day sim/calendar's oracle
// is checked against: a booked meeting 08:28:07 to 09:00:01 that shares from
// 08:38:45 to 08:49:23 (UTC, 3 Nov 2026).
func phaseDayKnobs() calendar.Knobs {
	return calendar.Knobs{BookingRate: 0.5, SlotMix: map[int]float64{15: 1, 30: 2, 60: 2}, NoShowRate: 0.2, EndEarlyRate: 0.2, EndLateRate: 0.15,
		Attendance: calendar.Range{Min: 2, Max: 6}, WalkInRate: 0.25, Hours: calendar.Hours{Start: 8, End: 14}, RecordingRate: 0.5}
}

const phaseDaySeed = 24

// phaseRoom is the room the pinned day belongs to: a day is seeded from the room key.
var phaseRoom = calendar.Room{Site: "hq", Building: "tower", Room: "tower-1-01"}

func phaseAt(h, m, s int) time.Time { return time.Date(2026, 11, 3, h, m, s, 0, time.UTC) }

func phaseRoomEngine(t *testing.T, k calendar.Knobs, at *time.Time, profileNames ...string) (*Engine, map[string]*Device) {
	t.Helper()
	e := shippedEngine(t)
	e.SetCalendar(calendar.New(phaseDaySeed, func(calendar.Room) (calendar.Knobs, bool) { return k, true }))
	var snaps []DeviceSnapshot
	for i, name := range profileNames {
		id := fmt.Sprintf("d-%d", i)
		snaps = append(snaps, DeviceSnapshot{DeviceID: id, Hostname: id, LoopbackIP: "127.0.0.1", ProfileName: profileOf(t, e, name), Product: name, Version: 1,
			SiteCode: phaseRoom.Site, Building: phaseRoom.Building, Room: phaseRoom.Room})
	}
	if err := e.LoadDevices(snaps); err != nil {
		t.Fatal(err)
	}
	out := map[string]*Device{}
	for _, d := range e.Devices() {
		d.SetClock(pinned(at))
		out[d.Product] = d
	}
	return e, out
}

func TestEveryDeviceInARoomReadsTheSameRoomPhase(t *testing.T) {
	at := phaseAt(8, 40, 0)
	_, ds := phaseRoomEngine(t, phaseDayKnobs(), &at, "newtron-core-3", "newtron-panel-7", "meridian-mx-84", "meridian-xl-1", "boreal-edge-86")
	for _, tc := range []struct {
		at                           time.Time
		phase, page, input, extender string
	}{
		{phaseAt(8, 26, 0), "idle", "idle", "dp1", "absent"},
		{phaseAt(8, 27, 30), "waking", "idle", "dp1", "absent"},
		{phaseAt(8, 30, 0), "in-meeting", "in-call", "hdmi2", "present"},
		{phaseAt(8, 40, 0), "sharing", "sharing", "hdmi1", "present"},
		{phaseAt(9, 2, 0), "cooldown", "idle", "dp1", "absent"},
	} {
		at = tc.at
		for name, d := range ds {
			if got := stampBehavior(t, d, "{{ (roomPhase).phase }}"); got != tc.phase {
				t.Errorf("%s at %s: %s reads phase %v, want %s", name, tc.at.Format("15:04:05"), name, got, tc.phase)
			}
		}
		ds["newtron-panel-7"].TickBehavior("room_meeting")
		if got := ds["newtron-panel-7"].ReadPath("status.panel.page"); got != tc.page {
			t.Errorf("%s: panel page %v, want %s", tc.at.Format("15:04:05"), got, tc.page)
		}
	}
}

// Every key of the map is always present, and a device with no calendar reads
// an idle room that says so: the fallback each profile keys on.
func TestRoomPhaseFuncsReadEveryKeyAndTheNoCalendarFallback(t *testing.T) {
	at := phaseAt(8, 30, 0)
	e, ds := phaseRoomEngine(t, phaseDayKnobs(), &at, "boreal-edge-86")
	d := ds["boreal-edge-86"]
	for _, key := range []string{"phase", "occupancy", "has_calendar", "scheduled", "in_use", "call_active", "attendees", "sharing", "impaired", "building_open", "use_until", "recording_pending", "meeting"} {
		if got := stampBehavior(t, d, fmt.Sprintf("{{ (roomPhase).%s }}", key)); got == "" || got == "<no value>" {
			t.Errorf("roomPhase.%s reads %q", key, got)
		}
	}
	if got := stampBehavior(t, d, `{{ (roomPhaseOf "hq" "tower" "tower-1-01").phase }}`); got != "in-meeting" {
		t.Errorf("roomPhaseOf = %q (every room of this calendar has the same knobs, a seeded day apiece)", got)
	}
	if got := fmt.Sprint(stampBehavior(t, d, `{{ (ownerPhase "nobody").has_calendar }}`)); got != "false" {
		t.Errorf("an owner the world does not name reads has_calendar %v", got)
	}
	e.SetCalendar(nil)
	if got := stampBehavior(t, d, `{{ (roomPhase).has_calendar }} {{ (roomPhase).in_use }} {{ (roomPhase).phase }}`); got != "false false idle" {
		t.Errorf("with no calendar: %q", got)
	}
}

// A room's state is computed once a second, however many devices read it and
// however often each reads: the engine-level memo, which also holds the
// booked-meetings reading the scheduling surfaces make.
func TestARoomsPhaseIsComputedOncePerSecondHoweverManyDevicesRead(t *testing.T) {
	at := phaseAt(8, 40, 0)
	e, ds := phaseRoomEngine(t, phaseDayKnobs(), &at, "newtron-core-3", "newtron-panel-7", "meridian-mx-84", "meridian-xl-1", "boreal-edge-86", "ferrite-pdu-4c")
	read := func() {
		for _, d := range ds {
			for i := 0; i < 8; i++ {
				stampBehavior(t, d, "{{ (roomPhase).phase }} {{ (roomPhase).in_use }} {{ (bookings).state }} {{ (bookings).today }}")
			}
		}
	}
	read()
	if s, b := e.rooms.states.Load(), e.rooms.books.Load(); s != 1 || b != 1 {
		t.Fatalf("one room, one second, six devices: %d state readings and %d booking readings, want 1 and 1", s, b)
	}
	at = at.Add(time.Second)
	read()
	if s, b := e.rooms.states.Load(), e.rooms.books.Load(); s != 2 || b != 2 {
		t.Fatalf("the next second: %d state readings and %d booking readings, want 2 and 2", s, b)
	}
	read()
	if s := e.rooms.states.Load(); s != 2 {
		t.Fatalf("a second read of the same second recomputes: %d", s)
	}
	// a device reading another room asks for that one only
	stampBehavior(t, ds["boreal-edge-86"], `{{ (roomPhaseOf "hq" "tower" "tower-1-02").phase }}`)
	if s := e.rooms.states.Load(); s != 3 {
		t.Fatalf("a second room: %d state readings, want 3", s)
	}
	// the memo does not outlive the calendar: a new one is read afresh
	e.SetCalendar(calendar.New(phaseDaySeed, func(calendar.Room) (calendar.Knobs, bool) { return phaseDayKnobs(), true }))
	read()
	if s := e.rooms.states.Load(); s != 4 {
		t.Fatalf("a new calendar: %d state readings, want 4", s)
	}
}

// BenchmarkRoomPhaseManyDevicesOneRoom is the cost of a room's devices all
// reading its phase in one second: one derivation, then lookups.
func BenchmarkRoomPhaseManyDevicesOneRoom(b *testing.B) {
	at := phaseAt(8, 40, 0)
	t := &testing.T{}
	_, ds := phaseRoomEngine(t, phaseDayKnobs(), &at, "newtron-core-3", "newtron-panel-7", "meridian-mx-84", "boreal-edge-86")
	for _, d := range ds {
		d.SetState(map[string]any{"cal_probe": ""}, 2)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		at = phaseAt(8, 40, 0).Add(time.Duration(i) * time.Second)
		for _, d := range ds {
			d.roomState(d.room)
		}
	}
}

// A scripted problem shows as the impaired phase, read through the calendar's
// problem entries, while the readings of the meeting under it do not move:
// the panel still says sharing, the room is still in use.
func TestAnImpairmentWindowShowsAsTheImpairedPhaseAndMovesNoReading(t *testing.T) {
	at := phaseAt(8, 45, 0)
	e, ds := phaseRoomEngine(t, phaseDayKnobs(), &at, "newtron-panel-7", "newtron-core-3")
	plain := e.Calendar()
	e.SetCalendar(plain.WithProblems(func(calendar.Room) []calendar.ProblemSpec {
		return []calendar.ProblemSpec{{Name: "morning", Cadence: "daily", At: "08:40", For: 30 * time.Minute,
			Scope:      calendar.Scope{Standard: "x", Role: "main-display"},
			Impairment: calendar.Impairment{Pin: &calendar.Pin{Path: "status.device.power", Value: "off"}}}}
	}))
	p := ds["newtron-panel-7"]
	if got := stampBehavior(t, p, `{{ (roomPhase).phase }} {{ (roomPhase).occupancy }} {{ (roomPhase).impaired }}`); got != "impaired sharing true" {
		t.Fatalf("inside the window: %q", got)
	}
	p.TickBehavior("room_meeting")
	if got := p.ReadPath("status.panel.page"); got != "sharing" {
		t.Errorf("the panel reads %v under an impairment, want sharing", got)
	}
	at = phaseAt(9, 11, 0)
	if got := stampBehavior(t, p, `{{ (roomPhase).phase }} {{ (roomPhase).impaired }}`); got != "idle false" {
		t.Errorf("after the window: %q", got)
	}
}

// The wake lead and the cooldown tail are the standard's knobs: with a
// cooldown of zero the processor stands the room down the second the meeting
// ends, with the default it holds the room five more minutes.
func TestTheProcessorFollowsTheStandardsCooldownKnob(t *testing.T) {
	zero, lead := 0, 600
	for _, tc := range []struct {
		name      string
		mut       func(*calendar.Knobs)
		at        time.Time
		wantPower string
	}{
		{"default cooldown holds the room", func(*calendar.Knobs) {}, phaseAt(9, 3, 0), "on"},
		{"no cooldown stands it down", func(k *calendar.Knobs) { k.CooldownTail = &zero }, phaseAt(9, 3, 0), "standby"},
		{"default lead has not woken it", func(*calendar.Knobs) {}, phaseAt(8, 20, 0), "standby"},
		{"a ten-minute lead has", func(k *calendar.Knobs) { k.WakeLead = &lead }, phaseAt(8, 20, 0), "on"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := phaseDayKnobs()
			tc.mut(&k)
			at := tc.at
			_, ds := phaseRoomEngine(t, k, &at, "newtron-core-3")
			d := ds["newtron-core-3"]
			d.TickBehavior("room_meeting")
			if got := d.ReadPath("status.control.room_power"); got != tc.wantPower {
				t.Errorf("room power %v, want %s", got, tc.wantPower)
			}
		})
	}
}

// The building's hours reach the phase from the world, as buildingHours does.
func TestRoomPhaseReadsTheBuildingOpen(t *testing.T) {
	at := phaseAt(8, 40, 0)
	e, ds := phaseRoomEngine(t, phaseDayKnobs(), &at, "boreal-edge-86")
	e.SetWorld(&world.World{})
	d := ds["boreal-edge-86"]
	if got := fmt.Sprint(stampBehavior(t, d, `{{ (roomPhase).building_open }}`)); got != "true" {
		t.Errorf("08:40 inside hours 8 to 14: %v", got)
	}
	at = phaseAt(15, 0, 0)
	if got := fmt.Sprint(stampBehavior(t, d, `{{ (roomPhase).building_open }}`)); got != "false" {
		t.Errorf("15:00 outside hours: %v", got)
	}
}
