package sim

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/calendar/caltest"
)

// The video bar's camera counts the people in its room (#2757): the calendar's
// attendees in a held meeting or a walk-in (less at most the one the camera
// misses), zero in a no-show and outside any meeting. With no calendar it
// counts the call it is in and nothing else, a steady 0 when idle.

func readPeople(t *testing.T, d *Device) int {
	t.Helper()
	n, err := strconv.Atoi(fmt.Sprint(d.ReadPath("status.video.people_count")))
	if err != nil {
		t.Fatalf("status.video.people_count = %v: %v", d.ReadPath("status.video.people_count"), err)
	}
	return n
}

func TestVideoBarCountsTheRoomsAttendeesOverAPinnedDay(t *testing.T) {
	k, seed, day, cal := pinnedDay(t)
	at := day
	d := roomEngine(t, seed, k, &at, "kestrel-vroom")["kestrel-vroom"]
	var held, empty int
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		if !d.TickBehavior("people_count") {
			t.Fatal("people_count did not tick")
		}
		got := readPeople(t, d)
		e, in := heldAt(cal, at)
		if !in {
			empty++
			if got != 0 {
				t.Fatalf("%s: no meeting held, the camera counts %d", at.Format("15:04"), got)
			}
			continue
		}
		held++
		actual := caltest.M(e).Actual
		if got != actual && !(actual > 1 && got == actual-1) {
			t.Fatalf("%s: the meeting has %d, the camera counts %d", at.Format("15:04"), actual, got)
		}
	}
	if held == 0 || empty == 0 {
		t.Fatalf("the day sampled %d held and %d empty minutes; want both", held, empty)
	}
}

func TestVideoBarCountsNobodyInANoShow(t *testing.T) {
	k, seed, day, cal := pinnedDay(t)
	at := day
	d := roomEngine(t, seed, k, &at, "kestrel-vroom")["kestrel-vroom"]
	var seen int
	for _, e := range cal.Entries(roomHQ, day, day.Add(24*time.Hour), calendar.MeetingKinds) {
		if caltest.M(e).Outcome != calendar.OutcomeNoShow {
			continue
		}
		seen++
		at = e.Start.Add(time.Minute)
		d.TickBehavior("people_count")
		if got := readPeople(t, d); got != 0 {
			t.Fatalf("a no-show at %s: the camera counts %d", at.Format("15:04"), got)
		}
	}
	if seen == 0 {
		t.Fatal("the pinned day holds no no-show")
	}
}

func TestVideoBarWithNoCalendarCountsOnlyItsCall(t *testing.T) {
	e := shippedEngine(t)
	if err := e.LoadDevices([]DeviceSnapshot{{DeviceID: "d", Hostname: "d", LoopbackIP: "127.0.0.1", ProfileName: "kestrel-vroom", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	at := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	d.SetClock(pinned(&at))
	for i := 0; i < 20; i++ {
		d.TickBehavior("people_count")
		if got := readPeople(t, d); got != 0 {
			t.Fatalf("an idle bar with no calendar counts %d, want a steady 0", got)
		}
		at = at.Add(time.Minute)
	}
	for path, v := range map[string]any{"status.call.active": true, "status.call.participants": 4} {
		if err := SetPath(d.state, path, v); err != nil {
			t.Fatal(err)
		}
	}
	d.TickBehavior("people_count")
	if got := readPeople(t, d); got != 4 {
		t.Fatalf("a bar in a 4-person call with no calendar counts %d, want 4", got)
	}
}
