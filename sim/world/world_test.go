package world

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
)

func testKnobs() calendar.Knobs {
	return calendar.Knobs{BookingRate: 0.5, SlotMix: map[int]float64{30: 1, 60: 1}, NoShowRate: 0.1,
		Attendance: calendar.Range{Min: 2, Max: 6}, WalkInRate: 0.1, Hours: calendar.Hours{Start: 8, End: 18}}
}

func testWorld() *World {
	return &World{
		Version: Version, Scenario: "t", Outpost: "edge-hq", CalendarSeed: 7,
		Meetings: map[string]calendar.Knobs{"huddle": testKnobs()},
		Sites: []Site{{Name: "hq", Rooms: []Room{
			{Name: "r1", Building: "tower", Standard: "huddle", System: "s1", Capacity: 6,
				Devices: []Device{{Name: "r1-display-1", Product: "p", Profile: "pp"}}},
			{Name: "r2", Building: "tower", Standard: "sign", System: "s2",
				Devices: []Device{{Name: "r2-display-1", Product: "p", Profile: "pp"}}},
		}}},
		Services: []Service{{Name: "svc", Profile: "b", Covers: []Cover{{System: "s2", Site: "hq", Building: "tower", Room: "r2"}}}},
	}
}

func TestAWorldRoundTripsThroughItsFile(t *testing.T) {
	w := testWorld()
	b, err := w.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "w.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := got.Marshal()
	if string(b) != string(b2) {
		t.Fatalf("round trip changed the file:\n%s\n%s", b, b2)
	}
}

func TestAWorldIsRefusedWhenItCannotBeTrusted(t *testing.T) {
	good, _ := testWorld().Marshal()
	for name, tc := range map[string]struct{ from, to, want string }{
		"another version":    {`"version": 1`, `"version": 9`, "version 9"},
		"an unknown field":   {`"outpost"`, `"nope"`, "unknown field"},
		"a device twice":     {`"name": "r2-display-1"`, `"name": "r1-display-1"`, "placed twice"},
		"knobs out of range": {`"booking_rate": 0.5`, `"booking_rate": 4`, "booking_rate"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(strings.Replace(string(good), tc.from, tc.to, 1)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
	if _, err := Load(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("a missing file loaded")
	}
}

func TestADeviceIsPlacedByItsName(t *testing.T) {
	w := testWorld()
	if p, ok := w.PlaceDevice("r2-display-1"); !ok || p != (Placement{Site: "hq", Building: "tower", Room: "r2"}) {
		t.Fatalf("second display placed at %+v, %v", p, ok)
	}
	if p, ok := w.PlaceDevice("r1-display-1"); !ok || p.Room != "r1" {
		t.Fatalf("first display placed at %+v, %v", p, ok)
	}
	for _, n := range []string{"display-1", "r1-display-2", ""} {
		if p, ok := w.PlaceDevice(n); ok {
			t.Errorf("%q placed at %+v, the world names no such device", n, p)
		}
	}
	var none *World
	if _, ok := none.PlaceDevice("r1-display-1"); ok {
		t.Error("a nil world placed a device")
	}
}

func TestASystemResolvesToItsRoomAndAServiceToItsCoverage(t *testing.T) {
	w := testWorld()
	if p, ok := w.PlaceSystem("s2"); !ok || p.Room != "r2" {
		t.Fatalf("s2 resolves to %+v, %v", p, ok)
	}
	if _, ok := w.PlaceSystem("s9"); ok {
		t.Fatal("an unknown system resolved")
	}
	if got := w.Covered("svc"); len(got) != 1 || got[0].Room != "r2" {
		t.Fatalf("svc covers %+v", got)
	}
	if got := w.Covered("other"); got != nil {
		t.Fatalf("a non-service covers %+v", got)
	}
}

func TestTheCalendarFollowsEachRoomsStandard(t *testing.T) {
	w := testWorld()
	cal := w.Calendar()
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	r1 := calendar.Room{Site: "hq", Building: "tower", Room: "r1"}
	want := calendar.Generate(r1, testKnobs(), 7, day, day.Add(24*time.Hour))
	got := cal.Entries(r1, day, day.Add(24*time.Hour))
	if len(want) == 0 || len(got) != len(want) {
		t.Fatalf("the huddle room holds %d entries, Generate gives %d", len(got), len(want))
	}
	if n := len(cal.Entries(calendar.Room{Site: "hq", Building: "tower", Room: "r2"}, day, day.Add(24*time.Hour))); n != 0 {
		t.Fatalf("a room of a standard with no knobs holds %d entries", n)
	}
	w.Meetings = nil
	if w.Calendar() != nil {
		t.Fatal("a world with no knobs has a calendar")
	}
}
