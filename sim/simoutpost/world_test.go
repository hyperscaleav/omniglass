package simoutpost

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/calendar/caltest"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
	"github.com/hyperscaleav/omniglass/sim/world"
)

func worldFixture() *world.World {
	knobs := calendar.Knobs{BookingRate: 0.6, SlotMix: map[int]float64{30: 1, 60: 1}, NoShowRate: 0.2,
		EndEarlyRate: 0.2, EndLateRate: 0.1, Attendance: calendar.Range{Min: 2, Max: 6}, WalkInRate: 0.2,
		Hours: calendar.Hours{Start: 8, End: 18}}
	return &world.World{
		Version: world.Version, Scenario: "t", Outpost: "edge-hq", CalendarSeed: 42,
		Meetings: map[string]calendar.Knobs{"huddle": knobs},
		Sites: []world.Site{{Name: "hq", Rooms: []world.Room{
			{Name: "r1", Building: "tower", Standard: "huddle", System: "s1",
				Devices: []world.Device{{Name: "r1-display-1", Product: "p", Profile: "pp"}}},
			{Name: "r2", Building: "annex", Standard: "huddle", System: "s2",
				Devices: []world.Device{{Name: "r2-display-1", Product: "p", Profile: "pp"}}},
		}}},
	}
}

// With a world file the simulator's engine holds a calendar whose entries are
// calendar.Generate's for the same knobs and seed; without one it holds none.
func TestARunWithAWorldFileInstallsTheCalendarItImplies(t *testing.T) {
	w := worldFixture()
	b, err := w.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "world.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadWorld(path)
	if err != nil {
		t.Fatal(err)
	}
	e := sim.NewEngine(store, profiles.Templates())
	installWorld(e, loaded)
	cal := e.Calendar()
	if cal == nil {
		t.Fatal("a world file installed no calendar")
	}
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	for _, r := range []calendar.Room{{Site: "hq", Building: "tower", Room: "r1"}, {Site: "hq", Building: "annex", Room: "r2"}} {
		want := calendar.Generate(r, w.Meetings["huddle"], 42, day, day.Add(24*time.Hour))
		got := cal.Entries(r, day, day.Add(24*time.Hour), calendar.MeetingKinds)
		if len(want) == 0 {
			t.Fatalf("no meetings generated for %v; the fixture proves nothing", r)
		}
		if len(got) != len(want) {
			t.Fatalf("%v: %d entries, Generate gives %d", r, len(got), len(want))
		}
		for i := range want {
			if got[i].Start != want[i].Start || got[i].End != want[i].End || caltest.M(got[i]) != caltest.M(want[i]) {
				t.Fatalf("%v entry %d = %+v, Generate gives %+v", r, i, got[i], want[i])
			}
		}
	}
	if e.World() == nil {
		t.Fatal("the engine holds no world for a service to read")
	}

	none, err := LoadWorld("")
	if err != nil || none != nil {
		t.Fatalf("no path loaded %v, %v", none, err)
	}
	bare := sim.NewEngine(store, profiles.Templates())
	installWorld(bare, none)
	if bare.Calendar() != nil || bare.World() != nil {
		t.Fatal("with no world file the engine holds a calendar or a world")
	}
	if _, err := LoadWorld(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("a world path that is not a file loaded")
	}
}

// A pulled device the world names, by component name, is placed in the
// world's room; one it does not name keeps empty room fields. The world gives
// every device a name of its own, so the lookup is exact.
func TestAPulledDeviceTheWorldNamesIsPlacedAndOneItDoesNotIsNot(t *testing.T) {
	profileFor := func(string) (string, bool) { return "pp", true }
	snaps, skips := plan([]collection.SimDevice{
		{ComponentID: "c1", Name: "r1-display-1", Product: "p", Address: "127.60.0.1"},
		{ComponentID: "c2", Name: "r2-display-1", Product: "p", Address: "127.60.0.2"},
		{ComponentID: "c3", Name: "r1-display-2", Product: "p", Address: "127.60.0.3"},
		{ComponentID: "c4", Name: "display-1", Product: "p", Address: "127.60.0.9"},
	}, profileFor, worldFixture())
	if len(skips) != 0 || len(snaps) != 4 {
		t.Fatalf("snaps %d, skips %v", len(snaps), skips)
	}
	type at struct{ site, building, room string }
	want := map[string]at{"c1": {"hq", "tower", "r1"}, "c2": {"hq", "annex", "r2"}, "c3": {}, "c4": {}}
	for _, s := range snaps {
		if got := (at{s.SiteCode, s.Building, s.Room}); got != want[s.DeviceID] {
			t.Errorf("%s placed at %+v, want %+v", s.DeviceID, got, want[s.DeviceID])
		}
	}
	// With no world, nothing is placed.
	snaps, _ = plan([]collection.SimDevice{{ComponentID: "c1", Name: "r1-display-1", Product: "p", Address: "127.60.0.1"}}, profileFor, nil)
	if s := snaps[0]; s.SiteCode != "" || s.Building != "" || s.Room != "" {
		t.Errorf("with no world a device is placed at %+v", s)
	}
}

// The offset the lab names reaches every engine of the simulator (#2800): the
// fleet's and the pushers'. An empty value is real time and a value that is no
// duration is refused.
func TestClockOffsetEnvReachesThePushersEngine(t *testing.T) {
	off, err := LoadClockOffset("36h")
	if err != nil || off != 36*time.Hour {
		t.Fatalf("LoadClockOffset(36h) = %v, %v", off, err)
	}
	if off, err := LoadClockOffset(""); err != nil || off != 0 {
		t.Fatalf("an empty offset is %v, %v, want none", off, err)
	}
	if _, err := LoadClockOffset("soon"); err == nil {
		t.Fatal("an offset that is no duration was read")
	}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e, err := startPushers(catalogOf(t, store), "http://127.0.0.1:1", nil, nil, sim.Clock{Offset: off})
	if err != nil {
		t.Fatal(err)
	}
	defer e.StopAll()
	if got := e.Clock().Offset; got != off {
		t.Fatalf("the pushers' engine runs offset %v, want %v", got, off)
	}
}
