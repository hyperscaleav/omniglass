//go:build linux

package sim

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/surface"
)

// A tick reads the meetings of its device's room over a window (#2835): the
// calendar's entries overlapping [from, to), none for a device with no
// calendar, so a read that follows the room across hours (a wireless pack's
// charge) is a function of the calendar and the clock alone.
func TestATickReadsTheRoomsMeetingsOverAWindow(t *testing.T) {
	const counted = "status.sim.spans"
	fam := spec.Spec{
		Name:       "spans-test",
		Identity:   spec.Identity{Manufacturer: "Test", Model: "Spans", DeviceType: "sensor"},
		Interfaces: []profile.Interface{{Name: "icmp", Protocol: "icmp"}},
		State:      map[string]any{"status": map[string]any{"sim": map[string]any{"spans": 0}}},
		Ticks: []spec.Tick{{Name: "count_spans", Interval: time.Minute, Writes: []string{counted},
			Run: func(_ spec.Device, c spec.TickContext) []surface.Change {
				return []surface.Change{{Path: counted, Value: len(c.Spans(parityDay, parityDay.Add(24*time.Hour)))}}
			}}},
	}
	e := NewEngineOver(shippedCatalog(t, fam), nil)
	e.SetCalendar(parityCal())
	snap := DeviceSnapshot{DeviceID: "d-1", Hostname: "d-1", LoopbackIP: "127.0.0.1", ProfileName: "spans-test", Version: 1,
		SiteCode: parityRoom.Site, Building: parityRoom.Building, Room: parityRoom.Room}
	if err := e.LoadDevices([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	if !d.TickBehavior("count_spans") {
		t.Fatal("the family has no count_spans tick")
	}
	want := len(parityCal().Entries(parityRoom, parityDay, parityDay.Add(24*time.Hour), calendar.MeetingKinds))
	if got := d.ReadPath(counted); want == 0 || got != want {
		t.Fatalf("the tick read %v meetings of the room's day, the calendar holds %d", got, want)
	}

	e.SetCalendar(nil)
	d.TickBehavior("count_spans")
	if got := d.ReadPath(counted); got != 0 {
		t.Fatalf("a device with no calendar read %v meetings", got)
	}
}
