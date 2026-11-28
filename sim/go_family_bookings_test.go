//go:build linux

package sim

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

// A Go family's tick reads its room's booked meetings (#2833): the scheduling
// panels' reading, the lab calendar's booked entries alone, through the
// device's own clock and room, as the `bookings` template function reads them
// for a YAML behavior.

const bookingsFamily = "wiretest-bookings"

func bookingsSpec() spec.Spec {
	return spec.Spec{
		Name:     bookingsFamily,
		Identity: spec.Identity{Manufacturer: "Wiretest", Model: "Door", DeviceType: "scheduling-panel", Summary: "A door panel the bookings reading is proven on"},
		State: map[string]any{"status": map[string]any{"room": map[string]any{
			"state": "", "now": "", "next": "", "today": 0, "remaining": 0,
		}}},
		Ticks: []spec.Tick{{
			Name: "door", Interval: 10 * time.Second, Writes: []string{"status.room.state", "status.room.now", "status.room.next", "status.room.today", "status.room.remaining"},
			Run: func(_ spec.Device, c spec.TickContext) []surface.Change {
				b := c.ReadBookings()
				return []surface.Change{
					{Path: "status.room.state", Value: b.State}, {Path: "status.room.now", Value: b.Now.Line()}, {Path: "status.room.next", Value: b.Next.Line()},
					{Path: "status.room.today", Value: b.Today}, {Path: "status.room.remaining", Value: b.Remaining},
				}
			},
		}},
	}
}

func bookingsDevice(t *testing.T, inRoom bool) (*Device, *time.Time) {
	t.Helper()
	e := familyEngine(t, bookingsSpec())
	e.SetCalendar(parityCal())
	at := parityAt("07:00:00")
	snap := DeviceSnapshot{DeviceID: "door-1", Hostname: "door-1", LoopbackIP: "127.0.0.1", ProfileName: bookingsFamily, Version: 1}
	if inRoom {
		snap.SiteCode, snap.Building, snap.Room = parityRoom.Site, parityRoom.Building, parityRoom.Room
	}
	if err := e.LoadDevices([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	d.StopBehaviors()
	d.SetClock(func() time.Time { return at })
	return d, &at
}

func TestAGoFamilyTickReadsTheBookingsOfItsRoom(t *testing.T) {
	d, at := bookingsDevice(t, true)
	read := func(path string) any { return d.ReadPath(path) }
	var sawBooked, sawFree, sawNext bool
	for tm := parityAt("07:00:00"); !tm.After(parityAt("15:00:00")); tm = tm.Add(time.Minute) {
		*at = tm
		if !d.TickBehavior("door") {
			t.Fatal("the family has no door tick")
		}
		want := behave.BookingsAt(parityCal(), parityRoom, tm)
		if got := read("status.room.state"); got != want.State {
			t.Fatalf("%s: state %v, want %s", tm.Format("15:04"), got, want.State)
		}
		if got := read("status.room.now"); got != want.Now.Line() {
			t.Fatalf("%s: now %v, want %s", tm.Format("15:04"), got, want.Now.Line())
		}
		if got := read("status.room.next"); got != want.Next.Line() {
			t.Fatalf("%s: next %v, want %s", tm.Format("15:04"), got, want.Next.Line())
		}
		if int(toInt64(read("status.room.today"))) != want.Today || int(toInt64(read("status.room.remaining"))) != want.Remaining {
			t.Fatalf("%s: today %v remaining %v, want %d and %d", tm.Format("15:04"), read("status.room.today"), read("status.room.remaining"), want.Today, want.Remaining)
		}
		sawBooked, sawFree, sawNext = sawBooked || want.Booked, sawFree || !want.Booked, sawNext || want.Next != nil
	}
	if !sawBooked || !sawFree || !sawNext {
		t.Fatalf("the day is too thin to prove it: booked %v free %v next %v", sawBooked, sawFree, sawNext)
	}
}

func TestAGoFamilyTickInNoRoomReadsFreeAndEmpty(t *testing.T) {
	d, at := bookingsDevice(t, false)
	*at = parityAt("10:00:00")
	d.TickBehavior("door")
	if d.ReadPath("status.room.state") != behave.StateFree || d.ReadPath("status.room.now") != "none" || d.ReadPath("status.room.today") != 0 {
		t.Fatalf("a device in no room reads %v %v %v, want free, none and 0", d.ReadPath("status.room.state"), d.ReadPath("status.room.now"), d.ReadPath("status.room.today"))
	}
}

func TestATickWithNoBookingsReaderReadsFreeAndEmpty(t *testing.T) {
	b := spec.TickContext{}.ReadBookings()
	if b.State != behave.StateFree || b.Booked || b.Now != nil || b.Next != nil {
		t.Fatalf("a context with no reader reads %+v, want a free, empty room", b)
	}
}
