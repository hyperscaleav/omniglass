package sim

import (
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
)

// censusDevice is the shipped tessellate-census-c1 in a room, its clock pinned
// and the lab calendar of the seed on its engine (#2766): the profile's own
// occupancy behavior, exactly as authored.
func censusDevice(t *testing.T, seed int64, room calendar.Room, at *time.Time) (*Device, *calendar.Calendar, *Engine) {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	k := calTestKnobs()
	cal := calendar.New(seed, func(calendar.Room) (calendar.Knobs, bool) { return k, true })
	e.SetCalendar(cal)
	snap := DeviceSnapshot{DeviceID: "census-" + room.Room, Hostname: "census-" + room.Room, LoopbackIP: "127.0.0.1",
		ProfileName: "tessellate-census-c1", Version: 1, SiteCode: room.Site, Building: room.Building, Room: room.Room}
	if err := e.LoadDevices([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	d.SetClock(pinned(at))
	return d, cal, e
}

// The people counter reads who is in its room from the lab calendar, never from
// a walk of its own: over simulated days its count is zero outside meetings and
// in a no-show, and inside a held meeting or a walk-in it is the meeting's
// actual attendees less at most the one who stepped out. Its derived signal
// and vacant flag stay consistent with the count it reports.
func TestCensusOccupancyFollowsTheLabCalendar(t *testing.T) {
	room := calendar.Room{Site: "hq", Building: "tower", Room: "r101"}
	at := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	d, cal, _ := censusDevice(t, 7, room, &at)
	const name = "occupancy_drift"

	var outside, noShow, held, walkIn, steppedOut, full int
	prev := int64(0)
	end := at.Add(4 * 24 * time.Hour)
	for ; at.Before(end); at = at.Add(20 * time.Second) {
		if !d.TickBehavior(name) {
			t.Fatalf("%s did not tick", name)
		}
		count, _, err := toFloat(d.ReadPath("status.occupancy.people_count"))
		if err != nil {
			t.Fatalf("at %s people_count = %v: %v", at, d.ReadPath("status.occupancy.people_count"), err)
		}
		got := int64(count)
		e, in := cal.Now(room, at)
		switch {
		case !in:
			outside++
			if got != 0 {
				t.Fatalf("at %s outside any meeting people_count = %d", at, got)
			}
		case e.Meeting.Outcome == calendar.OutcomeNoShow:
			noShow++
			if got != 0 {
				t.Fatalf("at %s in a no-show people_count = %d", at, got)
			}
		default:
			actual := int64(e.Meeting.Actual)
			if got != actual && !(actual > 1 && got == actual-1) {
				t.Fatalf("at %s in a %s %s people_count = %d, the meeting had %d", at, e.Meeting.Origin, e.Meeting.Outcome, got, actual)
			}
			if got == actual {
				full++
			} else {
				steppedOut++
			}
			if e.Meeting.Origin == calendar.OriginWalkIn {
				walkIn++
			} else {
				held++
			}
		}

		if want := map[bool]string{true: "true", false: "false"}[got == 0]; fmt.Sprint(d.ReadPath("status.occupancy.vacant")) != want {
			t.Fatalf("at %s vacant = %v with people_count %d", at, d.ReadPath("status.occupancy.vacant"), got)
		}
		wantEv := "steady"
		if got > prev {
			wantEv = "increased"
		} else if got < prev {
			wantEv = "decreased"
		}
		if ev := d.ReadPath("status.occupancy.last_event"); ev != wantEv {
			t.Fatalf("at %s last_event = %v after %d -> %d, want %s", at, ev, prev, got, wantEv)
		}
		prev = got
	}
	// The days must have exercised every branch, or the test proves nothing.
	if outside == 0 || noShow == 0 || held == 0 || walkIn == 0 || steppedOut == 0 || full == 0 {
		t.Fatalf("the simulated days left a branch unread: outside=%d noShow=%d held=%d walkIn=%d steppedOut=%d full=%d",
			outside, noShow, held, walkIn, steppedOut, full)
	}
}

// With no calendar (the dev fleet holds no world) the counter keeps the walk it
// had before the calendar: a room with no calendar is left as it was. Its
// count stays whole and inside [0, 14], moves, and keeps vacant and the signal
// consistent with it.
func TestCensusOccupancyWalksWithoutACalendar(t *testing.T) {
	at := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	d, _, e := censusDevice(t, 7, calendar.Room{Site: "hq", Building: "tower", Room: "r101"}, &at)
	e.SetCalendar(nil)
	valid := map[string]bool{"increased": true, "decreased": true, "steady": true}
	seen := map[int64]bool{}
	for i := 0; i < 200; i++ {
		d.TickBehavior("occupancy_drift")
		at = at.Add(20 * time.Second)
		count, _, err := toFloat(d.ReadPath("status.occupancy.people_count"))
		if err != nil || count < 0 || count > 14 || count != float64(int64(count)) {
			t.Fatalf("tick %d: people_count = %v (%v), want a whole number in [0,14]", i, d.ReadPath("status.occupancy.people_count"), err)
		}
		seen[int64(count)] = true
		if ev, _ := d.ReadPath("status.occupancy.last_event").(string); !valid[ev] {
			t.Fatalf("tick %d: last_event = %v", i, d.ReadPath("status.occupancy.last_event"))
		}
		if want := map[bool]string{true: "true", false: "false"}[count == 0]; fmt.Sprint(d.ReadPath("status.occupancy.vacant")) != want {
			t.Fatalf("tick %d: vacant = %v with people_count %v", i, d.ReadPath("status.occupancy.vacant"), count)
		}
	}
	if len(seen) < 3 {
		t.Fatalf("with no calendar the count only took %d values over 200 ticks: %v", len(seen), seen)
	}
}
